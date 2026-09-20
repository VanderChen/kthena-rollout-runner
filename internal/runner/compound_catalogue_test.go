// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestCompoundCatalogueIsExecutable(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "servinggroup-compound-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 35 {
		t.Fatalf("want 35 design cases, got %d", len(cases))
	}
	for i, c := range cases {
		want := fmt.Sprintf("RUN-%03d", i+618)
		if c.ID != want {
			t.Fatalf("catalogue position %d: %s, want %s", i, c.ID, want)
		}
		if c.Scenario == nil || c.Scenario.DesignID != compoundDesignID(i+618) {
			t.Fatalf("missing design mapping for %s", c.ID)
		}
	}
}

func TestCompoundCatalogueRejectsWeakDesignAndBlockedContracts(t *testing.T) {
	c := compoundCase(t, "RUN-623")
	c.Scenario.DesignID = "SG-S07"
	if err := c.Validate(); err == nil {
		t.Fatal("mismatched design ID accepted")
	}
	c = compoundCase(t, "RUN-623")
	for i := range c.Scenario.Steps {
		if c.Scenario.Steps[i].Expect.Compound.Blocked {
			c.Scenario.Steps[i].StableSeconds = 0
			break
		}
	}
	if err := c.Validate(); err == nil {
		t.Fatal("instant blocked checkpoint accepted")
	}
	c = compoundCase(t, "RUN-633")
	c.Scenario.Steps[0].TimeoutSeconds = 0
	if err := c.Validate(); err == nil {
		t.Fatal("unbounded transient source checkpoint accepted")
	}
}
