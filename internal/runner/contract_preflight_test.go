// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0
package runner

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentContractRejectsObsoleteCatalogueBeforeExecution(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "rejection"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"DENY-020", "DENY-035", "DENY-044", "DENY-045", "DENY-046"} {
		if err := currentContractError(cases, map[string]bool{id: true}); err == nil || !strings.Contains(err.Error(), "CATALOGUE_CONTRACT_CONFLICT") {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if err := currentContractError(cases, map[string]bool{"DENY-014": true}); err != nil {
		t.Fatal(err)
	}
}
func TestCurrentContractPairedRoundingAndLimits(t *testing.T) {
	c := normalCase(t, "RUN-061")
	s := cloneMap(c.Scenario.InitialSpec)
	s["replicas"] = float64(3)
	s["rolloutStrategy"] = map[string]interface{}{"type": "ServingGroupRollingUpdate", "rollingUpdateConfiguration": map[string]interface{}{"maxUnavailable": "25%", "maxSurge": float64(0)}}
	c.Scenario = &Scenario{InitialSpec: s}
	if got := ContractConflicts(c); len(got) != 1 || !strings.Contains(got[0], "0/0") {
		t.Fatal(got)
	}
	b := mapValue(mapValue(s, "rolloutStrategy"), "rollingUpdateConfiguration")
	b["maxSurge"] = float64(1)
	if got := ContractConflicts(c); len(got) != 0 {
		t.Fatal(got)
	}
	b["maxSurge"] = "250%"
	if got := ContractConflicts(c); len(got) != 0 {
		t.Fatal(got)
	}
	b["partition"] = float64(4)
	if got := ContractConflicts(c); len(got) != 1 || !strings.Contains(got[0], "P exceeds") {
		t.Fatal(got)
	}
	b["partition"] = float64(0)
	b["maxUnavailable"] = float64(1)
	b["maxSurge"] = float64(0)
	s["replicas"] = float64(0)
	if got := ContractConflicts(c); len(got) != 0 {
		t.Fatal(got)
	}
	b["maxUnavailable"] = float64(0)
	if got := ContractConflicts(c); len(got) != 1 {
		t.Fatal(got)
	}
}
func TestBudgetArithmeticDoesNotOverflowBeforeDivision(t *testing.T) {
	got, err := budget(map[string]interface{}{"maxSurge": "10000000000%"}, "maxSurge", 2147483647, false)
	if err != nil || int64(got) != 21474836470000000000/100 {
		t.Fatal(got, err)
	}
	if _, err := budget(map[string]interface{}{"maxSurge": "999999999999999999999999999999999%"}, "maxSurge", 2147483647, false); err == nil {
		t.Fatal("overflow accepted")
	}
}
