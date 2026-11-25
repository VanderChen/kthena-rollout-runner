// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GroupExpectation is one declared fact about the ServingGroup objects of one
// group name. A step is complete only while every declared fact holds, so a
// held step (holdSeconds) asserts the facts for its whole window. Fields that
// are absent are not checked.
type GroupExpectation struct {
	Name string `json:"name"`
	// Objects is the number of distinct objects (UIDs) observed under this
	// name since the case began; a same-name recreation adds one.
	Objects *int `json:"objects,omitempty"`
	// Exists, Terminating, Surge and Owned describe the object that exists now.
	Exists      *bool `json:"exists,omitempty"`
	Terminating *bool `json:"terminating,omitempty"`
	Surge       *bool `json:"surge,omitempty"`
	Owned       *bool `json:"owned,omitempty"`
	// The following compare with the snapshot taken at the end of a named
	// earlier step ("baseline" is the first one).
	UIDSameAs           string `json:"uidSameAs,omitempty"`
	UIDDiffersFrom      string `json:"uidDiffersFrom,omitempty"`
	PodUIDsSameAs       string `json:"podUIDsSameAs,omitempty"`
	PodUIDsDisjointFrom string `json:"podUIDsDisjointFrom,omitempty"`
	PodNamesSameAs      string `json:"podNamesSameAs,omitempty"`
	// Pods is the number of member Pods that carry this group name now.
	Pods *int `json:"pods,omitempty"`
	// Seen lists "Type=Status/Reason" condition values and
	// "role/index:Phase(generation)" record values that some object of this
	// name must have shown; NeverSeen lists values none may have shown.
	Seen      []string `json:"seen,omitempty"`
	NeverSeen []string `json:"neverSeen,omitempty"`
	// SurgeAtCreation is the mark of the newest object at its first observation.
	SurgeAtCreation *bool `json:"surgeAtCreation,omitempty"`
	// ForegroundDeletions is the number of earlier objects of this name that
	// were seen terminating with the foregroundDeletion finalizer.
	ForegroundDeletions *int `json:"foregroundDeletions,omitempty"`
}

// groupSnapshot is the identity of one group at the end of a step.
type groupSnapshot struct {
	UID  string            `json:"uid"`
	Pods map[string]string `json:"podUIDsByName"`
}

// groupSnapshots captures every group name that has an object or a member Pod.
// Pods are attributed by their group label, whoever owns them at the moment:
// an orphaned member still belongs to its group.
func groupSnapshots(objects Objects) map[string]groupSnapshot {
	out := map[string]groupSnapshot{}
	entry := func(name string) groupSnapshot {
		s, ok := out[name]
		if !ok {
			s = groupSnapshot{Pods: map[string]string{}}
		}
		return s
	}
	for uid, group := range objects[SGGVR.Resource] {
		s := entry(group.GetName())
		s.UID = uid
		out[group.GetName()] = s
	}
	for uid, pod := range objects["pods"] {
		name := pod.GetLabels()[LabelGroup]
		if name == "" {
			continue
		}
		s := entry(name)
		s.Pods[pod.GetName()] = uid
		out[name] = s
	}
	return out
}

func sameStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// checkGroupExpectations evaluates declared ServingGroup facts. It returns the
// first fact that does not hold, or "" when all hold. owner is the ModelServing
// UID of the case; snapshots are keyed by step name.
func checkGroupExpectations(expect []GroupExpectation, stream *GroupStream, objects Objects, owner string, snapshots map[string]map[string]groupSnapshot) string {
	if len(expect) == 0 {
		return ""
	}
	if stream == nil {
		return "ServingGroup expectations need the ServingGroup stream"
	}
	now := groupSnapshots(objects)
	for _, want := range expect {
		generations := stream.Generations(want.Name)
		var live *unstructured.Unstructured
		for _, group := range objects[SGGVR.Resource] {
			if group.GetName() == want.Name {
				live = group
			}
		}
		fail := func(format string, args ...interface{}) string {
			return "ServingGroup " + want.Name + ": " + fmt.Sprintf(format, args...)
		}
		if want.Objects != nil && len(generations) != *want.Objects {
			return fail("objects observed=%d want %d", len(generations), *want.Objects)
		}
		if want.Exists != nil && (live != nil) != *want.Exists {
			return fail("exists=%t want %t", live != nil, *want.Exists)
		}
		needsLive := want.Terminating != nil || want.Surge != nil || want.Owned != nil || want.UIDSameAs != "" || want.UIDDiffersFrom != "" || want.SurgeAtCreation != nil
		if needsLive && live == nil {
			return fail("no object exists")
		}
		if want.Terminating != nil && (live.GetDeletionTimestamp() != nil) != *want.Terminating {
			return fail("terminating=%t want %t", live.GetDeletionTimestamp() != nil, *want.Terminating)
		}
		if want.Surge != nil && (live.GetAnnotations()[AnnotationSurge] == surgeServingGroup) != *want.Surge {
			return fail("surge mark=%q want marked=%t", live.GetAnnotations()[AnnotationSurge], *want.Surge)
		}
		if want.Owned != nil && objectOwned(live, owner) != *want.Owned {
			return fail("owned by the ModelServing=%t want %t", objectOwned(live, owner), *want.Owned)
		}
		if want.SurgeAtCreation != nil {
			newest := stream.Objects[string(live.GetUID())]
			if newest == nil || newest.FirstEvent != "ADDED" {
				return fail("creation of the current object was not observed")
			}
			if (newest.SurgeAtCreation == surgeServingGroup) != *want.SurgeAtCreation {
				return fail("surge mark at creation=%q want marked=%t", newest.SurgeAtCreation, *want.SurgeAtCreation)
			}
		}
		snapshot := func(step string) (groupSnapshot, string) {
			s, ok := snapshots[step][want.Name]
			if !ok {
				return s, fail("no snapshot of step %q", step)
			}
			return s, ""
		}
		if want.UIDSameAs != "" {
			s, missing := snapshot(want.UIDSameAs)
			if missing != "" {
				return missing
			}
			if string(live.GetUID()) != s.UID {
				return fail("object UID changed since %s", want.UIDSameAs)
			}
		}
		if want.UIDDiffersFrom != "" {
			s, missing := snapshot(want.UIDDiffersFrom)
			if missing != "" {
				return missing
			}
			if s.UID == "" || string(live.GetUID()) == s.UID {
				return fail("object UID is still the one of %s", want.UIDDiffersFrom)
			}
		}
		pods := now[want.Name].Pods
		if want.Pods != nil && len(pods) != *want.Pods {
			return fail("member Pods=%d want %d", len(pods), *want.Pods)
		}
		if want.PodUIDsSameAs != "" {
			s, missing := snapshot(want.PodUIDsSameAs)
			if missing != "" {
				return missing
			}
			if !sameStringMap(pods, s.Pods) {
				return fail("member Pod UIDs changed since %s", want.PodUIDsSameAs)
			}
		}
		if want.PodNamesSameAs != "" {
			s, missing := snapshot(want.PodNamesSameAs)
			if missing != "" {
				return missing
			}
			if sortedKeys(pods) != sortedKeys(s.Pods) {
				return fail("member Pod names [%s] differ from %s [%s]", sortedKeys(pods), want.PodNamesSameAs, sortedKeys(s.Pods))
			}
		}
		if want.PodUIDsDisjointFrom != "" {
			s, missing := snapshot(want.PodUIDsDisjointFrom)
			if missing != "" {
				return missing
			}
			old := map[string]bool{}
			for _, uid := range s.Pods {
				old[uid] = true
			}
			for name, uid := range pods {
				if old[uid] {
					return fail("member Pod %s of %s was not replaced", name, want.PodUIDsDisjointFrom)
				}
			}
		}
		history := map[string]bool{}
		foreground := 0
		for i, g := range generations {
			for _, value := range g.Conditions {
				history[value] = true
			}
			for _, value := range g.Instances {
				history[value] = true
			}
			if i < len(generations)-1 && g.Foreground {
				foreground++
			}
		}
		// "Failed=True" also matches "Failed=True/<any reason>".
		shown := func(value string) bool {
			if history[value] {
				return true
			}
			for seen := range history {
				if strings.HasPrefix(seen, value+"/") {
					return true
				}
			}
			return false
		}
		for _, value := range want.Seen {
			if !shown(value) {
				return fail("never showed %s", value)
			}
		}
		for _, value := range want.NeverSeen {
			if shown(value) {
				return fail("showed %s", value)
			}
		}
		if want.ForegroundDeletions != nil && foreground != *want.ForegroundDeletions {
			return fail("earlier objects deleted in the foreground=%d want %d", foreground, *want.ForegroundDeletions)
		}
	}
	return ""
}

