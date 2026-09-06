// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	fake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func caseByID(t *testing.T, id string) Case {
	t.Helper()
	cs, e := LoadCases("../../cases/core")
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("missing %s", id)
	return Case{}
}
func TestCoreCatalogue(t *testing.T) {
	cs, e := LoadCases("../../cases/core")
	if e != nil {
		t.Fatal(e)
	}
	if len(cs) != 60 {
		t.Fatalf("count=%d", len(cs))
	}
	counts := map[string]int{}
	for i, c := range cs {
		if c.ID != fmt.Sprintf("RUN-%03d", i+1) {
			t.Fatalf("missing or reordered ID")
		}
		counts[c.Expect.Mode]++
		before, e := c.Manifest("test", "A")
		if e != nil {
			t.Fatal(e)
		}
		after, e := c.Manifest("test", "B")
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(c.Input.Spec["rolloutStrategy"])
		rendered, _ := json.Marshal(before.Object["spec"].(map[string]interface{})["rolloutStrategy"])
		if string(raw) != string(rendered) {
			t.Fatalf("%s lost raw presence: %s != %s", c.ID, raw, rendered)
		}
		roles, _, _ := unstructured.NestedSlice(after.Object, "spec", "template", "roles")
		for _, r := range roles {
			role := r.(map[string]interface{})
			containers, _, _ := unstructured.NestedSlice(role, "entryTemplate", "spec", "containers")
			env := containers[0].(map[string]interface{})["env"].([]interface{})[0].(map[string]interface{})["value"]
			expected := "A"
			if role["name"] == "frontend" {
				expected = "B"
			}
			if env != expected {
				t.Fatal("wrong update scope")
			}
		}
	}
	if counts["SG"] != 30 || counts["Role"] != 30 {
		t.Fatalf("mode counts %v", counts)
	}
}
func makePod(mode, role string, n int, version string, ready bool, member string) *unstructured.Unstructured {
	group := fmt.Sprintf("model-%d", n)
	roleID := role + "-0"
	if mode == "Role" {
		group = "model-0"
		roleID = fmt.Sprintf("%s-%d", role, n)
	}
	name := group + "-" + roleID + "-" + member + "-" + version
	p := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", UID: types.UID(name),
		OwnerReferences: []metav1.OwnerReference{{UID: "owner", Kind: "ModelServing", Name: "model"}},
		Labels:          map[string]string{LabelGroup: group, LabelRole: role, LabelRoleID: roleID, LabelEntry: member}},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Env: []corev1.EnvVar{{Name: "ROLLOUT_VERSION", Value: version}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}},
	}
	if ready {
		p.Status.Conditions[0].Status = corev1.ConditionTrue
	}
	m, e := runtime.DefaultUnstructuredConverter.ToUnstructured(&p)
	if e != nil {
		panic(e)
	}
	return &unstructured.Unstructured{Object: m}
}
func fixture(t *testing.T, id string, workers int) (*Ledger, map[string]*unstructured.Unstructured) {
	t.Helper()
	c := caseByID(t, id)
	roles := c.Input.Spec["template"].(map[string]interface{})["roles"].([]interface{})
	for _, v := range roles {
		r := v.(map[string]interface{})
		if r["name"] == "frontend" {
			r["workerReplicas"] = float64(workers)
		}
	}
	pods := map[string]*unstructured.Unstructured{}
	for i := 0; i < 3; i++ {
		for w := 0; w <= workers; w++ {
			member := "entry"
			if w > 0 {
				member = fmt.Sprintf("worker-%d", w-1)
			}
			p := makePod(c.Expect.Mode, "frontend", i, "A", true, member)
			pods[string(p.GetUID())] = p
		}
		if c.Expect.Mode == "Role" {
			p := makePod("Role", "backend", i, "A", true, "entry")
			pods[string(p.GetUID())] = p
		}
	}
	pg := map[string]*unstructured.Unstructured{}
	for i := 0; i < int(c.Input.Spec["replicas"].(float64)); i++ {
		name := fmt.Sprintf("model-%d", i)
		pg[name] = &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": name, "uid": name + "-pg"}}}
	}
	l, e := NewLedger(c, "owner", pods, pg)
	if e != nil {
		t.Fatal(e)
	}
	return l, pods
}
func event(l *Ledger, pods map[string]*unstructured.Unstructured, verb string, p *unstructured.Unstructured) {
	if verb == "DELETED" {
		delete(pods, string(p.GetUID()))
	} else {
		pods[string(p.GetUID())] = p
	}
	l.Observe("pods", verb, p, pods)
}
func noViolation(t *testing.T, l *Ledger) {
	t.Helper()
	if len(l.Violations) > 0 {
		t.Fatalf("unexpected violation: %v", l.Violations)
	}
}
func releasedEvent(l *Ledger, pods map[string]*unstructured.Unstructured, verb string, p *unstructured.Unstructured) {
	l.Released[string(p.GetUID())] = true
	event(l, pods, verb, p)
}
func hasViolation(t *testing.T, l *Ledger, code string) {
	t.Helper()
	for _, v := range l.Violations {
		if strings.Contains(v, code) {
			return
		}
	}
	t.Fatalf("missing %s: %v", code, l.Violations)
}
func TestDeletedOldSlotKeepsBudgetUntilReady(t *testing.T) {
	l, p := fixture(t, "RUN-001", 0)
	event(l, p, "DELETED", makePod("SG", "frontend", 2, "A", true, "entry"))
	noViolation(t, l)
	if l.Check(p).CommittedReady != 2 {
		t.Fatal("old disappearance released credit")
	}
	event(l, p, "ADDED", makePod("SG", "frontend", 2, "B", false, "entry"))
	event(l, p, "DELETED", makePod("SG", "frontend", 1, "A", true, "entry"))
	hasViolation(t, l, "BUDGET_VIOLATION")
	for n := 0; n < 3; n++ {
		releasedEvent(l, p, "ADDED", makePod("SG", "frontend", n, "B", true, "entry"))
	}
	hasViolation(t, l, "BUDGET_VIOLATION") // final health never clears the failure
}
func TestReadyCreditAllowsNextOldStart(t *testing.T) {
	l, p := fixture(t, "RUN-001", 0)
	for _, n := range []int{2, 1, 0} {
		event(l, p, "DELETED", makePod("SG", "frontend", n, "A", true, "entry"))
		event(l, p, "ADDED", makePod("SG", "frontend", n, "B", false, "entry"))
		noViolation(t, l)
		releasedEvent(l, p, "MODIFIED", makePod("SG", "frontend", n, "B", true, "entry"))
	}
	noViolation(t, l)
}
func TestSurgeMustBeReady(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(fmt.Sprint(ready), func(t *testing.T) {
			l, p := fixture(t, "RUN-016", 0)
			releasedEvent(l, p, "ADDED", makePod("SG", "frontend", 3, "B", ready, "entry"))
			event(l, p, "DELETED", makePod("SG", "frontend", 2, "A", true, "entry"))
			if ready {
				noViolation(t, l)
			} else {
				hasViolation(t, l, "BUDGET_VIOLATION")
			}
		})
	}
}
func TestHybridCanStartTwoWithOneReadySurge(t *testing.T) {
	l, p := fixture(t, "RUN-010", 0)
	releasedEvent(l, p, "ADDED", makePod("SG", "frontend", 3, "B", true, "entry"))
	for _, n := range []int{2, 1} {
		event(l, p, "DELETED", makePod("SG", "frontend", n, "A", true, "entry"))
	}
	noViolation(t, l)
	event(l, p, "DELETED", makePod("SG", "frontend", 0, "A", true, "entry"))
	hasViolation(t, l, "BUDGET_VIOLATION")
}
func TestReadyRegressionRevokesCredit(t *testing.T) {
	l, p := fixture(t, "RUN-010", 0)
	releasedEvent(l, p, "ADDED", makePod("SG", "frontend", 3, "B", true, "entry"))
	for _, n := range []int{2, 1} {
		event(l, p, "DELETED", makePod("SG", "frontend", n, "A", true, "entry"))
	}
	event(l, p, "MODIFIED", makePod("SG", "frontend", 3, "B", false, "entry"))
	hasViolation(t, l, "BUDGET_VIOLATION")
}
func TestRoleWorkersAreOneUnitAndNeedAllReady(t *testing.T) {
	l, p := fixture(t, "RUN-031", 1)
	for _, member := range []string{"entry", "worker-0"} {
		event(l, p, "DELETED", makePod("Role", "frontend", 2, "A", true, member))
	}
	if len(l.Started) != 1 {
		t.Fatal("counted Pods instead of Role")
	}
	releasedEvent(l, p, "ADDED", makePod("Role", "frontend", 2, "B", true, "entry"))
	event(l, p, "ADDED", makePod("Role", "frontend", 2, "B", false, "worker-0"))
	noViolation(t, l)
	if l.Check(p).TargetReady != 0 {
		t.Fatal("entry alone released Role credit")
	}
	event(l, p, "DELETED", makePod("Role", "frontend", 1, "A", true, "entry"))
	hasViolation(t, l, "BUDGET_VIOLATION")
}
func TestPartitionAndUnchangedRole(t *testing.T) {
	l, p := fixture(t, "RUN-003", 0)
	event(l, p, "DELETED", makePod("SG", "frontend", 0, "A", true, "entry"))
	hasViolation(t, l, "PROTECTED_REPLACED")
	l, p = fixture(t, "RUN-031", 0)
	event(l, p, "DELETED", makePod("Role", "backend", 1, "A", true, "entry"))
	hasViolation(t, l, "PROTECTED_REPLACED")
}
func TestFullPartitionForbidsSurge(t *testing.T) {
	l, p := fixture(t, "RUN-012", 0)
	event(l, p, "ADDED", makePod("SG", "frontend", 3, "B", false, "entry"))
	hasViolation(t, l, "PROTECTED_SURGE_CREATED")
}
func TestPodGroupEarlyStartsCountBeforePodsDisappear(t *testing.T) {
	l, p := fixture(t, "RUN-001", 0)
	for _, name := range []string{"model-2", "model-1"} {
		pg := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": name, "uid": name + "-pg"}}}
		l.Observe("podgroups", "DELETED", pg, p)
	}
	hasViolation(t, l, "PodGroup starts")
}
func fakeClient() *fake.FakeDynamicClient {
	return fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{PodGVR: "PodList", MSGVR: "ModelServingList", PGGVR: "PodGroupList", CRGVR: "ControllerRevisionList"})
}
func TestObserverDoesNotCoalesceChanges(t *testing.T) {
	client := fakeClient()
	dir := t.TempDir()
	obs, e := NewObserver(context.Background(), client, "test", dir)
	if e != nil {
		t.Fatal(e)
	}
	p := makePod("SG", "frontend", 0, "A", true, "entry")
	if _, e = client.Resource(PodGVR).Namespace("test").Create(context.Background(), p, metav1.CreateOptions{}); e != nil {
		t.Fatal(e)
	}
	for i := 1; i <= 20; i++ {
		p.SetAnnotations(map[string]string{"sequence": fmt.Sprint(i)})
		if _, e = client.Resource(PodGVR).Namespace("test").Update(context.Background(), p, metav1.UpdateOptions{}); e != nil {
			t.Fatal(e)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		ok, e := obs.Inspect(func(_ *Ledger, obj map[string]map[string]*unstructured.Unstructured) (bool, error) {
			v := obj["pods"][string(p.GetUID())]
			return v != nil && v.GetAnnotations()["sequence"] == "20", nil
		})
		if e != nil {
			t.Fatal(e)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watch stalled")
		}
		time.Sleep(time.Millisecond)
	}
	if e = obs.Close(); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(dir, "observations.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	if n := len(strings.Split(strings.TrimSpace(string(data)), "\n")); n != 21 {
		t.Fatalf("coalesced/lost events: got %d want 21", n)
	}
}
func TestWatchGapCannotPass(t *testing.T) {
	client := fakeClient()
	watchers := map[string]*watch.RaceFreeFakeWatcher{}
	client.PrependWatchReactor("*", func(action clienttesting.Action) (bool, watch.Interface, error) {
		w := watch.NewRaceFreeFake()
		watchers[action.GetResource().Resource] = w
		return true, w, nil
	})
	obs, e := NewObserver(context.Background(), client, "test", t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer obs.Close()
	watchers["pods"].Error(&metav1.Status{Status: "Failure", Code: 410, Reason: metav1.StatusReasonExpired, Message: "expired cursor"})
	deadline := time.Now().Add(time.Second)
	for {
		_, e := obs.Inspect(func(_ *Ledger, _ map[string]map[string]*unstructured.Unstructured) (bool, error) { return true, nil })
		if e != nil {
			if !strings.Contains(e.Error(), "INCONCLUSIVE") {
				t.Fatal(e)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("watch gap not detected")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRevisionHashResolvesToOwnedControllerRevisionName(t *testing.T) {
	l, pods := fixture(t, "RUN-001", 0)
	for _, n := range []int{2, 1, 0} {
		event(l, pods, "DELETED", makePod("SG", "frontend", n, "A", true, "entry"))
		p := makePod("SG", "frontend", n, "B", true, "entry")
		p.SetAnnotations(map[string]string{"scheduling.k8s.io/group-name": fmt.Sprintf("model-%d", n)})
		releasedEvent(l, pods, "ADDED", p)
	}
	objects := map[string]map[string]*unstructured.Unstructured{"pods": pods, "podgroups": {}, "modelservings": {}, "controllerrevisions": {}}
	for n := 0; n < 3; n++ {
		name := fmt.Sprintf("model-%d", n)
		pg := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": name}, "spec": map[string]interface{}{"minMember": int64(1), "minResources": map[string]interface{}{"cpu": "5m", "memory": "4Mi"}}}}
		pg.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
		objects["podgroups"][name] = pg
	}
	ms := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "model"}, "status": map[string]interface{}{"observedGeneration": int64(2), "availableReplicas": int64(3), "replicas": int64(3), "updatedReplicas": int64(3), "currentRevision": "abc123", "updateRevision": "abc123"}}}
	objects["modelservings"]["owner"] = ms
	cr := &unstructured.Unstructured{}
	cr.SetName("model-abc123")
	cr.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
	objects["controllerrevisions"]["revision"] = cr
	if !finalFacts(l, objects, 2) {
		t.Fatal("must resolve status hash abc123 to model-abc123")
	}
	cr.SetOwnerReferences([]metav1.OwnerReference{{UID: "different-owner"}})
	if finalFacts(l, objects, 2) {
		t.Fatal("must reject wrong-owner revision")
	}
	cr.SetOwnerReferences([]metav1.OwnerReference{{UID: "owner"}})
	objects["podgroups"]["model-0"].Object["spec"].(map[string]interface{})["minResources"] = map[string]interface{}{"cpu": "100m", "memory": "4Mi"}
	if finalFacts(l, objects, 2) {
		t.Fatal("must reject wrong gang resources")
	}
}
func TestDefaultingAndMalformedConfiguration(t *testing.T) {
	c := caseByID(t, "RUN-031")
	m, e := c.Manifest("test", "A")
	if e != nil {
		t.Fatal(e)
	}
	spec := m.Object["spec"].(map[string]interface{})
	if c.checkSpec(spec, true) == nil {
		t.Fatal("server must materialize Role maxUnavailable default")
	}
	roles := spec["template"].(map[string]interface{})["roles"].([]interface{})
	for _, r := range roles {
		r.(map[string]interface{})["maxUnavailable"] = int64(1)
	}
	if e = c.checkSpec(spec, true); e != nil {
		t.Fatal(e)
	}
	for _, r := range roles {
		role := r.(map[string]interface{})
		if role["name"] == "frontend" {
			role["maxUnavailable"] = int64(2)
		}
	}
	if c.checkSpec(spec, true) == nil {
		t.Fatal("wrong effective default accepted")
	}
	c.Input.Spec["template"] = "bad"
	if c.Validate() == nil {
		t.Fatal("malformed config accepted")
	}
}
func TestSurgeCeilingAndOrder(t *testing.T) {
	l, p := fixture(t, "RUN-010", 0)
	for _, n := range []int{3, 4} {
		event(l, p, "ADDED", makePod("SG", "frontend", n, "B", false, "entry"))
	}
	hasViolation(t, l, "SURGE_VIOLATION")
	l, p = fixture(t, "RUN-024", 0)
	event(l, p, "DELETED", makePod("SG", "frontend", 0, "A", true, "entry"))
	hasViolation(t, l, "ORDER_MISMATCH")
}

func TestFinalPassCannotOverrideLateLatchedViolation(t *testing.T) {
	for _, status := range []string{"PASS", "INCONCLUSIVE"} {
		r := Result{Status: status, Violations: []string{"BUDGET_VIOLATION: late event"}}
		r.enforceLatchedViolations()
		if r.Status != "FAIL" {
			t.Fatalf("latched violation became %s", r.Status)
		}
	}
}

func TestTargetCannotBecomeReadyBeforeRunnerRelease(t *testing.T) {
	l, p := fixture(t, "RUN-010", 0)
	// For example, a controller accidentally drops the fixture readinessProbe.
	// Capacity budgets alone would still pass, but this is not the declared run.
	event(l, p, "ADDED", makePod("SG", "frontend", 3, "B", true, "entry"))
	hasViolation(t, l, "CONTROL_VIOLATION")
}

func TestPartialTargetCannotBypassReadinessControl(t *testing.T) {
	l, p := fixture(t, "RUN-031", 1)
	for _, member := range []string{"entry", "worker-0"} {
		event(l, p, "DELETED", makePod("Role", "frontend", 2, "A", true, member))
	}
	// The Role is incomplete, but even a single unauthorized Ready member
	// means the driver can no longer enforce the intended hold barrier.
	event(l, p, "ADDED", makePod("Role", "frontend", 2, "B", true, "entry"))
	hasViolation(t, l, "CONTROL_VIOLATION")
}
