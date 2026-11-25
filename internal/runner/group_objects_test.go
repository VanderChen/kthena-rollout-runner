// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// testGroup builds one ServingGroup object owned by a ModelServing UID.
func testGroup(name, uid, owner string) *unstructured.Unstructured {
	g := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "workload.serving.volcano.sh/v1alpha1", "kind": servingGroupKind}}
	g.SetName(name)
	g.SetNamespace("test")
	g.SetUID(types.UID(uid))
	g.SetGeneration(1)
	if owner != "" {
		controller := true
		g.SetOwnerReferences([]metav1.OwnerReference{{Kind: "ModelServing", Name: "model", UID: types.UID(owner), Controller: &controller}})
	}
	return g
}

func groupRef(name, uid string) []metav1.OwnerReference {
	controller := true
	return []metav1.OwnerReference{{Kind: servingGroupKind, Name: name, UID: types.UID(uid), Controller: &controller}}
}

func setSurge(o *unstructured.Unstructured, value string) {
	annotations := o.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	delete(annotations, AnnotationSurge)
	if value != "" {
		annotations[AnnotationSurge] = value
	}
	o.SetAnnotations(annotations)
}

func setInstances(o *unstructured.Unstructured, instances ...GroupInstance) {
	var raw []interface{}
	for _, i := range instances {
		raw = append(raw, map[string]interface{}{"role": i.Role, "index": int64(i.Index), "phase": i.Phase, "generation": i.Generation})
	}
	_ = unstructured.SetNestedSlice(o.Object, raw, "status", "roleInstances")
}

// groupFixture is normalFixture on a build with ServingGroup objects: every
// Pod is owned by the ServingGroup object of its group, which the ModelServing
// owns. UIDs carry the test name, because the owner index is process-wide.
func groupFixture(t *testing.T, id string) (*NormalLedger, Objects) {
	t.Helper()
	l, o := normalFixture(t, id)
	l.GroupObjects = true
	o[SGGVR.Resource] = map[string]*unstructured.Unstructured{}
	for g := 0; g < l.Model.N; g++ {
		name := fmt.Sprintf("model-%d", g)
		group := testGroup(name, t.Name()+"/"+name, "owner")
		groupOwners.record(group)
		o[SGGVR.Resource][string(group.GetUID())] = group
	}
	for _, pod := range o["pods"] {
		name := pod.GetLabels()[LabelGroup]
		pod.SetOwnerReferences(groupRef(name, t.Name()+"/"+name))
	}
	return l, o
}

// observeGroup feeds one ServingGroup event through the ledger exactly as the
// observer does: Before sees the previous object, then the cache is updated.
func observeGroup(l *NormalLedger, o Objects, event string, group *unstructured.Unstructured) {
	groupOwners.record(group)
	l.Before(SGGVR.Resource, event, group, o)
	if event == "DELETED" {
		delete(o[SGGVR.Resource], string(group.GetUID()))
	} else {
		o[SGGVR.Resource][string(group.GetUID())] = group.DeepCopy()
	}
	l.After(SGGVR.Resource, event, group, o)
}

func observePod(l *NormalLedger, o Objects, event string, pod *unstructured.Unstructured) {
	l.Before("pods", event, pod, o)
	if event == "DELETED" {
		delete(o["pods"], string(pod.GetUID()))
	} else {
		o["pods"][string(pod.GetUID())] = pod.DeepCopy()
	}
	l.After("pods", event, pod, o)
}

func requireNoNormalViolation(t *testing.T, l *NormalLedger) {
	t.Helper()
	if err := l.error(); err != nil {
		t.Fatalf("unexpected violation: %v", err)
	}
}

