// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"

	"kthena.local/rollout-runner/internal/faultproxy"
)

func TestInitialSyncRequiresActualUnreleasedStartupRequest(t *testing.T) {
	good := faultproxy.RuleStatus{Rule: faultproxy.Rule{ID: "initial", InitialSync: true}, Active: true, Hits: 1}
	if err := requireInitialHold(faultproxy.State{Rules: []faultproxy.RuleStatus{good}}, "initial"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*faultproxy.RuleStatus){
		func(r *faultproxy.RuleStatus) { r.Hits = 0 },
		func(r *faultproxy.RuleStatus) { r.Active = false },
		func(r *faultproxy.RuleStatus) { r.Released = 1 },
		func(r *faultproxy.RuleStatus) { r.EndReason = "expired" },
		func(r *faultproxy.RuleStatus) { r.InitialSync = false },
		func(r *faultproxy.RuleStatus) { r.ID = "unrelated" },
	} {
		changed := good
		mutate(&changed)
		if requireInitialHold(faultproxy.State{Rules: []faultproxy.RuleStatus{changed}}, "initial") == nil {
			t.Fatal("unproven informer fault accepted")
		}
	}
}

func TestInitialSyncCasesRequireRecoveryAndPreserveSourceLayout(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "initial-sync"))
	if err != nil || len(cases) != 4 {
		t.Fatal("initial-sync coverage", len(cases), err)
	}
	for _, c := range cases {
		resource, err := initialSyncResource(c.ID)
		if err != nil || resource == "" {
			t.Fatal(err)
		}
		m, err := readModel(c.Scenario.InitialSpec)
		if err != nil {
			t.Fatal(err)
		}
		for _, role := range m.Roles {
			if role.W != 0 {
				t.Fatal("source W=0 altered")
			}
		}
		c.Scenario.Steps[0].Expect.NoReplacement = true
		if c.validateScenario() == nil {
			t.Fatal("permanent preservation cannot stand in for recovered B")
		}
		c.Scenario.Steps[0].Expect.NoReplacement = false
		c.Scenario.Steps[0].Action = "update"
		if c.validateScenario() == nil {
			t.Fatal("no-injection shortcut accepted")
		}
	}
	if _, err = initialSyncResource("RUN-449"); err == nil {
		t.Fatal("event replay scenario incorrectly mapped to initial sync")
	}
}
