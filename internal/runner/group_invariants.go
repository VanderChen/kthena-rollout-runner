// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const surgeServingGroup = "serving-group"

// declaredRange returns the smallest and largest spec.replicas of the current
// and the previously accepted spec. A group created or promoted around a
// replicas change is judged against the bound that favours the controller:
// the runner cannot know which of the two specs the decision was taken under.
func (l *NormalLedger) declaredRange() (low, high int) {
	low, high = l.Model.N, l.Model.N
	if n := len(l.History); n >= 2 {
		low, high = min(low, l.History[n-2].N), max(high, l.History[n-2].N)
	}
	return low, high
}

func groupInstances(o *unstructured.Unstructured) map[string]int64 {
	out := map[string]int64{}
	if o == nil {
		return out
	}
	for _, raw := range listValue(mapValue(o.Object, "status"), "roleInstances") {
		instance, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		index, _, _ := unstructured.NestedInt64(instance, "index")
		generation, _, _ := unstructured.NestedInt64(instance, "generation")
		out[fmt.Sprintf("%s/%d", textValue(instance, "role"), index)] = generation
	}
	return out
}

// groupObjectBefore compares one ServingGroup event with the previous
// observation of the same UID. Everything it checks is carried by this one
// stream, so no cross-stream order is assumed.
//
//   - the surge mark exists from creation and is never added afterwards;
//   - a group inside the declared range is never created as a surge;
//   - promotion (the mark is removed) leaves generation untouched and only
//     happens once the ordinal is inside the declared range;
//   - an instance generation never decreases, and the first record of a newly
//     created object starts at zero instead of continuing a previous object.
func (l *NormalLedger) groupObjectBefore(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if !l.GroupObjects || kind != SGGVR.Resource || event == "DELETED" || !objectOwned(o, l.Owner) {
		return
	}
	previous := objects[SGGVR.Resource][string(o.GetUID())]
	name, mark := o.GetName(), o.GetAnnotations()[AnnotationSurge]
	low, high := l.declaredRange()
	if previous == nil {
		if event == "ADDED" && l.Model.Mode == "SG" && mark == surgeServingGroup && ordinal(name) < low {
			l.fail(fmt.Sprintf("SURGE_MARK_ON_FORMAL_GROUP: %s created with the surge mark inside replicas=%d", name, low))
		}
		if event == "ADDED" {
			for instance, generation := range groupInstances(o) {
				if generation != 0 {
					l.fail(fmt.Sprintf("LEDGER_INHERITED: new ServingGroup %s starts %s at generation %d", name, instance, generation))
				}
			}
		}
		return
	}
	before := previous.GetAnnotations()[AnnotationSurge]
	if before == "" && mark != "" {
		l.fail(fmt.Sprintf("SURGE_MARK_ADDED_LATER: %s uid=%s was created without the surge mark", name, o.GetUID()))
	}
	if before == surgeServingGroup && mark == "" && o.GetDeletionTimestamp() == nil && l.Model.Mode == "SG" {
		if o.GetGeneration() != previous.GetGeneration() {
			l.fail(fmt.Sprintf("SURGE_PROMOTION_CHANGED_SPEC: %s generation %d -> %d", name, previous.GetGeneration(), o.GetGeneration()))
		}
		if ordinal(name) >= high {
			l.fail(fmt.Sprintf("SURGE_PROMOTED_OUTSIDE_RANGE: %s promoted with replicas=%d", name, high))
		}
		if l.PromotedGroups == nil {
			l.PromotedGroups = map[string]bool{}
		}
		l.PromotedGroups[string(o.GetUID())] = true
	}
	old, current := groupInstances(previous), groupInstances(o)
	for instance, generation := range current {
		earlier, existed := old[instance]
		if existed && generation < earlier {
			l.fail(fmt.Sprintf("LEDGER_GENERATION_DECREASED: %s %s %d -> %d", name, instance, earlier, generation))
		}
		if !existed && len(old) == 0 && generation != 0 && l.CreatedGroups[string(o.GetUID())] {
			l.fail(fmt.Sprintf("LEDGER_INHERITED: new ServingGroup %s starts %s at generation %d", name, instance, generation))
		}
	}
}

// groupObjectAfter remembers which objects were seen from their creation; only
// those are known to start with an empty record.
func (l *NormalLedger) groupObjectAfter(kind, event string, o *unstructured.Unstructured) {
	if !l.GroupObjects || kind != SGGVR.Resource || event != "ADDED" || !objectOwned(o, l.Owner) {
		return
	}
	if l.CreatedGroups == nil {
		l.CreatedGroups = map[string]bool{}
	}
	l.CreatedGroups[string(o.GetUID())] = true
}