func TestOwnerChainResolvesOnlyThroughTheServingGroupUID(t *testing.T) {
	prefix := t.Name() + "/"
	groupOwners.record(testGroup("model-0", prefix+"known", "owner"))
	groupOwners.record(testGroup("model-0", prefix+"foreign", "other-owner"))
	groupOwners.record(testGroup("model-0", prefix+"ownerless", ""))
	lookups := 0
	groupOwners.setLookup(func(namespace, name string) (*unstructured.Unstructured, error) {
		lookups++
		switch name {
		case "late":
			return testGroup("late", prefix+"late", "owner"), nil
		case "recreated":
			// The name now belongs to a newer object of the same ModelServing.
			return testGroup("recreated", prefix+"recreated-new", "owner"), nil
		}
		return nil, fmt.Errorf("not found")
	})
	defer groupOwners.setLookup(nil)
	replicaSet := groupRef("model-0", prefix+"known")
	replicaSet[0].Kind = "ReplicaSet"
	for _, c := range []struct {
		name string
		refs []metav1.OwnerReference
		want bool
	}{
		{"direct ModelServing owner", []metav1.OwnerReference{{Kind: "ModelServing", UID: "owner"}}, true},
		{"ServingGroup of this ModelServing", groupRef("model-0", prefix+"known"), true},
		{"ServingGroup of another ModelServing", groupRef("model-0", prefix+"foreign"), false},
		{"ServingGroup without owner", groupRef("model-0", prefix+"ownerless"), false},
		{"ServingGroup not yet on its stream", groupRef("late", prefix+"late"), true},
		{"ServingGroup replaced by a same-name object", groupRef("recreated", prefix+"recreated-old"), false},
		{"ServingGroup that is gone", groupRef("gone", prefix+"gone"), false},
		{"another kind with a known UID", replicaSet, false},
		{"no owner", nil, false},
	} {
		if got := ownerMatches("test", c.refs, "owner"); got != c.want {
			t.Errorf("%s: owned=%t want %t", c.name, got, c.want)
		}
	}
	// One read per unseen UID, including the negative answers.
	before := lookups
	ownerMatches("test", groupRef("recreated", prefix+"recreated-old"), "owner")
	ownerMatches("test", groupRef("gone", prefix+"gone"), "owner")
	if lookups != before || lookups != 3 {
		t.Fatalf("ServingGroup owner lookups=%d, want one per unseen UID", lookups)
	}
	if ownerMatches("test", groupRef("model-0", prefix+"known"), "") {
		t.Fatal("empty owner UID matched")
	}
}

func TestServingGroupDeletionIsTheGroupSignalOnlyWithGroupObjects(t *testing.T) {
	s := normalCase(t, "RUN-062").Scenario.Steps[0]
	deleting := func(t *testing.T, o Objects, kind string, g int) *unstructured.Unstructured {
		if kind == SGGVR.Resource {
			for _, group := range o[kind] {
				if ordinal(group.GetName()) == g {
					copy := group.DeepCopy()
					now := metav1.Now()
					copy.SetDeletionTimestamp(&now)
					return copy
				}
			}
		}
		pg := &unstructured.Unstructured{}
		pg.SetName(fmt.Sprintf("model-%d", g))
		pg.SetNamespace("test")
		pg.SetUID(types.UID(fmt.Sprintf("%s/pg-%d", t.Name(), g)))
		pg.SetOwnerReferences(groupRef(pg.GetName(), t.Name()+"/"+pg.GetName()))
		return pg
	}
	for _, c := range []struct {
		name         string
		groupObjects bool
		kind         string
		violation    string
	}{
		{"two ServingGroups terminating exceed the budget", true, SGGVR.Resource, "BUDGET_VIOLATION: ServingGroup deletion"},
		{"PodGroups collected by the garbage collector are not a start", true, "podgroups", ""},
		{"two PodGroups deleted exceed the budget without ServingGroup objects", false, "podgroups", "BUDGET_VIOLATION: PodGroup deletion"},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, o := groupFixture(t, "RUN-062")
			l.GroupObjects = c.groupObjects
			if err := l.Transition(s.Spec, "B", s.Expect, o); err != nil {
				t.Fatal(err)
			}
			event := "DELETED"
			if c.kind == SGGVR.Resource {
				// The group object stays, terminating, until its Pods are gone.
				event = "MODIFIED"
			}
			for _, g := range []int{2, 1} {
				l.Before(c.kind, event, deleting(t, o, c.kind, g), o)
			}
			if c.violation == "" {
				requireNoNormalViolation(t, l)
				return
			}
			requireNormalViolation(t, l, c.violation)
		})
	}
}