// conditionExpectations compares "Type" -> "Status/Reason" (or just "Status")
// with the conditions of the ModelServing under test.
func conditionExpectations(want map[string]string, model *unstructured.Unstructured) string {
	if len(want) == 0 {
		return ""
	}
	actual := map[string]string{}
	if model != nil {
		for _, raw := range listValue(mapValue(model.Object, "status"), "conditions") {
			c, ok := raw.(map[string]interface{})
			if ok {
				actual[textValue(c, "type")] = textValue(c, "status") + "/" + textValue(c, "reason")
			}
		}
	}
	kinds := make([]string, 0, len(want))
	for kind := range want {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		got, expected := actual[kind], want[kind]
		if got != expected && !(!strings.Contains(expected, "/") && strings.HasPrefix(got, expected+"/")) {
			return fmt.Sprintf("ModelServing condition %s=%q want %q", kind, got, expected)
		}
	}
	return ""
}

// PodLife is the reduced life of one Pod UID on the Pod stream. Sequences of
// two Pods compare in API server order, because both come from that stream.
type PodLife struct {
	Name     string `json:"name"`
	UID      string `json:"uid"`
	Group    string `json:"group"`
	GroupUID string `json:"groupUID,omitempty"`
	First    int64  `json:"firstSequence"`
	Deleting int64  `json:"deletionRequestedSequence,omitempty"`
	Deleted  int64  `json:"deletedSequence,omitempty"`
}

// PodTimeline records every Pod UID of the case namespace.
type PodTimeline struct {
	Lives map[string]*PodLife `json:"pods"`
}

func (t *PodTimeline) observe(sequence int64, event string, o *unstructured.Unstructured) {
	if t == nil {
		return
	}
	if t.Lives == nil {
		t.Lives = map[string]*PodLife{}
	}
	uid := string(o.GetUID())
	life := t.Lives[uid]
	if life == nil {
		life = &PodLife{Name: o.GetName(), UID: uid, Group: o.GetLabels()[LabelGroup], First: sequence}
		t.Lives[uid] = life
	}
	if ref := groupOwnerRef(o); ref != nil {
		life.GroupUID = string(ref.UID)
	}
	if (o.GetDeletionTimestamp() != nil || event == "DELETED") && life.Deleting == 0 {
		life.Deleting = sequence
	}
	if event == "DELETED" && life.Deleted == 0 {
		life.Deleted = sequence
	}
}

// checkTwoStepGroupRecovery asserts the order of a whole-group recovery whose
// trigger is a faulty member that still exists (not a Pod that is already
// gone). scope maps every original member UID of the failed group to its name.
//
//   - the faulty member is the first member of the group to be deleted;
//   - until it has disappeared no other original member is being deleted;
//   - afterwards every original member is deleted (healthy ones included);
//   - the replacement members appear only after all original members are gone.
func checkTwoStepGroupRecovery(pods *PodTimeline, faulty string, scope map[string]string) error {
	if pods == nil || pods.Lives[faulty] == nil || scope[faulty] == "" {
		return fmt.Errorf("INCONCLUSIVE: faulty member is not part of the observed recovery scope")
	}
	fault := pods.Lives[faulty]
	if fault.Deleted == 0 {
		return fmt.Errorf("GROUP_RECOVERY_INCOMPLETE: faulty member %s was never deleted", fault.Name)
	}
	lastGone := fault.Deleted
	for uid, name := range scope {
		life := pods.Lives[uid]
		if life == nil {
			return fmt.Errorf("INCONCLUSIVE: original member %s was not observed", name)
		}
		if uid == faulty {
			continue
		}
		if life.Deleting == 0 || life.Deleted == 0 {
			return fmt.Errorf("GROUP_RECOVERY_INCOMPLETE: original member %s of the failed group was kept", name)
		}
		if life.Deleting < fault.Deleted {
			return fmt.Errorf("GROUP_RECOVERY_ORDER: healthy member %s was being deleted before faulty member %s had disappeared", name, fault.Name)
		}
		lastGone = max(lastGone, life.Deleted)
	}
	for uid, life := range pods.Lives {
		// Only members created after the recovery began are replacements.
		if scope[uid] != "" || life.Group != fault.Group || life.First < fault.Deleting {
			continue
		}
		if life.First < lastGone {
			return fmt.Errorf("GROUP_RECOVERY_ORDER: replacement member %s appeared before every original member of %s was gone", life.Name, fault.Group)
		}
	}
	return nil
}
