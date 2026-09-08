// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"path/filepath"
	"testing"
)

func TestSparseCompletionCannotReplaceAutomaticPromotionWithRestart(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-completion"))
	if err != nil || len(cases) != 6 {
		t.Fatal(len(cases), err)
	}
	for _, c := range cases {
		step := &c.Scenario.Steps[0]
		saved := *step
		for _, action := range []string{"update", "restart-controller", "restore-status", "observe"} {
			step.Action = action
			if c.Validate() == nil {
				t.Fatal(c.ID, "skipped source and automatic promotion", action)
			}
		}
		*step = saved
		step.Expect.RequireCompleted = false
		if c.Validate() == nil {
			t.Fatal(c.ID, "incomplete status allowed")
		}
	}
}

func TestSparseCompletionSourceRequiresExactReadyB12Identities(t *testing.T) {
	var pods []corev1.Pod
	ids := map[string]types.UID{}
	for _, role := range []string{"frontend", "backend"} {
		for _, n := range []int{1, 2} {
			o := normalTestPod("Role", role, n, "B", true, "entry")
			var pod corev1.Pod
			if err := convertPod(o, &pod); err != nil {
				t.Fatal(err)
			}
			pods = append(pods, pod)
			ids[pod.Name] = pod.UID
		}
	}
	if err := sparseCompletionPods(pods, "owner", ids); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"old", "extra", "missing", "uid", "not-ready"} {
		changed := make([]corev1.Pod, len(pods))
		for i := range pods {
			changed[i] = *pods[i].DeepCopy()
		}
		switch failure {
		case "old":
			changed[0].Spec.Containers[0].Env[0].Value = "A"
		case "extra":
			changed = append(changed, changed[0])
		case "missing":
			changed = changed[1:]
		case "uid":
			changed[0].UID = "replacement"
		case "not-ready":
			changed[0].Status.Conditions[0].Status = corev1.ConditionFalse
		}
		if sparseCompletionPods(changed, "owner", ids) == nil {
			t.Fatal("accepted", failure)
		}
	}
}

func TestSparseCompletionRejectsStaleOrStillUpdatingStatus(t *testing.T) {
	status := map[string]interface{}{"currentRevision": "B", "updateRevision": "B"}
	if err := completedBoundaryStatus(status); err != nil {
		t.Fatal(err)
	}
	status["currentRevision"] = "A"
	if completedBoundaryStatus(status) == nil {
		t.Fatal("old current counted complete")
	}
	status["currentRevision"] = "B"
	for _, name := range []string{"UpdateInProgress", "CoordinatedRoleRolloutBlocked"} {
		status["conditions"] = []interface{}{map[string]interface{}{"type": name, "status": "True"}}
		if completedBoundaryStatus(status) == nil {
			t.Fatal("still updating counted complete")
		}
	}
}