func TestSurgeMarkLifecycleInvariants(t *testing.T) {
	type step func(t *testing.T, l *NormalLedger, o Objects)
	surgeGroup := func(t *testing.T, ordinal int, mark string) *unstructured.Unstructured {
		name := fmt.Sprintf("model-%d", ordinal)
		g := testGroup(name, t.Name()+"/surge-"+name, "owner")
		setSurge(g, mark)
		return g
	}
	scaleTo := func(n int64) step {
		return func(t *testing.T, l *NormalLedger, o Objects) {
			spec := cloneMap(l.Model.Spec)
			spec["replicas"] = n
			if err := l.Transition(spec, fmt.Sprintf("replicas-%d", n), ScenarioExpectation{}, o); err != nil {
				t.Fatal(err)
			}
		}
	}
	add := func(ordinal int, mark string) step {
		return func(t *testing.T, l *NormalLedger, o Objects) {
			observeGroup(l, o, "ADDED", surgeGroup(t, ordinal, mark))
		}
	}
	modify := func(ordinal int, change func(*unstructured.Unstructured)) step {
		return func(t *testing.T, l *NormalLedger, o Objects) {
			g := o[SGGVR.Resource][t.Name()+fmt.Sprintf("/surge-model-%d", ordinal)].DeepCopy()
			change(g)
			observeGroup(l, o, "MODIFIED", g)
		}
	}
	unmark := func(g *unstructured.Unstructured) { setSurge(g, "") }
	for _, c := range []struct {
		name      string
		steps     []step
		violation string
	}{
		{"surge created outside the range with its mark", []step{add(3, surgeServingGroup)}, ""},
		{"group inside the range created as surge", []step{add(2, surgeServingGroup)}, "SURGE_MARK_ON_FORMAL_GROUP"},
		{"surge decided before a replicas increase arrives late", []step{scaleTo(4), add(3, surgeServingGroup)}, ""},
		{"mark added after creation", []step{add(3, ""), modify(3, func(g *unstructured.Unstructured) { setSurge(g, surgeServingGroup) })}, "SURGE_MARK_ADDED_LATER"},
		{"promotion once replicas covers the ordinal", []step{add(3, surgeServingGroup), scaleTo(4), modify(3, unmark)}, ""},
		{"promotion by count while the ordinal stays outside", []step{add(3, surgeServingGroup), modify(3, unmark)}, "SURGE_PROMOTED_OUTSIDE_RANGE"},
		{"promotion that also changes the spec", []step{add(3, surgeServingGroup), scaleTo(4), modify(3, func(g *unstructured.Unstructured) { unmark(g); g.SetGeneration(2) })}, "SURGE_PROMOTION_CHANGED_SPEC"},
		{"mark dropped while the surge is being deleted", []step{add(3, surgeServingGroup), modify(3, func(g *unstructured.Unstructured) {
			now := metav1.Now()
			g.SetDeletionTimestamp(&now)
			unmark(g)
		})}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, o := groupFixture(t, "RUN-062")
			for _, apply := range c.steps {
				apply(t, l, o)
			}
			if c.violation == "" {
				requireNoNormalViolation(t, l)
				return
			}
			requireNormalViolation(t, l, c.violation)
		})
	}
}

func TestRoleInstanceRecordNeverRunsBackwardsOrIsInherited(t *testing.T) {
	established := func(generation int64) GroupInstance {
		return GroupInstance{Role: "frontend", Index: 0, Phase: "Established", Generation: generation}
	}
	for _, c := range []struct {
		name      string
		created   bool
		history   [][]GroupInstance
		violation string
	}{
		{"drained and created again", true, [][]GroupInstance{{{Role: "frontend", Phase: "Creating"}}, {established(0)}, {{Role: "frontend", Phase: "Draining"}}, {{Role: "frontend", Phase: "Creating", Generation: 1}}, {established(1)}}, ""},
		{"generation runs backwards", true, [][]GroupInstance{{established(2)}, {established(1)}}, "LEDGER_GENERATION_DECREASED"},
		{"new object continues the previous object's record", true, [][]GroupInstance{{established(3)}}, "LEDGER_INHERITED"},
		{"object that already existed may be at any generation", false, [][]GroupInstance{{established(3)}}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, o := groupFixture(t, "RUN-062")
			g := testGroup("model-3", t.Name()+"/model-3", "owner")
			event := "LIST"
			if c.created {
				event = "ADDED"
			}
			observeGroup(l, o, event, g)
			for _, instances := range c.history {
				next := g.DeepCopy()
				setInstances(next, instances...)
				observeGroup(l, o, "MODIFIED", next)
			}
			if c.violation == "" {
				requireNoNormalViolation(t, l)
				return
			}
			requireNormalViolation(t, l, c.violation)
		})
	}
}

