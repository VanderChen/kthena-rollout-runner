// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func normalCase(t *testing.T, id string) Case {
	t.Helper()
	cs, err := LoadCases(filepath.Join("..", "..", "cases", "normal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	t.Fatal(id)
	return Case{}
}
func normalFixture(t *testing.T, id string) (*NormalLedger, Objects) {
	t.Helper()
	c := normalCase(t, id)
	l, err := newNormalLedger(c.Scenario.InitialSpec, "owner", "controlled")
	if err != nil {
		t.Fatal(err)
	}
	l.RevisionLayouts["fixture-A"] = l.Model
	o := Objects{"pods": {}, "podgroups": {}, "controllerrevisions": {}}
	for g := 0; g < l.Model.N; g++ {
		for name, r := range l.Model.Roles {
			for n := 0; n < r.R; n++ {
				for w := 0; w <= r.W; w++ {
					member, v := "entry", r.Entry
					if w > 0 {
						member = fmt.Sprintf("worker-%d", w)
						v = r.Worker
					}
					p := normalTestPod("Role", name, n, v, true, member)
					labels := p.GetLabels()
					labels[LabelGroup] = fmt.Sprintf("model-%d", g)
					p.SetLabels(labels)
					p.SetName(fmt.Sprintf("%d-%s", g, p.GetName()))
					p.SetUID(types.UID(p.GetName()))
					o["pods"][string(p.GetUID())] = p
					l.Released[string(p.GetUID())] = true
				}
			}
		}
	}
	return l, o
}
func deleteNormal(l *NormalLedger, o Objects, role string, group, n int) {
	for uid, p := range o["pods"] {
		if p.GetLabels()[LabelRole] == role && ordinal(p.GetLabels()[LabelGroup]) == group && ordinal(p.GetLabels()[LabelRoleID]) == n {
			u := p.DeepCopy()
			now := metav1.Now()
			u.SetDeletionTimestamp(&now)
			l.Before("pods", "MODIFIED", u, o)
			o["pods"][uid] = u
			l.After("pods", "MODIFIED", u, o)
		}
	}
}
func requireNormalViolation(t *testing.T, l *NormalLedger, code string) {
	t.Helper()
	if l.error() == nil || !strings.Contains(l.error().Error(), code) {
		t.Fatalf("missing %s: %v", code, l.Violations)
	}
}
func TestNormalCatalogueIsExecutable(t *testing.T) {
	cs, err := LoadCases(filepath.Join("..", "..", "cases", "normal"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 303 {
		t.Fatalf("incomplete first category: %d", len(cs))
	}
	for i, c := range cs {
		if c.ID != fmt.Sprintf("RUN-%03d", i+1) {
			t.Fatalf("missing/duplicate ID at %d", i)
		}
		if i < 60 {
			a, err := os.ReadFile(filepath.Join("..", "..", "cases", "core", c.ID+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join("..", "..", "cases", "normal", c.ID+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) {
				t.Fatal("original core input altered: " + c.ID)
			}
		}
		if c.Scenario != nil {
			for _, s := range c.Scenario.Steps {
				if s.Until == "settled" && s.StableSeconds < 1 {
					t.Fatalf("no final observation: %s", c.ID)
				}
			}
		}
	}
}
func TestNormalBudgetRetainsDeletedGap(t *testing.T) {
	l, o := normalFixture(t, "RUN-062")
	s := normalCase(t, "RUN-062").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "B", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, o, "frontend", 2, 0)
	if l.error() != nil {
		t.Fatal(l.error())
	}
	for uid, p := range o["pods"] {
		if p.GetDeletionTimestamp() != nil {
			delete(o["pods"], uid)
		}
	}
	deleteNormal(l, o, "frontend", 1, 0)
	requireNormalViolation(t, l, "BUDGET_VIOLATION")
}
func TestNormalIndependentRoleBudgets(t *testing.T) {
	l, o := normalFixture(t, "RUN-077")
	s := normalCase(t, "RUN-077").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "B", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, o, "frontend", 0, 2)
	deleteNormal(l, o, "backend", 0, 2)
	if l.error() != nil {
		t.Fatal(l.error())
	}
	deleteNormal(l, o, "frontend", 0, 1)
	requireNormalViolation(t, l, "BUDGET_VIOLATION")
}
func TestNormalScaleUpDoesNotGrantDeleteCredit(t *testing.T) {
	l, o := normalFixture(t, "RUN-062")
	s := normalCase(t, "RUN-062").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "B", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, o, "frontend", 2, 0)
	spec := cloneMap(s.Spec)
	spec["replicas"] = float64(5)
	if err := l.Transition(spec, "expand", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	if len(l.Committed) != 1 {
		t.Fatal("lost old deletion commitment")
	}
	deleteNormal(l, o, "frontend", 1, 0)
	requireNormalViolation(t, l, "BUDGET_VIOLATION")
}
func TestNormalScaleDownIsBounded(t *testing.T) {
	l, o := normalFixture(t, "RUN-062")
	spec := cloneMap(l.Model.Spec)
	spec["replicas"] = float64(2)
	if err := l.Transition(spec, "shrink", ScenarioExpectation{NoReplacement: true}, o); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, o, "frontend", 2, 0)
	if l.error() != nil {
		t.Fatal(l.error())
	}
	deleteNormal(l, o, "frontend", 1, 0)
	requireNormalViolation(t, l, "PROTECTED_REPLACED")
}
func TestNormalTargetReadyWithoutReleaseFails(t *testing.T) {
	l, o := normalFixture(t, "RUN-062")
	p := normalTestPod("SG", "frontend", 3, "B", true, "entry")
	l.After("pods", "ADDED", p, o)
	requireNormalViolation(t, l, "CONTROL_VIOLATION")
}
func TestNormalPercentageRounding(t *testing.T) {
	for _, tc := range []struct {
		k    string
		sg   bool
		want int
	}{{"maxUnavailable", true, 1}, {"maxUnavailable", false, 0}, {"maxSurge", true, 1}, {"partition", false, 1}} {
		got, err := budget(map[string]interface{}{tc.k: "20%"}, tc.k, 3, tc.sg)
		if err != nil || got != tc.want {
			t.Fatalf("%+v got %d %v", tc, got, err)
		}
	}
}
func TestNormalMissingWorkerDoesNotGrantCredit(t *testing.T) {
	l, o := normalFixture(t, "RUN-126")
	spec := normalCase(t, "RUN-126").Scenario.Steps[0].Spec
	if err := l.Transition(spec, "workers", ScenarioExpectation{}, o); err != nil {
		t.Fatal(err)
	}
	for _, u := range l.units(o["pods"]) {
		if !u.Ready {
			t.Fatal("historical W=0 baseline became incomplete")
		}
	}
	p := normalTestPod("SG", "frontend", 3, "A", true, "entry")
	o["pods"][string(p.GetUID())] = p
	l.Released[string(p.GetUID())] = true
	// An A entry alone has a valid historical W=0 shape. It must not satisfy
	// the target layout that requires a B worker, even though entry is Ready.
	u := l.units(o["pods"])["model-3"]
	if l.unitTarget(u) {
		t.Fatal("entry-only layout counted as completed target")
	}
}
func TestNormalUnknownStepRejected(t *testing.T) {
	c := normalCase(t, "RUN-062")
	c.Scenario.Steps[0].Action = "skip-unimplemented"
	if c.Validate() == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestNormalSourceRowsPreserved(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "cases", "normal", "suite.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue struct {
		Cases []map[string]interface{} `json:"cases"`
	}
	if err = json.Unmarshal(data, &catalogue); err != nil {
		t.Fatal(err)
	}
	rows := map[string][]byte{}
	for _, r := range catalogue.Cases {
		b, _ := json.Marshal(r)
		rows[textValue(r, "id")] = b
	}
	cs, err := LoadCases(filepath.Join("..", "..", "cases", "normal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Scenario != nil {
			b, _ := json.Marshal(c.Scenario.Source)
			if !bytes.Equal(b, rows[c.ID]) {
				t.Fatal("source row altered: " + c.ID)
			}
		}
	}
}

func TestNormalRevisionIdentifiesMissingNewWorkers(t *testing.T) {
	l, o := normalFixture(t, "RUN-130")
	s := normalCase(t, "RUN-130").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "new-worker-layout", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	l.RevisionLayouts["new"] = l.Model
	p := normalTestPod("Role", "frontend", 3, "A", true, "entry")
	labels := p.GetLabels()
	labels["modelserving.volcano.sh/revision"] = "new"
	p.SetLabels(labels)
	o["pods"][string(p.GetUID())] = p
	for _, u := range l.roleUnits(o["pods"]) {
		if u.Role == "frontend" && u.Ordinal == 3 && (u.Ready || u.Complete) {
			t.Fatal("new revision entry incorrectly matched historical W=0")
		}
	}
}
func TestNormalPodGroupDeletionCannotBypassBudget(t *testing.T) {
	l, o := normalFixture(t, "RUN-062")
	s := normalCase(t, "RUN-062").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "B", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	for _, g := range []int{2, 1} {
		pg := &unstructured.Unstructured{}
		pg.SetName(fmt.Sprintf("model-%d", g))
		pg.SetUID(types.UID(fmt.Sprintf("pg-%d", g)))
		pg.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
		l.Before("podgroups", "DELETED", pg, o)
	}
	requireNormalViolation(t, l, "BUDGET_VIOLATION")
}
func TestNormalCommittedDeletionSurvivesPartitionRaise(t *testing.T) {
	l, o := normalFixture(t, "RUN-219")
	s := normalCase(t, "RUN-219").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "B", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, o, "frontend", 2, 0)
	next := normalCase(t, "RUN-219").Scenario.Steps[1]
	if err := l.Transition(next.Spec, "expand", next.Expect, o); err != nil {
		t.Fatal(err)
	}
	for uid := range l.Committed {
		if _, ok := l.Protected[uid]; ok {
			t.Fatal("new partition attempted to revoke deletion")
		}
	}
	deleteNormal(l, o, "frontend", 1, 0)
	requireNormalViolation(t, l, "PROTECTED_REPLACED")
}
func TestNormalDependencyRequiresStableReady(t *testing.T) {
	l, o := normalFixture(t, "RUN-298")
	s := normalCase(t, "RUN-298").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "B", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	p := normalTestPod("Role", "frontend", 3, "B", false, "entry")
	l.Before("pods", "ADDED", p, o)
	requireNormalViolation(t, l, "DEPENDENCY_VIOLATION")
}
func TestNormalExactTargetOrdinalsCannotBeSubstituted(t *testing.T) {
	target := ScenarioTarget{Versions: map[string]int{"B": 2}, Ordinals: map[string]string{"0": "B", "1": "B"}}
	if ok, _ := targetFacts([]NormalUnit{{Ordinal: 0, Version: "B"}, {Ordinal: 2, Version: "B"}}, target); ok {
		t.Fatal("sparse target passed exact ordinal requirement")
	}
}

func TestNormalRoleExpansionDoesNotReclassifyHealthyServingGroup(t *testing.T) {
	l, o := normalFixture(t, "RUN-227")
	s := normalCase(t, "RUN-227").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "B", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, o, "frontend", 2, 0)
	next := normalCase(t, "RUN-227").Scenario.Steps[1]
	if err := l.Transition(next.Spec, "expand-role", next.Expect, o); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, o, "frontend", 1, 0)
	requireNormalViolation(t, l, "BUDGET_VIOLATION")
}

