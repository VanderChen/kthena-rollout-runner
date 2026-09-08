// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"
)

func TestHistoryReadCatalogueRequiresProtectedMixedStateAndRecovery(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "history-read"))
	if err != nil || len(cases) != 2 {
		t.Fatal("history read catalogue", len(cases), err)
	}
	modes := map[string]bool{}
	for _, c := range cases {
		a, err := readModel(c.Scenario.InitialSpec)
		if err != nil {
			t.Fatal(err)
		}
		b, err := readModel(c.Scenario.Steps[0].Spec)
		if err != nil {
			t.Fatal(err)
		}
		if a.Roles["frontend"].Entry != "A" || a.Roles["frontend"].W != 1 || b.Roles["frontend"].Entry != "B" || b.Roles["frontend"].W != 1 {
			t.Fatal("source template lost", c.ID)
		}
		if a.Mode == "SG" && a.P != 1 || a.Mode == "Role" && a.Roles["frontend"].P != 1 {
			t.Fatal("protected ordinal zero lost", c.ID)
		}
		modes[a.Mode] = true
		original := c.Scenario.Steps[1]
		c.Scenario.Steps[1].Expect.NoReplacement = false
		if c.validateScenario() == nil {
			t.Fatal("unbounded healthy deletion permission", c.ID)
		}
		c.Scenario.Steps[1] = original
		c.Scenario.Steps[1].Action = "observe"
		if c.validateScenario() == nil {
			t.Fatal("read fault omitted", c.ID)
		}
	}
	if len(modes) != 2 {
		t.Fatal("missing SG/Role coverage")
	}
}