func TestPodsOfTwoObjectsOfOneGroupNeverCoexist(t *testing.T) {
	replacement := func(t *testing.T, uid string) *unstructured.Unstructured {
		pod := normalTestPod("SG", "frontend", 2, "B", false, "entry")
		pod.SetName("model-2-frontend-0-0-new")
		pod.SetUID(types.UID(t.Name() + "/new-pod"))
		pod.SetOwnerReferences(groupRef("model-2", uid))
		return pod
	}
	oldPods := func(o Objects) []*unstructured.Unstructured {
		var out []*unstructured.Unstructured
		for _, pod := range o["pods"] {
			if pod.GetLabels()[LabelGroup] == "model-2" {
				out = append(out, pod)
			}
		}
		return out
	}
	t.Run("new object after the old Pods are gone", func(t *testing.T) {
		l, o := groupFixture(t, "RUN-062")
		for _, pod := range oldPods(o) {
			delete(o["pods"], string(pod.GetUID()))
		}
		next := testGroup("model-2", t.Name()+"/model-2-new", "owner")
		observeGroup(l, o, "ADDED", next)
		observePod(l, o, "ADDED", replacement(t, string(next.GetUID())))
		requireNoNormalViolation(t, l)
	})
	t.Run("another Pod of the same object", func(t *testing.T) {
		l, o := groupFixture(t, "RUN-062")
		observePod(l, o, "ADDED", replacement(t, t.Name()+"/model-2"))
		if err := l.error(); err != nil && strings.Contains(err.Error(), "GROUP_OBJECT_OVERLAP") {
			t.Fatalf("members of one object reported as overlap: %v", err)
		}
	})
	t.Run("new object while an old Pod still terminates", func(t *testing.T) {
		l, o := groupFixture(t, "RUN-062")
		for _, pod := range oldPods(o) {
			now := metav1.Now()
			pod.SetDeletionTimestamp(&now)
		}
		next := testGroup("model-2", t.Name()+"/model-2-new", "owner")
		observeGroup(l, o, "ADDED", next)
		observePod(l, o, "ADDED", replacement(t, string(next.GetUID())))
		requireNormalViolation(t, l, "GROUP_OBJECT_OVERLAP")
	})
	t.Run("not checked without ServingGroup objects", func(t *testing.T) {
		l, o := groupFixture(t, "RUN-062")
		l.GroupObjects = false
		next := testGroup("model-2", t.Name()+"/model-2-new", "owner")
		groupOwners.record(next)
		l.After("pods", "ADDED", replacement(t, string(next.GetUID())), o)
		if err := l.error(); err != nil && strings.Contains(err.Error(), "GROUP_OBJECT_OVERLAP") {
			t.Fatalf("object overlap reported for a build without ServingGroup objects: %v", err)
		}
	})
}

func TestPromotionMustNotRecreateThePodsOfTheSurgeGroup(t *testing.T) {
	setup := func(t *testing.T) (*NormalLedger, Objects, *unstructured.Unstructured, *unstructured.Unstructured) {
		l, o := groupFixture(t, "RUN-062")
		surge := testGroup("model-3", t.Name()+"/model-3", "owner")
		setSurge(surge, surgeServingGroup)
		observeGroup(l, o, "ADDED", surge)
		pod := normalTestPod("SG", "frontend", 3, "A", true, "entry")
		pod.SetUID(types.UID(t.Name() + "/surge-pod"))
		pod.SetOwnerReferences(groupRef("model-3", string(surge.GetUID())))
		setSurge(pod, surgeServingGroup)
		o["pods"][string(pod.GetUID())] = pod
		spec := cloneMap(l.Model.Spec)
		spec["replicas"] = int64(4)
		if err := l.Transition(spec, "scale-out", ScenarioExpectation{}, o); err != nil {
			t.Fatal(err)
		}
		promoted := surge.DeepCopy()
		setSurge(promoted, "")
		observeGroup(l, o, "MODIFIED", promoted)
		return l, o, promoted, pod
	}
	hasCode := func(l *NormalLedger) bool {
		return l.error() != nil && strings.Contains(l.error().Error(), "SURGE_PROMOTION_RECREATED_POD")
	}
	t.Run("copy removed in place", func(t *testing.T) {
		l, o, _, pod := setup(t)
		edited := pod.DeepCopy()
		setSurge(edited, "")
		observePod(l, o, "MODIFIED", edited)
		if hasCode(l) {
			t.Fatalf("in-place removal of the copy reported: %v", l.Violations)
		}
	})
	t.Run("Pod deleted while it still carries the copy", func(t *testing.T) {
		l, o, _, pod := setup(t)
		deleting := pod.DeepCopy()
		now := metav1.Now()
		deleting.SetDeletionTimestamp(&now)
		l.After("pods", "MODIFIED", deleting, o)
		if !hasCode(l) {
			t.Fatalf("recreating the Pods of a promoted group was accepted: %v", l.Violations)
		}
	})
	t.Run("whole promoted group deleted later", func(t *testing.T) {
		l, o, promoted, pod := setup(t)
		terminating := promoted.DeepCopy()
		now := metav1.Now()
		terminating.SetDeletionTimestamp(&now)
		o[SGGVR.Resource][string(terminating.GetUID())] = terminating
		deleting := pod.DeepCopy()
		deleting.SetDeletionTimestamp(&now)
		l.After("pods", "MODIFIED", deleting, o)
		if hasCode(l) {
			t.Fatalf("deleting the whole group reported as a promotion error: %v", l.Violations)
		}
	})
}

