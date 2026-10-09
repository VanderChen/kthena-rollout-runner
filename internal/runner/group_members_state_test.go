// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	// Match the current production state seen in RUN-307. Legacy two-field
	// records remain valid, while independent recovery/surge facts coexist.
	for key, raw := range map[string]string{
		"surge-groups.json": `["model-3"]`,
		"recovery.json":     `{"model-0-frontend-0-0":{"uid":"pod-uid","startedAt":"2026-10-09T06:15:30Z"}}`,
		"created.json":      `{"model-0":{"frontend-0":{"role":"frontend","groupInstance":"sg-uid","roleInstance":"role-uid","revision":"rev","hash":"hash"}}}`,
	} {
		_ = unstructured.SetNestedField(cm.Object, raw, "data", key)
		if !validGroupMembersState(cm, "owner") {
			t.Fatalf("valid shared state rejected after adding %s", key)
		}
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
		{"invalid-surge", func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, `["other-3"]`, "data", "surge-groups.json")
		}},
		{"missing-recovery-uid", func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, `{"pod":{"startedAt":"2026-10-09T06:15:30Z"}}`, "data", "recovery.json")
		}},
		{"missing-recovery-time", func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, `{"pod":{"uid":"pod-uid"}}`, "data", "recovery.json")
		}},
		{"invalid-created-json", func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, `{`, "data", "created.json")
		}},
		{"wrong-created-role", func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, `{"model-0":{"frontend-0":{"role":"backend","revision":"rev","hash":"hash"}}}`, "data", "created.json")
		}},
		{"missing-created-revision", func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, `{"model-0":{"frontend-0":{"role":"frontend","hash":"hash"}}}`, "data", "created.json")
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
