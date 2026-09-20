// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionPlanSurvivesPreflightFailure(t *testing.T) {
	out := t.TempDir()
	missing := filepath.Join(out, "missing-kubeconfig")
	err := Run(context.Background(), Options{CaseDir: filepath.Join("..", "..", "cases", "core"),
		OutDir: out, RunID: "plan-test", Select: "RUN-001", Kubeconfig: missing})
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("expected kubeconfig preflight failure, got %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "plan-test", "execution-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		RunID       string            `json:"runID"`
		SelectedIDs []string          `json:"selectedIDs"`
		CaseSHA256  map[string]string `json:"caseSHA256"`
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.RunID != "plan-test" || len(plan.SelectedIDs) != 1 || plan.SelectedIDs[0] != "RUN-001" || plan.CaseSHA256["RUN-001.yaml"] == "" {
		t.Fatalf("incomplete offline plan: %+v", plan)
	}
}
