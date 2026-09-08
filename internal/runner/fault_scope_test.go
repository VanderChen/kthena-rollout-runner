// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func faultScopeFixture(t *testing.T) ([]corev1.Pod, string) {
	t.Helper()
	_, objects := normalFixture(t, "RUN-228")
	var pods []corev1.Pod
	var uid string
	for _, object := range objects["pods"] {
		var p corev1.Pod
		if err := convertPod(object, &p); err != nil {
			t.Fatal(err)
		}
		p.Namespace = "fault-test"
		pods = append(pods, p)
		if p.Labels[LabelGroup] == "model-0" && p.Labels[LabelRole] == "frontend" && ordinal(p.Labels[LabelRoleID]) == 0 && podIsEntry(&p) {
			uid = string(p.UID)
		}
	}
	if uid == "" {
		t.Fatal("fixture missing exact entry")
	}
	return pods, uid
}

func TestPodFaultScopeFollowsRecoverySemantics(t *testing.T) {
	for _, policy := range []string{"", "RoleRecreate", "ServingGroupRecreate", "None"} {
		t.Run(policy, func(t *testing.T) {
			pods, uid := faultScopeFixture(t)
			plan, err := capturePodFaultScope("owner", uid, policy, pods)
			if err != nil {
				t.Fatal(err)
			}
			if !plan.contains(findFaultPod(pods, uid)) {
				t.Fatal("fault target must be recoverable, including None")
			}
			want := 2
			if policy == "None" {
				want = 1
			} else if policy == "ServingGroupRecreate" {
				want = 0
				for _, p := range pods {
					if p.Labels[LabelGroup] == "model-0" {
						want++
					}
				}
			}
			if len(plan.RecoveryUIDs) != want || len(plan.OutsideRecoveryUIDs)+want != len(pods) {
				t.Fatalf("unexpected cohort: %+v", plan)
			}
		})
	}
}
func findFaultPod(pods []corev1.Pod, uid string) *corev1.Pod {
	for i := range pods {
		if string(pods[i].UID) == uid {
			return &pods[i]
		}
	}
	return nil
}
func TestPodFaultPermissionCannotTransferToReplacement(t *testing.T) {
	pods, uid := faultScopeFixture(t)
	plan, err := capturePodFaultScope("owner", uid, "ServingGroupRecreate", pods)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"uid", "owner", "namespace", "name"} {
		p := findFaultPod(pods, uid).DeepCopy()
		switch field {
		case "uid":
			p.UID = types.UID("replacement")
		case "owner":
			p.OwnerReferences = nil
		case "namespace":
			p.Namespace = "other"
		case "name":
			p.Name = "other"
		}
		if plan.contains(p) {
			t.Fatalf("permission transferred across %s", field)
		}
	}
}
func TestPodFaultRequiresActualReadyTarget(t *testing.T) {
	for _, kind := range []string{"missing", "owner", "deleting", "duplicate", "unready", "policy"} {
		t.Run(kind, func(t *testing.T) {
			pods, uid := faultScopeFixture(t)
			p := findFaultPod(pods, uid)
			policy := "None"
			switch kind {
			case "missing":
				uid = "absent"
			case "owner":
				p.OwnerReferences = nil
			case "deleting":
				now := metav1.Now()
				p.DeletionTimestamp = &now
			case "duplicate":
				pods = append(pods, *p.DeepCopy())
			case "unready":
				p.Status.Conditions = nil
			case "policy":
				policy = "unknown"
			}
			if _, err := capturePodFaultScope("owner", uid, policy, pods); err == nil {
				t.Fatal("invalid injection prerequisite accepted")
			}
		})
	}
}
