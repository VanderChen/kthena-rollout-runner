// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RecoveryRecord is a finite permission for a captured fault, not a temporary
// switch that disables the ordinary rollout oracle.
type RecoveryRecord struct {
	Scope          PodFaultScope     `json:"scope"`
	Armed          time.Time         `json:"armed"`
	Deleted        map[string]bool   `json:"observedDeletingUIDs"`
	WantVersions   map[string]string `json:"firstReplacementVersions"`
	ProtectedNames map[string]bool   `json:"protectedRecoveryNames"`
	Replacements   map[string]string `json:"replacementUIDsByName"`
	PodGroups      map[string]string `json:"recoverablePodGroupUIDs"`
	OrderUnits     map[string]bool   `json:"recoveringOrderUnits"`
	OrderComplete  bool              `json:"orderRecoveryComplete"`
}

func (l *NormalLedger) armRecovery(scope PodFaultScope, objects Objects) error {
	if scope.OwnerUID != l.Owner || len(scope.RecoveryUIDs) == 0 {
		return fmt.Errorf("invalid recovery scope owner/UIDs")
	}
	r := &RecoveryRecord{Scope: scope, Armed: time.Now().UTC(), Deleted: map[string]bool{}, WantVersions: map[string]string{}, ProtectedNames: map[string]bool{}, Replacements: map[string]string{}, PodGroups: map[string]string{}}
	r.OrderUnits = map[string]bool{}
	for uid, name := range scope.RecoveryUIDs {
		object := objects["pods"][uid]
		var pod corev1.Pod
		if object == nil || convertPod(object, &pod) != nil || !scope.contains(&pod) || !podReady(&pod) || pod.DeletionTimestamp != nil || l.Committed[uid] {
			return fmt.Errorf("recovery scope no longer has original Ready Pod %s", name)
		}
		layout, ok := l.Model.Roles[pod.Labels[LabelRole]]
		if !ok {
			return fmt.Errorf("recovery Role no longer declared")
		}
		want := layout.Entry
		if !podIsEntry(&pod) {
			want = layout.Worker
		}
		if l.Protected[uid] != "" {
			r.ProtectedNames[name] = true
			want = podVersion(&pod)
		}
		r.WantVersions[name] = want
		r.OrderUnits["SG/"+pod.Labels[LabelGroup]] = true
		r.OrderUnits["Role/"+roleUnitKey(&pod)] = true
		if scope.Recovery == "ServingGroupRecreate" {
			for pgUID, pg := range objects["podgroups"] {
				if objectOwned(pg, l.Owner) && pg.GetNamespace() == scope.Namespace && pg.GetName() == pod.Labels[LabelGroup] && pg.GetDeletionTimestamp() == nil {
					r.PodGroups[pgUID] = pg.GetName()
				}
			}
		}
	}
	if scope.Recovery == "ServingGroupRecreate" && len(r.PodGroups) != 1 {
		return fmt.Errorf("full SG recovery requires its exact existing PodGroup UID")
	}
	// Only old protected identities inside this exact recovery scope can go.
	// All other protection, capacity, ordering and plugin checks remain armed.
	for uid := range scope.RecoveryUIDs {
		delete(l.Protected, uid)
	}
	l.Recoveries = append(l.Recoveries, r)
	return nil
}

// A recovery does not become a fresh old-bad candidate just because its last
// captured Pod disappeared. This affects precedence only, never Ready credit
// or permission to delete a replacement UID.
func (l *NormalLedger) recoveryOrderPending(u NormalUnit) bool {
	for _, r := range l.Recoveries {
		if r.Scope.OwnerUID != l.Owner || !r.Deleted[r.Scope.TargetUID] || r.OrderComplete || !r.OrderUnits[l.Model.Mode+"/"+u.Key] {
			continue
		}
		for _, pod := range u.Pods {
			if pod.Namespace != r.Scope.Namespace {
				continue
			}
			if r.Scope.RecoveryUIDs[string(pod.UID)] != "" {
				return true
			}
		}
		// A newly superseded replacement is an old candidate again; an old
		// recovery record must not lock obsolete bad versions indefinitely.
		superseded := false
		for _, pod := range u.Pods {
			if r.Replacements[pod.Name] != string(pod.UID) || pod.Namespace != r.Scope.Namespace {
				continue
			}
			layout := l.Model.Roles[pod.Labels[LabelRole]]
			want := layout.Entry
			if !podIsEntry(pod) {
				want = layout.Worker
			}
			if want != r.WantVersions[pod.Name] && !r.ProtectedNames[pod.Name] {
				superseded = true
			}
		}
		if !superseded {
			return true
		}
	}
	return false
}