func TestSettledStateRequiresConsistentGroupObjectsAndSurgeMarks(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(t *testing.T, l *NormalLedger, o Objects)
		reason string
	}{
		{"one live object per group", func(*testing.T, *NormalLedger, Objects) {}, ""},
		{"foreign same-namespace object is not counted", func(t *testing.T, l *NormalLedger, o Objects) {
			foreign := testGroup("model-3", t.Name()+"/foreign", "someone-else")
			groupOwners.record(foreign)
			o[SGGVR.Resource][string(foreign.GetUID())] = foreign
		}, ""},
		{"group object still terminating", func(t *testing.T, l *NormalLedger, o Objects) {
			now := metav1.Now()
			o[SGGVR.Resource][t.Name()+"/model-1"].SetDeletionTimestamp(&now)
		}, "ServingGroup still terminating: model-1"},
		{"group object missing", func(t *testing.T, l *NormalLedger, o Objects) {
			delete(o[SGGVR.Resource], t.Name()+"/model-1")
		}, "ServingGroup objects 2 want 3"},
		{"group inside the range never promoted", func(t *testing.T, l *NormalLedger, o Objects) {
			setSurge(o[SGGVR.Resource][t.Name()+"/model-2"], surgeServingGroup)
			for _, pod := range o["pods"] {
				if pod.GetLabels()[LabelGroup] == "model-2" {
					setSurge(pod, surgeServingGroup)
				}
			}
		}, "ServingGroup model-2 inside replicas=3 still carries the surge mark"},
		{"Pod keeps the copy of a removed mark", func(t *testing.T, l *NormalLedger, o Objects) {
			for _, pod := range o["pods"] {
				if pod.GetLabels()[LabelGroup] == "model-2" {
					setSurge(pod, surgeServingGroup)
				}
			}
		}, "surge copy=true differs from ServingGroup model-2 mark=false"},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, o := groupFixture(t, "RUN-062")
			c.change(t, l, o)
			ok, reason := l.groupObjectsSettled(o, l.Model.N)
			if (c.reason == "") != ok || !strings.Contains(reason, c.reason) {
				t.Fatalf("settled=%t reason=%q want %q", ok, reason, c.reason)
			}
		})
	}
	t.Run("surge Pod without the copy of its group's mark", func(t *testing.T) {
		l, o := groupFixture(t, "RUN-062")
		surge := testGroup("model-3", t.Name()+"/model-3", "owner")
		setSurge(surge, surgeServingGroup)
		groupOwners.record(surge)
		o[SGGVR.Resource][string(surge.GetUID())] = surge
		pod := normalTestPod("SG", "frontend", 3, "B", true, "entry")
		pod.SetOwnerReferences(groupRef("model-3", string(surge.GetUID())))
		o["pods"][string(pod.GetUID())] = pod
		if ok, reason := l.surgeMarksSettled(o); ok || !strings.Contains(reason, "surge copy=false differs from ServingGroup model-3 mark=true") {
			t.Fatalf("unsynchronized copy accepted: %t %q", ok, reason)
		}
		setSurge(pod, surgeServingGroup)
		if ok, reason := l.surgeMarksSettled(o); !ok {
			t.Fatalf("consistent surge outside the range rejected: %q", reason)
		}
	})
	t.Run("nothing is required without ServingGroup objects", func(t *testing.T) {
		l, o := normalFixture(t, "RUN-062")
		if ok, reason := l.groupObjectsSettled(o, 99); !ok {
			t.Fatalf("object checks ran for a build without ServingGroup objects: %q", reason)
		}
	})
}

