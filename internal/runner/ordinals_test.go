// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func TestCanonicalOrdinalSets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		desired int
		actual  []int
		valid   bool
	}{
		{"unordered-list", 3, []int{2, 0, 1}, true},
		{"shifted", 3, []int{1, 2, 3}, false},
		{"gap", 3, []int{0, 2, 3}, false},
		{"duplicate", 3, []int{0, 1, 1}, false},
		{"surge-at-end", 3, []int{0, 1, 2, 3}, false},
		{"empty", 0, nil, true},
		{"phantom", 0, []int{0}, false},
		{"expanded", 5, []int{4, 3, 2, 1, 0}, true},
		{"shrunk", 2, []int{0, 1}, true},
		{"stale-size", 2, []int{0, 1, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := canonicalOrdinalSet("test", tc.desired, tc.actual)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}

func TestCanonicalOrdinalsCoverGroupsAllRolesAndIdentity(t *testing.T) {
	for _, id := range []string{"RUN-001", "RUN-031"} {
		t.Run(id, func(t *testing.T) {
			l, pods := fixture(t, id, 2)
			if err := l.canonicalOrdinals(pods); err != nil {
				t.Fatal(err)
			}
			for _, role := range []string{"frontend", "backend"} {
				if role == "backend" && l.Case.Expect.Mode == "SG" {
					continue
				}
				changed := map[string]*unstructured.Unstructured{}
				for uid, pod := range pods {
					p := pod.DeepCopy()
					labels := p.GetLabels()
					if l.Case.Expect.Mode == "SG" {
						labels[LabelGroup] = fmt.Sprintf("model-%d", ordinal(labels[LabelGroup])+1)
					} else if labels[LabelRole] == role {
						labels[LabelRoleID] = fmt.Sprintf("%s-%d", role, ordinal(labels[LabelRoleID])+1)
					}
					p.SetLabels(labels)
					changed[uid] = p
				}
				if err := l.canonicalOrdinals(changed); err == nil || !strings.Contains(err.Error(), "actual=[1 2 3]") {
					t.Fatal(role, err)
				}
				if _, err := NewLedger(l.Case, l.Owner, changed, nil); err == nil {
					t.Fatal("shifted baseline accepted")
				}
			}
			// A foreign Pod cannot supply missing owned capacity or change its set.
			foreign := makePod("SG", "frontend", 99, "B", true, "entry")
			foreign.SetOwnerReferences([]metav1.OwnerReference{{UID: "someone-else"}})
			pods["foreign"] = foreign
			if err := l.canonicalOrdinals(pods); err != nil {
				t.Fatal(err)
			}
			duplicate := pods[string(makePod(l.Case.Expect.Mode, "frontend", 0, "A", true, "entry").GetUID())].DeepCopy()
			duplicate.SetUID(types.UID("duplicate-entry"))
			pods["duplicate-entry"] = duplicate
			if err := l.canonicalOrdinals(pods); err == nil {
				t.Fatal("duplicate entry identity accepted")
			}
		})
	}
}

func TestCanonicalCheckDoesNotForbidIntermediateSurge(t *testing.T) {
	for _, id := range []string{"RUN-016", "RUN-046"} {
		l, pods := fixture(t, id, 0)
		surge := makePod(l.Case.Expect.Mode, "frontend", 3, "B", false, "entry")
		event(l, pods, "ADDED", surge)
		l.Check(pods)
		noViolation(t, l)
		if l.canonicalOrdinals(pods) == nil {
			t.Fatal("surge would be accepted as a final endpoint")
		}
	}
}

func TestEveryNormalCaseUsesCanonicalEndpointContract(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "normal"))
	if err != nil || len(cases) != 303 {
		t.Fatal(len(cases), err)
	}
	for _, c := range cases {
		if !c.normalFlow() {
			t.Fatal("normal case omitted", c.ID)
		}
		if c.Scenario == nil {
			continue
		}
		spec := cloneMap(c.Scenario.InitialSpec)
		for _, step := range c.Scenario.Steps {
			if step.Spec != nil {
				spec = cloneMap(step.Spec)
			}
			if patch := mapValue(step.Patch, "spec"); patch != nil {
				mergeInto(spec, patch)
			}
			if step.Until != "settled" || step.Expect.NoFullPromotion || step.Expect.BlockedByBudget {
				continue
			}
			n, layouts := fixedOrdinalLayouts(spec)
			for _, target := range step.Expect.Targets {
				desired := n
				if target.Scope != "SG" {
					desired = layouts[0][target.Role]
				}
				for key := range target.Ordinals {
					ord := ordinal(key)
					if ord < 0 || ord >= desired {
						t.Fatal(c.ID, step.Name, "conflicting final ordinal", key, desired)
					}
				}
			}
		}
	}
	for _, id := range []string{"RUN-304", "RUN-604", "DENY-001"} {
		if (Case{ID: id}).normalFlow() {
			t.Fatal("non-normal fixture silently redefined", id)
		}
	}
}

