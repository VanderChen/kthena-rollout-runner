// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func boolPtr(v bool) *bool { return &v }
func intPtr(v int) *int    { return &v }

// expectationFixture is one group "model-2" whose first object was replaced by
// a second one of the same name: the state after a same-name recreation.
func expectationFixture(t *testing.T) (*GroupStream, Objects, map[string]map[string]groupSnapshot) {
	t.Helper()
	prefix := t.Name() + "/"
	stream := newGroupStream()
	objects := Objects{SGGVR.Resource: {}, "pods": {}}
	at := time.Unix(1, 0).UTC()
	member := func(name, uid, group string) {
		pod := normalTestPod("SG", "frontend", 2, "A", true, "entry")
		pod.SetName(name)
		pod.SetUID(types.UID(uid))
		pod.SetOwnerReferences(groupRef("model-2", group))
		objects["pods"][uid] = pod
	}
	first := testGroup("model-2", prefix+"first", "owner")
	groupOwners.record(first)
	stream.observe(1, at, "ADDED", first)
	objects[SGGVR.Resource][string(first.GetUID())] = first
	member("model-2-frontend-0-0", prefix+"old-pod", prefix+"first")
	snapshots := map[string]map[string]groupSnapshot{"baseline": groupSnapshots(objects)}

	_ = unstructured.SetNestedSlice(first.Object, []interface{}{map[string]interface{}{"type": "Failed", "status": "True", "reason": "MemberFaulty"}}, "status", "conditions")
	now := metav1.Now()
	first.SetDeletionTimestamp(&now)
	first.SetFinalizers([]string{foregroundFinalizer})
	stream.observe(2, at, "MODIFIED", first)
	stream.observe(3, at, "DELETED", first)
	delete(objects[SGGVR.Resource], string(first.GetUID()))
	delete(objects["pods"], prefix+"old-pod")

	second := testGroup("model-2", prefix+"second", "owner")
	groupOwners.record(second)
	stream.observe(4, at, "ADDED", second)
	objects[SGGVR.Resource][string(second.GetUID())] = second
	member("model-2-frontend-0-0", prefix+"new-pod", prefix+"second")
	return stream, objects, snapshots
}