func TestGroupStreamReducesSameNameRecreation(t *testing.T) {
	stream := newGroupStream()
	at := time.Unix(1, 0).UTC()
	first := testGroup("model-3", "first", "owner")
	setSurge(first, surgeServingGroup)
	stream.observe(1, at, "ADDED", first)
	_ = unstructured.SetNestedSlice(first.Object, []interface{}{
		map[string]interface{}{"type": "Ready", "status": "True", "reason": "AllInstancesReady"},
		map[string]interface{}{"type": "Failed", "status": "True", "reason": "MemberFaulty"},
	}, "status", "conditions")
	_ = unstructured.SetNestedField(first.Object, "revision-a", "spec", "revision")
	_ = unstructured.SetNestedField(first.Object, int64(2), "status", "pods")
	setInstances(first, GroupInstance{Role: "frontend", Phase: "Established"}, GroupInstance{Role: "backend", Phase: "Draining"})
	stream.observe(2, at, "MODIFIED", first)
	setSurge(first, "")
	stream.observe(3, at, "MODIFIED", first)
	now := metav1.Now()
	first.SetDeletionTimestamp(&now)
	first.SetFinalizers([]string{foregroundFinalizer})
	stream.observe(4, at, "MODIFIED", first)
	first.SetFinalizers(nil)
	stream.observe(5, at, "DELETED", first)
	stream.observe(6, at, "ADDED", testGroup("model-3", "second", "owner"))
	stream.observe(7, at, "ADDED", testGroup("model-4", "other", "owner"))

	generations := stream.Generations("model-3")
	if len(generations) != 2 || generations[0].UID != "first" || generations[1].UID != "second" {
		t.Fatalf("same-name objects: %+v", generations)
	}
	old, replacement := generations[0], generations[1]
	if old.FirstEvent != "ADDED" || old.SurgeAtCreation != surgeServingGroup || !old.SurgeEver || old.Promoted != 3 || old.DeletionSequence != 4 || !old.Foreground || old.DeletedSequence != 5 || old.OwnerUID != "owner" {
		t.Fatalf("reduced life of the first object: %+v", old)
	}
	if got := strings.Join(old.Conditions, " "); got != "Failed=True/MemberFaulty Ready=True/AllInstancesReady" {
		t.Fatalf("condition history: %q", got)
	}
	if got := strings.Join(old.Instances, " "); got != "backend/0:Draining(0) frontend/0:Established(0)" {
		t.Fatalf("role instance history: %q", got)
	}
	if !stream.Events[1].conditionTrue("Failed") || stream.Events[0].conditionTrue("Failed") || stream.Events[1].Revision != "revision-a" || stream.Events[1].Pods != 2 {
		t.Fatalf("reduced event: %+v", stream.Events[1])
	}
	if replacement.SurgeEver || replacement.DeletionSequence != 0 || replacement.Promoted != 0 || len(replacement.Instances) != 0 {
		t.Fatalf("replacement inherited facts of the previous object: %+v", replacement)
	}
	if summary := stream.Summary(); len(summary) != 3 || summary[2].Name != "model-4" {
		t.Fatalf("summary order: %+v", summary)
	}
	var none *GroupStream
	none.observe(1, at, "ADDED", first)
	if none.Summary() != nil || len(none.Generations("model-3")) != 0 {
		t.Fatal("a build without the stream must reduce to nothing")
	}
}

func TestFixturesFollowTheObjectModel(t *testing.T) {
	has := func(resources []controllerResource, resource, subresource string) bool {
		for _, r := range resources {
			if r.resource == resource && r.subresource == subresource {
				return true
			}
		}
		return false
	}
	without, with := controllerResources(false), controllerResources(true)
	if len(without) != 9 || has(without, SGGVR.Resource, "") || has(without, SGGVR.Resource, "status") {
		t.Fatalf("pause without ServingGroup objects changed: %+v", without)
	}
	if len(with) != 11 || !has(with, SGGVR.Resource, "") || !has(with, SGGVR.Resource, "status") {
		t.Fatalf("a controller with ServingGroup objects could keep creating and deleting groups while paused: %+v", with)
	}
	// Fixtures prove that a new spec was held with the first rule.
	if without[0] != (controllerResource{"modelservings", ""}) || with[0] != without[0] {
		t.Fatal("the ModelServing rule must stay first")
	}
	if partialScaleResource(false) != "podgroups" || partialScaleResource(true) != SGGVR.Resource {
		t.Fatal("partial scale boundary must reject the first object of a new group")
	}
	for _, c := range []struct {
		logs string
		want bool
	}{
		{"I1125 controller.go:1] initial sync has been done", true},
		{`I1125 run.go:124] "Starting the ModelServing controller" workers=5`, true},
		{`I1125 run.go:168] "Starting the ServingGroup controller" workers=5`, false},
		{`I1125 leaderelection.go:272] "Successfully acquired lease"`, false},
		{"", false},
	} {
		if controllerSynced(c.logs) != c.want {
			t.Errorf("controllerSynced(%q) != %t", c.logs, c.want)
		}
	}
}