// Build an otherwise settled state so a missing integration hook cannot be
// hidden by unrelated missing status, history, plugins or PodGroup resources.
func settledOrdinalFixture(t *testing.T, id string) *normalExecution {
	t.Helper()
	c := normalCase(t, id)
	l, objects := normalFixture(t, id)
	delete(l.Model.Spec, "plugins")
	current := objectForSpec("test", id, l.Model.Spec)
	current.SetUID("owner")
	current.SetGeneration(1)
	current.Object["status"] = map[string]interface{}{"observedGeneration": int64(1), "replicas": int64(l.Model.N), "availableReplicas": int64(l.Model.N), "updatedReplicas": int64(l.Model.N), "currentRevision": "fixture-A", "updateRevision": "fixture-A"}
	objects["modelservings"] = map[string]*unstructured.Unstructured{"owner": current}
	cr := &unstructured.Unstructured{}
	cr.SetName("model-fixture-A")
	cr.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
	objects["controllerrevisions"]["revision"] = cr
	total := 0
	for _, r := range l.Model.Roles {
		total += r.R * (1 + r.W)
	}
	for g := 0; g < l.Model.N; g++ {
		name := fmt.Sprintf("model-%d", g)
		pg := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": name}, "spec": map[string]interface{}{"minMember": int64(total), "minResources": map[string]interface{}{"cpu": fmt.Sprintf("%dm", 5*total), "memory": fmt.Sprintf("%dMi", 4*total)}}}}
		pg.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
		objects["podgroups"][name] = pg
	}
	for _, p := range objects["pods"] {
		p.SetAnnotations(map[string]string{"scheduling.k8s.io/group-name": p.GetLabels()[LabelGroup]})
	}
	e := &normalExecution{c: c, l: l, current: current, o: &Observer{objects: objects}}
	if ok, reason := e.settled(ScenarioExpectation{}); !ok {
		t.Fatal("invalid settled test fixture", id, reason)
	}
	return e
}

func TestNormalSettledRejectsShiftedFinalOrdinals(t *testing.T) {
	for _, id := range []string{"RUN-195", "RUN-081"} {
		t.Run(id, func(t *testing.T) {
			e := settledOrdinalFixture(t, id)
			for _, p := range e.o.objects["pods"] {
				labels := p.GetLabels()
				if e.l.Model.Mode == "SG" {
					labels[LabelGroup] = fmt.Sprintf("model-%d", ordinal(labels[LabelGroup])+1)
					p.SetAnnotations(map[string]string{"scheduling.k8s.io/group-name": labels[LabelGroup]})
				} else if labels[LabelRole] == "frontend" {
					labels[LabelRoleID] = fmt.Sprintf("frontend-%d", ordinal(labels[LabelRoleID])+1)
				}
				p.SetLabels(labels)
			}
			if e.l.Model.Mode == "SG" {
				for _, pg := range e.o.objects["podgroups"] {
					pg.SetName(fmt.Sprintf("model-%d", ordinal(pg.GetName())+1))
				}
			}
			if ok, reason := e.settled(ScenarioExpectation{}); ok || !strings.Contains(reason, "FINAL_ORDINAL_MISMATCH") {
				t.Fatal("noncanonical endpoint passed or unrelated failure", ok, reason)
			}
		})
	}
}
