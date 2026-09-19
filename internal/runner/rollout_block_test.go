// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestRolloutBlockingCatalogue(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "rollout-blocking"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 6 {
		t.Fatalf("got %d blocking cases, want 6", len(cases))
	}
	for i, c := range cases {
		if c.ID != fmt.Sprintf("RUN-%03d", 612+i) || c.Format != "rollout-runner/v2" {
			t.Fatalf("unexpected supplemental identity: %s %s", c.ID, c.Format)
		}
		steps := c.Scenario.Steps
		if steps[0].Action != "update" || steps[0].Conditions[0].Kind != "running-not-ready" || steps[len(steps)-1].Until != "settled" || steps[len(steps)-1].StableSeconds < 30 {
			t.Fatalf("%s misses the real failed readiness or final recovery gate", c.ID)
		}
		foundHold := false
		for _, step := range steps {
			foundHold = foundHold || step.Action == "hold-block" && step.Expect.PreserveHealthyOld && step.HoldSeconds >= 30 || step.Action == "observe" && step.Expect.PreserveHealthyOld && step.HoldSeconds >= 30
		}
		if !foundHold {
			t.Fatalf("%s lacks a 30-second guarded stop", c.ID)
		}
		if i >= 4 && (steps[2].Action != "drop-ready" || steps[2].ReadinessTarget == nil || steps[3].Release != "one" || steps[len(steps)-1].Action != "restore-ready-or-replaced") {
			t.Fatalf("%s misses old-A fault and B readiness release", c.ID)
		}
	}
}

func TestBlockedHealthyOldUIDDeletionIsRejected(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "rollout-blocking"))
	if err != nil {
		t.Fatal(err)
	}
	c := cases[0]
	l, err := newNormalLedger(c.Scenario.InitialSpec, "owner", "controlled")
	if err != nil {
		t.Fatal(err)
	}
	l.RevisionLayouts["fixture-A"] = l.Model
	objects := Objects{"pods": {}, "podgroups": {}, "controllerrevisions": {}}
	for g := 0; g < 4; g++ {
		p := normalTestPod("SG", "frontend", 0, "A", true, "entry")
		labels := p.GetLabels()
		labels[LabelGroup] = fmt.Sprintf("model-%d", g)
		p.SetLabels(labels)
		p.SetName(fmt.Sprintf("model-%d-frontend-0-0", g))
		p.SetUID(types.UID(fmt.Sprintf("old-%d", g)))
		objects["pods"][string(p.GetUID())] = p
	}
	if err := l.Transition(c.Scenario.Steps[0].Spec, "fault-stop", ScenarioExpectation{PreserveHealthyOld: true}, objects); err != nil {
		t.Fatal(err)
	}
	if len(l.BlockedOld) != 4 {
		t.Fatalf("guarded %d old UIDs, want 4", len(l.BlockedOld))
	}
	deleteNormal(l, objects, "frontend", 1, 0)
	requireNormalViolation(t, l, "ROLLOUT_BLOCK_VIOLATION")
}

func TestBlockedOldGuardExcludesAlreadyFaultedPod(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "rollout-blocking"))
	if err != nil {
		t.Fatal(err)
	}
	c := cases[4]
	l, err := newNormalLedger(c.Scenario.InitialSpec, "owner", "controlled")
	if err != nil {
		t.Fatal(err)
	}
	l.RevisionLayouts["fixture-A"] = l.Model
	objects := Objects{"pods": {}, "podgroups": {}, "controllerrevisions": {}}
	for g := 0; g < 3; g++ {
		p := normalTestPod("SG", "frontend", 0, "A", g != 0, "entry")
		labels := p.GetLabels()
		labels[LabelGroup] = fmt.Sprintf("model-%d", g)
		p.SetLabels(labels)
		p.SetName(fmt.Sprintf("model-%d-frontend-0-0", g))
		p.SetUID(types.UID(fmt.Sprintf("old-%d", g)))
		objects["pods"][string(p.GetUID())] = p
	}
	if err := l.Transition(c.Scenario.Steps[0].Spec, "old-A-fault", ScenarioExpectation{PreserveHealthyOld: true}, objects); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.BlockedOld["old-0"]; ok {
		t.Fatal("faulted old Pod received healthy-old protection")
	}
	if len(l.BlockedOld) != 2 {
		t.Fatalf("guarded %d healthy UIDs, want 2", len(l.BlockedOld))
	}
}
