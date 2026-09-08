// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"reflect"
	"testing"

	"kthena.local/rollout-runner/internal/faultproxy"
)

func TestOldReplayRequestUsesCapturedOrderAndUniqueActualStream(t *testing.T) {
	scope := PodFaultScope{OwnerUID: "owner", Namespace: "test", RecoveryUIDs: map[string]string{"entry": "old-entry", "worker": "old-worker"}}
	rule := faultproxy.RuleStatus{Rule: faultproxy.Rule{ID: "r", Mode: "replay-deletion", Namespace: "test", OwnerUID: "owner", UIDs: []string{"entry", "worker"}}, Active: true, Hits: 2, Captured: map[string]string{"worker": "worker-hash", "entry": "entry-hash"}, CaptureOrder: []string{"worker", "entry"}}
	state := faultproxy.State{Rules: []faultproxy.RuleStatus{rule}, ReplayStreams: []faultproxy.ReplayStreamState{{Request: 12, RuleID: "r", Forwarded: rule.Captured}}}
	r, ok := oldReplayRequest(state, "r", scope)
	if !ok || !reflect.DeepEqual(r.Order, []string{"entry", "worker", "entry", "worker"}) {
		t.Fatal("did not reverse actual capture order", r)
	}
	state.ReplayStreams = append(state.ReplayStreams, faultproxy.ReplayStreamState{Request: 13, RuleID: "r", Forwarded: rule.Captured})
	if _, ok = oldReplayRequest(state, "r", scope); ok {
		t.Fatal("ambiguous controller stream accepted")
	}
	state.ReplayStreams = state.ReplayStreams[:1]
	scope.RecoveryUIDs = map[string]string{"new-entry": "old-entry", "worker": "old-worker"}
	if _, ok = oldReplayRequest(state, "r", scope); ok {
		t.Fatal("same-name new UID accepted as old replay scope")
	}
}

func TestDeletionReplayCasesPreserveProtectedWorkersAndRequireRealReplay(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "deletion-replay"))
	if err != nil || len(cases) != 2 || cases[0].ID != "RUN-449" || cases[1].ID != "RUN-452" {
		t.Fatal("replay coverage", err)
	}
	for _, c := range cases {
		model, err := readModel(c.Scenario.InitialSpec)
		if err != nil || model.Roles["frontend"].W != 1 {
			t.Fatal("protected source worker count changed", err)
		}
		if model.Mode == "SG" && model.P != 1 || model.Mode == "Role" && model.Roles["frontend"].P != 1 {
			t.Fatal("protected ordinal0 not retained")
		}
		c.Scenario.Steps[0].Action = "recover-pod"
		if c.validateScenario() == nil {
			t.Fatal("ordinary single Pod deletion cannot substitute for old event replay")
		}
	}
}
