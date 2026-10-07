// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func compoundCase(t *testing.T, id string) Case {
	t.Helper()
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "servinggroup-compound-v2"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.ID == id {
			return c
		}
	}
	t.Fatal("case absent: " + id)
	return Case{}
}

func compoundFixture(t *testing.T, id string, bad ...int) (*NormalLedger, Objects) {
	t.Helper()
	c := compoundCase(t, id)
	l, err := newNormalLedgerForContract(c.Scenario.InitialSpec, "owner", "controlled", true)
	if err != nil {
		t.Fatal(err)
	}
	l.RevisionLayouts["fixture-A"] = l.Model
	objects := Objects{"pods": {}, "podgroups": {}, "controllerrevisions": {}}
	for g := 0; g < l.Model.N; g++ {
		ready := true
		for _, unavailable := range bad {
			ready = ready && unavailable != g
		}
		p := normalTestPod("SG", "frontend", 0, "A", ready, "entry")
		labels := p.GetLabels()
		labels[LabelGroup] = fmt.Sprintf("model-%d", g)
		p.SetLabels(labels)
		p.SetName(fmt.Sprintf("sg-%d-entry", g))
		p.SetUID(types.UID(p.GetName()))
		objects["pods"][string(p.GetUID())] = p
		l.Released[string(p.GetUID())] = ready
		if role := l.Model.Roles["frontend"]; role.W > 0 {
			worker := normalTestPod("SG", "frontend", 0, role.Worker, ready, "worker-1")
			workerLabels := worker.GetLabels()
			workerLabels[LabelGroup] = fmt.Sprintf("model-%d", g)
			worker.SetLabels(workerLabels)
			worker.SetName(fmt.Sprintf("sg-%d-worker", g))
			worker.SetUID(types.UID(worker.GetName()))
			objects["pods"][string(worker.GetUID())] = worker
			l.Released[string(worker.GetUID())] = ready
		}
		l.CompoundFormal[g] = true
	}
	return l, objects
}

func TestCompoundPercentageUnavailableFloors(t *testing.T) {
	m := map[string]interface{}{"maxUnavailable": "20%"}
	if got, err := budgetForContract(m, "maxUnavailable", 3, true, true); err != nil || got != 0 {
		t.Fatalf("compound 3*20%% must floor to zero: %d, %v", got, err)
	}
	if got, err := budgetForContract(m, "maxUnavailable", 3, true, false); err != nil || got != 0 {
		t.Fatalf("normal contract must also floor U: %d, %v", got, err)
	}
	m["maxUnavailable"] = "25%"
	for _, point := range []struct{ n, want int }{{5, 1}, {9, 2}} {
		if got, err := budgetForContract(m, "maxUnavailable", point.n, true, true); err != nil || got != point.want {
			t.Fatalf("compound 25%% at N=%d: got=%d want=%d err=%v", point.n, got, point.want, err)
		}
	}
}

func TestCompoundScaleDeletionCostMakesSparseSource(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-618")
	for _, pod := range objects["pods"] {
		group := ordinal(pod.GetLabels()[LabelGroup])
		annotations := pod.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		if group == 1 || group == 2 {
			annotations["controller.kubernetes.io/pod-deletion-cost"] = "-100"
		} else {
			annotations["controller.kubernetes.io/pod-deletion-cost"] = "100"
		}
		pod.SetAnnotations(annotations)
	}
	next := cloneMap(l.Model.Spec)
	next["replicas"] = float64(2)
	if err := l.Transition(next, "shrink", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	if !l.ScaleGroups[1] || !l.ScaleGroups[2] || l.ScaleGroups[0] || l.ScaleGroups[3] {
		t.Fatalf("wrong cost-ranked shrink selection: %+v", l.ScaleGroups)
	}
}

func TestCompoundScaleCanRemoveUnreadyProtectedGroup(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-625", 1)
	next := cloneMap(l.Model.Spec)
	next["replicas"] = float64(2)
	budget := mapValue(mapValue(next, "rolloutStrategy"), "rollingUpdateConfiguration")
	budget["partition"] = float64(2)
	if err := l.Transition(next, "shrink", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	if !l.ScaleGroups[1] || l.ScaleGroups[2] {
		t.Fatalf("unready protected group should be removed before healthy group: %+v", l.ScaleGroups)
	}
}

func TestCompoundTerminatingStillOccupiesCapacity(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-625")
	for _, p := range objects["pods"] {
		if ordinal(p.GetLabels()[LabelGroup]) == 2 {
			now := metav1.Now()
			p.SetDeletionTimestamp(&now)
			break
		}
	}
	if got := l.compoundBudget(objects).C; got != 3 {
		t.Fatalf("deleting group released physical slot early: %d", got)
	}
}

func TestCompoundOldPodsAndReplacementPodGroupCountTwice(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-625")
	oldTime := metav1.NewTime(time.Now().Add(-time.Minute))
	newTime := metav1.NewTime(time.Now())
	for _, pod := range objects["pods"] {
		if ordinal(pod.GetLabels()[LabelGroup]) == 2 {
			pod.SetCreationTimestamp(oldTime)
			now := metav1.Now()
			pod.SetDeletionTimestamp(&now)
		}
	}
	group := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "scheduling.volcano.sh/v1beta1", "kind": "PodGroup", "metadata": map[string]interface{}{"name": "model-2"}}}
	group.SetUID("replacement-pg")
	group.SetCreationTimestamp(newTime)
	group.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner", Kind: "ModelServing", Name: "model"}})
	objects["podgroups"][string(group.GetUID())] = group
	if got := l.compoundBudget(objects).C; got != 4 {
		t.Fatalf("old Pods plus new PodGroup must count as two physical generations: %d", got)
	}
}