func TestAPIRetryDeleteFaultTargetsTheServingGroupOnlyForGroupRollouts(t *testing.T) {
	var old corev1.Pod
	if err := convertPod(makePod("SG", "frontend", 2, "A", true, "true"), &old); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id           string
		groupObjects bool
		resource     string
		name         string
		uids         int
	}{
		{"RUN-456", false, "pods", old.Name, 1},
		{"RUN-456", true, SGGVR.Resource, "model-2", 0},
		// A Role rollout deletes Pods itself in both object models.
		{"RUN-460", false, "pods", old.Name, 1},
		{"RUN-460", true, "pods", old.Name, 1},
	} {
		rule, err := apiRetryRule("fault", "test", "owner", 2, c.id, &old, c.groupObjects)
		if err != nil || rule.Resource != c.resource || rule.Name != c.name || len(rule.UIDs) != c.uids || len(rule.Methods) != 1 || rule.Methods[0] != "DELETE" || rule.Count != 1 {
			t.Errorf("%s groupObjects=%t: %+v %v", c.id, c.groupObjects, rule, err)
		}
	}
	unlabeled := old.DeepCopy()
	delete(unlabeled.Labels, LabelGroup)
	if _, err := apiRetryRule("fault", "test", "owner", 2, "RUN-456", unlabeled, true); err == nil {
		t.Fatal("ServingGroup delete fault accepted without the group of the captured Pod")
	}
	for _, c := range []struct {
		id, subresource, method string
		owner, collection       bool
	}{
		{"RUN-663", "", "POST", true, false},
		{"RUN-664", "status", "PUT", true, false},
		{"RUN-665", "", "GET", false, true},
	} {
		rule, err := apiRetryRule("fault", "test", "owner", 2, c.id, nil, true)
		if err != nil || rule.Resource != SGGVR.Resource || rule.Subresource != c.subresource || len(rule.Methods) != 1 || rule.Methods[0] != c.method || (rule.OwnerUID == "owner") != c.owner || rule.CollectionOnly != c.collection || rule.Count != 1 {
			t.Errorf("%s: %+v %v", c.id, rule, err)
		}
		if _, err := apiRetryRule("fault", "test", "owner", 2, c.id, nil, false); err == nil {
			t.Errorf("%s: ServingGroup request fault accepted for a build without ServingGroup objects", c.id)
		}
	}
	if _, err := apiRetryRule("fault", "test", "owner", 2, "RUN-666", nil, true); err == nil {
		t.Error("undeclared API fault identity accepted")
	}
	for _, id := range []string{"RUN-455", "RUN-457", "RUN-458"} {
		a, errA := apiRetryRule("fault", "test", "owner", 2, id, &old, false)
		b, errB := apiRetryRule("fault", "test", "owner", 2, id, &old, true)
		if errA != nil || errB != nil || a.Resource != b.Resource || a.Subresource != b.Subresource {
			t.Errorf("%s must select the same request in both object models: %+v %+v", id, a, b)
		}
	}
}

func TestCommittedScopeEvidenceReplacesThePodMarker(t *testing.T) {
	pod := func(mode string) *corev1.Pod {
		var p corev1.Pod
		if err := convertPod(makePod(mode, "frontend", 2, "A", true, "true"), &p); err != nil {
			t.Fatal(err)
		}
		p.OwnerReferences = groupRef(p.Labels[LabelGroup], "group")
		return &p
	}
	group := func(name string, deleting bool, instances ...GroupInstance) *unstructured.Unstructured {
		g := testGroup(name, "group", "owner")
		if deleting {
			now := metav1.Now()
			g.SetDeletionTimestamp(&now)
		}
		setInstances(g, instances...)
		return g
	}
	draining := GroupInstance{Role: "frontend", Index: 2, Phase: "Draining"}
	replaced := group("model-2", true)
	replaced.SetUID("another-object")
	for _, c := range []struct {
		name  string
		mode  string
		group *unstructured.Unstructured
		want  string
	}{
		{"terminating ServingGroup", "SG", group("model-2", true), ""},
		{"ServingGroup not terminating", "SG", group("model-2", false), "is not terminating"},
		{"same-name object of another UID", "SG", replaced, "changed identity"},
		{"ServingGroup read failed", "SG", nil, "changed identity"},
		{"Role instance Draining", "Role", group("model-0", false, draining), ""},
		{"Role instance still Established", "Role", group("model-0", false, GroupInstance{Role: "frontend", Index: 2, Phase: "Established"}), "is Established, not Draining"},
		{"another Role instance Draining", "Role", group("model-0", false, GroupInstance{Role: "frontend", Index: 1, Phase: "Draining"}), "no record of Role instance frontend/2"},
		{"terminating group does not prove a Role scope", "Role", group("model-0", true), "no record of Role instance frontend/2"},
	} {
		err := committedScope(c.mode, c.group, pod(c.mode))
		if (c.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v want %q", c.name, err, c.want)
		}
	}
}

