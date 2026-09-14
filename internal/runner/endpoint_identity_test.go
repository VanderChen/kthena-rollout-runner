// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func TestLegacyEndpointIdentitySets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		desired int
		actual  []int
		valid   bool
	}{
		{"unordered-list", 3, []int{2, 0, 1}, true},
		{"shifted", 3, []int{1, 2, 3}, true},
		{"gap", 3, []int{0, 2, 3}, true},
		{"invalid", 3, []int{-1, 0, 2}, false},
		{"duplicate", 3, []int{0, 1, 1}, false},
		{"surge-at-end", 3, []int{0, 1, 2, 3}, false},
		{"empty", 0, nil, true},
		{"phantom", 0, []int{0}, false},
		{"expanded", 5, []int{4, 3, 2, 1, 0}, true},
		{"shrunk", 2, []int{0, 1}, true},
		{"stale-size", 2, []int{0, 1, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := endpointIdentitySet("test", tc.desired, tc.actual)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}

func TestLegacyEndpointIdentityCoversAllRoles(t *testing.T) {
	for _, id := range []string{"RUN-001", "RUN-031"} {
		t.Run(id, func(t *testing.T) {
			l, pods := fixture(t, id, 2)
			if err := l.endpointIdentities(pods); err != nil {
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
				if err := l.endpointIdentities(changed); err != nil {
					t.Fatal(role, err)
				}
				// Core fixture setup still requires the catalogue
				// baseline IDs used to measure descending replacement starts.
			}
			// A foreign Pod cannot supply missing owned capacity or change its set.
			foreign := makePod("SG", "frontend", 99, "B", true, "entry")
			foreign.SetOwnerReferences([]metav1.OwnerReference{{UID: "someone-else"}})
			pods["foreign"] = foreign
			if err := l.endpointIdentities(pods); err != nil {
				t.Fatal(err)
			}
			duplicate := pods[string(makePod(l.Case.Expect.Mode, "frontend", 0, "A", true, "entry").GetUID())].DeepCopy()
			duplicate.SetUID(types.UID("duplicate-entry"))
			pods["duplicate-entry"] = duplicate
			if err := l.endpointIdentities(pods); err == nil {
				t.Fatal("duplicate entry identity accepted")
			}
		})
	}
}

func TestEndpointIdentityCheckAllowsIntermediateSurge(t *testing.T) {
	for _, id := range []string{"RUN-016", "RUN-046"} {
		l, pods := fixture(t, id, 0)
		surge := makePod(l.Case.Expect.Mode, "frontend", 3, "B", false, "entry")
		event(l, pods, "ADDED", surge)
		l.Check(pods)
		noViolation(t, l)
		if l.endpointIdentities(pods) == nil {
			t.Fatal("surge would be accepted as a final endpoint")
		}
	}
}

func TestEveryNormalCaseUsesLegacyEndpointContract(t *testing.T) {
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
		for _, step := range c.Scenario.Steps {
			for _, target := range step.Expect.Targets {
				for _, version := range target.Ordinals {
					if version != "A" && !(c.ID >= "RUN-154" && c.ID <= "RUN-159") {
						t.Fatal(c.ID, step.Name, "target endpoint still pins new ordinals")
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

func TestNormalSettledAcceptsShiftedFinalOrdinals(t *testing.T) {
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
			if ok, reason := e.settled(ScenarioExpectation{}); !ok {
				t.Fatal("valid shifted endpoint rejected", reason)
			}
		})
	}
}

func TestLegacyEndpointsDoNotExcuseTargetChurn(t *testing.T) {
	for _, id := range []string{"RUN-195", "RUN-071"} {
		for _, variant := range []string{"out-of-range", "zero-budget", "in-range", "no-replacement"} {
			t.Run(id+"/"+variant, func(t *testing.T) {
				l, objects := normalFixture(t, id)
				for _, p := range objects["pods"] {
					labels := p.GetLabels()
					if l.Model.Mode == "SG" {
						labels[LabelGroup] = fmt.Sprintf("model-%d", ordinal(labels[LabelGroup])+1)
					} else if labels[LabelRole] == "frontend" {
						labels[LabelRoleID] = fmt.Sprintf("frontend-%d", ordinal(labels[LabelRoleID])+1)
					}
					p.SetLabels(labels)
				}
				spec := cloneMap(l.Model.Spec)
				if variant == "zero-budget" {
					if l.Model.Mode == "SG" {
						mapValue(mapValue(spec, "rolloutStrategy"), "rollingUpdateConfiguration")["maxUnavailable"] = float64(0)
					} else {
						for _, raw := range listValue(mapValue(spec, "template"), "roles") {
							raw.(map[string]interface{})["maxUnavailable"] = float64(0)
						}
					}
				}
				expect := ScenarioExpectation{NoReplacement: variant == "no-replacement"}
				if err := l.Transition(spec, "normalize-after-budget-change", expect, objects); err != nil {
					t.Fatal(err)
				}
				n := 3
				if variant == "in-range" {
					n = 1
				}
				if l.Model.Mode == "SG" {
					deleteNormal(l, objects, "frontend", n, 0)
				} else {
					deleteNormal(l, objects, "frontend", 0, n)
				}
				requireNormalViolation(t, l, "PROTECTED_REPLACED")
			})
		}
	}
}

func TestLegacySettledStillRejectsQualityFailures(t *testing.T) {
	for _, id := range []string{"RUN-195", "RUN-081"} {
		for _, defect := range []string{"not-ready", "wrong-version", "foreign-member", "missing-member", "duplicate-entry", "missing-pg", "stale-status", "missing-history"} {
			t.Run(id+"/"+defect, func(t *testing.T) {
				e := settledOrdinalFixture(t, id)
				var uid string
				var pod *unstructured.Unstructured
				for uid, pod = range e.o.objects["pods"] {
					if pod.GetLabels()[LabelEntry] == "true" {
						break
					}
				}
				switch defect {
				case "not-ready":
					pod.Object["status"] = map[string]interface{}{}
				case "wrong-version":
					containers := listValue(mapValue(pod.Object, "spec"), "containers")
					containers[0].(map[string]interface{})["env"] = []interface{}{map[string]interface{}{"name": "ROLLOUT_VERSION", "value": "wrong"}}
				case "foreign-member":
					pod.SetOwnerReferences([]metav1.OwnerReference{{UID: "someone-else"}})
				case "missing-member":
					delete(e.o.objects["pods"], uid)
				case "duplicate-entry":
					p := pod.DeepCopy()
					p.SetUID("duplicate")
					e.o.objects["pods"]["duplicate"] = p
				case "missing-pg":
					e.o.objects["podgroups"] = map[string]*unstructured.Unstructured{}
				case "stale-status":
					e.current.Object["status"].(map[string]interface{})["observedGeneration"] = int64(0)
				case "missing-history":
					e.o.objects["controllerrevisions"] = map[string]*unstructured.Unstructured{}
				}
				if ok, reason := e.settled(ScenarioExpectation{}); ok {
					t.Fatal("quality failure passed", reason)
				}
			})
		}
	}
}
