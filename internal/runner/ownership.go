// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// SGGVR is the ServingGroup custom resource. A build that has it makes every
// ServingGroup an object: ModelServing owns ServingGroups, and each
// ServingGroup owns its Pods, PodGroup and Services. A build without it keeps
// the earlier model in which the ModelServing owns those objects directly.
var SGGVR = schema.GroupVersionResource{Group: "workload.serving.volcano.sh", Version: "v1alpha1", Resource: "servinggroups"}

const (
	servingGroupKind = "ServingGroup"
	servingGroupCRD  = "servinggroups.workload.serving.volcano.sh"
	// AnnotationSurge marks a temporary surge ServingGroup object
	// ("serving-group"). Pods carry a copy; a temporary Role instance uses "role".
	AnnotationSurge = "modelserving.volcano.sh/surge"
	// LabelInstanceGeneration counts how often a Role instance was drained and
	// created again inside one ServingGroup object.
	LabelInstanceGeneration = "modelserving.volcano.sh/instance-generation"
)

// groupOwnerIndex remembers which object owns each ServingGroup UID the runner
// has seen. UIDs are never reused, so entries stay valid after the object is
// gone and one process-wide index serves every case.
type groupOwnerIndex struct {
	mu     sync.RWMutex
	owners map[string][]string
	// lookup reads one ServingGroup that no stream has delivered yet. Pod and
	// ServingGroup watches share no order, so a Pod can be seen first.
	lookup func(namespace, name string) (*unstructured.Unstructured, error)
}

var groupOwners = &groupOwnerIndex{owners: map[string][]string{}}

func (x *groupOwnerIndex) record(group *unstructured.Unstructured) {
	if group == nil || group.GetUID() == "" {
		return
	}
	owners := []string{}
	for _, ref := range group.GetOwnerReferences() {
		owners = append(owners, string(ref.UID))
	}
	x.mu.Lock()
	x.owners[string(group.GetUID())] = owners
	x.mu.Unlock()
}

func (x *groupOwnerIndex) setLookup(lookup func(namespace, name string) (*unstructured.Unstructured, error)) {
	x.mu.Lock()
	x.lookup = lookup
	x.mu.Unlock()
}

// resolve learns an unseen ServingGroup owner by one direct read. A group that
// is gone, or whose name now belongs to another UID, is remembered as owned by
// nobody: ownership is never guessed from a name.
func (x *groupOwnerIndex) resolve(namespace string, ref metav1.OwnerReference) {
	x.mu.RLock()
	_, known := x.owners[string(ref.UID)]
	lookup := x.lookup
	x.mu.RUnlock()
	if known || lookup == nil || ref.Kind != servingGroupKind || ref.UID == "" {
		return
	}
	group, err := lookup(namespace, ref.Name)
	if err == nil && group != nil && group.GetUID() == ref.UID {
		x.record(group)
		return
	}
	x.mu.Lock()
	if _, known = x.owners[string(ref.UID)]; !known {
		x.owners[string(ref.UID)] = nil
	}
	x.mu.Unlock()
}

func (x *groupOwnerIndex) ownedBy(group, uid string) bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	for _, owner := range x.owners[group] {
		if owner == uid {
			return true
		}
	}
	return false
}

// ownerMatches reports whether uid owns an object directly or through its
// ServingGroup. The second hop is the only one followed: a ServingGroup is
// owned by its ModelServing and nothing else is ever resolved transitively.
func ownerMatches(namespace string, refs []metav1.OwnerReference, uid string) bool {
	if uid == "" {
		return false
	}
	for _, ref := range refs {
		if string(ref.UID) == uid {
			return true
		}
		if ref.Kind != servingGroupKind {
			continue
		}
		groupOwners.resolve(namespace, ref)
		if groupOwners.ownedBy(string(ref.UID), uid) {
			return true
		}
	}
	return false
}

// groupOwnerRef returns the ServingGroup that controls an object, if any.
func groupOwnerRef(o metav1.Object) *metav1.OwnerReference {
	for _, ref := range o.GetOwnerReferences() {
		if ref.Kind == servingGroupKind && ref.Controller != nil && *ref.Controller {
			return &ref
		}
	}
	return nil
}

