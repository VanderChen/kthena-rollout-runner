// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"
)

func TestMixedHistoryPreparationCannotReplaceHistoricalFault(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "history-source"))
	if err != nil || len(cases) != 5 {
		t.Fatal(len(cases), err)
	}
	for _, c := range cases {
		original := c.Scenario.Steps[1].Action
		c.Scenario.Steps[1].Action = "prepare-history-source"
		if c.validateScenario() == nil {
			t.Fatal("preparation substituted for actual historical fault", c.ID)
		}
		c.Scenario.Steps[1].Action = original
		c.Scenario.Steps[0].Action = "observe"
		if c.validateScenario() == nil {
			t.Fatal("actual mixed source omitted", c.ID)
		}
	}
	read, err := LoadCases(filepath.Join("..", "..", "cases", "history-read"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range read {
		if c.ID != "RUN-524" {
			continue
		}
		c.Scenario.Steps[0].Action = "prepare-history-source"
		if c.validateScenario() == nil {
			t.Fatal("Role-only cleanup exposed to SG")
		}
	}
}
