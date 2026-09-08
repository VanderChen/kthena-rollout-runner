// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFaultConditionsDistinguishRunningReadinessFromPendingAndPullBackoff(t *testing.T) {
	p := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "workload", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	if !actualContainerFault(p, "running-not-ready") || actualContainerFault(p, "image-pull-backoff") {
		t.Fatal("running condition mismatch")
	}
	p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	if actualContainerFault(p, "running-not-ready") {
		t.Fatal("Ready is not a readiness failure")
	}
	p.Status.Conditions = nil
	p.Status.Phase = corev1.PodPending
	p.Status.ContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}
	if actualContainerFault(p, "running-not-ready") || actualContainerFault(p, "image-pull-backoff") {
		t.Fatal("Pending alone proves neither fault")
	}
	p.Status.ContainerStatuses[0].State.Waiting.Reason = "ImagePullBackOff"
	if !actualContainerFault(p, "image-pull-backoff") {
		t.Fatal("actual image backoff not detected")
	}
	now := metav1.Now()
	p.DeletionTimestamp = &now
	if actualContainerFault(p, "image-pull-backoff") {
		t.Fatal("terminating Pod cannot establish active fault stop")
	}
}

func TestMidRolloutCatalogueUsesActualFaultStopsBeforeRecovery(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "midrollout-faults"))
	if err != nil || len(cases) != 24 {
		t.Fatalf("24 midrollout scenarios: %d %v", len(cases), err)
	}
	for _, c := range cases {
		steps := c.Scenario.Steps
		if steps[len(steps)-1].Until != "settled" || steps[len(steps)-1].StableSeconds < 30 {
			t.Fatal("final convergence window missing")
		}
		stops := 0
		for _, step := range steps {
			if step.Until == "conditions" && step.HoldSeconds >= 10 {
				stops++
			}
			for _, condition := range step.Conditions {
				if condition.Kind == "pending" {
					t.Fatal("Pending cannot stand in for running-readiness or image backoff")
				}
			}
		}
		if stops != 1 {
			t.Fatal("each scenario needs exactly one observed fault hold")
		}
	}
}
