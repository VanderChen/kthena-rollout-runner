// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPendingCasesHoldMoreCPUThanOneOldDeletionFrees(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "pending-faults"))
	if err != nil || len(cases) != 6 {
		t.Fatalf("six Pending cases: %d %v", len(cases), err)
	}
	for _, c := range cases {
		a, _ := readModel(c.Scenario.InitialSpec)
		b, _ := readModel(c.Scenario.Steps[1].Spec)
		before, err := gangFixtureResources(a, a.Roles)
		if err != nil {
			t.Fatal(err)
		}
		after, err := gangFixtureResources(b, b.Roles)
		if err != nil {
			t.Fatal(err)
		}
		want := "20m"
		if b.Mode == "Role" {
			want = "75m"
		}
		if q := after["cpu"]; q.Cmp(resource.MustParse(want)) != 0 {
			t.Fatalf("B gang CPU got %s want %s", q.String(), want)
		}
		old, next := before["cpu"], after["cpu"]
		if next.Cmp(old) <= 0 {
			t.Fatal("new B fits CPU released by old A")
		}
		if c.Scenario.Steps[2].Action != "verify-resource-stop" || c.Scenario.Steps[3].Action != "restore-resources" {
			t.Fatal("missing physical proof before capacity restoration")
		}
	}
}

func TestSchedulingCPUIncludesRestartableInitAndOverhead(t *testing.T) {
	req := func(n string) corev1.ResourceRequirements {
		return corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(n)}}
	}
	always := corev1.ContainerRestartPolicyAlways
	p := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Resources: req("10m")}}, InitContainers: []corev1.Container{{Resources: req("7m"), RestartPolicy: &always}, {Resources: req("20m")}}, Overhead: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2m")}}}
	if got := scheduledMilliCPU(p); got != 29 {
		t.Fatalf("init peak plus running sidecar/overhead: %dm", got)
	}
}
