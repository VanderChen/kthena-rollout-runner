// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// physicalGroups keeps terminating objects in the active count. A group remains
// active until its last owned Pod and PodGroup have disappeared from the Watch.
func (l *NormalLedger) physicalGroups(objects Objects) map[int]bool {
	groups := map[int]bool{}
	for _, kind := range []string{"pods", "podgroups"} {
		for _, o := range objects[kind] {
			if !objectOwned(o, l.Owner) {
				continue
			}
			name := o.GetName()
			if kind == "pods" {
				name = o.GetLabels()[LabelGroup]
			}
			groups[ordinal(name)] = true
		}
	}
	return groups
}

// A replacement PodGroup can appear after the old PodGroup has gone while old
// Pods of the same ordinal are still terminating. They are two physical SGs
// until the older Pods disappear, even though the name/ordinal is identical.
func (l *NormalLedger) physicalGroupCount(objects Objects) int {
	count := len(l.physicalGroups(objects))
	for _, group := range objects["podgroups"] {
		created := group.GetCreationTimestamp()
		if !objectOwned(group, l.Owner) || created.IsZero() {
			continue
		}
		for _, pod := range objects["pods"] {
			podCreated := pod.GetCreationTimestamp()
			if !objectOwned(pod, l.Owner) || pod.GetLabels()[LabelGroup] != group.GetName() || podCreated.IsZero() {
				continue
			}
			if podCreated.Before(&created) {
				count++
				break
			}
		}
	}
	return count
}

func compoundGroupUIDs(units map[string]NormalUnit) map[int]map[string]bool {
	out := map[int]map[string]bool{}
	for _, unit := range units {
		if out[unit.Group] == nil {
			out[unit.Group] = map[string]bool{}
		}
		for _, pod := range unit.Pods {
			out[unit.Group][string(pod.UID)] = true
		}
	}
	return out
}

func (e *normalExecution) rememberCompoundSnapshot(name string) {
	e.compoundSnapshots[name] = compoundGroupUIDs(e.l.units(e.o.objects["pods"]))
}

func (e *normalExecution) compoundFacts(expect *CompoundExpectation) (bool, string) {
	if expect == nil {
		return false, "missing compound expectation"
	}
	l := e.l
	physical := l.physicalGroups(e.o.objects)
	if count := l.physicalGroupCount(e.o.objects); count != expect.Active {
		return false, fmt.Sprintf("physical groups=%d want=%d", count, expect.Active)
	}
	units := l.units(e.o.objects["pods"])
	actual := map[int]NormalUnit{}
	ready := 0
	for _, unit := range units {
		actual[unit.Group] = unit
		if unit.Ready {
			ready++
		}
	}
	if ready != expect.Ready {
		return false, fmt.Sprintf("Ready groups=%d want=%d", ready, expect.Ready)
	}
	if len(actual) != len(expect.Groups) {
		return false, fmt.Sprintf("observed group identities=%d want=%d", len(actual), len(expect.Groups))
	}
	uids := compoundGroupUIDs(units)
	roles := l.roleUnits(e.o.objects["pods"])
	for _, want := range expect.Groups {
		got, ok := actual[want.Ordinal]
		if !ok || !physical[want.Ordinal] {
			return false, fmt.Sprintf("group %d absent", want.Ordinal)
		}
		if got.Version != want.Version || got.Ready != want.Ready {
			return false, fmt.Sprintf("group %d version/Ready=%s/%t want=%s/%t", want.Ordinal, got.Version, got.Ready, want.Version, want.Ready)
		}
		if want.RunningNotReady {
			if !got.Complete || len(got.Pods) == 0 {
				return false, fmt.Sprintf("group %d not a complete Running/NotReady cohort", want.Ordinal)
			}
			for _, pod := range got.Pods {
				if !actualContainerFault(pod, "running-not-ready") {
					return false, fmt.Sprintf("group %d Pod %s not Running/NotReady", want.Ordinal, pod.Name)
				}
			}
		}
		if want.Members > 0 {
			members := 0
			for _, role := range roles {
				if role.Group == want.Ordinal && role.Role == "frontend" {
					members++
				}
			}
			if members != want.Members {
				return false, fmt.Sprintf("group %d frontend members=%d want=%d", want.Ordinal, members, want.Members)
			}
		}
		if want.UIDSameAs != "" {
			before := e.compoundSnapshots[want.UIDSameAs][want.Ordinal]
			if before == nil || !sameUIDSet(before, uids[want.Ordinal]) {
				return false, fmt.Sprintf("group %d UID changed since %s", want.Ordinal, want.UIDSameAs)
			}
		}
	}
	if expect.Blocked && !compoundInProgress(e.current, e.o.objects["modelservings"]) {
		return false, "blocked rollout was reported complete"
	}
	return true, ""
}

func sameUIDSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for uid := range a {
		if !b[uid] {
			return false
		}
	}
	return true
}

func compoundInProgress(current *unstructured.Unstructured, models map[string]*unstructured.Unstructured) bool {
	if current == nil {
		return false
	}
	ms := current
	for _, observed := range models {
		if observed.GetUID() == current.GetUID() {
			ms = observed
			break
		}
	}
	status := mapValue(ms.Object, "status")
	update, _, _ := unstructured.NestedString(status, "updateRevision")
	currentRevision, _, _ := unstructured.NestedString(status, "currentRevision")
	if update != "" && currentRevision != update {
		return true
	}
	for _, raw := range listValue(status, "conditions") {
		c, ok := raw.(map[string]interface{})
		if ok && c["type"] == "UpdateInProgress" && c["status"] == "True" {
			return true
		}
	}
	return false
}
