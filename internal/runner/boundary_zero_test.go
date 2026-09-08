// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"path/filepath"
	"testing"
)

func TestZeroBoundaryRequiresTargetHistoryAndNoPhantomPods(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-zero"))
	if err != nil || len(cases) != 7 {
		t.Fatal(len(cases), err)
	}
	for _, c := range cases {
		initial, err := readModel(c.Scenario.InitialSpec)
		if err != nil {
			t.Fatal(err)
		}
		l, err := newNormalLedger(c.Scenario.Steps[0].Spec, "owner", "controlled")
		if err != nil {
			t.Fatal(err)
		}
		if l.Model.N > 0 && l.Model.Roles["frontend"].R != 0 {
			t.Fatal("not zero", c.ID)
		}
		if c.ID == "RUN-571" && (l.Model.U != 0 || l.Model.S != 0 || l.Model.P != 0) {
			t.Fatal("SG zero percentage", l.Model)
		}
		if c.ID == "RUN-572" {
			f := l.Model.Roles["frontend"]
			if f.U != 0 || f.S != 0 || f.P != 0 {
				t.Fatal("Role zero percentage", f)
			}
		}
		l.RevisionLayouts["A"], l.RevisionLayouts["B"] = initial, l.Model
		if activeTemplateChange(l, "A") {
			t.Fatal("inactive template requires rollout", c.ID)
		}
		if c.ID == "RUN-567" || c.ID == "RUN-569" {
			expanded, _ := readModel(c.Scenario.Steps[1].Spec)
			l.Model = expanded
			if !activeTemplateChange(l, "A") {
				t.Fatal("expanded B does not require real template", c.ID)
			}
			l.Model = l.RevisionLayouts["B"]
		}
		if l.Model.N != 0 {
			continue
		}
		current := objectForSpec("test", c.ID, l.Model.Spec)
		current.SetUID("owner")
		current.SetGeneration(2)
		current.Object["status"] = map[string]interface{}{"observedGeneration": int64(2), "replicas": int64(0), "availableReplicas": int64(0), "updatedReplicas": int64(0), "currentRevision": "A", "updateRevision": "B"}
		objects := Objects{"pods": {}, "configmaps": {}, "services": {}, "podgroups": {}, "modelservings": {"owner": current}, "controllerrevisions": {}}
		for _, version := range []string{"A", "B"} {
			cr := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "model-" + version}}}
			cr.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
			objects["controllerrevisions"][version] = cr
		}
		e := &normalExecution{l: l, current: current, o: &Observer{objects: objects}}
		if ok, reason := e.settled(ScenarioExpectation{}); !ok {
			t.Fatal(c.ID, reason)
		}
		l.RevisionLayouts["B"] = initial
		if ok, _ := e.settled(ScenarioExpectation{}); ok {
			t.Fatal("wrong target history accepted", c.ID)
		}
		l.RevisionLayouts["B"] = l.Model
		pod := normalTestPod("Role", "frontend", 0, "B", true, "entry")
		objects["pods"][string(pod.GetUID())] = pod
		if ok, _ := e.settled(ScenarioExpectation{}); ok {
			t.Fatal("zero replica phantom Pod accepted", c.ID)
		}
	}
}

func TestZeroRoleDoesNotRequirePhantomRoleButActiveRoleMustRemain(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "boundary-zero"))
	if err != nil {
		t.Fatal(err)
	}
	var c Case
	for _, candidate := range cases {
		if candidate.ID == "RUN-568" {
			c = candidate
		}
	}
	initial, _ := readModel(c.Scenario.InitialSpec)
	spec := cloneMap(c.Scenario.Steps[0].Spec)
	spec["plugins"] = []interface{}{}
	l, err := newNormalLedger(spec, "owner", "controlled")
	if err != nil {
		t.Fatal(err)
	}
	l.RevisionLayouts["fixture-A"], l.RevisionLayouts["B"] = initial, l.Model
	current := objectForSpec("test", c.ID, spec)
	current.SetUID("owner")
	current.SetGeneration(2)
	current.Object["status"] = map[string]interface{}{"observedGeneration": int64(2), "replicas": int64(1), "availableReplicas": int64(1), "updatedReplicas": int64(1), "currentRevision": "fixture-A", "updateRevision": "B"}
	objects := Objects{"pods": {}, "configmaps": {}, "services": {}, "podgroups": {}, "modelservings": {"owner": current}, "controllerrevisions": {}}
	for _, version := range []string{"fixture-A", "B"} {
		cr := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "model-" + version}}}
		cr.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
		objects["controllerrevisions"][version] = cr
	}
	for n := 0; n < 3; n++ {
		p := normalTestPod("Role", "backend", n, "A", true, "entry")
		p.SetAnnotations(map[string]string{"scheduling.k8s.io/group-name": "model-0"})
		objects["pods"][string(p.GetUID())] = p
		l.Released[string(p.GetUID())] = true
	}
	pg := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "model-0"}, "spec": map[string]interface{}{"minMember": int64(3), "minResources": map[string]interface{}{"cpu": "15m", "memory": "12Mi"}}}}
	pg.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
	objects["podgroups"]["pg"] = pg
	e := &normalExecution{l: l, current: current, o: &Observer{objects: objects}}
	if ok, why := e.settled(ScenarioExpectation{}); !ok {
		t.Fatal(why)
	}
	for uid := range objects["pods"] {
		delete(objects["pods"], uid)
		break
	}
	if ok, _ := e.settled(ScenarioExpectation{}); ok {
		t.Fatal("missing active backend accepted")
	}
}
