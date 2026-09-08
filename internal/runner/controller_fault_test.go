// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"
)

func TestControllerFaultRequiresExactlyOneDeclaredTermination(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "controller-restart"))
	if err != nil || len(cases) != 6 {
		t.Fatalf("six controller recovery cases: %d %v", len(cases), err)
	}
	for _, c := range cases {
		count := 0
		var action ScenarioStep
		for _, step := range c.Scenario.Steps {
			if step.Action == "terminate-controller" {
				count++
				action = step
			}
		}
		if count != 1 {
			t.Fatal("missing bounded termination")
		}
		copyCase := c
		scenario := *c.Scenario
		copyCase.Scenario = &scenario
		scenario.Steps = append(append([]ScenarioStep{}, c.Scenario.Steps...), action)
		if copyCase.validateScenario() == nil {
			t.Fatal("repeated restarts could conceal failed autonomous recovery")
		}
		copyCase = c
		copyCase.ID = "RUN-434"
		if copyCase.validateScenario() == nil {
			t.Fatal("termination permitted outside declared restart scope")
		}
	}
}