func TestDeclaredServingGroupFactsRejectEveryDeviation(t *testing.T) {
	recreated := GroupExpectation{Name: "model-2", Objects: intPtr(2), Exists: boolPtr(true), Terminating: boolPtr(false), Surge: boolPtr(false), Owned: boolPtr(true),
		UIDDiffersFrom: "baseline", PodNamesSameAs: "baseline", PodUIDsDisjointFrom: "baseline", Pods: intPtr(1),
		Seen: []string{"Failed=True"}, NeverSeen: []string{"Drained=True"}, SurgeAtCreation: boolPtr(false), ForegroundDeletions: intPtr(1)}
	stream, objects, snapshots := expectationFixture(t)
	if reason := checkGroupExpectations([]GroupExpectation{recreated}, stream, objects, "owner", snapshots); reason != "" {
		t.Fatalf("a correct same-name recreation was rejected: %s", reason)
	}
	for _, c := range []struct {
		name   string
		change func(*GroupExpectation)
		reason string
	}{
		{"the group was never recreated", func(e *GroupExpectation) { e.Objects = intPtr(1) }, "objects observed=2 want 1"},
		{"the object is gone", func(e *GroupExpectation) { e.Exists = boolPtr(false) }, "exists=true want false"},
		{"the object still terminates", func(e *GroupExpectation) { e.Terminating = boolPtr(true) }, "terminating=false want true"},
		{"the object is a surge", func(e *GroupExpectation) { e.Surge = boolPtr(true) }, "want marked=true"},
		{"the object belongs to someone else", func(e *GroupExpectation) { e.Owned = boolPtr(false) }, "owned by the ModelServing=true want false"},
		{"the same object", func(e *GroupExpectation) { e.UIDDiffersFrom, e.UIDSameAs = "", "baseline" }, "object UID changed since baseline"},
		{"the same Pods", func(e *GroupExpectation) { e.PodUIDsSameAs = "baseline" }, "member Pod UIDs changed since baseline"},
		{"more members", func(e *GroupExpectation) { e.Pods = intPtr(2) }, "member Pods=1 want 2"},
		{"a reason that never showed", func(e *GroupExpectation) { e.Seen = []string{"Failed=True/AllPodsLost"} }, "never showed Failed=True/AllPodsLost"},
		{"a condition that must not show", func(e *GroupExpectation) { e.NeverSeen = []string{"Failed=True"} }, "showed Failed=True"},
		{"created as a surge", func(e *GroupExpectation) { e.SurgeAtCreation = boolPtr(true) }, "surge mark at creation"},
		{"deleted without foreground propagation", func(e *GroupExpectation) { e.ForegroundDeletions = intPtr(0) }, "deleted in the foreground=1 want 0"},
		{"unknown snapshot", func(e *GroupExpectation) { e.PodNamesSameAs = "missing-step" }, `no snapshot of step "missing-step"`},
	} {
		want := recreated
		c.change(&want)
		if reason := checkGroupExpectations([]GroupExpectation{want}, stream, objects, "owner", snapshots); !strings.Contains(reason, c.reason) {
			t.Errorf("%s: reason=%q want %q", c.name, reason, c.reason)
		}
	}
	t.Run("old member kept under the new object", func(t *testing.T) {
		stream, objects, snapshots := expectationFixture(t)
		kept := snapshots["baseline"]["model-2"].Pods["model-2-frontend-0-0"]
		for uid, pod := range objects["pods"] {
			delete(objects["pods"], uid)
			pod.SetUID(types.UID(kept))
			objects["pods"][kept] = pod
		}
		if reason := checkGroupExpectations([]GroupExpectation{recreated}, stream, objects, "owner", snapshots); !strings.Contains(reason, "was not replaced") {
			t.Fatalf("a member that survived the recreation was accepted: %q", reason)
		}
	})
	t.Run("member under another name", func(t *testing.T) {
		stream, objects, snapshots := expectationFixture(t)
		for _, pod := range objects["pods"] {
			pod.SetName("model-2-frontend-1-0")
		}
		if reason := checkGroupExpectations([]GroupExpectation{recreated}, stream, objects, "owner", snapshots); !strings.Contains(reason, "member Pod names") {
			t.Fatalf("a recreation under other Pod names was accepted: %q", reason)
		}
	})
	t.Run("absent group", func(t *testing.T) {
		stream, objects, snapshots := expectationFixture(t)
		absent := []GroupExpectation{{Name: "model-4", Objects: intPtr(0), Exists: boolPtr(false), Pods: intPtr(0)}}
		if reason := checkGroupExpectations(absent, stream, objects, "owner", snapshots); reason != "" {
			t.Fatalf("an ordinal that was never used was rejected: %s", reason)
		}
		absent[0].Terminating = boolPtr(false)
		if reason := checkGroupExpectations(absent, stream, objects, "owner", snapshots); !strings.Contains(reason, "no object exists") {
			t.Fatalf("a fact about a missing object passed: %q", reason)
		}
	})
	if reason := checkGroupExpectations([]GroupExpectation{recreated}, nil, objects, "owner", snapshots); reason == "" {
		t.Fatal("facts accepted without the ServingGroup stream")
	}
	if reason := checkGroupExpectations(nil, nil, objects, "owner", snapshots); reason != "" {
		t.Fatalf("no declared fact must hold trivially: %s", reason)
	}
}

func TestOrphanedMembersStayInTheirGroupSnapshot(t *testing.T) {
	_, objects, _ := expectationFixture(t)
	for _, pod := range objects["pods"] {
		pod.SetOwnerReferences(nil)
	}
	snapshot := groupSnapshots(objects)["model-2"]
	if len(snapshot.Pods) != 1 || snapshot.UID == "" {
		t.Fatalf("an orphaned member left its group: %+v", snapshot)
	}
}

func TestModelServingConditionExpectations(t *testing.T) {
	model := &unstructured.Unstructured{Object: map[string]interface{}{"status": map[string]interface{}{"conditions": []interface{}{
		map[string]interface{}{"type": "RolloutBlocked", "status": "True", "reason": "OrdinalOccupied"},
		map[string]interface{}{"type": "Available", "status": "True", "reason": "AllGroupsReady"},
	}}}}
	for _, c := range []struct {
		name   string
		want   map[string]string
		model  *unstructured.Unstructured
		reason string
	}{
		{"status and reason", map[string]string{"RolloutBlocked": "True/OrdinalOccupied"}, model, ""},
		{"status only", map[string]string{"Available": "True"}, model, ""},
		{"nothing declared", nil, nil, ""},
		{"another reason", map[string]string{"RolloutBlocked": "True/WaitingForDeletion"}, model, `RolloutBlocked="True/OrdinalOccupied" want "True/WaitingForDeletion"`},
		{"another status", map[string]string{"RolloutBlocked": "False"}, model, `want "False"`},
		{"condition absent", map[string]string{"Progressing": "True"}, model, `Progressing="" want "True"`},
		{"ModelServing absent", map[string]string{"Available": "True"}, nil, `Available="" want "True"`},
	} {
		if reason := conditionExpectations(c.want, c.model); (c.reason == "") != (reason == "") || !strings.Contains(reason, c.reason) {
			t.Errorf("%s: reason=%q want %q", c.name, reason, c.reason)
		}
	}
}

