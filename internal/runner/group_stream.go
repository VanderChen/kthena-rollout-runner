// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GroupInstance is one entry of ServingGroup.status.roleInstances: the record
// that says whether a Role instance was established, is being drained, or is
// being created again, and how many times it has been drained.
type GroupInstance struct {
	Role       string `json:"role"`
	Index      int    `json:"index"`
	Phase      string `json:"phase"`
	Generation int64  `json:"generation"`
}

// GroupEvent is one observation of a ServingGroup object, reduced to the facts
// that cases assert on. The complete object stays in observations.jsonl.
type GroupEvent struct {
	Sequence   int64             `json:"sequence"`
	At         time.Time         `json:"at"`
	Event      string            `json:"event"`
	Name       string            `json:"name"`
	UID        string            `json:"uid"`
	OwnerUID   string            `json:"ownerUID,omitempty"`
	Generation int64             `json:"generation"`
	Surge      string            `json:"surge,omitempty"`
	Deleting   bool              `json:"deleting,omitempty"`
	Foreground bool              `json:"foregroundDeletion,omitempty"`
	Revision   string            `json:"revision,omitempty"`
	Conditions map[string]string `json:"conditions,omitempty"`
	Instances  []GroupInstance   `json:"roleInstances,omitempty"`
	Pods       int64             `json:"pods"`
	ReadyPods  int64             `json:"readyPods"`
}

// GroupObject is the reduced life of one ServingGroup UID. A same-name
// recreation is a second GroupObject with the same Name and another UID.
type GroupObject struct {
	Name     string `json:"name"`
	UID      string `json:"uid"`
	OwnerUID string `json:"ownerUID,omitempty"`
	// First is the sequence of the first observation; FirstEvent tells whether
	// the creation itself was observed (ADDED) or the object already existed.
	First            int64      `json:"firstSequence"`
	FirstEvent       string     `json:"firstEvent"`
	SurgeAtCreation  string     `json:"surgeAtCreation,omitempty"`
	SurgeEver        bool       `json:"surgeEver,omitempty"`
	Promoted         int64      `json:"promotedSequence,omitempty"`
	DeletionSequence int64      `json:"deletionRequestedSequence,omitempty"`
	DeletionAt       *time.Time `json:"deletionRequestedAt,omitempty"`
	Foreground       bool       `json:"foregroundDeletion,omitempty"`
	DeletedSequence  int64      `json:"deletedSequence,omitempty"`
	DeletedAt        *time.Time `json:"deletedAt,omitempty"`
	// Conditions lists every "Type=Status/Reason" value in first-seen order;
	// Instances lists every "role/index:Phase(generation)" value likewise.
	Conditions []string   `json:"conditionHistory,omitempty"`
	Instances  []string   `json:"roleInstanceHistory,omitempty"`
	Last       GroupEvent `json:"last"`
}

// GroupStream is the fifth observation stream: every ServingGroup event of the
// case namespace in arrival order. It is nil-safe; a build without ServingGroup
// objects simply has no stream.
type GroupStream struct {
	Events  []GroupEvent            `json:"events"`
	Objects map[string]*GroupObject `json:"-"`
	order   []string
}

func newGroupStream() *GroupStream { return &GroupStream{Objects: map[string]*GroupObject{}} }

const foregroundFinalizer = "foregroundDeletion"

