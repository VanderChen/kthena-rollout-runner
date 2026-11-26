// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

const revisionLabel = "modelserving.volcano.sh/revision"

// layoutChange is normal RUN-126 after its update was accepted: only frontend
// gains a worker, so backend and frontend entry Pods carry version A in both
// revisions and the worker Pod alone carries B. The surge group model-3 is
// complete and Ready. withObjects selects the object model of the build.
type layoutChange struct {
	t           *testing.T
	l           *NormalLedger
	o           Objects
	withObjects bool
}

func newLayoutChange(t *testing.T, withObjects bool) *layoutChange {
	t.Helper()
	c := &layoutChange{t: t, withObjects: withObjects}
	if withObjects {
		c.l, c.o = groupFixture(t, "RUN-126")
		for _, group := range c.o[SGGVR.Resource] {
			setRevision(group, "fixture-A")
		}
	} else {
		c.l, c.o = normalFixture(t, "RUN-126")
	}
	step := normalCase(t, "RUN-126").Scenario.Steps[0]
	if err := c.l.Transition(step.Spec, step.Name, step.Expect, c.o); err != nil {
		t.Fatal(err)
	}
	// The observer learns this from the ControllerRevision of the new template.
	c.l.RevisionLayouts["fixture-B"] = c.l.Model
	c.group("model-3", "model-3", "fixture-B", surgeServingGroup)
	for _, member := range [][3]string{{"backend", "entry", "A"}, {"frontend", "entry", "A"}, {"frontend", "worker-1", "B"}} {
		c.pod("model-3", "model-3", member[0], member[1], member[2], "fixture-B", true, surgeServingGroup)
	}
	requireNoNormalViolation(t, c.l)
	return c
}

func setRevision(group *unstructured.Unstructured, revision string) {
	_ = unstructured.SetNestedField(group.Object, revision, "spec", "revision")
}

func (c *layoutChange) uid(object string) string { return c.t.Name() + "/" + object }

// group creates a ServingGroup object; on a build without them it does nothing.
func (c *layoutChange) group(name, object, revision, surge string) {
	if !c.withObjects {
		return
	}
	group := testGroup(name, c.uid(object), "owner")
	setRevision(group, revision)
	setSurge(group, surge)
	observeGroup(c.l, c.o, "ADDED", group)
}

// pod creates one member. object is the ServingGroup object that controls it.
func (c *layoutChange) pod(group, object, role, member, version, revision string, ready bool, surge string) {
	p := normalTestPod("Role", role, 0, version, ready, member)
	labels := p.GetLabels()
	labels[LabelGroup], labels[revisionLabel] = group, revision
	p.SetLabels(labels)
	p.SetName(object + "-" + role + "-" + member)
	p.SetUID(types.UID(c.uid(p.GetName())))
	if surge != "" {
		p.SetAnnotations(map[string]string{AnnotationSurge: surge})
	}
	if c.withObjects {
		p.SetOwnerReferences(groupRef(group, c.uid(object)))
	}
	if ready {
		c.l.Released[string(p.GetUID())] = true
	}
	observePod(c.l, c.o, "ADDED", p)
}

// start is the controller's first destructive step on an old group: the
// deletionTimestamp of its ServingGroup object, or of its Pods on a build
// without the objects.
func (c *layoutChange) start(group int) {
	now := metav1.Now()
	if c.withObjects {
		for _, object := range c.o[SGGVR.Resource] {
			if ordinal(object.GetName()) == group && object.GetDeletionTimestamp() == nil {
				deleting := object.DeepCopy()
				deleting.SetDeletionTimestamp(&now)
				observeGroup(c.l, c.o, "MODIFIED", deleting)
			}
		}
	}
	for _, pod := range c.o["pods"] {
		if ordinal(pod.GetLabels()[LabelGroup]) == group && pod.GetDeletionTimestamp() == nil {
			deleting := pod.DeepCopy()
			deleting.SetDeletionTimestamp(&now)
			observePod(c.l, c.o, "MODIFIED", deleting)
		}
	}
}

// gone removes the terminating members and object of a group.
func (c *layoutChange) gone(group int) {
	for _, pod := range c.o["pods"] {
		if ordinal(pod.GetLabels()[LabelGroup]) == group && pod.GetDeletionTimestamp() != nil {
			observePod(c.l, c.o, "DELETED", pod)
		}
	}
	for _, object := range c.o[SGGVR.Resource] {
		if ordinal(object.GetName()) == group && object.GetDeletionTimestamp() != nil {
			observeGroup(c.l, c.o, "DELETED", object)
		}
	}
}