func TestCompoundNotReadyOldSkipsHealthyHigherOld(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-623", 1)
	c := compoundCase(t, "RUN-623")
	if err := l.Transition(c.Scenario.Steps[2].Spec, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 1, 0)
	if err := l.error(); err != nil {
		t.Fatalf("legal old NotReady repair rejected: %v", err)
	}
}

func TestCompoundHealthyOldCannotSkipLowerNotReady(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-623", 1)
	c := compoundCase(t, "RUN-623")
	if err := l.Transition(c.Scenario.Steps[0].Spec, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 4, 0)
	requireNormalViolation(t, l, "COMPOUND_ORDER_MISMATCH")
}

func TestCompoundProtectedFailurePreservesHealthyFloor(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-649", 0)
	c := compoundCase(t, "RUN-649")
	if err := l.Transition(c.Scenario.Steps[0].Spec, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 2, 0)
	requireNormalViolation(t, l, "COMPOUND_READY_BUDGET")
}

func TestOldCandidateOrderByModeAndSurgeIdentity(t *testing.T) {
	for _, mode := range []string{"SG", "Role", "coordinated", "excluded-role"} {
		t.Run(mode, func(t *testing.T) {
			l, _ := compoundFixture(t, "RUN-623")
			if mode != "SG" {
				l.Model.Mode = "Role"
				l.Model.Roles["frontend"] = RoleLayout{R: 3}
			}
			coordinated := mode == "coordinated" || mode == "excluded-role"
			if coordinated {
				mapValue(l.Model.Spec, "rolloutStrategy")["roleCoordination"] = map[string]interface{}{"roles": []interface{}{"a", "b"}, "maxSkew": "50%"}
			}
			low := NormalUnit{Role: "frontend", Ordinal: 0, Ready: false}
			high := NormalUnit{Role: "frontend", Ordinal: 2, Ready: true}
			if l.oldCandidatePrecedes(high, low) != coordinated || l.oldCandidatePrecedes(low, high) == coordinated {
				t.Fatal("wrong health/ordinal priority")
			}
			high.Ready = false
			if !l.oldCandidatePrecedes(high, low) || l.oldCandidatePrecedes(low, high) {
				t.Fatal("same health class must descend")
			}
			if coordinated {
				pod := normalTestPod("Role", "frontend", 3, "A", false, "entry")
				var typed corev1.Pod
				if err := convertPod(pod, &typed); err != nil {
					t.Fatal(err)
				}
				typed.Annotations = map[string]string{"modelserving.volcano.sh/surge": "role"}
				surge := NormalUnit{Role: "frontend", Ordinal: 3, Pods: []*corev1.Pod{&typed}}
				if l.oldCandidatePrecedes(surge, low) {
					t.Fatal("temporary surge blocked stable prefix")
				}
				delete(typed.Annotations, "modelserving.volcano.sh/surge")
				if !l.oldCandidatePrecedes(surge, low) {
					t.Fatal("unmarked high ordinal treated as surge")
				}
				typed.Annotations["modelserving.volcano.sh/surge"] = "role"
				l.Model.Roles["frontend"] = RoleLayout{R: 4}
				if !l.oldCandidatePrecedes(surge, low) {
					t.Fatal("adopted surge did not become stable")
				}
			}
		})
	}
}

