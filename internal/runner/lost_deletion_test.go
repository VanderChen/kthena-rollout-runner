// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestLostDeletionPreservesDefaultAuditAndRequiresBoundedWindow(t *testing.T) {
	for _, args := range [][]string{{"--v=2"}, {"--modelserving-audit-period=5m", "--modelserving-audit-timeout=30s"}, {"--modelserving-audit-period", "300s"}} {
		if err := requireDefaultAudit(args); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"--modelserving-audit-period=0"}, {"--modelserving-audit-period=1s"}, {"--modelserving-audit-period", "1m"}, {"--modelserving-audit-timeout=1s"}, {"--modelserving-audit-period"}} {
		if requireDefaultAudit(args) == nil {
			t.Fatal("altered audit schedule accepted")
		}
	}
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "lost-deletion"))
	if err != nil || len(cases) != 6 {
		t.Fatalf("six notification cases: %d %v", len(cases), err)
	}
	for _, c := range cases {
		if len(c.Scenario.Steps) != 1 || c.Scenario.Steps[0].StableSeconds < 30 {
			t.Fatal("missing complete recovery observation")
		}
		c.Scenario.Steps[0].TimeoutSeconds = 30
		if c.validateScenario() == nil {
			t.Fatal("shortened audit window accepted")
		}
	}
}

func TestLostDeletionScopeSelectsRealFirstOldFrontendOnly(t *testing.T) {
	for _, mode := range []string{"SG", "Role"} {
		pods := &corev1.PodList{}
		for i := 0; i < 3; i++ {
			var p corev1.Pod
			if err := convertPod(makePod(mode, "frontend", i, "A", true, "true"), &p); err != nil {
				t.Fatal(err)
			}
			pods.Items = append(pods.Items, p)
		}
		members, err := deletionNotificationMembers(pods, "owner", mode)
		if err != nil || len(members) != 1 || members[0].UID != pods.Items[2].UID {
			t.Fatal("wrong old instance", members, err)
		}
		pods.Items[2].OwnerReferences[0].UID = "other-owner"
		if _, err = deletionNotificationMembers(pods, "owner", mode); err == nil {
			t.Fatal("foreign UID selected")
		}
		pods.Items[2].OwnerReferences[0].UID = "owner"
		pods.Items[2].Status.Conditions = nil
		if _, err = deletionNotificationMembers(pods, "owner", mode); err == nil {
			t.Fatal("unhealthy old instance selected")
		}
	}
}