// replaceGroup2 rolls old group 2 and creates its replacement as the
// ServingGroup controller does: the object first, then the Pods one by one.
// The replacement is left with its backend and frontend entry Pods, both of
// version A; its worker Pod, the only one of version B, does not exist yet.
func (c *layoutChange) replaceGroup2(objectRevision, podRevision string) {
	c.start(2)
	c.gone(2)
	c.group("model-2", "model-2-new", objectRevision, "")
	c.pod("model-2", "model-2-new", "backend", "entry", "A", podRevision, false, "")
	c.pod("model-2", "model-2-new", "frontend", "entry", "A", podRevision, false, "")
	requireNoNormalViolation(c.t, c.l)
}

func orderViolations(l *NormalLedger) []string {
	var out []string
	for _, violation := range l.Violations {
		if strings.Contains(violation, "ORDER_MISMATCH") {
			out = append(out, violation)
		}
	}
	return out
}

// With ServingGroup objects the replacement of group 2 is a unit of the new
// template from the moment its object exists. Rolling old group 1 next is the
// contract's order (2, then 1) and must not be reported.
func TestReplacementGroupWithoutItsChangedPodIsNotAnOldCandidate(t *testing.T) {
	c := newLayoutChange(t, true)
	c.replaceGroup2("fixture-B", "fixture-B")
	c.start(1)
	requireNoNormalViolation(t, c.l)
	if len(c.l.Starts) != 2 || c.l.Starts[0].Ordinal != 2 || c.l.Starts[1].Ordinal != 1 {
		t.Fatalf("starts: %+v", c.l.Starts)
	}
}

// Without ServingGroup objects nothing but the Pods can say what a unit is.
// The ledger reads them exactly as before: the same Pods are an old unit that
// is not Ready, and it is reported.
func TestUnitIsReadFromItsPodsWithoutServingGroupObjects(t *testing.T) {
	c := newLayoutChange(t, false)
	c.replaceGroup2("", "fixture-B")
	c.start(1)
	got := orderViolations(c.l)
	if len(got) != 1 || !strings.Contains(got[0], "SG started 1 before eligible 2") {
		t.Fatalf("order violations without ServingGroup objects: %v (all: %v)", got, c.l.Violations)
	}
}

// The object is believed only when it is a live object of the case at a
// recorded revision of the current template and no Pod contradicts it.
func TestReplacementGroupIsReadFromItsPodsWhenTheObjectDoesNotSettleIt(t *testing.T) {
	for _, c := range []struct {
		name           string
		objectRevision string
		podRevision    string
		spoil          func(*layoutChange)
	}{
		{"object at the old revision", "fixture-A", "fixture-A", nil},
		{"object at a revision the runner never saw recorded", "unrecorded", "unrecorded", nil},
		{"object names the new revision, its Pods the old one", "fixture-B", "fixture-A", nil},
		{"object without a revision", "", "fixture-B", nil},
		{"object is terminating", "fixture-B", "fixture-B", func(c *layoutChange) {
			now := metav1.Now()
			c.o[SGGVR.Resource][c.uid("model-2-new")].SetDeletionTimestamp(&now)
		}},
		{"object is gone from the cache", "fixture-B", "fixture-B", func(c *layoutChange) {
			delete(c.o[SGGVR.Resource], c.uid("model-2-new"))
		}},
		{"recorded revision is not the current template", "fixture-B", "fixture-B", func(c *layoutChange) {
			c.l.RevisionLayouts["fixture-B"] = c.l.History[0]
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			state := newLayoutChange(t, true)
			state.replaceGroup2(c.objectRevision, c.podRevision)
			if c.spoil != nil {
				c.spoil(state)
			}
			state.start(1)
			got := orderViolations(state.l)
			if len(got) != 1 || !strings.Contains(got[0], "SG started 1 before eligible 2") {
				t.Fatalf("order violations: %v (all: %v)", got, state.l.Violations)
			}
		})
	}
}

// The order of old candidates is checked as before on a build with
// ServingGroup objects: rolling group 0 while old groups 1 and 2 are Ready
// is wrong, with or without a replacement elsewhere.
func TestOldCandidateOrderIsStillCheckedWithServingGroupObjects(t *testing.T) {
	t.Run("lowest old group first", func(t *testing.T) {
		c := newLayoutChange(t, true)
		c.start(0)
		got := orderViolations(c.l)
		if len(got) == 0 || !strings.Contains(strings.Join(got, "; "), "SG started 0 before eligible 2") {
			t.Fatalf("order violations: %v (all: %v)", got, c.l.Violations)
		}
	})
	t.Run("old group 0 before old group 1 while the replacement of group 2 is being created", func(t *testing.T) {
		c := newLayoutChange(t, true)
		c.replaceGroup2("fixture-B", "fixture-B")
		c.start(0)
		got := orderViolations(c.l)
		if len(got) != 1 || !strings.Contains(got[0], "SG started 0 before eligible 1") {
			t.Fatalf("order violations: %v (all: %v)", got, c.l.Violations)
		}
	})
}