// unitAtTargetByObject reports whether a ServingGroup-level unit belongs to the
// current template according to the ServingGroup object that controls its Pods.
//
// On a build that keeps ServingGroups as objects the group object exists before
// its Pods: the ServingGroup controller creates them some milliseconds later.
// A replacement group can therefore show, for a moment, only Pods whose own
// template did not change (an unchanged Role, an unchanged entry when only the
// worker layout changes). Read from those Pods alone it looks like an old unit
// that is not Ready. The object says which revision the group was created for.
//
// The answer is yes only when all of this holds; in every other case the unit
// is read from its Pods, exactly as on a build without the objects:
//   - the build has ServingGroup objects and the rollout unit is the group;
//   - every Pod of the unit is controlled by one ServingGroup UID, and that
//     object is live, not terminating and owned by the ModelServing of the case;
//   - its spec.revision names a revision the runner has seen recorded (a
//     ControllerRevision) and the recorded template is the current template;
//   - no Pod of the unit carries the label of another revision.
func (l *NormalLedger) unitAtTargetByObject(u NormalUnit, objects Objects) bool {
	if !l.GroupObjects || l.Model.Mode != "SG" || u.Scope != "SG" || len(u.Pods) == 0 {
		return false
	}
	var uid types.UID
	for _, pod := range u.Pods {
		ref := groupOwnerRef(pod)
		if ref == nil || ref.UID == "" || uid != "" && ref.UID != uid {
			return false
		}
		uid = ref.UID
	}
	group := objects[SGGVR.Resource][string(uid)]
	if group == nil || group.GetUID() != uid || group.GetDeletionTimestamp() != nil || !objectOwned(group, l.Owner) {
		return false
	}
	revision := textValue(mapValue(group.Object, "spec"), "revision")
	recorded, known := l.RevisionLayouts[revision]
	if revision == "" || !known || !sameTemplates(recorded, l.Model) {
		return false
	}
	for _, pod := range u.Pods {
		if label := pod.Labels["modelserving.volcano.sh/revision"]; label != "" && label != revision {
			return false
		}
	}
	return true
}

// groupKind names the stream whose object stands for a whole ServingGroup:
// the PodGroup while the ModelServing owns Pods directly, the ServingGroup
// itself in a build that keeps it as an object.
func (l *NormalLedger) groupKind() string {
	if l.GroupObjects {
		return SGGVR.Resource
	}
	return PGGVR.Resource
}

// groupSignal reports whether a deletion seen on this stream is the start of a
// whole-group destruction.
func (l *NormalLedger) groupSignal(kind string) bool { return kind == l.groupKind() }

func (l *NormalLedger) groupNoun() string {
	if l.GroupObjects {
		return "ServingGroup"
	}
	return "PodGroup"
}

func (l *NormalLedger) groupCode() string {
	if l.GroupObjects {
		return "SERVINGGROUP"
	}
	return "PODGROUP"
}

type controllerResource struct{ resource, subresource string }

// controllerResources lists every request class a "controller paused" or
// "mutations rejected" fixture must cover. The first entry stays the
// ModelServing itself: fixtures use its rule to prove that a new spec was held.
// A build with ServingGroup objects also creates, deletes and reports groups,
// so leaving those requests free would let it keep working through the pause.
func controllerResources(groupObjects bool) []controllerResource {
	resources := []controllerResource{{"modelservings", ""}, {"modelservings", "status"}, {"pods", ""}, {"pods", "status"}, {"controllerrevisions", ""}, {"services", ""}, {"configmaps", ""}, {"podgroups", ""}, {"podgroups", "status"}}
	if groupObjects {
		resources = append(resources, controllerResource{SGGVR.Resource, ""}, controllerResource{SGGVR.Resource, "status"})
	}
	return resources
}

// controllerSyncMarkers are the log lines a controller prints only after its
// informer caches are filled: the single controller of a build without
// ServingGroup objects, and the ModelServing controller of a build with them
// (its workers start once the shared informers have synced).
var controllerSyncMarkers = []string{"initial sync has been done", "Starting the ModelServing controller"}

func controllerSynced(logs string) bool {
	for _, marker := range controllerSyncMarkers {
		if strings.Contains(logs, marker) {
			return true
		}
	}
	return false
}