func TestCoreLedgerUsesTheServingGroupAsTheEarlyGroupSignal(t *testing.T) {
	deleting := func(name, uid string) *unstructured.Unstructured {
		o := &unstructured.Unstructured{}
		o.SetName(name)
		o.SetUID(types.UID(uid))
		return o
	}
	for _, c := range []struct {
		name      string
		groupKind string
		kind      string
		starts    int
	}{
		{"PodGroup without ServingGroup objects", PGGVR.Resource, PGGVR.Resource, 1},
		{"ServingGroup with ServingGroup objects", SGGVR.Resource, SGGVR.Resource, 1},
		{"PodGroup collected with ServingGroup objects", SGGVR.Resource, PGGVR.Resource, 0},
		{"ServingGroup event without ServingGroup objects", PGGVR.Resource, SGGVR.Resource, 0},
	} {
		l, pods := fixture(t, "RUN-001", 0)
		l.GroupKind = c.groupKind
		l.BaselinePG["model-2"] = "baseline"
		l.Observe(c.kind, "DELETED", deleting("model-2", "baseline"), pods)
		if len(l.PGStarted) != c.starts {
			t.Errorf("%s: group starts=%d want %d", c.name, len(l.PGStarted), c.starts)
		}
	}
	l, pods := fixture(t, "RUN-001", 0)
	l.GroupKind = SGGVR.Resource
	l.BaselinePG["model-2"], l.BaselinePG["model-1"] = "g2", "g1"
	l.Observe(SGGVR.Resource, "MODIFIED", func() *unstructured.Unstructured {
		o := deleting("model-2", "g2")
		now := metav1.Now()
		o.SetDeletionTimestamp(&now)
		return o
	}(), pods)
	l.Observe(SGGVR.Resource, "DELETED", deleting("model-1", "g1"), pods)
	hasViolation(t, l, "BUDGET_VIOLATION: ServingGroup starts")
}

func TestServingGroupRecoveryIsBoundToTheCapturedGroupObject(t *testing.T) {
	arm := func(t *testing.T) (*NormalLedger, Objects, PodFaultScope, error) {
		l, o := groupFixture(t, "RUN-062")
		pg := &unstructured.Unstructured{}
		pg.SetName("model-0")
		pg.SetNamespace("test")
		pg.SetUID(types.UID(t.Name() + "/pg-0"))
		pg.SetOwnerReferences(groupRef("model-0", t.Name()+"/model-0"))
		o["podgroups"][string(pg.GetUID())] = pg
		var pods []corev1.Pod
		target := ""
		for uid, object := range o["pods"] {
			var pod corev1.Pod
			if err := convertPod(object, &pod); err != nil {
				t.Fatal(err)
			}
			pods = append(pods, pod)
			if pod.Labels[LabelGroup] == "model-0" && target == "" {
				target = uid
			}
		}
		scope, err := capturePodFaultScope("owner", target, "ServingGroupRecreate", pods)
		if err != nil {
			t.Fatal(err)
		}
		return l, o, scope, nil
	}
	t.Run("captured object commits the scope when it starts terminating", func(t *testing.T) {
		l, o, scope, _ := arm(t)
		if err := l.armRecovery(scope, o); err != nil {
			t.Fatal(err)
		}
		record := l.Recoveries[0]
		if len(record.Groups) != 1 || record.Groups[t.Name()+"/model-0"] != "model-0" {
			t.Fatalf("captured ServingGroup: %+v", record.Groups)
		}
		l.Armed = true
		group := o[SGGVR.Resource][t.Name()+"/model-0"].DeepCopy()
		now := metav1.Now()
		group.SetDeletionTimestamp(&now)
		if !l.recoveryBefore(SGGVR.Resource, "MODIFIED", group, o) {
			t.Fatal("deletion of the captured ServingGroup was not attributed to its recovery")
		}
		for uid := range scope.RecoveryUIDs {
			if !l.Committed[uid] {
				t.Fatalf("recovery scope member %s not committed", uid)
			}
		}
		for uid := range scope.OutsideRecoveryUIDs {
			if l.Committed[uid] {
				t.Fatalf("member outside the recovery scope committed: %s", uid)
			}
		}
	})
	t.Run("same-name replacement object is not the captured scope", func(t *testing.T) {
		l, o, scope, _ := arm(t)
		if err := l.armRecovery(scope, o); err != nil {
			t.Fatal(err)
		}
		replacement := testGroup("model-0", t.Name()+"/model-0-new", "owner")
		groupOwners.record(replacement)
		if l.recoveryBefore(SGGVR.Resource, "DELETED", replacement, o) {
			t.Fatal("recovery permission moved to a same-name ServingGroup of another UID")
		}
	})
	t.Run("group object must exist when the fault is armed", func(t *testing.T) {
		l, o, scope, _ := arm(t)
		delete(o[SGGVR.Resource], t.Name()+"/model-0")
		if err := l.armRecovery(scope, o); err == nil || !strings.Contains(err.Error(), "ServingGroup UID") {
			t.Fatalf("recovery armed without its ServingGroup object: %v", err)
		}
	})
}
