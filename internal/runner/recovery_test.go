// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func recoveryCase(t *testing.T, id string) Case {
	t.Helper()
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "recovery"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.ID == id {
			return c
		}
	}
	t.Fatal("missing recovery case", id)
	return Case{}
}

func recoveryFixture(t *testing.T, id, policy string) (*NormalLedger, Objects, PodFaultScope) {
	t.Helper()
	c := recoveryCase(t, id)
	l, err := newNormalLedger(c.Scenario.InitialSpec, "owner", "controlled")
	if err != nil {
		t.Fatal(err)
	}
	l.RevisionLayouts["fixture-A"] = l.Model
	objects := Objects{"pods": {}, "podgroups": {}, "controllerrevisions": {}}
	var pods []corev1.Pod
	var target string
	for g := 0; g < l.Model.N; g++ {
		pg := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": fmt.Sprintf("model-%d", g), "namespace": "fault-test", "uid": fmt.Sprintf("pg-%d", g), "ownerReferences": []interface{}{map[string]interface{}{"uid": "owner"}}}}}
		objects["podgroups"][string(pg.GetUID())] = pg
		for name, role := range l.Model.Roles {
			for n := 0; n < role.R; n++ {
				for w := 0; w <= role.W; w++ {
					member := "entry"
					if w > 0 {
						member = fmt.Sprintf("worker-%d", w)
					}
					p := normalTestPod("Role", name, n, "A", true, member)
					labels := p.GetLabels()
					labels[LabelGroup] = fmt.Sprintf("model-%d", g)
					p.SetLabels(labels)
					p.SetNamespace("fault-test")
					p.SetName(fmt.Sprintf("%d-%s", g, p.GetName()))
					p.SetUID(types.UID(p.GetName()))
					objects["pods"][string(p.GetUID())] = p
					l.Released[string(p.GetUID())] = true
					var typed corev1.Pod
					if err := convertPod(p, &typed); err != nil {
						t.Fatal(err)
					}
					pods = append(pods, typed)
					if g == 0 && name == "frontend" && n == 0 && member == "entry" {
						target = string(p.GetUID())
					}
				}
			}
		}
	}
	if err = l.Transition(c.Scenario.Steps[0].Spec, "fault", c.Scenario.Steps[0].Expect, objects); err != nil {
		t.Fatal(err)
	}
	scope, err := capturePodFaultScope("owner", target, policy, pods)
	if err != nil {
		t.Fatal(err)
	}
	return l, objects, scope
}

func deleteRecoveryPod(l *NormalLedger, objects Objects, uid string) {
	p := objects["pods"][uid].DeepCopy()
	now := metav1.Now()
	p.SetDeletionTimestamp(&now)
	l.Before("pods", "MODIFIED", p, objects)
	objects["pods"][uid] = p
	l.Before("pods", "DELETED", p, objects)
	delete(objects["pods"], uid)
}

func TestRecoveryExemptsOnlyFiniteFaultLossAndStillRejectsExtraHealthyLoss(t *testing.T) {
	l, objects, scope := recoveryFixture(t, "RUN-309", "RoleRecreate")
	if err := l.armRecovery(scope, objects); err != nil {
		t.Fatal(err)
	}
	deleteRecoveryPod(l, objects, scope.TargetUID)
	if l.error() != nil || len(l.Starts) != 1 || l.Starts[0].Reason != "external-fault" {
		t.Fatalf("external deletion was charged as rollout: %v", l.error())
	}
	for uid := range scope.RecoveryUIDs {
		if uid != scope.TargetUID {
			deleteRecoveryPod(l, objects, uid)
		}
	}
	if l.error() != nil {
		t.Fatal("finite Role recovery spent normal budget", l.error())
	}
	deleteNormal(l, objects, "frontend", 2, 0)
	requireNormalViolation(t, l, "BUDGET_VIOLATION")
}

func TestNoneDoesNotGrantWorkerOrProtectedGroupRecovery(t *testing.T) {
	l, objects, scope := recoveryFixture(t, "RUN-311", "None")
	if err := l.armRecovery(scope, objects); err != nil {
		t.Fatal(err)
	}
	deleteRecoveryPod(l, objects, scope.TargetUID)
	for uid, p := range objects["pods"] {
		if p.GetLabels()[LabelGroup] == "model-0" && p.GetLabels()[LabelRole] == "frontend" {
			deleteRecoveryPod(l, objects, uid)
			break
		}
	}
	requireNormalViolation(t, l, "PROTECTED_REPLACED")
}

