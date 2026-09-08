//go:build kind

// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// This fixture exercises the runner's Kubernetes write/409/retry path using
// one owned ConfigMap. It does not inject faults into the Kthena controller and
// does not replace rerunning the affected ModelServing scenario.
func TestScenarioUpdateKindConflict(t *testing.T) {
	kubeconfig, dir := os.Getenv("RUNNER_CONFLICT_KUBECONFIG"), os.Getenv("RUNNER_CONFLICT_ARTIFACTS")
	if kubeconfig == "" || dir == "" {
		t.Fatal("explicit RUNNER_CONFLICT_KUBECONFIG and RUNNER_CONFLICT_ARTIFACTS are required")
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	api := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("rollout-runner")
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": fmt.Sprintf("runner-write-conflict-022-%d", time.Now().UnixNano()), "namespace": "rollout-runner", "labels": map[string]interface{}{"rollout-runner/fixture": "write-conflict-022"}},
		"data":     map[string]interface{}{"value": "before"},
	}}
	old, err := api.Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	uid := old.GetUID()
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if err := api.Delete(cleanup, old.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
			t.Error(err)
			return
		}
		if _, err := api.Get(cleanup, old.GetName(), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
			t.Errorf("fixture cleanup did not complete: %v", err)
		}
	}()
	next := old.DeepCopy()
	next.Object["data"].(map[string]interface{})["value"] = "after"
	checks := 0
	write, err := updateScenario(ctx, api, old, next, dir, "kind-conflict", func(prefix string) error {
		checks++
		if checks != 1 {
			return nil
		}
		// A competing real API write makes the runner's first resourceVersion
		// stale. No controller, deployment, Pod or ModelServing is changed.
		competing := old.DeepCopy()
		competing.SetAnnotations(map[string]string{"rollout-runner/concurrent-write": "1"})
		accepted, err := api.Update(ctx, competing, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		return saveYAML(filepath.Join(dir, prefix+"-competing-server.yaml"), accepted.Object)
	})
	if err != nil || checks != 2 || write == nil || write.Prefix != "kind-conflict-write-02" {
		t.Fatalf("real conflict was not retried once: checks=%d result=%+v err=%v", checks, write, err)
	}
	bytes, err := os.ReadFile(filepath.Join(dir, "kind-conflict-write-01-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]interface{}
	if err = json.Unmarshal(bytes, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["status"].(map[string]interface{})["code"] != float64(409) {
		t.Fatal("first attempt did not receive actual HTTP409")
	}
	final, err := api.Get(ctx, old.GetName(), metav1.GetOptions{})
	if err != nil || final.GetUID() != uid || final.Object["data"].(map[string]interface{})["value"] != "after" {
		t.Fatalf("real update did not converge: %v %v", final, err)
	}
	if err = saveYAML(filepath.Join(dir, "final-server.yaml"), final.Object); err != nil {
		t.Fatal(err)
	}
	t.Logf("actual API409 then success; same UID %s; two trigger checks; fixture %s deleted on exit", uid, old.GetName())
}
