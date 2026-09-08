// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHistoryObjectCatalogueRequiresExactSourceAnomaly(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "history-object"))
	if err != nil || len(cases) != 8 {
		t.Fatal(len(cases), err)
	}
	kinds := map[string]int{}
	for _, c := range cases {
		kinds[c.Scenario.Steps[1].HistoryFault]++
		c.Scenario.Steps[1].HistoryFault = "read-error"
		if c.validateScenario() == nil {
			t.Fatal("read error substituted for actual object fault", c.ID)
		}
	}
	for _, kind := range []string{"missing", "corrupt-data", "missing-role", "foreign-owner"} {
		if kinds[kind] != 2 {
			t.Fatal("missing SG/Role fixture", kind, kinds)
		}
	}
}

func TestInjectedInvalidHistoryExceptionCannotEscapeUIDAndData(t *testing.T) {
	for _, mutation := range []string{"none", "new-uid", "new-data"} {
		l, objects := normalFixture(t, "RUN-143")
		pod := normalTestPod("Role", "frontend", 0, "A", true, "entry")
		cr := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: "model-fixture-A", UID: "injected", OwnerReferences: pod.GetOwnerReferences()}, Data: runtime.RawExtension{Raw: []byte(`{"data":"intentional-invalid"}`)}}
		value, err := historyFixtureData(cr)
		if err != nil {
			t.Fatal(err)
		}
		l.HistoryFixtureData = map[string]string{"injected": value}
		object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cr)
		if err != nil {
			t.Fatal(err)
		}
		u := &unstructured.Unstructured{Object: object}
		l.After("controllerrevisions", "ADDED", u, objects)
		if l.error() != nil {
			t.Fatal("injected fixture incorrectly became product failure", l.error())
		}
		switch mutation {
		case "new-uid":
			u.SetUID("unrelated")
		case "new-data":
			u.Object["data"] = map[string]interface{}{"data": "another-invalid-value"}
		}
		l.After("controllerrevisions", "MODIFIED", u, objects)
		if mutation == "none" && l.error() != nil || mutation != "none" && l.error() == nil {
			t.Fatal(mutation, l.error())
		}
		if mutation == "new-data" && !strings.Contains(l.error().Error(), "HISTORY_MUTATED") {
			t.Fatal("immutable exception escaped", l.error())
		}
	}
}

func TestRestoreHistoricalFixtureDoesNotDeleteDifferentUID(t *testing.T) {
	original := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: "model-old", Namespace: "history-test", UID: "original", OwnerReferences: []metav1.OwnerReference{{UID: "owner"}}}, Data: runtime.RawExtension{Raw: []byte(`{"data":[]}`)}}
	injected := original.DeepCopy()
	injected.UID = "injected"
	unrelated := original.DeepCopy()
	unrelated.UID = "foreign"
	unrelated.OwnerReferences = []metav1.OwnerReference{{UID: "other"}}
	client := fake.NewSimpleClientset(unrelated)
	e := &normalExecution{r: &Runner{kube: client}, namespace: "history-test", dir: t.TempDir()}
	_, err := e.restoreHistoryObject(context.Background(), &historyObjectFault{Original: original, Injected: injected}, "restore")
	if err == nil || !strings.Contains(err.Error(), "HISTORY_UNEXPECTED_REPLACEMENT") {
		t.Fatal("different UID replaced", err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("unexpected mutation", action)
		}
	}
}

func TestMissingRoleFixturePreservesOtherRoleData(t *testing.T) {
	l, _ := normalFixture(t, "RUN-143")
	raw, err := json.Marshal(map[string]interface{}{"data": mapValue(l.Model.Spec, "template")["roles"]})
	if err != nil {
		t.Fatal(err)
	}
	cr := &appsv1.ControllerRevision{Data: runtime.RawExtension{Raw: raw}}
	data, err := invalidHistoryData(cr, "missing-role")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err = json.Unmarshal(data.Raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, role := range got.Data {
		if role["name"] == "frontend" {
			t.Fatal("frontend history still present")
		}
	}
	if string(cr.Data.Raw) != string(raw) {
		t.Fatal("original history mutated")
	}
}
