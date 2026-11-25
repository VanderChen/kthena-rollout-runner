// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"strings"
	"testing"
)

func groupObjectCases(t *testing.T) []Case {
	t.Helper()
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "servinggroup-objects"))
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestServingGroupObjectCatalogueIsExecutableAndGuarded(t *testing.T) {
	cases := groupObjectCases(t)
	if len(cases) != 13 {
		t.Fatalf("ServingGroup object cases: %d", len(cases))
	}
	for _, c := range cases {
		if c.Scenario.Requires != requiresGroupObjects || c.Format != "rollout-runner/v3" {
			t.Fatalf("%s must be restricted to builds with ServingGroup objects", c.ID)
		}
		declared := false
		for _, step := range c.Scenario.Steps {
			declared = declared || len(step.Expect.ServingGroups) > 0
		}
		if !declared || c.Scenario.Source["mustReject"] == "" || c.Scenario.Source["contract"] != "2.14" {
			t.Fatalf("%s declares no ServingGroup fact, rejected error or contract version", c.ID)
		}
	}
	mutate := func(id string, change func(*Case)) error {
		for _, c := range groupObjectCases(t) {
			if c.ID == id {
				change(&c)
				return c.Validate()
			}
		}
		t.Fatal(id)
		return nil
	}
	for name, change := range map[string]func(*Case){
		"case could run without ServingGroup objects": func(c *Case) { c.Scenario.Requires = "" },
		"unknown build requirement":                   func(c *Case) { c.Scenario.Requires = "somethingElse" },
		"snapshot of a later step": func(c *Case) {
			c.Scenario.Steps[0].Expect.ServingGroups = []GroupExpectation{{Name: "model-2", UIDSameAs: "release-the-pod-and-converge"}}
		},
		"snapshot of the step itself":            func(c *Case) { s := &c.Scenario.Steps[2]; s.Expect.ServingGroups[0].UIDSameAs = s.Name },
		"group without ordinal":                  func(c *Case) { c.Scenario.Steps[2].Expect.ServingGroups[0].Name = "model" },
		"two-step order without a faulty member": func(c *Case) { c.Scenario.Steps[2].Expect.TwoStepGroupRecovery = true },
		"deletion fixture on another action":     func(c *Case) { c.Scenario.Steps[2].GroupDelete = &GroupDeleteAction{Group: 1, Propagation: "Orphan"} },
	} {
		if err := mutate("RUN-653", change); err == nil {
			t.Errorf("accepted: %s", name)
		}
	}
	if err := mutate("RUN-656", func(c *Case) { c.Scenario.Steps[0].GroupDelete.Propagation = "Default" }); err == nil {
		t.Error("accepted a deletion without an explicit propagation policy")
	}
	if err := mutate("RUN-654", func(c *Case) { c.Scenario.Steps[0].Release = "none" }); err == nil {
		t.Error("accepted a faulty member whose replacements could never become Ready")
	}
	if resource, err := initialSyncResource("RUN-662"); err != nil || resource != "servinggroups" {
		t.Fatalf("ServingGroup informer hold: %q %v", resource, err)
	}
	if _, err := initialSyncResource("RUN-653"); err == nil {
		t.Fatal("initial-sync fault accepted for a case that does not declare it")
	}
	// The object expectations must not leak into catalogues that also run on
	// builds without ServingGroup objects.
	normal := normalCase(t, "RUN-062")
	normal.Scenario.Steps[0].Expect.ServingGroups = []GroupExpectation{{Name: "model-0"}}
	if err := normal.Validate(); err == nil || !strings.Contains(err.Error(), "ServingGroup object") {
		t.Fatalf("ServingGroup facts accepted in a catalogue for every build: %v", err)
	}
}
