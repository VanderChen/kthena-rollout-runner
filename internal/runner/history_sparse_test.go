// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestSparseHistoryCataloguePreservesActualPartition(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "history-sparse"))
	if err != nil || len(cases) != 30 {
		t.Fatal("sparse coverage", len(cases), err)
	}
	for _, c := range cases {
		model, err := readModel(c.Scenario.Steps[0].Spec)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]int{"B": 3}
		ord := map[string]string{}
		if model.Roles["frontend"].P > 0 {
			want = map[string]int{"A": 1, "B": 2}
			ord["0"] = "A"
		}
		target := historyTargets(model, "B", true).Targets[0]
		if !reflect.DeepEqual(target.Versions, want) || !reflect.DeepEqual(target.Ordinals, ord) {
			t.Fatal(c.ID, target)
		}
		c.Scenario.Fixture = ""
		if c.validateScenario() == nil {
			t.Fatal("missing sparse fixture accepted", c.ID)
		}
	}
	continuous, err := LoadCases(filepath.Join("..", "..", "cases", "history-create"))
	if err != nil {
		t.Fatal(err)
	}
	continuous[0].Scenario.Fixture = "sparse-history-A"
	if continuous[0].validateScenario() == nil {
		t.Fatal("fixture enabled outside sparse source")
	}
}

func TestSparseSourceRequiresExactReadyAIdentities(t *testing.T) {
	var pods []corev1.Pod
	ids := map[string]types.UID{}
	for role, ordinals := range map[string][]int{"frontend": {0, 3, 4}, "backend": {0, 1, 2}} {
		for _, ord := range ordinals {
			obj := normalTestPod("Role", role, ord, "A", true, "entry")
			var pod corev1.Pod
			if err := convertPod(obj, &pod); err != nil {
				t.Fatal(err)
			}
			pods = append(pods, pod)
			ids[pod.Name] = pod.UID
		}
	}
	if err := sparseHistoryPods(pods, "owner", ids); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"uid", "version", "hole"} {
		copy := make([]corev1.Pod, len(pods))
		for i := range pods {
			copy[i] = *pods[i].DeepCopy()
		}
		switch change {
		case "uid":
			copy[0].UID = "replacement"
		case "version":
			copy[0].Spec.Containers[0].Env[0].Value = "B"
		case "hole":
			copy = copy[1:]
		}
		if sparseHistoryPods(copy, "owner", ids) == nil {
			t.Fatal("accepted", change)
		}
	}
}

func TestSparseSourceHighOrdinalOrderingUsesFiniteUIDs(t *testing.T) {
	for _, source := range []bool{false, true} {
		l, objects := normalFixture(t, "RUN-143")
		// This fixture has one group and W=0; preserve actual source A0/A3/A4.
		for uid, o := range objects["pods"] {
			if o.GetLabels()[LabelRole] == "frontend" && ordinal(o.GetLabels()[LabelRoleID]) != 0 {
				delete(objects["pods"], uid)
			}
		}
		for _, n := range []int{3, 4} {
			o := normalTestPod("Role", "frontend", n, "A", true, "entry")
			objects["pods"][string(o.GetUID())] = o
			l.Released[string(o.GetUID())] = true
			if source {
				if l.SourceAboveDesiredUIDs == nil {
					l.SourceAboveDesiredUIDs = map[string]bool{}
				}
				l.SourceAboveDesiredUIDs[string(o.GetUID())] = true
			}
		}
		spec := historyVersionSpec(l.Model.Spec, "B")
		for _, raw := range listValue(mapValue(spec, "template"), "roles") {
			r := raw.(map[string]interface{})
			if textValue(r, "name") == "frontend" {
				r["replicas"] = float64(3)
			}
		}
		if err := l.Transition(spec, "B", ScenarioExpectation{}, objects); err != nil {
			t.Fatal(err)
		}
		deleteNormal(l, objects, "frontend", 0, 0)
		if source {
			requireNormalViolation(t, l, "ORDER_MISMATCH")
		} else {
			for _, v := range l.Violations {
				if strings.Contains(v, "ORDER_MISMATCH") {
					t.Fatal("new surge inherited initial identity", v)
				}
			}
		}
	}
}
