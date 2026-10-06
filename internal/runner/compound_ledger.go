// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type compoundBudget struct {
	N, U, S, P int
	C, R, V, Q int
}

func (l *NormalLedger) compoundBudget(objects Objects) compoundBudget {
	b := compoundBudget{N: l.Model.N, U: l.Model.U, S: l.Model.S, P: l.Model.P}
	b.C = l.physicalGroupCount(objects)
	target := l.Model.Roles["frontend"].Entry
	for _, unit := range l.units(objects["pods"]) {
		if unit.Ready {
			b.R++
		} else if unit.Version == target {
			b.V++
		}
	}
	for _, group := range objects["podgroups"] {
		if !objectOwned(group, l.Owner) || l.CompoundPGVersion[string(group.GetUID())] != target {
			continue
		}
		readyGenerationPod := false
		created := group.GetCreationTimestamp()
		for _, pod := range objects["pods"] {
			if !objectOwned(pod, l.Owner) || pod.GetLabels()[LabelGroup] != group.GetName() {
				continue
			}
			var typed corev1.Pod
			if convertPod(pod, &typed) != nil || podVersion(&typed) != target {
				continue
			}
			podCreated := pod.GetCreationTimestamp()
			if created.IsZero() || podCreated.IsZero() || !podCreated.Before(&created) {
				readyGenerationPod = true
				break
			}
		}
		if !readyGenerationPod {
			b.V++
		}
	}
	b.Q = max(0, b.C-max(0, b.N-b.U)-b.V)
	return b
}

func compoundUnitKey(u NormalUnit) string {
	ids := make([]string, 0, len(u.Pods))
	for _, pod := range u.Pods {
		ids = append(ids, string(pod.UID))
	}
	sort.Strings(ids)
	return fmt.Sprintf("%d/%s", u.Group, strings.Join(ids, ","))
}

func compoundHasOriginalPod(u NormalUnit, key string) bool {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 {
		return false
	}
	for _, uid := range strings.Split(parts[1], ",") {
		for _, pod := range u.Pods {
			if string(pod.UID) == uid {
				return true
			}
		}
	}
	return false
}

// compoundBefore evaluates the first accepted deletion signal for a ServingGroup.
// A PodGroup and its Pods can report that signal in either Watch order.
func (l *NormalLedger) compoundBefore(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if !l.CompoundV2 || !l.Armed || l.Model.Mode != "SG" || (kind != "pods" && kind != "podgroups") || (event != "DELETED" && o.GetDeletionTimestamp() == nil) || !objectOwned(o, l.Owner) {
		return
	}
	// The first destructive signal reserves every original Pod UID. As
	// members disappear the cohort key changes, but those later notifications
	// are still the same action, not another use of Q.
	if kind == "pods" && (l.Committed[string(o.GetUID())] || l.PGPods[string(o.GetUID())]) {
		return
	}
	groupName := o.GetName()
	if kind == "pods" {
		groupName = o.GetLabels()[LabelGroup]
	}
	ordinal := ordinal(groupName)
	u, ok := l.units(objects["pods"])[groupName]
	if !ok {
		return
	}
	key := compoundUnitKey(u)
	if l.CompoundStarted[key] {
		return
	}
	l.CompoundStarted[key] = true
	if l.Model.U == 0 && ordinal >= l.Model.N && !l.CompoundFormal[ordinal] && l.compoundTarget(u) && u.Ready {
		for _, other := range l.units(objects["pods"]) {
			if other.Group < l.Model.N && other.Group >= l.Model.P && !l.compoundTarget(other) && other.Active && !l.ScaleGroups[other.Group] {
				l.fail(fmt.Sprintf("COMPOUND_NEEDED_SURGE_REPLACED: group=%d before old group=%d", ordinal, other.Group))
				break
			}
		}
	}
	if l.ScaleGroups[ordinal] {
		delete(l.CompoundFormal, ordinal)
		return
	}
	for shrinking := range l.ScaleGroups {
		if shrinking == ordinal || !l.physicalGroups(objects)[shrinking] || l.compoundTarget(u) {
			continue
		}
		keptServingSurge := false
		for _, candidate := range l.units(objects["pods"]) {
			if candidate.Group == shrinking && shrinking >= l.Model.N && l.Model.U == 0 && l.compoundTarget(candidate) {
				keptServingSurge = true
			}
		}
		if !keptServingSurge {
			l.fail(fmt.Sprintf("COMPOUND_SCALE_FIRST: rolling group=%d while shrinking group=%d still exists", ordinal, shrinking))
			break
		}
	}
	if ordinal < l.Model.P {
		l.fail(fmt.Sprintf("COMPOUND_PROTECTED_REPLACED: group=%d", ordinal))
		return
	}
	budget := l.compoundBudget(objects)
	if l.compoundTarget(u) {
		if l.CompoundFormal[ordinal] {
			l.fail(fmt.Sprintf("COMPOUND_TARGET_REPLACED: retained group=%d", ordinal))
		} else if u.Ready && budget.R-1 < budget.N-budget.U {
			l.fail(fmt.Sprintf("COMPOUND_SURGE_SERVICE_LOSS: group=%d Ready=%d minimum=%d", ordinal, budget.R, budget.N-budget.U))
		}
		return
	}
	for _, higher := range l.units(objects["pods"]) {
		if higher.Group == ordinal || higher.Group < l.Model.P || l.compoundTarget(higher) || !higher.Active || l.ScaleGroups[higher.Group] || l.CompoundStarted[compoundUnitKey(higher)] || !l.oldCandidatePrecedes(higher, u) {
			continue
		}
		l.fail(fmt.Sprintf("COMPOUND_ORDER_MISMATCH: group=%d before higher old group=%d", ordinal, higher.Group))
		break
	}
	if u.Ready {
		if budget.R-1 < budget.N-budget.U {
			l.fail(fmt.Sprintf("COMPOUND_READY_BUDGET: group=%d Ready=%d minimum=%d", ordinal, budget.R, budget.N-budget.U))
		}
	}
	pending := 0
	for oldOrdinal, inFlightKey := range l.CompoundPending {
		for _, current := range l.units(objects["pods"]) {
			if current.Group == oldOrdinal && compoundHasOriginalPod(current, inFlightKey) && !l.compoundTarget(current) {
				pending++
				break
			}
		}
	}
	if budget.Q-pending <= 0 {
		l.fail(fmt.Sprintf("COMPOUND_Q_EXHAUSTED: group=%d C=%d N=%d U=%d V=%d Q=%d pending=%d", ordinal, budget.C, budget.N, budget.U, budget.V, budget.Q, pending))
		return
	}
	l.CompoundPending[ordinal] = key
}

