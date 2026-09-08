// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestGraceRequiresActualUnfinishedBAndHealthyOldWorker(t *testing.T) {
	makePod := func(name, version string, entry, ready bool) corev1.Pod {
		p := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("uid-" + name), OwnerReferences: []metav1.OwnerReference{{UID: "owner"}}, Labels: map[string]string{LabelGroup: "model-0", LabelRole: "frontend", LabelRoleID: "frontend-0"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Env: []corev1.EnvVar{{Name: "ROLLOUT_VERSION", Value: version}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "workload", Ready: ready, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
		if entry {
			p.Labels[LabelEntry] = "true"
		}
		if ready {
			p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		}
		return p
	}
	entry := makePod("entry", "A", true, true)
	worker := makePod("worker", "A", false, true)
	blocked := makePod("new-entry", "B", true, false)
	blocked.Labels[LabelRoleID] = "frontend-3"
	list := &corev1.PodList{Items: []corev1.Pod{entry, worker, blocked}}
	if got, err := graceRestartMembers(&entry, list, "owner"); err != nil || len(got) != 1 || got[0].UID != worker.UID {
		t.Fatal(got, err)
	}
	for _, change := range []func(*corev1.PodList){
		func(p *corev1.PodList) { p.Items = p.Items[:2] },
		func(p *corev1.PodList) { p.Items[1].Status.Conditions = nil },
		func(p *corev1.PodList) { p.Items[1].OwnerReferences[0].UID = "other-owner" },
		func(p *corev1.PodList) {
			p.Items[2].Status.ContainerStatuses[0].State.Running = nil
			p.Items[2].Status.Phase = corev1.PodPending
		},
		func(p *corev1.PodList) { now := metav1.Now(); p.Items[1].DeletionTimestamp = &now },
	} {
		p := list.DeepCopy()
		change(p)
		if _, err := graceRestartMembers(&entry, p, "owner"); err == nil {
			t.Fatal("false restart precondition accepted")
		}
	}
}

func TestGraceCatalogueMapsAcceptedTemplateFieldAndRejectsWholeRolloutExemption(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "grace-restart"))
	if err != nil || len(cases) != 2 {
		t.Fatalf("grace cases: %d %v", len(cases), err)
	}
	for _, c := range cases {
		if _, wrong := c.Scenario.InitialSpec["restartGracePeriodSeconds"]; wrong {
			t.Fatal("invalid top-level grace field")
		}
		if c.Scenario.Steps[0].Release != "none" || c.Scenario.Steps[1].ContainerRestart == nil || c.Scenario.Steps[2].StableSeconds < 30 {
			t.Fatal("missing bounded B window or final stability")
		}
		c.Scenario.Steps[1].Expect.NoReplacement = true
		if c.validateScenario() == nil {
			t.Fatal("whole rollout cannot be frozen as grace protection")
		}
		c.Scenario.Steps[1].Expect.NoReplacement = false
		delete(mapValue(c.Scenario.InitialSpec, "template"), "restartGracePeriodSeconds")
		c.Scenario.InitialSpec["restartGracePeriodSeconds"] = 30
		if c.validateScenario() == nil {
			t.Fatal("unadmitted location accepted")
		}
	}
}