func TestProtectedRecoveryUsesHistoryAndCannotTransferOldUIDPermission(t *testing.T) {
	for _, wrongVersion := range []bool{false, true} {
		t.Run(fmt.Sprint(wrongVersion), func(t *testing.T) {
			l, objects, scope := recoveryFixture(t, "RUN-311", "RoleRecreate")
			original := objects["pods"][scope.TargetUID].DeepCopy()
			if err := l.armRecovery(scope, objects); err != nil {
				t.Fatal(err)
			}
			for uid := range scope.RecoveryUIDs {
				deleteRecoveryPod(l, objects, uid)
			}
			p := original.DeepCopy()
			p.SetUID("new-protected-entry")
			if wrongVersion {
				containers, _, _ := unstructured.NestedSlice(p.Object, "spec", "containers")
				env := containers[0].(map[string]interface{})["env"].([]interface{})
				env[0].(map[string]interface{})["value"] = "B"
				_ = unstructured.SetNestedSlice(p.Object, containers, "spec", "containers")
			}
			objects["pods"][string(p.GetUID())] = p
			l.recoveryAfter("pods", "ADDED", p)
			if wrongVersion {
				requireNormalViolation(t, l, "RECOVERY_WRONG_TARGET")
				return
			}
			if l.error() != nil || l.Protected[string(p.GetUID())] != p.GetName() {
				t.Fatal("historical replacement was not protected", l.error())
			}
			deleteRecoveryPod(l, objects, string(p.GetUID()))
			requireNormalViolation(t, l, "PROTECTED_REPLACED")
		})
	}
}

func TestRecoveryRejectsRepeatedCreationAndMissingOldMemberCleanup(t *testing.T) {
	l, objects, scope := recoveryFixture(t, "RUN-311", "RoleRecreate")
	p := objects["pods"][scope.TargetUID].DeepCopy()
	if err := l.armRecovery(scope, objects); err != nil {
		t.Fatal(err)
	}
	deleteRecoveryPod(l, objects, scope.TargetUID)
	if ok, reason := l.recoverySettled(objects); ok || !strings.Contains(reason, "old recovery member") {
		t.Fatal("partial recovery was accepted", reason)
	}
	for _, uid := range []types.UID{"replacement-1", "replacement-2"} {
		p.SetUID(uid)
		l.recoveryAfter("pods", "ADDED", p)
	}
	requireNormalViolation(t, l, "RECOVERY_DUPLICATE_CREATION")
}

func TestServingGroupRecoveryPodGroupPermissionIsBoundToOriginalUID(t *testing.T) {
	l, objects, scope := recoveryFixture(t, "RUN-311", "ServingGroupRecreate")
	if err := l.armRecovery(scope, objects); err != nil {
		t.Fatal(err)
	}
	pg := objects["podgroups"]["pg-0"].DeepCopy()
	if !l.recoveryBefore("podgroups", "DELETED", pg, objects) || l.error() != nil {
		t.Fatal("captured full SG recovery cannot delete its original PodGroup")
	}
	pg.SetUID("new-pg-0")
	if l.recoveryBefore("podgroups", "DELETED", pg, objects) {
		t.Fatal("old PG permission transferred to replacement")
	}
	other := objects["podgroups"]["pg-1"]
	if l.recoveryBefore("podgroups", "DELETED", other, objects) {
		t.Fatal("recovery expanded to another SG")
	}
}

func TestRecoveryCataloguePreservesConfigurationAndCompilesOnlyFrontendBChange(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "recovery"))
	if err != nil || len(cases) != 84 {
		t.Fatalf("84 Pod fault cases: %d %v", len(cases), err)
	}
	for i, c := range cases {
		if c.ID != fmt.Sprintf("RUN-%03d", i+305) {
			t.Fatal("missing ID")
		}
		config := mapValue(c.Scenario.Source, "config")
		spec := cloneMap(c.Scenario.InitialSpec)
		if textValue(config, "recovery") == "omitted" {
			if _, exists := spec["recoveryPolicy"]; exists {
				t.Fatal("omitted recovery policy was filled in")
			}
		} else if spec["recoveryPolicy"] != config["recovery"] {
			t.Fatal("recovery policy changed")
		}
		for _, raw := range listValue(mapValue(spec, "template"), "roles") {
			role := raw.(map[string]interface{})
			if role["name"] != "frontend" {
				continue
			}
			for _, field := range []string{"entryTemplate", "workerTemplate"} {
				containers := listValue(mapValue(mapValue(role, field), "spec"), "containers")
				for _, container := range containers {
					for _, rawEnv := range listValue(container.(map[string]interface{}), "env") {
						env := rawEnv.(map[string]interface{})
						if env["name"] == "ROLLOUT_VERSION" {
							env["value"] = "B"
						}
					}
				}
			}
		}
		if !reflect.DeepEqual(spec, c.Scenario.Steps[0].Spec) {
			t.Fatal("fault scenario changed more than frontend B", c.ID)
		}
		wantMember := "entry"
		if (i+305)%2 == 0 {
			wantMember = "worker"
		}
		if c.Scenario.Steps[0].PodFault.Member != wantMember {
			t.Fatal("wrong injected member")
		}
	}
}