func TestTwoStepGroupRecoveryOrder(t *testing.T) {
	type life struct {
		uid, name             string
		first, deleting, gone int64
	}
	scope := map[string]string{"faulty": "model-0-frontend-0-0", "healthy": "model-0-frontend-0-1"}
	timeline := func(lives ...life) *PodTimeline {
		out := &PodTimeline{Lives: map[string]*PodLife{}}
		for _, l := range lives {
			group := "model-0"
			if strings.HasPrefix(l.name, "model-1") {
				group = "model-1"
			}
			out.Lives[l.uid] = &PodLife{UID: l.uid, Name: l.name, Group: group, First: l.first, Deleting: l.deleting, Deleted: l.gone}
		}
		return out
	}
	faulty := life{"faulty", "model-0-frontend-0-0", 1, 10, 12}
	other := life{"other-group", "model-1-frontend-0-0", 3, 0, 0}
	earlier := life{"replaced-long-ago", "model-0-frontend-0-0", 0, 0, 0}
	for _, c := range []struct {
		name  string
		lives []life
		want  string
	}{
		{"faulty first, healthy after it is gone, replacements last", []life{faulty, {"healthy", "model-0-frontend-0-1", 2, 13, 15}, {"new-entry", "model-0-frontend-0-0", 16, 0, 0}, {"new-worker", "model-0-frontend-0-1", 17, 0, 0}, other, earlier}, ""},
		{"healthy member deleted together with the faulty one", []life{faulty, {"healthy", "model-0-frontend-0-1", 2, 11, 15}}, "GROUP_RECOVERY_ORDER: healthy member model-0-frontend-0-1 was being deleted before faulty member"},
		{"healthy member deleted before the faulty one", []life{faulty, {"healthy", "model-0-frontend-0-1", 2, 9, 15}}, "GROUP_RECOVERY_ORDER"},
		{"healthy member kept: only the faulty Pod was refilled", []life{faulty, {"healthy", "model-0-frontend-0-1", 2, 0, 0}, {"new-entry", "model-0-frontend-0-0", 16, 0, 0}}, "GROUP_RECOVERY_INCOMPLETE: original member model-0-frontend-0-1"},
		{"replacement created while a healthy original remains", []life{faulty, {"healthy", "model-0-frontend-0-1", 2, 13, 18}, {"new-entry", "model-0-frontend-0-0", 16, 0, 0}}, "GROUP_RECOVERY_ORDER: replacement member model-0-frontend-0-0 appeared before"},
		{"faulty member never deleted", []life{{"faulty", "model-0-frontend-0-0", 1, 0, 0}, {"healthy", "model-0-frontend-0-1", 2, 0, 0}}, "GROUP_RECOVERY_INCOMPLETE: faulty member"},
		{"healthy member never observed", []life{faulty}, "INCONCLUSIVE: original member"},
	} {
		err := checkTwoStepGroupRecovery(timeline(c.lives...), "faulty", scope)
		if (c.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v want %q", c.name, err, c.want)
		}
	}
	if err := checkTwoStepGroupRecovery(timeline(faulty), "unknown", scope); err == nil || !strings.Contains(err.Error(), "INCONCLUSIVE") {
		t.Fatalf("an unobserved faulty member must be inconclusive: %v", err)
	}
	if err := checkTwoStepGroupRecovery(nil, "faulty", scope); err == nil {
		t.Fatal("order accepted without a Pod timeline")
	}
}

func TestPodTimelineKeepsFirstDeletionAndRemoval(t *testing.T) {
	timeline := &PodTimeline{}
	pod := normalTestPod("SG", "frontend", 0, "A", true, "entry")
	pod.SetOwnerReferences(groupRef("model-0", "group"))
	timeline.observe(5, "ADDED", pod)
	timeline.observe(6, "MODIFIED", pod)
	now := metav1.Now()
	pod.SetDeletionTimestamp(&now)
	timeline.observe(7, "MODIFIED", pod)
	timeline.observe(8, "MODIFIED", pod)
	timeline.observe(9, "DELETED", pod)
	life := timeline.Lives[string(pod.GetUID())]
	if life == nil || life.First != 5 || life.Deleting != 7 || life.Deleted != 9 || life.Group != "model-0" || life.GroupUID != "group" {
		t.Fatalf("reduced Pod life: %+v", life)
	}
	// A Pod removed without a terminating observation was still deleted then.
	abrupt := normalTestPod("SG", "frontend", 1, "A", true, "entry")
	timeline.observe(10, "ADDED", abrupt)
	timeline.observe(11, "DELETED", abrupt)
	if gone := timeline.Lives[string(abrupt.GetUID())]; gone.Deleting != 11 || gone.Deleted != 11 {
		t.Fatalf("abrupt removal: %+v", gone)
	}
	var none *PodTimeline
	none.observe(1, "ADDED", pod)
}
