// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRetiredMembersConfigMapIsUnexpected(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": fmt.Sprintf("modelserving-members-%x", sha256.Sum256([]byte("ns/model/owner"))), "namespace": "ns",
			"ownerReferences": []interface{}{map[string]interface{}{"apiVersion": "workload.serving.volcano.sh/v1alpha1", "kind": "ModelServing", "name": "model", "uid": "owner", "controller": true}}},
		"data": map[string]interface{}{"targets.json": `{"model-0":{"frontend":1}}`},
	}}
	e := &normalExecution{l: &NormalLedger{Owner: "owner", Model: NormalModel{Spec: map[string]interface{}{}}}, o: &Observer{objects: map[string]map[string]*unstructured.Unstructured{"configmaps": {"cm": cm}}}}
	ok, reason := e.pluginFacts(nil)
	if ok || reason != "unexpected ConfigMap "+cm.GetName() {
		t.Fatalf("retired state must be rejected: %t %s", ok, reason)
	}
	delete(e.o.objects["configmaps"], "cm")
	if ok, reason := e.pluginFacts(nil); !ok {
		t.Fatalf("no-plugin workload should settle: %s", reason)
	}
}

func TestMemberStateRemovalPreservesRanktableValidation(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "model-model-0-frontend-0-ranktable", "namespace": "ns",
			"ownerReferences": []interface{}{map[string]interface{}{"uid": "owner"}}},
		"data": map[string]interface{}{"ranktable.json": `{"version":"A","status":"Completed","server_count":"1"}`},
	}}
	groups := map[int]map[string][]NormalUnit{0: {"frontend": {{Ordinal: 0}}}}
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"complete", `{"version":"A","status":"Completed","server_count":"1"}`, true},
		{"missing", "", false},
		{"wrong-count", `{"version":"A","status":"Completed","server_count":"2"}`, false},
		{"wrong-template", `{"version":"B","status":"Completed","server_count":"1"}`, false},
		{"incomplete", `{"version":"A","status":"Initializing","server_count":"1"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tables := map[string]*unstructured.Unstructured{}
			if tc.raw != "" {
				table := cm.DeepCopy()
				if err := unstructured.SetNestedField(table.Object, tc.raw, "data", "ranktable.json"); err != nil {
					t.Fatal(err)
				}
				tables[table.GetName()] = table
			}
			e := &normalExecution{tableVersion: "A",
				l: &NormalLedger{Owner: "owner", Model: NormalModel{Spec: map[string]interface{}{
					"plugins": []interface{}{map[string]interface{}{"name": "ranktable"}},
				}}},
				o: &Observer{objects: map[string]map[string]*unstructured.Unstructured{"configmaps": tables}},
			}
			if ok, reason := e.pluginFacts(groups); ok != tc.want {
				t.Fatalf("ranktable verdict=%t want=%t: %s", ok, tc.want, reason)
			}
		})
	}
}