// Membership-only changes do not turn a current-template ServingGroup into
// an old-template rollout candidate while its Role replicas are expanding.
func (l *NormalLedger) compoundTarget(u NormalUnit) bool {
	return l.unitTarget(u) || u.Version != "" && u.Version == l.Model.Roles["frontend"].Entry
}

func (l *NormalLedger) compoundAfter(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if !l.CompoundV2 || l.Model.Mode != "SG" || (kind != "pods" && kind != "podgroups") || !objectOwned(o, l.Owner) {
		return
	}
	name := o.GetName()
	if kind == "pods" {
		name = o.GetLabels()[LabelGroup]
	}
	groupOrdinal := ordinal(name)
	if kind == "podgroups" && event == "ADDED" && l.Armed && groupOrdinal >= l.Model.P {
		l.CompoundPGVersion[string(o.GetUID())] = l.Model.Roles["frontend"].Entry
	}
	if event == "LIST" || event == "ADDED" {
		if groupOrdinal < l.Model.N {
			l.CompoundFormal[groupOrdinal] = true
		}
	}
	if l.Armed && event == "ADDED" {
		b := l.compoundBudget(objects)
		if b.C > b.N+b.S {
			l.fail(fmt.Sprintf("COMPOUND_SURGE_VIOLATION: created group=%d active=%d ceiling=%d", groupOrdinal, b.C, b.N+b.S))
		}
		if kind == "pods" && len(l.History) >= 2 && sameTemplates(l.History[len(l.History)-2], l.Model) {
			role := o.GetLabels()[LabelRole]
			previous, hadPrevious := l.Base.Roles[role]
			current, hasCurrent := l.Model.Roles[role]
			if hadPrevious && hasCurrent && current.R > previous.R && ordinal(o.GetLabels()[LabelRoleID]) >= previous.R && b.R < b.N-b.U {
				l.fail(fmt.Sprintf("COMPOUND_ROLE_READY_BUDGET: added member to group=%d Ready=%d minimum=%d", groupOrdinal, b.R, b.N-b.U))
			}
		}
	}
	if kind != "pods" || event == "DELETED" {
		return
	}
	unit, ok := l.units(objects["pods"])[name]
	if !ok || !unit.Ready || !l.compoundTarget(unit) {
		return
	}
	newUID := false
	for _, pod := range unit.Pods {
		if !l.CompoundReadyTargetUID[string(pod.UID)] {
			l.CompoundReadyTargetUID[string(pod.UID)] = true
			newUID = true
		}
	}
	if !newUID {
		return
	}
	if len(l.CompoundPending) == 0 {
		return
	}
	if _, ok := l.CompoundPending[groupOrdinal]; ok {
		delete(l.CompoundPending, groupOrdinal)
		return
	}
	var pending []int
	for old := range l.CompoundPending {
		pending = append(pending, old)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(pending)))
	if len(pending) > 0 {
		delete(l.CompoundPending, pending[0])
	}
}