func TestNormalCompleteMixedRoleCapacityCanProvideReadyCredit(t *testing.T) {
	l, o := normalFixture(t, "RUN-227")
	s := normalCase(t, "RUN-227").Scenario.Steps[1]
	if err := l.Transition(s.Spec, "expand-and-roll", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	l.RevisionLayouts["fixture-B"] = l.Model
	for _, member := range []string{"entry", "worker-1"} {
		p := normalTestPod("Role", "frontend", 1, "B", true, member)
		labels := p.GetLabels()
		labels[LabelGroup] = "model-1"
		p.SetLabels(labels)
		o["pods"][string(p.GetUID())] = p
		l.Released[string(p.GetUID())] = true
	}
	u := l.units(o["pods"])["model-1"]
	if !u.Ready || !u.Complete {
		t.Fatal("complete historical A plus complete B expansion lost real Ready credit")
	}
	if l.unitTarget(u) {
		t.Fatal("mixed capacity must not count as the completed B template")
	}
}

func TestNormalPendingRenamedRolePreservesOldServingCapacity(t *testing.T) {
	l, o := normalFixture(t, "RUN-125")
	s := normalCase(t, "RUN-125").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "rename", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	addPendingExtraRoles(l, o)
	for _, u := range l.units(o["pods"]) {
		if !u.Ready || l.unitTarget(u) {
			t.Fatal("complete old membership must remain available without satisfying the renamed target")
		}
	}
	l.rememberReady(l.units(o["pods"]))
	for uid, p := range o["pods"] {
		if p.GetLabels()[LabelRole] == "extra" && l.Served[uid] {
			t.Fatal("pending extra Role was recorded as previously Ready")
		}
	}
	deleteNormal(l, o, "backend", 2, 0)
	if err := l.error(); err != nil {
		t.Fatal("first SG replacement has three healthy old cohorts: ", err)
	}
	deleteNormal(l, o, "backend", 1, 0)
	requireNormalViolation(t, l, "BUDGET_VIOLATION")
}

func TestNormalAddedRoleIsReleasedWhileOldMembershipServes(t *testing.T) {
	l, o := normalFixture(t, "RUN-123")
	s := normalCase(t, "RUN-123").Scenario.Steps[0]
	if err := l.Transition(s.Spec, "add", s.Expect, o); err != nil {
		t.Fatal(err)
	}
	addPendingExtraRoles(l, o)
	candidates := l.releaseCandidates(o, nil)
	if len(candidates) != 3 {
		t.Fatalf("pending new Roles must still be released, got %d SG candidates", len(candidates))
	}
	for _, u := range candidates {
		if !u.Complete || !u.Ready {
			t.Fatal("new complete membership should coexist with Ready old membership")
		}
	}
	deleteNormal(l, o, "frontend", 2, 0)
	if l.units(o["pods"])["model-2"].Ready {
		t.Fatal("remaining backend plus pending extra cannot replace the lost old frontend capacity")
	}
}

func addPendingExtraRoles(l *NormalLedger, o Objects) {
	l.RevisionLayouts["fixture-membership"] = l.Model
	for group := 0; group < l.Model.N; group++ {
		p := normalTestPod("Role", "extra", 0, "A", false, "entry")
		labels := p.GetLabels()
		labels[LabelGroup] = fmt.Sprintf("model-%d", group)
		labels["modelserving.volcano.sh/revision"] = "fixture-membership"
		p.SetLabels(labels)
		p.SetName(fmt.Sprintf("model-%d-extra-0-0", group))
		p.SetUID(types.UID(p.GetName()))
		o["pods"][string(p.GetUID())] = p
	}
}

func TestNormalProductionWorkerIdentityWithoutEntryMarker(t *testing.T) {
	for _, tc := range []struct {
		name   string
		marker string
		valid  bool
	}{
		{"model-3-frontend-0-1", "", true},
		{"model-3-frontend-0-1", "false", true},
		{"model-3-frontend-0-0", "", false},
		{"model-3-frontend-0-1", "invalid", false},
	} {
		t.Run(tc.name+"/"+tc.marker, func(t *testing.T) {
			l, o := normalFixture(t, "RUN-126")
			p := normalTestPod("Role", "frontend", 0, "A", false, "worker-1")
			p.SetName(tc.name)
			labels := p.GetLabels()
			delete(labels, LabelEntry)
			if tc.marker != "" {
				labels[LabelEntry] = tc.marker
			}
			p.SetLabels(labels)
			o["pods"][string(p.GetUID())] = p
			l.After("pods", "ADDED", p, o)
			if tc.valid {
				if err := l.error(); err != nil {
					t.Fatal("valid production worker identity rejected: ", err)
				}
			} else {
				requireNormalViolation(t, l, "IDENTITY_MISSING")
			}
		})
	}
}

func TestNormalAcceptedGroupScaleIntentSurvivesImmediateRestore(t *testing.T) {
	l, o := normalFixture(t, "RUN-166")
	spec := cloneMap(l.Model.Spec)
	spec["replicas"] = float64(1)
	if err := l.Transition(spec, "shrink", ScenarioExpectation{}, o); err != nil {
		t.Fatal(err)
	}
	pg := &unstructured.Unstructured{}
	pg.SetName("model-1")
	pg.SetUID("pg-1")
	pg.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
	l.Before("podgroups", "DELETED", pg, o)
	spec["replicas"] = float64(3)
	if err := l.Transition(spec, "restore", ScenarioExpectation{}, o); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, o, "frontend", 1, 0)
	if l.error() != nil {
		t.Fatal("already accepted scale deletion was revoked: ", l.error())
	}
	for uid, p := range o["pods"] {
		if p.GetDeletionTimestamp() != nil {
			delete(o["pods"], uid)
		}
	}
	p := normalTestPod("SG", "frontend", 1, "A", true, "entry")
	p.SetUID("replacement-uid")
	o["pods"][string(p.GetUID())] = p
	l.Released[string(p.GetUID())] = true
	if l.PGScale[string(p.GetUID())] {
		t.Fatal("old group intent leaked onto new UID")
	}
	deleteNormal(l, o, "frontend", 1, 0)
	requireNormalViolation(t, l, "UNEXPECTED_TARGET_REPLACED")
}

// Use the real API entry label. The original core test helper predates member
// layout checking and uses descriptive labels that are not production values.
func normalTestPod(mode, role string, n int, version string, ready bool, member string) *unstructured.Unstructured {
	p := makePod(mode, role, n, version, ready, member)
	labels := p.GetLabels()
	labels[LabelEntry] = "false"
	labels["modelserving.volcano.sh/revision"] = "fixture-" + version
	if member == "entry" {
		labels[LabelEntry] = "true"
	}
	p.SetLabels(labels)
	return p
}
