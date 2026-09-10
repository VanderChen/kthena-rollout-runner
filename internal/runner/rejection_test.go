// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestRejectionCatalogueLiteralRequestsAndContinuation(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "rejection"))
	if err != nil || len(cases) != 97 {
		t.Fatal(len(cases), err)
	}
	for _, c := range cases {
		if _, err = readModel(c.Scenario.InitialSpec); err != nil {
			t.Fatal(c.ID, err)
		}
		var rejection *ScenarioStep
		for i := range c.Scenario.Steps {
			if strings.HasPrefix(c.Scenario.Steps[i].Action, "reject-") {
				rejection = &c.Scenario.Steps[i]
			}
		}
		if rejection == nil {
			t.Fatal(c.ID, "missing rejection")
		}
		original := rejection.Action
		rejection.Action = "observe"
		if c.Validate() == nil {
			t.Fatal(c.ID, "accepted missing API action")
		}
		rejection.Action = original
		switch c.ID {
		case "DENY-018":
			if got := mapValue(mapValue(rejection.Spec, "rolloutStrategy"), "rollingUpdateConfiguration")["maxUnavailable"]; got != 0.5 {
				t.Fatal("float lost", got)
			}
		case "DENY-044":
			if v, ok := mapValue(rejection.Spec, "rolloutStrategy")["rollingUpdateConfiguration"]; !ok || len(v.(map[string]interface{})) != 0 {
				t.Fatal("explicit empty map lost")
			}
		case "DENY-075":
			deps := listValue(mapValue(mapValue(rejection.Spec, "rolloutStrategy"), "roleCoordination"), "dependencies")
			if len(deps) != 2 || !reflect.DeepEqual(deps[0], deps[1]) {
				t.Fatal("duplicate dependency normalized")
			}
		case "DENY-080", "DENY-087":
			for _, r := range listValue(mapValue(rejection.Spec, "template"), "roles") {
				if textValue(r.(map[string]interface{}), "name") == "frontend" {
					if _, ok := r.(map[string]interface{})["workerReplicas"]; ok {
						t.Fatal("missing required field supplied")
					}
				}
			}
		case "DENY-086", "DENY-093":
			if v, ok := mapValue(c.Scenario.InitialSpec, "template")["networkTopology"]; !ok || len(v.(map[string]interface{})) != 0 {
				t.Fatal("initial topology absent")
			}
			if v, ok := mapValue(mapValue(rejection.Patch, "spec"), "template")["networkTopology"]; !ok || v != nil {
				t.Fatal("explicit null lost")
			}
		case "DENY-096", "DENY-097":
			if len(c.Scenario.Steps) != 3 || rejection.Expect.NoReplacement || len(rejection.Conditions) == 0 || c.Scenario.Steps[2].Release != "one" {
				t.Fatal("active continuation missing")
			}
		}
	}
}