func TestCompoundSurgeCountsBothCAndV(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-640", 2)
	c := compoundCase(t, "RUN-640")
	if err := l.Transition(c.Scenario.Steps[1].Spec, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	before := l.compoundBudget(objects)
	if before.C != 3 || before.R != 2 || before.V != 0 || before.Q != 1 {
		t.Fatalf("bad pre-surge budget: %+v", before)
	}
	p := normalTestPod("SG", "frontend", 0, "B", false, "entry")
	labels := p.GetLabels()
	labels[LabelGroup] = "model-3"
	p.SetLabels(labels)
	p.SetName("sg-3-B")
	p.SetUID(types.UID(p.GetName()))
	objects["pods"][string(p.GetUID())] = p
	after := l.compoundBudget(objects)
	if after.C != 4 || after.R != 2 || after.V != 1 || after.Q != 1 {
		t.Fatalf("unready surge must increment C and V together: %+v", after)
	}
}

func TestCompoundPodGroupWithoutPodReservesUnknownCapacity(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-640", 2)
	c := compoundCase(t, "RUN-640")
	if err := l.Transition(c.Scenario.Steps[1].Spec, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	group := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "scheduling.volcano.sh/v1beta1", "kind": "PodGroup", "metadata": map[string]interface{}{"name": "model-3"}}}
	group.SetUID("target-pg-3")
	group.SetCreationTimestamp(metav1.Now())
	group.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner", Kind: "ModelServing", Name: "model"}})
	objects["podgroups"][string(group.GetUID())] = group
	l.compoundAfter("podgroups", "ADDED", group, objects)
	budget := l.compoundBudget(objects)
	if budget.C != 4 || budget.V != 0 || budget.I != 1 || budget.Q != 1 {
		t.Fatalf("PodGroup before any member must count in C and I, without guessing a version: %+v", budget)
	}
}

func TestCompoundBadVersionSourceMustActuallyBeRunning(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-640", 2)
	e := &normalExecution{l: l, o: &Observer{objects: objects}}
	want := &CompoundExpectation{Active: 3, Ready: 2, Groups: []CompoundGroupExpectation{
		{Ordinal: 0, Version: "A", Ready: true},
		{Ordinal: 1, Version: "A", Ready: true},
		{Ordinal: 2, Version: "A", RunningNotReady: true},
	}}
	if ok, _ := e.compoundFacts(want); ok {
		t.Fatal("missing Running container status accepted as a bad-version source")
	}
	for _, pod := range objects["pods"] {
		if ordinal(pod.GetLabels()[LabelGroup]) == 2 {
			mapValue(pod.Object, "status")["containerStatuses"] = []interface{}{map[string]interface{}{
				"name": "workload", "ready": false, "state": map[string]interface{}{"running": map[string]interface{}{}},
			}}
		}
	}
	if ok, reason := e.compoundFacts(want); !ok {
		t.Fatalf("actual Running/NotReady source rejected: %s", reason)
	}
}

func TestCompoundPendingCreditNotDoubleChargedAfterOldGone(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-623", 3, 4)
	// S05 starts at N=3. Use its template with five replicas and U=2 for the
	// design's two-unavailable batch.
	initial := cloneMap(l.Model.Spec)
	initial["replicas"] = float64(5)
	budget := mapValue(mapValue(initial, "rolloutStrategy"), "rollingUpdateConfiguration")
	budget["maxUnavailable"] = float64(2)
	model, err := readModelForContract(initial, true)
	if err != nil {
		t.Fatal(err)
	}
	l.Model, l.Base, l.History = model, model, []NormalModel{model}
	for g := 3; g < 5; g++ {
		p := normalTestPod("SG", "frontend", 0, "A", false, "entry")
		labels := p.GetLabels()
		labels[LabelGroup] = fmt.Sprintf("model-%d", g)
		p.SetLabels(labels)
		p.SetName(fmt.Sprintf("sg-%d-entry", g))
		p.SetUID(types.UID(p.GetName()))
		objects["pods"][string(p.GetUID())] = p
	}
	target := cloneMap(initial)
	role := listValue(mapValue(target, "template"), "roles")[0].(map[string]interface{})
	entry := mapValue(role, "entryTemplate")
	container := listValue(mapValue(entry, "spec"), "containers")[0].(map[string]interface{})
	listValue(container, "env")[0].(map[string]interface{})["value"] = "B"
	if err := l.Transition(target, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 4, 0)
	if err := l.error(); err != nil {
		t.Fatal(err)
	}
	for uid, pod := range objects["pods"] {
		if ordinal(pod.GetLabels()[LabelGroup]) == 4 {
			delete(objects["pods"], uid)
		}
	}
	deleteNormal(l, objects, "frontend", 3, 0)
	if err := l.error(); err != nil {
		t.Fatalf("second legal Q replacement rejected: %v", err)
	}
}

