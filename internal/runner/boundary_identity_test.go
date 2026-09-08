// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityBoundaryCannotSkipActualOldOwnerResidue(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-identity"))
	if err != nil || len(cases) != 1 || cases[0].ID != "RUN-611" {
		t.Fatal(len(cases), err)
	}
	c := cases[0]
	c.Scenario.Steps[0].Action = "update"
	if c.Validate() == nil {
		t.Fatal("ordinary rollout substituted for identity boundary")
	}
}

func TestOldIdentityCannotSupplyNewCapacityOrReadiness(t *testing.T) {
	old := normalTestPod("Role", "frontend", 0, "A", true, "entry")
	var pod corev1.Pod
	if err := convertPod(old, &pod); err != nil {
		t.Fatal(err)
	}
	now := metav1.Now()
	pod.DeletionTimestamp = &now
	ids := map[string]types.UID{pod.Name: pod.UID}
	model := map[string]interface{}{"status": map[string]interface{}{}}
	if err := identityCapacity(model, []corev1.Pod{pod}, "new", "owner", ids); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"replicas", "availableReplicas"} {
		model["status"] = map[string]interface{}{field: float64(1)}
		if err := identityCapacity(model, []corev1.Pod{pod}, "new", "owner", ids); err == nil || !strings.Contains(err.Error(), "FOREIGN_CAPACITY_COUNTED") {
			t.Fatal(field, err)
		}
	}
	model["status"] = map[string]interface{}{}
	adopted := pod.DeepCopy()
	adopted.OwnerReferences = []metav1.OwnerReference{{UID: "new"}}
	if err := identityCapacity(model, []corev1.Pod{*adopted}, "new", "owner", ids); err == nil || !strings.Contains(err.Error(), "FOREIGN_OWNER_ADOPTED") {
		t.Fatal(err)
	}
	fresh := pod.DeepCopy()
	fresh.Name = "fresh"
	fresh.UID = "fresh"
	fresh.OwnerReferences = []metav1.OwnerReference{{UID: "new"}}
	fresh.DeletionTimestamp = nil
	fresh.Status.Conditions[0].Status = corev1.ConditionFalse
	model["status"] = map[string]interface{}{"replicas": float64(1)}
	if err := identityCapacity(model, []corev1.Pod{pod, *fresh}, "new", "owner", ids); err != nil {
		t.Fatal(err)
	}
	fresh.Status.Conditions[0].Status = corev1.ConditionTrue
	if err := identityCapacity(model, []corev1.Pod{pod, *fresh}, "new", "owner", ids); err == nil || !strings.Contains(err.Error(), "CONTROL_VIOLATION") {
		t.Fatal(err)
	}
}

func TestForeignUIDAdoptionLatchesEvenBeforeNewPopulationIsArmed(t *testing.T) {
	for _, kind := range []string{"pods", "controllerrevisions"} {
		l, objects := normalFixture(t, "RUN-143")
		l.Owner = "new"
		l.Armed = false
		old := normalTestPod("Role", "backend", 0, "A", false, "entry")
		l.ForeignResidueUIDs = map[string]string{string(old.GetUID()): "owner"}
		old.SetOwnerReferences([]metav1.OwnerReference{{UID: "new"}})
		l.After(kind, "MODIFIED", old, objects)
		requireNormalViolation(t, l, "FOREIGN_OWNER_ADOPTED")
	}
}

func TestNormalCleanupRefusesReusedNamespaceIdentity(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test", UID: "someone-else"}})
	r := Runner{kube: client}
	err := r.cleanupNormalNamespace("test", "ours")
	if err == nil || !strings.Contains(err.Error(), "different namespace UID") {
		t.Fatal(err)
	}
	for _, a := range client.Actions() {
		if a.GetVerb() != "get" {
			t.Fatal("mutated reused namespace", a.GetVerb())
		}
	}
}
