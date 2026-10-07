// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func addRecoveryOrderSurge(l *NormalLedger, objects Objects) {
	l.RevisionLayouts["fixture-B"] = l.Model
	var additions []*unstructured.Unstructured
	for _, p := range objects["pods"] {
		if p.GetLabels()[LabelGroup] != "model-1" {
			continue
		}
		n := p.DeepCopy()
		n.SetName("surge-" + p.GetName())
		n.SetUID(types.UID(n.GetName()))
		labels := n.GetLabels()
		labels[LabelGroup] = "model-3"
		labels["modelserving.volcano.sh/revision"] = "fixture-B"
		n.SetLabels(labels)
		if labels[LabelRole] == "frontend" {
			containers, _, _ := unstructured.NestedSlice(n.Object, "spec", "containers")
			env := containers[0].(map[string]interface{})["env"].([]interface{})
			env[0].(map[string]interface{})["value"] = "B"
			_ = unstructured.SetNestedSlice(n.Object, containers, "spec", "containers")
		}
		additions = append(additions, n)
	}
	for _, p := range additions {
		objects["pods"][string(p.GetUID())] = p
		l.Released[string(p.GetUID())] = true
	}
}

func recoveryOrderFixture(t *testing.T) (*NormalLedger, Objects, string) {
	t.Helper()
	l, objects, scope := recoveryFixture(t, "RUN-313", "RoleRecreate")
	if err := l.armRecovery(scope, objects); err != nil {
		t.Fatal(err)
	}
	addRecoveryOrderSurge(l, objects)
	deleteRecoveryPod(l, objects, scope.TargetUID)
	var worker string
	for uid := range scope.RecoveryUIDs {
		if uid != scope.TargetUID {
			worker = uid
		}
	}
	p := objects["pods"][worker].DeepCopy()
	now := metav1.Now()
	p.SetDeletionTimestamp(&now)
	l.Before("pods", "MODIFIED", p, objects)
	objects["pods"][worker] = p
	return l, objects, worker
}

func startRecoveryOrderGroup(l *NormalLedger, objects Objects) {
	l.Before("podgroups", "DELETED", objects["podgroups"]["pg-2"], objects)
	delete(objects["podgroups"], "pg-2")
}

func makeRecoveryOrderGroupUnavailable(objects Objects, group string) {
	for uid, pod := range objects["pods"] {
		if pod.GetLabels()[LabelGroup] != group || pod.GetLabels()[LabelRole] != "backend" {
			continue
		}
		p := pod.DeepCopy()
		_ = unstructured.SetNestedSlice(p.Object, []interface{}{map[string]interface{}{"type": "Ready", "status": "False"}}, "status", "conditions")
		objects["pods"][uid] = p
	}
}

func TestRecoveryOrderKeepsFirstPodGroupStartAcrossWatchStreams(t *testing.T) {
	for _, finalWorkerFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(finalWorkerFirst), func(t *testing.T) {
			l, objects, worker := recoveryOrderFixture(t)
			startRecoveryOrderGroup(l, objects)
			if err := l.error(); err != nil {
				t.Fatal("valid start rejected", err)
			}
			if finalWorkerFirst {
				deleteRecoveryPod(l, objects, worker)
			}
			deleteNormal(l, objects, "backend", 2, 0)
			if err := l.error(); err != nil {
				t.Fatal("old worker final frame changed an already observed start", err)
			}
		})
	}
}

func TestRecoveryOrderRejectsWrongOrderAtFirstPodGroupEvidence(t *testing.T) {
	l, objects, _ := recoveryFixture(t, "RUN-313", "RoleRecreate")
	addRecoveryOrderSurge(l, objects)
	makeRecoveryOrderGroupUnavailable(objects, "model-0")
	startRecoveryOrderGroup(l, objects)
	requireNormalViolation(t, l, "ORDER_MISMATCH")
}

func TestRecoveryOrderStillRechecksActualReadyAtPodDeletion(t *testing.T) {
	l, objects, _ := recoveryOrderFixture(t)
	startRecoveryOrderGroup(l, objects)
	if err := l.error(); err != nil {
		t.Fatal(err)
	}
	makeRecoveryOrderGroupUnavailable(objects, "model-1")
	deleteNormal(l, objects, "backend", 2, 0)
	requireNormalViolation(t, l, "BUDGET_VIOLATION")
}

func TestRecoveryOrderPermissionCannotTransferToReplacementUIDs(t *testing.T) {
	l, objects, _ := recoveryOrderFixture(t)
	startRecoveryOrderGroup(l, objects)
	if err := l.error(); err != nil {
		t.Fatal(err)
	}
	var replacements []*unstructured.Unstructured
	for uid, pod := range objects["pods"] {
		if pod.GetLabels()[LabelGroup] != "model-2" {
			continue
		}
		p := pod.DeepCopy()
		p.SetUID(types.UID("replacement-" + uid))
		replacements = append(replacements, p)
		delete(objects["pods"], uid)
	}
	for _, p := range replacements {
		objects["pods"][string(p.GetUID())] = p
		l.Released[string(p.GetUID())] = true
	}
	makeRecoveryOrderGroupUnavailable(objects, "model-1")
	deleteNormal(l, objects, "backend", 2, 0)
	requireNormalViolation(t, l, "ORDER_MISMATCH")
}

func TestRecoveryOrderAcceptsPodBeforePodGroupDelivery(t *testing.T) {
	l, objects, worker := recoveryOrderFixture(t)
	deleteNormal(l, objects, "backend", 2, 0)
	if err := l.error(); err != nil {
		t.Fatal(err)
	}
	deleteRecoveryPod(l, objects, worker)
	startRecoveryOrderGroup(l, objects)
	if err := l.error(); err != nil {
		t.Fatal("late PodGroup frame changed the already observed Pod start", err)
	}
}