func groupEvent(sequence int64, at time.Time, event string, o *unstructured.Unstructured) GroupEvent {
	e := GroupEvent{Sequence: sequence, At: at, Event: event, Name: o.GetName(), UID: string(o.GetUID()), Generation: o.GetGeneration(), Surge: o.GetAnnotations()[AnnotationSurge], Deleting: o.GetDeletionTimestamp() != nil}
	for _, ref := range o.GetOwnerReferences() {
		if ref.Controller != nil && *ref.Controller {
			e.OwnerUID = string(ref.UID)
		}
	}
	for _, f := range o.GetFinalizers() {
		e.Foreground = e.Foreground || f == foregroundFinalizer
	}
	spec, status := mapValue(o.Object, "spec"), mapValue(o.Object, "status")
	e.Revision = textValue(spec, "revision")
	if e.Revision == "" {
		e.Revision = textValue(spec, "targetRevision")
	}
	e.Pods, _, _ = unstructured.NestedInt64(status, "pods")
	e.ReadyPods, _, _ = unstructured.NestedInt64(status, "readyPods")
	for _, raw := range listValue(status, "conditions") {
		c, ok := raw.(map[string]interface{})
		if !ok || textValue(c, "type") == "" {
			continue
		}
		if e.Conditions == nil {
			e.Conditions = map[string]string{}
		}
		e.Conditions[textValue(c, "type")] = textValue(c, "status") + "/" + textValue(c, "reason")
	}
	for _, raw := range listValue(status, "roleInstances") {
		i, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		index, _, _ := unstructured.NestedInt64(i, "index")
		generation, _, _ := unstructured.NestedInt64(i, "generation")
		e.Instances = append(e.Instances, GroupInstance{Role: textValue(i, "role"), Index: int(index), Phase: textValue(i, "phase"), Generation: generation})
	}
	sort.Slice(e.Instances, func(a, b int) bool {
		if e.Instances[a].Role != e.Instances[b].Role {
			return e.Instances[a].Role < e.Instances[b].Role
		}
		return e.Instances[a].Index < e.Instances[b].Index
	})
	return e
}

func (i GroupInstance) String() string {
	return fmt.Sprintf("%s/%d:%s(%d)", i.Role, i.Index, i.Phase, i.Generation)
}

func appendNew(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}

// conditionTrue reports whether the event carries the condition with status True.
func (e GroupEvent) conditionTrue(kind string) bool {
	value := e.Conditions[kind]
	return len(value) >= 5 && value[:5] == "True/"
}

func (s *GroupStream) observe(sequence int64, at time.Time, event string, o *unstructured.Unstructured) {
	if s == nil {
		return
	}
	e := groupEvent(sequence, at, event, o)
	s.Events = append(s.Events, e)
	g := s.Objects[e.UID]
	if g == nil {
		g = &GroupObject{Name: e.Name, UID: e.UID, OwnerUID: e.OwnerUID, First: sequence, FirstEvent: event, SurgeAtCreation: e.Surge}
		s.Objects[e.UID] = g
		s.order = append(s.order, e.UID)
	}
	if e.Surge != "" {
		g.SurgeEver = true
	} else if g.Last.Surge != "" && g.Promoted == 0 && event != "DELETED" && !e.Deleting {
		g.Promoted = sequence
	}
	if e.Deleting && g.DeletionSequence == 0 {
		g.DeletionSequence = sequence
		g.DeletionAt = &at
	}
	g.Foreground = g.Foreground || e.Deleting && e.Foreground
	if event == "DELETED" && g.DeletedSequence == 0 {
		g.DeletedSequence = sequence
		g.DeletedAt = &at
	}
	kinds := make([]string, 0, len(e.Conditions))
	for kind := range e.Conditions {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		g.Conditions = appendNew(g.Conditions, kind+"="+e.Conditions[kind])
	}
	for _, instance := range e.Instances {
		g.Instances = appendNew(g.Instances, instance.String())
	}
	g.Last = e
}

// Generations returns every object observed under one name, oldest first.
func (s *GroupStream) Generations(name string) []*GroupObject {
	var out []*GroupObject
	if s == nil {
		return out
	}
	for _, uid := range s.order {
		if s.Objects[uid].Name == name {
			out = append(out, s.Objects[uid])
		}
	}
	return out
}

// Summary lists the objects in first-seen order for result.json and reports.
func (s *GroupStream) Summary() []GroupObject {
	if s == nil {
		return nil
	}
	out := make([]GroupObject, 0, len(s.order))
	for _, uid := range s.order {
		out = append(out, *s.Objects[uid])
	}
	return out
}

// saveGroupStream writes the reduced ServingGroup stream next to the raw
// journal and attaches its summary to the case result.
func (o *Observer) saveGroupStream(dir string, res *Result) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.groupStream == nil {
		return nil
	}
	res.ServingGroups = o.groupStream.Summary()
	return writeJSON(filepath.Join(dir, "servinggroups.json"), o.groupStream)
}
