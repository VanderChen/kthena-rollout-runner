// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"
)

func TestProtectedRecoveryCombinesLatestReplicasWithHistoricalWorkerLayout(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "protected-recovery"))
	if err != nil || len(cases) != 1 || cases[0].ID != "RUN-304" {
		t.Fatalf("missing RUN-304: %v", err)
	}
	c := cases[0]
	a, err := readModel(c.Scenario.InitialSpec)
	if err != nil {
		t.Fatal(err)
	}
	b, err := readModel(c.Scenario.Steps[0].Spec)
	if err != nil {
		t.Fatal(err)
	}
	if a.Roles["frontend"].R != 1 || a.Roles["frontend"].W != 1 || b.Roles["frontend"].R != 2 || b.Roles["frontend"].W != 2 {
		t.Fatal("source replica/worker expansion not applied")
	}
	target := c.Scenario.Steps[0].Expect.Targets[1]
	if target.Group == nil || *target.Group != 0 || target.Role != "frontend" {
		t.Fatal("protected group target missing")
	}
	correct := []NormalUnit{{Ordinal: 0, Version: "A", Workers: 1}, {Ordinal: 1, Version: "A", Workers: 1}}
	if ok, reason := targetFacts(correct, target); !ok {
		t.Fatal("latest R/historical W rejected", reason)
	}
	if ok, _ := targetFacts(correct[:1], target); ok {
		t.Fatal("old R=1 frozen forever was accepted")
	}
	for _, wrong := range []NormalUnit{{Ordinal: 1, Version: "B", Workers: 2}, {Ordinal: 1, Version: "A", Workers: 2}} {
		if ok, _ := targetFacts([]NormalUnit{correct[0], wrong}, target); ok {
			t.Fatal("latest B/W=2 leaked into protected group")
		}
	}
}
