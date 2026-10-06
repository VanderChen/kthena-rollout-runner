// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"
)

func TestHistoricalNumericInputsResolveWithCurrentRounding(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-numeric"))
	if err != nil || len(cases) != 26 {
		t.Fatal(len(cases), err)
	}
	want := map[string][3]int{"RUN-543": {0, 0, 0}, "RUN-556": {0, 1, 0}, "RUN-546": {1, 2, 2}, "RUN-558": {1, 2, 2}, "RUN-551": {0, 4, 0}, "RUN-552": {1, 0, 4}, "RUN-565": {4, 0, 0}}
	for _, c := range cases {
		model, err := readModel(c.Scenario.InitialSpec)
		if err != nil {
			t.Fatal(err)
		}
		got := [3]int{model.U, model.S, model.P}
		if model.Mode == "Role" {
			f := model.Roles["frontend"]
			got = [3]int{f.U, f.S, f.P}
		}
		if expected, ok := want[c.ID]; ok {
			if expected != got {
				t.Fatal(c.ID, got, expected)
			}
			delete(want, c.ID)
		}
		original := c.Scenario.Steps[0]
		c.Scenario.Steps[0].Action = "observe"
		if c.validateScenario() == nil {
			t.Fatal("boundary request skipped", c.ID)
		}
		c.Scenario.Steps[0] = original
	}
	if len(want) != 0 {
		t.Fatal("missing numeric cases", want)
	}
}