func (l *NormalLedger) updateRecoveryOrderState(objects Objects) {
	for _, r := range l.Recoveries {
		if r.OrderComplete || !r.Deleted[r.Scope.TargetUID] {
			continue
		}
		complete := true
		for uid := range r.Scope.RecoveryUIDs {
			complete = complete && objects["pods"][uid] == nil
		}
		for name, version := range r.WantVersions {
			object := objects["pods"][r.Replacements[name]]
			var pod corev1.Pod
			if object == nil || convertPod(object, &pod) != nil || !owned(&pod, r.Scope.OwnerUID) || pod.Namespace != r.Scope.Namespace || pod.Name != name || podVersion(&pod) != version || !podReady(&pod) || pod.DeletionTimestamp != nil {
				complete = false
			}
		}
		r.OrderComplete = complete
	}
}

func (l *NormalLedger) recoveryBefore(kind, event string, object *unstructured.Unstructured, objects Objects) bool {
	if event != "DELETED" && object.GetDeletionTimestamp() == nil {
		return false
	}
	if kind == "podgroups" && objectOwned(object, l.Owner) {
		for _, r := range l.Recoveries {
			if r.Scope.Namespace == object.GetNamespace() && r.PodGroups[string(object.GetUID())] == object.GetName() && object.GetName() != "" {
				l.PGCommitted[string(object.GetUID())] = true
				// The permission remains bound to old Pod UIDs, even if PG and Pod
				// streams deliver their destructive events in different orders.
				for uid := range r.Scope.RecoveryUIDs {
					l.Committed[uid] = true
				}
				return true
			}
		}
	}
	if kind != "pods" {
		return false
	}
	var pod corev1.Pod
	if convertPod(object, &pod) != nil {
		return false
	}
	for _, r := range l.Recoveries {
		if !r.Scope.contains(&pod) {
			continue
		}
		uid := string(pod.UID)
		if !r.Deleted[uid] {
			key := roleUnitKey(&pod)
			if l.Model.Mode == "SG" {
				key = pod.Labels[LabelGroup]
			}
			u := l.units(objects["pods"])[key]
			reason := "fault-recovery"
			if uid == r.Scope.TargetUID {
				reason = "external-fault"
			}
			l.Starts = append(l.Starts, NormalStart{At: time.Now().UTC(), Phase: l.Phase, Key: key, Scope: u.Scope, Group: u.Group, Role: pod.Labels[LabelRole], Ordinal: u.Ordinal, Version: podVersion(&pod), Reason: reason, UIDs: []string{uid}})
		}
		r.Deleted[uid] = true
		l.Committed[uid] = true
		return true
	}
	return false
}

func (l *NormalLedger) recoveryAfter(kind, event string, object *unstructured.Unstructured) {
	if kind != "pods" || event == "DELETED" || !objectOwned(object, l.Owner) {
		return
	}
	var pod corev1.Pod
	if convertPod(object, &pod) != nil {
		return
	}
	uid := string(pod.UID)
	for _, r := range l.Recoveries {
		want, selected := r.WantVersions[pod.Name]
		if !selected || pod.Namespace != r.Scope.Namespace || r.Scope.RecoveryUIDs[uid] != "" {
			continue
		}
		if earlier := r.Replacements[pod.Name]; earlier != "" && earlier != uid {
			l.fail("RECOVERY_DUPLICATE_CREATION: " + pod.Name)
		}
		r.Replacements[pod.Name] = uid
		if podVersion(&pod) != want {
			l.fail(fmt.Sprintf("RECOVERY_WRONG_TARGET: %s version=%s want=%s", pod.Name, podVersion(&pod), want))
		}
		if r.ProtectedNames[pod.Name] {
			l.Protected[uid] = pod.Name
		}
	}
}

func (l *NormalLedger) recoverySettled(objects Objects) (bool, string) {
	for _, r := range l.Recoveries {
		if !r.Deleted[r.Scope.TargetUID] {
			return false, "external deletion not observed for exact target UID"
		}
		for uid, name := range r.Scope.RecoveryUIDs {
			if objects["pods"][uid] != nil {
				return false, "old recovery member still present: " + name
			}
		}
		for name := range r.ProtectedNames {
			uid := r.Replacements[name]
			if uid == "" || objects["pods"][uid] == nil {
				return false, "protected recovery member not restored: " + name
			}
		}
	}
	return true, ""
}
