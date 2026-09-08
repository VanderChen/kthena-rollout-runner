// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPluginCleanupFaultCannotFollowNewUIDOrAnotherRole(t *testing.T) {
	var old corev1.Pod
	if err := convertPod(makePod("SG", "frontend", 2, "A", true, "true"), &old); err != nil {
		t.Fatal(err)
	}
	controllerOwner := true
	old.OwnerReferences[0].Controller = &controllerOwner
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "old-ranktable", Namespace: old.Namespace, UID: "old-cm-uid", Labels: old.Labels, OwnerReferences: old.OwnerReferences}}
	rule, err := pluginRetryRule("fault", "RUN-537", "test", "owner", &old, cm)
	if err != nil || rule.Methods[0] != "DELETE" || rule.Name != cm.Name || rule.UIDs[0] != string(cm.UID) || rule.OwnerUID != "" || rule.Count != 2 {
		t.Fatal("ranktable cleanup must bind actual old UID DELETE", rule, err)
	}
	for _, mutate := range []func(*corev1.ConfigMap){
		func(c *corev1.ConfigMap) { c.OwnerReferences[0].UID = "foreign" },
		func(c *corev1.ConfigMap) { c.Labels[LabelRoleID] = "frontend-1" },
		func(c *corev1.ConfigMap) { c.UID = "" },
		func(c *corev1.ConfigMap) { c.Namespace = "another" },
	} {
		changed := cm.DeepCopy()
		mutate(changed)
		if _, err := pluginRetryRule("fault", "RUN-539", "test", "owner", &old, changed); err == nil {
			t.Fatal("unrelated ConfigMap accepted")
		}
	}
	for _, id := range []string{"RUN-536", "RUN-538"} {
		rule, err = pluginRetryRule("fault", id, "test", "owner", &old, nil)
		if err != nil || rule.Resource != "services" || rule.Methods[0] != "GET" || rule.Name != old.Name || len(rule.UIDs) != 0 || rule.Count != 2 {
			t.Fatal("headless cleanup must fault exact live lookup even if Service is absent", rule, err)
		}
	}
}

func TestPluginScenariosPreserveWZeroAndRequireActualBlockedB(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "plugin-retry"))
	if err != nil || len(cases) != 4 {
		t.Fatal("plugin case coverage", len(cases), err)
	}
	for i, c := range cases {
		if c.ID != fmt.Sprintf("RUN-%03d", 536+i) {
			t.Fatal("plugin coverage gap")
		}
		model, err := readModel(c.Scenario.InitialSpec)
		if err != nil {
			t.Fatal(err)
		}
		for _, role := range model.Roles {
			if role.W != 0 {
				t.Fatal("source W=0 changed to manufacture headless resources")
			}
		}
		c.Scenario.Steps[0].Release = "one"
		if c.validateScenario() == nil {
			t.Fatal("plugin faults may be installed after rollout already finished")
		}
	}
}