func TestAdmissionRejectDoesNotAcceptConflictTransportOrRBAC(t *testing.T) {
	resource := schema.GroupResource{Group: "workload.serving.volcano.sh", Resource: "modelservings"}
	cases := []struct {
		err  error
		want bool
	}{
		{apierrors.NewBadRequest("invalid type"), true},
		{&apierrors.StatusError{ErrStatus: metav1.Status{Code: 400, Status: metav1.StatusFailure, Message: `admission webhook "validate-modelserving" denied the request: invalid budget`}}, true},
		{&apierrors.StatusError{ErrStatus: metav1.Status{Code: 400, Message: "error calling admission webhook: connection reset"}}, false},
		{&apierrors.StatusError{ErrStatus: metav1.Status{Code: 400, Status: metav1.StatusFailure, Reason: metav1.StatusReasonTimeout, Message: `admission webhook "validate-modelserving" denied the request: timeout`}}, false},
		{&apierrors.StatusError{ErrStatus: metav1.Status{Code: 422, Reason: metav1.StatusReasonInvalid}}, true},
		{apierrors.NewForbidden(resource, "model", errors.New("RBAC cannot update")), false},
		{apierrors.NewForbidden(resource, "model", errors.New("admission webhook test denied the request")), true},
		{apierrors.NewConflict(resource, "model", errors.New("RV conflict")), false},
		{apierrors.NewInternalError(errors.New("webhook unreachable")), false},
		{errors.New("connection reset"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := admissionRejection(c.err); got != c.want {
			t.Fatal(c.err, got)
		}
	}
}

func TestRejectedUpdatePreservesRawTypesAndDoesNotCount409(t *testing.T) {
	current := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": MSGVR.Group + "/" + MSGVR.Version, "kind": "ModelServing", "metadata": map[string]interface{}{"name": "model", "namespace": "test", "uid": "owner", "resourceVersion": "10", "generation": int64(2)}, "spec": map[string]interface{}{"replicas": int64(3)}}}
	desired := current.DeepCopy()
	desired.Object["spec"] = map[string]interface{}{"replicas": 0.5, "literal": nil}
	for _, mode := range []string{"rejected", "accepted", "conflicts", "mutated"} {
		t.Run(mode, func(t *testing.T) {
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), current.DeepCopy())
			attempts := 0
			client.PrependReactor("update", "modelservings", func(action ktesting.Action) (bool, runtime.Object, error) {
				attempts++
				got := action.(ktesting.UpdateAction).GetObject().(*unstructured.Unstructured)
				if !reflect.DeepEqual(got.Object["spec"], desired.Object["spec"]) {
					t.Fatal("invalid request transformed")
				}
				if mode == "accepted" {
					return true, got, nil
				}
				if attempts == 1 || mode == "conflicts" {
					return true, nil, apierrors.NewConflict(MSGVR.GroupResource(), "model", errors.New("status raced"))
				}
				if mode == "mutated" {
					changed := current.DeepCopy()
					changed.Object["spec"].(map[string]interface{})["replicas"] = int64(4)
					if err := client.Tracker().Update(MSGVR, changed, "test"); err != nil {
						t.Fatal(err)
					}
				}
				return true, nil, apierrors.NewBadRequest("invalid number")
			})
			dir := t.TempDir()
			err := submitRejectedRequest(context.Background(), client.Resource(MSGVR).Namespace("test"), current, desired, nil, dir, "step-01", nil)
			if mode == "rejected" {
				if err != nil || attempts != 2 {
					t.Fatal(attempts, err)
				}
				raw, err := os.ReadFile(filepath.Join(dir, "step-01-rejection.json"))
				if err != nil {
					t.Fatal(err)
				}
				var proof map[string]interface{}
				if err = json.Unmarshal(raw, &proof); err != nil {
					t.Fatal(err)
				}
				if proof["attemptPrefix"] != "step-01-reject-02" {
					t.Fatal("conflict became accepted rejection")
				}
			} else {
				want := map[string]string{"accepted": "ADMISSION_UNEXPECTED_ACCEPT", "conflicts": "INCONCLUSIVE", "mutated": "REJECTION_ATOMICITY"}[mode]
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatal(mode, err)
				}
			}
		})
	}
}

func TestRejectedSpecGuardAllowsStatusButLatchesSpecMutation(t *testing.T) {
	l, objects := normalFixture(t, "RUN-143")
	spec := map[string]interface{}{"replicas": int64(3)}
	l.RejectedSpec = runtime.DeepCopyJSON(spec)
	l.RejectedGeneration = 2
	o := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "model", "uid": l.Owner, "generation": int64(2)}, "spec": spec, "status": map[string]interface{}{"availableReplicas": int64(1)}}}
	l.After("modelservings", "MODIFIED", o, objects)
	if l.error() != nil {
		t.Fatal(l.error())
	}
	o.Object["status"] = map[string]interface{}{"availableReplicas": int64(3)}
	l.After("modelservings", "MODIFIED", o, objects)
	if l.error() != nil {
		t.Fatal(l.error())
	}
	o.Object["spec"].(map[string]interface{})["replicas"] = int64(1)
	l.After("modelservings", "MODIFIED", o, objects)
	if l.error() == nil || !strings.Contains(l.error().Error(), "REJECTION_ATOMICITY") {
		t.Fatal("spec mutation escaped", l.error())
	}
}
