// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"crypto/sha256"
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"
)

func TestAppliedMembersConfigMapIsNarrowlyRecognized(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": fmt.Sprintf("modelserving-members-%x", sha256.Sum256([]byte("ns/model/owner"))), "namespace": "ns",
			"labels":          map[string]interface{}{"modelserving.volcano.sh/name": "model"},
			"ownerReferences": []interface{}{map[string]interface{}{"apiVersion": "workload.serving.volcano.sh/v1alpha1", "kind": "ModelServing", "name": "model", "uid": "owner", "controller": true}},
		},
		"data": map[string]interface{}{"targets.json": `{"model-0":{"frontend":1}}`, "revisions.json": `{"model-0":"rev"}`},
	}}
	if !validGroupMembersState(cm, "owner") {
		t.Fatal("valid internal state rejected")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*unstructured.Unstructured)
	}{
		{"foreign-owner", func(o *unstructured.Unstructured) { o.SetOwnerReferences(nil) }},
		{"different-name", func(o *unstructured.Unstructured) { o.SetName("unrelated-configmap") }},
		{"missing-history", func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, `{}`, "data", "revisions.json")
		}},
		{"negative-count", func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, `{"model-0":{"frontend":-1}}`, "data", "targets.json")
		}},
		{"extra-data", func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, "unexpected", "data", "other")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := cm.DeepCopy()
			tc.mutate(bad)
			if validGroupMembersState(bad, "owner") {
				t.Fatal("unexpected object accepted")
			}
		})
	}
}