// groupPodAfter is the stronger form of the earlier "two physical groups"
// accounting. With ServingGroup objects a replacement object of the same name
// cannot exist before the previous one is gone, and that one stays until its
// Pods are gone. A Pod of the new object next to a Pod of the previous object
// is therefore an error, not a second unit to count. Both Pods are on the Pod
// stream, so their order is the API server's order.
//
// It also keeps a promoted surge group honest: promotion edits metadata, so
// the Pods that carried the copy of the mark must lose it in place.
func (l *NormalLedger) groupPodAfter(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if !l.GroupObjects || kind != "pods" || !objectOwned(o, l.Owner) {
		return
	}
	group := groupOwnerRef(o)
	if group == nil {
		return
	}
	if event == "DELETED" || o.GetDeletionTimestamp() != nil {
		owner := objects[SGGVR.Resource][string(group.UID)]
		if l.PromotedGroups[string(group.UID)] && owner != nil && owner.GetDeletionTimestamp() == nil && o.GetAnnotations()[AnnotationSurge] == surgeServingGroup {
			l.fail(fmt.Sprintf("SURGE_PROMOTION_RECREATED_POD: %s deleted while it still carried the copy of the removed surge mark of %s", o.GetName(), group.Name))
		}
		return
	}
	if event != "ADDED" {
		return
	}
	for _, other := range objects["pods"] {
		if other.GetUID() == o.GetUID() || other.GetNamespace() != o.GetNamespace() || other.GetLabels()[LabelGroup] != o.GetLabels()[LabelGroup] || !objectOwned(other, l.Owner) {
			continue
		}
		if previous := groupOwnerRef(other); previous != nil && previous.UID != group.UID {
			l.fail(fmt.Sprintf("GROUP_OBJECT_OVERLAP: Pod %s of ServingGroup %s uid=%s created while Pod %s of the previous object uid=%s still exists", o.GetName(), group.Name, group.UID, other.GetName(), previous.UID))
			return
		}
	}
}

// groupObjectsSettled is the ServingGroup part of a settled state: one live
// owned object per expected group (groups < 0 skips the count) and consistent
// surge marks.
func (l *NormalLedger) groupObjectsSettled(objects Objects, groups int) (bool, string) {
	if !l.GroupObjects {
		return true, ""
	}
	owned := 0
	for _, group := range objects[SGGVR.Resource] {
		if !objectOwned(group, l.Owner) {
			continue
		}
		owned++
		if group.GetDeletionTimestamp() != nil {
			return false, "ServingGroup still terminating: " + group.GetName()
		}
	}
	if groups >= 0 && owned != groups {
		return false, fmt.Sprintf("ServingGroup objects %d want %d", owned, groups)
	}
	return l.surgeMarksSettled(objects)
}

// surgeMarksSettled holds at every stable checkpoint of a ServingGroup
// rollout: a group inside the declared range has been promoted, and each Pod
// carries the copy of the mark exactly when its ServingGroup carries the mark.
// The copy follows one step behind, so a mismatch is "not settled yet"; a
// mismatch that never clears times the step out with this reason.
func (l *NormalLedger) surgeMarksSettled(objects Objects) (bool, string) {
	if !l.GroupObjects || l.Model.Mode != "SG" {
		return true, ""
	}
	marked := map[string]bool{}
	for uid, group := range objects[SGGVR.Resource] {
		if !objectOwned(group, l.Owner) || group.GetAnnotations()[AnnotationSurge] != surgeServingGroup {
			continue
		}
		marked[uid] = true
		if group.GetDeletionTimestamp() == nil && ordinal(group.GetName()) < l.Model.N {
			return false, fmt.Sprintf("ServingGroup %s inside replicas=%d still carries the surge mark", group.GetName(), l.Model.N)
		}
	}
	for _, object := range objects["pods"] {
		var pod corev1.Pod
		if convertPod(object, &pod) != nil || !owned(&pod, l.Owner) || pod.DeletionTimestamp != nil {
			continue
		}
		group := groupOwnerRef(&pod)
		if group == nil {
			continue
		}
		copied := pod.Annotations[AnnotationSurge] == surgeServingGroup
		if copied != marked[string(group.UID)] {
			return false, fmt.Sprintf("Pod %s surge copy=%t differs from ServingGroup %s mark=%t", pod.Name, copied, group.Name, marked[string(group.UID)])
		}
	}
	return true, ""
}