func TestCompoundQCannotBeSpentThreeTimesBeforeReplacementReady(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-623", 2, 3, 4)
	c := compoundCase(t, "RUN-623")
	if err := l.Transition(c.Scenario.Steps[0].Spec, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 4, 0)
	deleteNormal(l, objects, "frontend", 3, 0)
	if err := l.error(); err != nil {
		t.Fatalf("two Q credits should be legal: %v", err)
	}
	deleteNormal(l, objects, "frontend", 2, 0)
	requireNormalViolation(t, l, "COMPOUND_Q_EXHAUSTED")
}

func TestRoleOldNotReadyUsesQWithoutBorrowingOtherRoleCapacity(t *testing.T) {
	l, objects := normalFixture(t, "RUN-143")
	for _, pod := range objects["pods"] {
		if pod.GetLabels()[LabelRole] == "frontend" {
			mapValue(pod.Object, "status")["conditions"] = []interface{}{map[string]interface{}{"type": "Ready", "status": "False"}}
		}
	}
	next := historyVersionSpec(l.Model.Spec, "B")
	if err := l.Transition(next, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 0, 2)
	if err := l.error(); err != nil {
		t.Fatalf("first old-bad repair rejected: %v", err)
	}
	deleteNormal(l, objects, "frontend", 0, 1)
	requireNormalViolation(t, l, "Q_EXHAUSTED")
}

func TestCompoundPartialCohortDeletionDoesNotSpendQAgain(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-652")
	c := compoundCase(t, "RUN-652")
	if err := l.Transition(c.Scenario.Steps[0].Spec, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 2, 0)
	if err := l.error(); err != nil {
		t.Fatal(err)
	}
	for uid, pod := range objects["pods"] {
		if ordinal(pod.GetLabels()[LabelGroup]) == 2 && pod.GetLabels()[LabelEntry] == "true" {
			delete(objects["pods"], uid)
		}
	}
	deleteNormal(l, objects, "frontend", 2, 0)
	if err := l.error(); err != nil {
		t.Fatalf("same remaining worker deletion charged again: %v", err)
	}
	deleteNormal(l, objects, "frontend", 1, 0)
	requireNormalViolation(t, l, "COMPOUND_Q_EXHAUSTED")
}

func TestCompoundHealthyTargetHighGroupCannotBeRepositioned(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-618")
	for uid, pod := range objects["pods"] {
		group := ordinal(pod.GetLabels()[LabelGroup])
		if group == 1 || group == 2 {
			delete(objects["pods"], uid)
		}
		if group == 3 {
			container := listValue(mapValue(pod.Object, "spec"), "containers")[0].(map[string]interface{})
			listValue(container, "env")[0].(map[string]interface{})["value"] = "B"
		}
	}
	l.Model.N = 2
	l.Model.Roles["frontend"] = RoleLayout{Name: "frontend", R: 1, W: 0, Entry: "B"}
	l.Armed = true
	for _, pod := range objects["pods"] {
		if ordinal(pod.GetLabels()[LabelGroup]) == 3 {
			l.compoundBefore("pods", "MODIFIED", deletingCopy(pod), objects)
		}
	}
	requireNormalViolation(t, l, "COMPOUND_TARGET_REPLACED")
}

