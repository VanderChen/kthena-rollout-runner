// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"
)

func TestHistoryGCCaseCannotUseAnArbitraryPodListOrPlainWait(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "history-gc"))
	if err != nil || len(cases) != 1 || cases[0].ID != "RUN-535" {
		t.Fatal("missing actual GC case", err)
	}
	c := cases[0]
	if intValue(c.Scenario.InitialSpec, "revisionHistoryLimit", -1) != 0 {
		t.Fatal("limit zero omitted")
	}
	b, err := readModel(c.Scenario.Steps[0].Spec)
	if err != nil || b.Roles["frontend"].Entry != "B" || b.Roles["backend"].Entry != "A" {
		t.Fatal("both live histories not established", err)
	}
	r := historyGCListRule("fault", "isolated")
	if !r.CollectionOnly || r.ListLimit == nil || *r.ListLimit != 0 || r.LabelSelector != "modelserving.volcano.sh/name=model" || r.Count != 1 || r.Resource != "pods" || r.Namespace != "isolated" {
		t.Fatal("generic Pod read substituted for exact live-reference read", r)
	}
	c.Scenario.Steps[1].Action = "observe"
	if c.validateScenario() == nil {
		t.Fatal("ordinary observation substituted for real List failure")
	}
}
