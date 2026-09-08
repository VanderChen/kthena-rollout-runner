// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"
)

func TestDependencyBoundaryRequiresPhysicalCompoundAction(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-dependency"))
	if err != nil || len(cases) != 1 || cases[0].ID != "RUN-573" {
		t.Fatal(len(cases), err)
	}
	c := cases[0]
	model, err := readModel(c.Scenario.Steps[0].Spec)
	if err != nil || model.Roles["frontend"].Entry != "B" || model.Roles["backend"].Entry != "B" {
		t.Fatal("both targets must change", err)
	}
	c.Scenario.Steps[0].Action = "update"
	if c.Validate() == nil {
		t.Fatal("ordinary rollout skipped high-only source and post-clear gate")
	}
}

func TestReadyHighSurgeCannotSubstituteForStableDependency(t *testing.T) {
	for _, stableReady := range []bool{false, true} {
		l, objects := normalFixture(t, "RUN-143")
		cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-dependency"))
		if err != nil {
			t.Fatal(err)
		}
		step := cases[0].Scenario.Steps[0]
		if err := l.Transition(step.Spec, "dependency", step.Expect, objects); err != nil {
			t.Fatal(err)
		}
		l.RevisionLayouts["fixture-B"] = l.Model
		high := normalTestPod("Role", "backend", 3, "B", true, "entry")
		objects["pods"][string(high.GetUID())] = high
		if stableReady {
			for uid, o := range objects["pods"] {
				if o.GetLabels()[LabelRole] == "backend" && ordinal(o.GetLabels()[LabelRoleID]) == 2 {
					delete(objects["pods"], uid)
				}
			}
			stable := normalTestPod("Role", "backend", 2, "B", true, "entry")
			objects["pods"][string(stable.GetUID())] = stable
		}
		front := normalTestPod("Role", "frontend", 3, "B", false, "entry")
		l.Before("pods", "ADDED", front, objects)
		if !stableReady {
			requireNormalViolation(t, l, "DEPENDENCY_VIOLATION")
		} else if l.error() != nil {
			t.Fatal("stable Ready gate did not open", l.error())
		}
	}
}