func TestCompoundServingSurgeCannotBeDiscardedBeforeZeroUnavailableRoll(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-636")
	c := compoundCase(t, "RUN-636")
	if err := l.Transition(c.Scenario.Steps[0].Spec, "B-surge", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	l.RevisionLayouts["fixture-B"] = l.Model
	p := normalTestPod("SG", "frontend", 0, "B", true, "entry")
	labels := p.GetLabels()
	labels[LabelGroup] = "model-3"
	p.SetLabels(labels)
	p.SetName("sg-3-B")
	p.SetUID(types.UID(p.GetName()))
	objects["pods"][string(p.GetUID())] = p
	l.Released[string(p.GetUID())] = true
	if err := l.Transition(c.Scenario.Steps[1].Spec, "shrink", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 3, 0)
	requireNormalViolation(t, l, "COMPOUND_NEEDED_SURGE_REPLACED")
}

func TestCompoundShrinkCompletesBeforeRetainedOldRolls(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-631")
	c := compoundCase(t, "RUN-631")
	if err := l.Transition(c.Scenario.Steps[0].Spec, "atomic-shrink-and-B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 2, 0)
	requireNormalViolation(t, l, "COMPOUND_SCALE_FIRST")
}

func deletingCopy(pod *unstructured.Unstructured) *unstructured.Unstructured {
	copy := pod.DeepCopy()
	now := metav1.Now()
	copy.SetDeletionTimestamp(&now)
	return copy
}

func TestCompoundRoleMemberIncreaseConsumesReadyCredit(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-652")
	current := cloneMap(l.Model.Spec)
	role := listValue(mapValue(current, "template"), "roles")[0].(map[string]interface{})
	role["replicas"] = float64(2)
	if err := l.Transition(current, "grow-role", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	p := normalTestPod("SG", "frontend", 1, "A", false, "entry")
	labels := p.GetLabels()
	labels[LabelGroup] = "model-2"
	labels[LabelRoleID] = "frontend-1"
	p.SetLabels(labels)
	p.SetName("sg-2-role-1")
	p.SetUID(types.UID(p.GetName()))
	objects["pods"][string(p.GetUID())] = p
	if l.units(objects["pods"])["model-2"].Ready {
		t.Fatal("partially grown group counted Ready")
	}
	if !l.units(objects["pods"])["model-1"].Ready {
		t.Fatal("untouched old-membership group lost Ready credit")
	}
}

func TestCompoundRoleMemberIncreaseCannotDropSecondGroupBelowFloor(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-652")
	current := cloneMap(l.Model.Spec)
	role := listValue(mapValue(current, "template"), "roles")[0].(map[string]interface{})
	role["replicas"] = float64(2)
	if err := l.Transition(current, "grow-role", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	for _, group := range []int{2, 1} {
		var added *unstructured.Unstructured
		for _, member := range []struct{ kind, version string }{{"entry", "A"}, {"worker-1", "W1"}} {
			p := normalTestPod("SG", "frontend", 1, member.version, false, member.kind)
			labels := p.GetLabels()
			labels[LabelGroup] = fmt.Sprintf("model-%d", group)
			labels[LabelRoleID] = "frontend-1"
			p.SetLabels(labels)
			p.SetName(fmt.Sprintf("sg-%d-role-1-%s", group, member.kind))
			p.SetUID(types.UID(p.GetName()))
			objects["pods"][string(p.GetUID())] = p
			added = p
		}
		l.compoundAfter("pods", "ADDED", added, objects)
	}
	requireNormalViolation(t, l, "COMPOUND_ROLE_READY_BUDGET")
}

func TestCompoundShrinkDoesNotCommitServingSurge(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-636")
	// Healthy target surge is temporary; the three original groups are formal.
	p := normalTestPod("SG", "frontend", 0, "A", true, "entry")
	labels := p.GetLabels()
	labels[LabelGroup] = "model-3"
	p.SetLabels(labels)
	p.SetName("sg-3-B")
	p.SetUID("serving-surge")
	objects["pods"][string(p.GetUID())] = p
	l.Released[string(p.GetUID())] = true
	next := cloneMap(l.Model.Spec)
	next["replicas"] = float64(2)
	if err := l.Transition(next, "shrink", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	if l.ScaleGroups[3] || !l.ScaleGroups[2] {
		t.Fatalf("shrink must select formal group only: %+v", l.ScaleGroups)
	}
	pg := &unstructured.Unstructured{}
	pg.SetName("model-2")
	pg.SetUID("pg-2")
	pg.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
	l.podGroupBefore("podgroups", "DELETED", pg, objects)
	if l.PGPods["serving-surge"] || l.PGScale["serving-surge"] {
		t.Fatal("serving surge was committed without destructive evidence")
	}
	if !l.units(objects["pods"])["model-3"].Ready {
		t.Fatal("serving surge lost Ready credit")
	}
}

func TestCompoundNewPodGroupUsesActualOldMemberVersion(t *testing.T) {
	l, objects := compoundFixture(t, "RUN-623", 1)
	c := compoundCase(t, "RUN-623")
	if err := l.Transition(c.Scenario.Steps[2].Spec, "B", ScenarioExpectation{}, objects); err != nil {
		t.Fatal(err)
	}
	pg := &unstructured.Unstructured{}
	pg.SetName("model-1")
	pg.SetUID("new-pg-old-member")
	pg.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
	objects["podgroups"][string(pg.GetUID())] = pg
	l.compoundAfter("podgroups", "ADDED", pg, objects)
	b := l.compoundBudget(objects)
	if b.V != 0 || b.I != 0 {
		t.Fatalf("observed old member is neither new-unavailable nor unknown: %+v", b)
	}
}
