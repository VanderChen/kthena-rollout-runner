// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestEvictionTrackerExceptionCannotHideAnUnrelatedConfigMap(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": evictionTrackerName, "uid": "actual-admission-tracker", "labels": map[string]interface{}{"modelserving.volcano.sh/name": "model"}, "ownerReferences": []interface{}{map[string]interface{}{"uid": "owner"}}},
		"data":     map[string]interface{}{"entries": `{"Role/frontend/model-0/frontend-0":{"expiresAt":"2026-09-08T15:00:00Z","triggerPodUID":"actual-evicted-uid","triggerPodName":"model-0-frontend-0-0"}}`},
	}}
	if !validEvictionTracker(cm, "owner", "actual-admission-tracker") {
		t.Fatal("actual admission tracker rejected")
	}
	for _, change := range []func(*unstructured.Unstructured){
		func(c *unstructured.Unstructured) { c.SetUID("unrelated-uid") },
		func(c *unstructured.Unstructured) { c.SetName("orphan-ranktable") },
		func(c *unstructured.Unstructured) { c.SetOwnerReferences(nil) },
		func(c *unstructured.Unstructured) { c.SetLabels(nil) },
		func(c *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(c.Object, `{"key":{"expiresAt":"broken","triggerPodUID":"uid","triggerPodName":"pod"}}`, "data", "entries")
		},
		func(c *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(c.Object, `null`, "data", "entries")
		},
		func(c *unstructured.Unstructured) { at := metav1.Now(); c.SetDeletionTimestamp(&at) },
	} {
		obj := cm.DeepCopy()
		change(obj)
		if validEvictionTracker(obj, "owner", "actual-admission-tracker") {
			t.Fatal("invalid resource hidden by tracker exception")
		}
	}
	if validEvictionTracker(cm, "owner", "") {
		t.Fatal("tracker accepted without actual admission UID proof")
	}
}

func TestEvictionScenariosUseOriginalRoleBudgetAndActualAPIActions(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "eviction"))
	if err != nil || len(cases) != 2 || cases[0].ID != "RUN-433" || cases[1].ID != "RUN-434" {
		t.Fatal("eviction coverage", err)
	}
	for _, c := range cases {
		model, err := readModel(c.Scenario.InitialSpec)
		if err != nil {
			t.Fatal(err)
		}
		if model.Roles["frontend"].R != 3 || model.Roles["frontend"].W != 0 {
			t.Fatal("source R/W changed")
		}
		ev := mapValue(mapValue(c.Scenario.InitialSpec, "rolloutStrategy"), "evictionStrategy")
		if textValue(ev, "protectionLevel") != "Role" || intValue(mapValue(ev, "roleMinAvailable"), "frontend", -1) != 1 {
			t.Fatal("eviction budget differs from source")
		}
		c.Scenario.Steps[1].Action = "observe"
		if c.validateScenario() == nil {
			t.Fatal("a normal observe action cannot stand in for real external Eviction")
		}
	}
}
