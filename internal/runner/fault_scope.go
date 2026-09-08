// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// PodFaultScope captures the existing identities before fault injection. It is
// not a selector that could accidentally grant recovery permission to new Pods.
type PodFaultScope struct {
	OwnerUID            string            `json:"ownerUID"`
	Namespace           string            `json:"namespace"`
	TargetUID           string            `json:"targetUID"`
	TargetName          string            `json:"targetName"`
	Recovery            string            `json:"recovery"`
	RecoveryUIDs        map[string]string `json:"recoveryUIDs"`
	OutsideRecoveryUIDs map[string]string `json:"outsideRecoveryUIDs"`
}

func capturePodFaultScope(owner, targetUID, recovery string, pods []corev1.Pod) (PodFaultScope, error) {
	plan := PodFaultScope{OwnerUID: owner, TargetUID: targetUID, Recovery: recovery, RecoveryUIDs: map[string]string{}, OutsideRecoveryUIDs: map[string]string{}}
	if owner == "" || targetUID == "" {
		return plan, fmt.Errorf("fault requires exact owner and target UID")
	}
	if recovery == "" {
		plan.Recovery = "RoleRecreate"
	}
	if plan.Recovery != "None" && plan.Recovery != "RoleRecreate" && plan.Recovery != "ServingGroupRecreate" {
		return plan, fmt.Errorf("unsupported recovery policy %q", recovery)
	}
	var target *corev1.Pod
	seen := map[string]bool{}
	for i := range pods {
		p := &pods[i]
		uid := string(p.UID)
		if uid == "" || seen[uid] {
			return plan, fmt.Errorf("missing or duplicate Pod UID in fault snapshot")
		}
		seen[uid] = true
		if uid == targetUID {
			target = p
		}
	}
	if target == nil || !owned(target, owner) || target.DeletionTimestamp != nil || !podReady(target) {
		return plan, fmt.Errorf("fault target is not an existing Ready owned Pod")
	}
	for _, key := range []string{LabelGroup, LabelRole, LabelRoleID} {
		if target.Labels[key] == "" {
			return plan, fmt.Errorf("fault target missing identity label %s", key)
		}
	}
	plan.Namespace, plan.TargetName = target.Namespace, target.Name
	for i := range pods {
		p := &pods[i]
		if !owned(p, owner) || p.Namespace != target.Namespace {
			continue
		}
		selected := string(p.UID) == targetUID
		if plan.Recovery == "RoleRecreate" {
			selected = roleUnitKey(p) == roleUnitKey(target)
		} else if plan.Recovery == "ServingGroupRecreate" {
			selected = p.Labels[LabelGroup] == target.Labels[LabelGroup]
		}
		if selected {
			if p.DeletionTimestamp != nil {
				return plan, fmt.Errorf("recovery cohort already deleting: %s", p.Name)
			}
			plan.RecoveryUIDs[string(p.UID)] = p.Name
		} else {
			plan.OutsideRecoveryUIDs[string(p.UID)] = p.Name
		}
	}
	return plan, nil
}

// contains never transfers an old object's recovery permission to a replacement
// with the same name, another namespace, or another ModelServing owner.
func (p PodFaultScope) contains(pod *corev1.Pod) bool {
	return pod != nil && pod.Namespace == p.Namespace && owned(pod, p.OwnerUID) && p.RecoveryUIDs[string(pod.UID)] == pod.Name && pod.Name != ""
}
