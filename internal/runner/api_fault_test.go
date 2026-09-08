// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestAPIFaultMapsActualCollectionAndStatusGeneration(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "api-retry"))
	if err != nil || len(cases) != 8 {
		t.Fatalf("API cases: %d %v", len(cases), err)
	}
	for i, c := range cases {
		if c.ID != fmt.Sprintf("RUN-%03d", 455+i) {
			t.Fatal("API coverage gap")
		}
		var old corev1.Pod
		if err := convertPod(makePod("SG", "frontend", 2, "A", true, "true"), &old); err != nil {
			t.Fatal(err)
		}
		rule, err := apiRetryRule("fault", "test", "owner", 2, c.ID, &old)
		if err != nil || rule.Count != 1 || rule.StatusCode != 503 || rule.DurationSeconds > 900 {
			t.Fatal("unbounded API failure", rule, err)
		}
		switch i % 4 {
		case 0:
			if rule.Resource != "pods" || rule.Methods[0] != "POST" || rule.OwnerUID != "owner" {
				t.Fatal("unowned create")
			}
		case 1:
			if rule.CollectionOnly || rule.Methods[0] != "DELETE" || rule.Name != old.Name || rule.OwnerUID != "" || len(rule.UIDs) != 1 || rule.UIDs[0] != string(old.UID) {
				t.Fatal("actual native DELETE must bind the captured UID")
			}
			if _, err := apiRetryRule("fault", "test", "owner", 2, c.ID, nil); err == nil {
				t.Fatal("unguarded deletion fault accepted")
			}
		case 2:
			if rule.Resource != "modelservings" || rule.Subresource != "status" || rule.Generation != 2 || rule.OwnerUID != "owner" {
				t.Fatal("status fault could hit old generation")
			}
		case 3:
			if !rule.CollectionOnly || rule.Resource != "controllerrevisions" || rule.Methods[0] != "GET" {
				t.Fatal("named Get is not List")
			}
		}
		c.Scenario.Steps[0].Release = "one"
		if c.validateScenario() == nil {
			t.Fatal("fault can be installed after B has already finished")
		}
	}
}

func TestAPIRetryWindowRequiresRealBlockedBAndFullOldCapacity(t *testing.T) {
	pods := &corev1.PodList{}
	for i := 0; i < 4; i++ {
		v, ready := "A", true
		if i == 3 {
			v, ready = "B", false
		}
		var p corev1.Pod
		if err := convertPod(makePod("SG", "frontend", i, v, ready, "true"), &p); err != nil {
			t.Fatal(err)
		}
		p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "workload", Ready: ready, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
		pods.Items = append(pods.Items, p)
	}
	if !apiRetryWindow(pods, "owner") {
		t.Fatal("valid B window rejected")
	}
	for _, change := range []func(*corev1.PodList){
		func(p *corev1.PodList) { p.Items[0].Status.Conditions = nil },
		func(p *corev1.PodList) {
			p.Items[3].Status.ContainerStatuses[0].State.Running = nil
			p.Items[3].Status.Phase = corev1.PodPending
		},
		func(p *corev1.PodList) { p.Items[3].OwnerReferences[0].UID = "other-owner" },
	} {
		p := pods.DeepCopy()
		change(p)
		if apiRetryWindow(p, "owner") {
			t.Fatal("false API fault trigger accepted")
		}
	}
}
