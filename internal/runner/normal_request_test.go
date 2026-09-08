// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
)

func writeFixture() (*unstructured.Unstructured, *unstructured.Unstructured) {
	old := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "workload.serving.volcano.sh/v1alpha1", "kind": "ModelServing",
		"metadata": map[string]interface{}{"name": "model", "namespace": "test", "uid": "original", "resourceVersion": "20"},
		"spec":     map[string]interface{}{"replicas": int64(6)},
	}}
	next := old.DeepCopy()
	next.Object["spec"].(map[string]interface{})["replicas"] = int64(8)
	return old, next
}

func TestScenarioUpdateConflictRefreshesVersionAndTrigger(t *testing.T) {
	old, next := writeFixture()
	client := fakeClient()
	latest := old.DeepCopy()
	latest.SetResourceVersion("21")
	latest.Object["status"] = map[string]interface{}{"observedGeneration": int64(2)}
	client.PrependReactor("get", MSGVR.Resource, func(clienttesting.Action) (bool, runtime.Object, error) { return true, latest, nil })
	calls := 0
	client.PrependReactor("update", MSGVR.Resource, func(a clienttesting.Action) (bool, runtime.Object, error) {
		calls++
		o := a.(clienttesting.UpdateAction).GetObject().(*unstructured.Unstructured)
		want := "20"
		if calls == 2 {
			want = "21"
		}
		if o.GetResourceVersion() != want || o.Object["spec"].(map[string]interface{})["replicas"] != int64(8) {
			t.Fatalf("wrong request on attempt %d: %v", calls, o.Object)
		}
		if calls == 1 {
			return true, nil, apierrors.NewConflict(MSGVR.GroupResource(), "model", errors.New("status advanced"))
		}
		accepted := o.DeepCopy()
		accepted.SetResourceVersion("22")
		accepted.SetGeneration(3)
		return true, accepted, nil
	})
	dir := t.TempDir()
	var proofs []string
	write, err := updateScenario(context.Background(), client.Resource(MSGVR).Namespace("test"), old, next, dir, "step-02", func(prefix string) error {
		proofs = append(proofs, prefix)
		if calls != len(proofs)-1 {
			t.Fatal("trigger must precede each actual write")
		}
		return nil
	})
	if err != nil || calls != 2 || len(proofs) != 2 || write.Prefix != "step-02-write-02" || write.Request.GetResourceVersion() != "21" || write.Object.GetResourceVersion() != "22" {
		t.Fatalf("missing refreshed request/trigger: write=%+v err=%v calls=%d proofs=%v", write, err, calls, proofs)
	}
	if next.GetResourceVersion() != "20" || old.Object["spec"].(map[string]interface{})["replicas"] != int64(6) {
		t.Fatal("caller request or prior spec was mutated")
	}
	for i := 1; i <= 2; i++ {
		b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("step-02-write-%02d-receipt.json", i)))
		if err != nil {
			t.Fatal(err)
		}
		var receipt map[string]interface{}
		if err = json.Unmarshal(b, &receipt); err != nil {
			t.Fatal(err)
		}
		if i == 1 && receipt["status"].(map[string]interface{})["code"] != float64(409) {
			t.Fatal("original conflict was not retained")
		}
		if i == 2 && (receipt["error"] != nil || receipt["resourceVersion"] != "22") {
			t.Fatal("successful receipt does not match accepted request")
		}
	}
}

func TestScenarioUpdateDoesNotOverwriteChangedObject(t *testing.T) {
	for _, change := range []string{"uid", "spec"} {
		t.Run(change, func(t *testing.T) {
			old, next := writeFixture()
			client := fakeClient()
			latest := old.DeepCopy()
			latest.SetResourceVersion("21")
			if change == "uid" {
				latest.SetUID("recreated")
			} else {
				latest.Object["spec"].(map[string]interface{})["replicas"] = int64(9)
			}
			client.PrependReactor("get", MSGVR.Resource, func(clienttesting.Action) (bool, runtime.Object, error) { return true, latest, nil })
			writes := 0
			client.PrependReactor("update", MSGVR.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
				writes++
				return true, nil, apierrors.NewConflict(MSGVR.GroupResource(), "model", errors.New("changed"))
			})
			_, err := updateScenario(context.Background(), client.Resource(MSGVR).Namespace("test"), old, next, t.TempDir(), "step-02", nil)
			if err == nil || !strings.Contains(err.Error(), "RUNNER_WRITE_PRECONDITION") || writes != 1 {
				t.Fatalf("changed object overwritten: writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestScenarioUpdateRechecksLostTrigger(t *testing.T) {
	old, next := writeFixture()
	client := fakeClient()
	client.PrependReactor("get", MSGVR.Resource, func(clienttesting.Action) (bool, runtime.Object, error) { return true, old.DeepCopy(), nil })
	writes, checks := 0, 0
	client.PrependReactor("update", MSGVR.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		writes++
		return true, nil, apierrors.NewConflict(MSGVR.GroupResource(), "model", errors.New("status changed"))
	})
	_, err := updateScenario(context.Background(), client.Resource(MSGVR).Namespace("test"), old, next, t.TempDir(), "step-02", func(string) error {
		checks++
		if checks == 2 {
			return errors.New("TRIGGER_MISSED: original rollout already finished")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "TRIGGER_MISSED") || checks != 2 || writes != 1 {
		t.Fatalf("write occurred after trigger disappeared: checks=%d writes=%d err=%v", checks, writes, err)
	}
}

func TestScenarioUpdateRetryBoundAndNonConflict(t *testing.T) {
	for _, conflict := range []bool{true, false} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			old, next := writeFixture()
			client := fakeClient()
			client.PrependReactor("get", MSGVR.Resource, func(clienttesting.Action) (bool, runtime.Object, error) { return true, old.DeepCopy(), nil })
			writes := 0
			client.PrependReactor("update", MSGVR.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
				writes++
				if conflict {
					return true, nil, apierrors.NewConflict(MSGVR.GroupResource(), "model", errors.New("busy"))
				}
				return true, nil, apierrors.NewForbidden(MSGVR.GroupResource(), "model", errors.New("denied"))
			})
			_, err := updateScenario(context.Background(), client.Resource(MSGVR).Namespace("test"), old, next, t.TempDir(), "step-02", nil)
			if conflict {
				if writes != 5 || !apierrors.IsConflict(err) {
					t.Fatalf("conflict retry was unbounded or lost error: %d %v", writes, err)
				}
			} else if writes != 1 || !apierrors.IsForbidden(err) {
				t.Fatalf("non-conflict error was retried: %d %v", writes, err)
			}
		})
	}
}
