// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"encoding/json"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"kthena.local/rollout-runner/internal/faultproxy"
)

func TestHistoryCollisionCatalogueRequiresNativeCreatePath(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "history-collision"))
	if err != nil || len(cases) != 2 {
		t.Fatal(len(cases), err)
	}
	for _, c := range cases {
		c.Scenario.Steps[0].Action = "update"
		if c.validateScenario() == nil {
			t.Fatal("collision replaced by ordinary rollout", c.ID)
		}
	}
}

func TestNativeCollisionProofRejectsSyntheticStatusAndOtherRequests(t *testing.T) {
	held := faultproxy.Record{Action: "hold-request", RuleID: "rule", Namespace: "ns", Resource: "controllerrevisions", Method: "POST", Name: "model-b", Request: 12}
	release := faultproxy.Record{Action: "release-request", RuleID: "rule", Request: 12}
	response := faultproxy.Record{Action: "response", Status: 409, Request: 12}
	for _, mutation := range []string{"valid", "synthetic", "other-request", "wrong-namespace", "unreleased", "other-rule"} {
		records := []faultproxy.Record{held, release, response}
		switch mutation {
		case "synthetic":
			records[2].Action = "error-request"
		case "other-request":
			records[2].Request = 13
		case "wrong-namespace":
			records[0].Namespace = "other"
		case "unreleased":
			records = append(records[:1], records[2])
		case "other-rule":
			records[1].RuleID = "another"
		}
		_, ok := nativeCollisionResponse(records, "rule", "ns", "model-b")
		if ok != (mutation == "valid") {
			t.Fatal(mutation, ok)
		}
	}
}

func TestCollisionTargetRequiresActualAdmittedTemplateMatch(t *testing.T) {
	var source collisionTarget
	if err := json.Unmarshal(observedCollisionTarget, &source); err != nil {
		t.Fatal(err)
	}
	spec := map[string]interface{}{"template": map[string]interface{}{"roles": source.Data["data"]}}
	if _, err := verifyObservedCollisionTarget(spec); err != nil {
		t.Fatal(err)
	}
	role := source.Data["data"].([]interface{})[0].(map[string]interface{})
	role["workerReplicas"] = float64(1)
	if _, err := verifyObservedCollisionTarget(spec); err == nil {
		t.Fatal("different admitted template targeted by guessed hash")
	}
}

func TestNoNewHistoryGuardIgnoresForeignOwnerOnly(t *testing.T) {
	for _, owner := range []string{"owner", "foreign"} {
		l, objects := normalFixture(t, "RUN-143")
		if err := l.Transition(l.Model.Spec, "guard", ScenarioExpectation{NoNewRevision: true}, objects); err != nil {
			t.Fatal(err)
		}
		cr := &unstructured.Unstructured{Object: map[string]interface{}{"data": map[string]interface{}{"data": mapValue(l.Model.Spec, "template")["roles"]}}}
		cr.SetName("unrelated-history")
		cr.SetUID("new")
		cr.SetOwnerReferences([]metav1.OwnerReference{{UID: types.UID(owner)}})
		l.After("controllerrevisions", "ADDED", cr, objects)
		if owner == "owner" {
			requireNormalViolation(t, l, "UNEXPECTED_REVISION")
		} else if l.error() != nil {
			t.Fatal("foreign owner incorrectly counted", l.error())
		}
	}
}

func TestEquivalentHistoryCollisionCannotSkipNativeRaceOrAllowNewHistory(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "history-equal"))
	if err != nil || len(cases) != 1 || cases[0].ID != "RUN-610" {
		t.Fatal(len(cases), err)
	}
	c := cases[0]
	p := &c.Scenario.Steps[0]
	original := *p
	p.Action = "update"
	if c.Validate() == nil {
		t.Fatal("equivalent collision replaced by ordinary rollout")
	}
	*p = original
	p.Expect.NoNewRevision = false
	if c.Validate() == nil {
		t.Fatal("equivalent precreated history may be replaced")
	}
	*p = original
	p.Expect.NoReplacement = true
	if c.Validate() == nil {
		t.Fatal("B recovery prohibited by an invalid unchanged-UID guard")
	}
}
