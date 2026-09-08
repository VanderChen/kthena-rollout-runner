// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func TestSparseBoundaryOrdinalProtectionAndBudgetTraps(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-sparse"))
	if err != nil || len(cases) != 30 {
		t.Fatal(len(cases), err)
	}
	for _, c := range cases {
		p := c.Scenario.Steps[0]
		switch c.ID {
		case "RUN-576", "RUN-585", "RUN-597", "RUN-603":
			if p.Expect.Targets[0].Versions["A"] != 1 || p.Expect.Targets[0].Versions["B"] != 2 || p.Expect.Targets[0].Ordinals["0"] != "A" {
				t.Fatal("partition protected a count instead of original ordinals", c.ID)
			}
		case "RUN-592", "RUN-598":
			if !p.Expect.BlockedByBudget || !p.Expect.NoReplacement || p.Expect.Targets[0].Versions["A"] != 3 {
				t.Fatal("zero budget pretends to complete", c.ID)
			}
			c.Scenario.Steps[0].Expect.BlockedByBudget = false
			if c.Validate() == nil {
				t.Fatal("trap completion oracle omitted")
			}
		case "RUN-593", "RUN-599":
			if !p.Expect.NoReplacement || p.Expect.Targets[0].Versions["A"] != 3 {
				t.Fatal("P5 failed to protect actual sparse source", c.ID)
			}
		}
		c.Scenario.Fixture = ""
		if c.Validate() == nil {
			t.Fatal("sparse actual source omitted", c.ID)
		}
	}
}

func TestSparseServingGroupRequiresOriginal034AndNoExtraPods(t *testing.T) {
	var pods []corev1.Pod
	ids := map[string]types.UID{}
	for _, ord := range []int{0, 3, 4} {
		object := normalTestPod("SG", "frontend", ord, "A", true, "entry")
		var pod corev1.Pod
		if err := convertPod(object, &pod); err != nil {
			t.Fatal(err)
		}
		pods = append(pods, pod)
		ids[pod.Name] = pod.UID
	}
	if err := sparseSourcePods(pods, "owner", ids, true); err != nil {
		t.Fatal(err)
	}
	if sparseSourcePods(pods, "owner", ids, false) == nil {
		t.Fatal("SG accepted as Role source")
	}
	for _, change := range []string{"uid", "missing", "extra", "not-ready"} {
		copy := make([]corev1.Pod, len(pods))
		for i := range pods {
			copy[i] = *pods[i].DeepCopy()
		}
		switch change {
		case "uid":
			copy[1].UID = "different"
		case "missing":
			copy = copy[:2]
		case "extra":
			copy = append(copy, *pods[0].DeepCopy())
		case "not-ready":
			copy[2].Status.Conditions = nil
		}
		if sparseSourcePods(copy, "owner", ids, true) == nil {
			t.Fatal(change, "source validation escaped")
		}
	}
}

func TestSparseZeroBudgetRequiresIncompleteStatusWithRealEligibleOldUnits(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-sparse"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.ID != "RUN-592" && c.ID != "RUN-598" {
			continue
		}
		initial := cloneMap(c.Scenario.InitialSpec)
		initial["plugins"] = []interface{}{}
		target := cloneMap(c.Scenario.Steps[0].Spec)
		target["plugins"] = []interface{}{}
		l, err := newNormalLedger(initial, "owner", "controlled")
		if err != nil {
			t.Fatal(err)
		}
		objects := Objects{"pods": {}, "configmaps": {}, "services": {}, "podgroups": {}, "modelservings": {}, "controllerrevisions": {}}
		sg := l.Model.Mode == "SG"
		for role, ordinals := range map[string][]int{"frontend": {0, 3, 4}, "backend": {0, 1, 2}} {
			if sg && role == "backend" {
				continue
			}
			for _, ord := range ordinals {
				o := normalTestPod(l.Model.Mode, role, ord, "A", true, "entry")
				group := o.GetLabels()[LabelGroup]
				o.SetAnnotations(map[string]string{"scheduling.k8s.io/group-name": group})
				objects["pods"][string(o.GetUID())] = o
				l.Released[string(o.GetUID())] = true
				count, cpu, memory := int64(6), "30m", "24Mi"
				if sg {
					count, cpu, memory = 1, "5m", "4Mi"
				}
				pg := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": group}, "spec": map[string]interface{}{"minMember": count, "minResources": map[string]interface{}{"cpu": cpu, "memory": memory}}}}
				pg.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
				objects["podgroups"][group] = pg
			}
		}
		a := l.Model
		if err = l.Transition(target, "blocked", c.Scenario.Steps[0].Expect, objects); err != nil {
			t.Fatal(err)
		}
		l.RevisionLayouts["fixture-A"], l.RevisionLayouts["fixture-B"] = a, l.Model
		for _, revision := range []string{"fixture-A", "fixture-B"} {
			cr := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "model-" + revision}}}
			cr.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
			objects["controllerrevisions"][revision] = cr
		}
		current := objectForSpec("test", c.ID, target)
		current.SetUID("owner")
		current.SetGeneration(2)
		status := map[string]interface{}{"observedGeneration": int64(2), "replicas": int64(l.Model.N), "availableReplicas": int64(l.Model.N), "updatedReplicas": int64(0), "currentRevision": "fixture-A", "updateRevision": "fixture-B", "conditions": []interface{}{map[string]interface{}{"type": "UpdateInProgress", "status": "True"}}}
		current.Object["status"] = status
		objects["modelservings"]["owner"] = current
		e := &normalExecution{l: l, current: current, o: &Observer{objects: objects}}
		if ok, why := e.settled(c.Scenario.Steps[0].Expect); !ok {
			t.Fatal(c.ID, why)
		}
		status["currentRevision"] = "fixture-B"
		if ok, _ := e.settled(c.Scenario.Steps[0].Expect); ok {
			t.Fatal(c.ID, "false full promotion accepted")
		}
		status["currentRevision"] = "fixture-A"
		status["conditions"] = []interface{}{}
		if ok, _ := e.settled(c.Scenario.Steps[0].Expect); ok {
			t.Fatal(c.ID, "missing in-progress status accepted")
		}
	}
}
