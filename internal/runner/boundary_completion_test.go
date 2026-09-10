// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

func TestCompletionSourceRetainsActualOldHistoryButRejectsUnknownTemplate(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-completion"))
	if err != nil {
		t.Fatal(err)
	}
	c := cases[0]
	prior, err := newNormalLedger(c.Scenario.InitialSpec, "owner", "controlled")
	if err != nil {
		t.Fatal(err)
	}
	target, err := readModel(c.Scenario.Steps[0].Spec)
	if err != nil {
		t.Fatal(err)
	}
	prior.Committed["old-commitment"] = true
	fresh, err := completionSourceLedger(target, prior)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Committed) != 0 {
		t.Fatal("preparation commitments retained")
	}
	for i, spec := range []map[string]interface{}{c.Scenario.InitialSpec, c.Scenario.Steps[0].Spec} {
		cr := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "history"}, "data": map[string]interface{}{"data": listValue(mapValue(spec, "template"), "roles")}}}
		cr.SetUID(types.UID([]string{"A", "B"}[i]))
		cr.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
		fresh.After("controllerrevisions", "LIST", cr, Objects{})
		if fresh.error() != nil {
			t.Fatal("actual accepted history lost", fresh.error())
		}
	}
	unknown := cloneMap(c.Scenario.Steps[0].Spec)
	role := listValue(mapValue(unknown, "template"), "roles")[0].(map[string]interface{})
	container := listValue(mapValue(mapValue(role, "entryTemplate"), "spec"), "containers")[0].(map[string]interface{})
	for _, raw := range listValue(container, "env") {
		env := raw.(map[string]interface{})
		if textValue(env, "name") == "ROLLOUT_VERSION" {
			env["value"] = "C"
		}
	}
	cr := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "unknown", "uid": "unknown"}, "data": map[string]interface{}{"data": listValue(mapValue(unknown, "template"), "roles")}}}
	cr.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
	fresh.After("controllerrevisions", "LIST", cr, Objects{})
	requireNormalViolation(t, fresh, "UNEXPECTED_HISTORY_TEMPLATE")
}
