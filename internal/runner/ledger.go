// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	LabelGroup  = "modelserving.volcano.sh/group-name"
	LabelRole   = "modelserving.volcano.sh/role"
	LabelRoleID = "modelserving.volcano.sh/role-id"
	LabelEntry  = "modelserving.volcano.sh/entry"
)

type Unit struct {
	Key     string        `json:"key"`
	Ordinal int           `json:"ordinal"`
	Version string        `json:"version"`
	Pods    []*corev1.Pod `json:"-"`
	Ready   bool          `json:"ready"`
	Active  bool          `json:"active"`
}
type Metrics struct {
	At             time.Time `json:"at"`
	Starts         int       `json:"starts"`
	PGStarts       int       `json:"podGroupStarts"`
	TargetReady    int       `json:"targetReady"`
	CommittedReady int       `json:"committedReady"`
	Active         int       `json:"active"`
	Old            int       `json:"old"`
	New            int       `json:"new"`
	Terminating    int       `json:"terminatingPods"`
}
type Ledger struct {
	Case          Case
	Owner         string
	Baseline      map[string]Unit
	ProtectedUIDs map[string]string
	BaselinePG    map[string]string
	PodUnit       map[string]string
	Started       map[string]bool
	PGStarted     map[string]bool
	Released      map[string]bool
	ReleasedUnits map[string]bool
	StartSequence []int
	Violations    []string
	History       []Metrics
	last          Metrics
}

func ordinal(s string) int {
	n, e := strconv.Atoi(s[strings.LastIndex(s, "-")+1:])
	if e != nil {
		return -1
	}
	return n
}
func podVersion(p *corev1.Pod) string {
	for _, c := range p.Spec.Containers {
		for _, v := range c.Env {
			if v.Name == "ROLLOUT_VERSION" {
				return v.Value
			}
		}
	}
	return ""
}
func podReady(p *corev1.Pod) bool {
	if p.DeletionTimestamp != nil {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
func owned(p *corev1.Pod, uid string) bool {
	for _, r := range p.OwnerReferences {
		if string(r.UID) == uid {
			return true
		}
	}
	return false
}
func (l *Ledger) units(objects map[string]*unstructured.Unstructured) map[string]Unit {
	groups := map[string]Unit{}
	for _, o := range objects {
		var p corev1.Pod
		if runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &p) != nil || !owned(&p, l.Owner) {
			continue
		}
		if l.Case.Expect.Mode == "Role" && p.Labels[LabelRole] != "frontend" {
			continue
		}
		key := p.Labels[LabelGroup]
		ord := ordinal(key)
		if l.Case.Expect.Mode == "Role" {
			key += "/" + p.Labels[LabelRole] + "/" + p.Labels[LabelRoleID]
			ord = ordinal(p.Labels[LabelRoleID])
		}
		unit, exists := groups[key]
		if !exists {
			unit = Unit{Key: key, Ordinal: ord, Version: podVersion(&p), Ready: true}
		}
		if unit.Version != podVersion(&p) {
			unit.Version = "mixed"
		}
		unit.Pods = append(unit.Pods, &p)
		unit.Ready = unit.Ready && podReady(&p)
		unit.Active = unit.Active || p.DeletionTimestamp == nil
		groups[key] = unit
	}
	expected := l.expectedPods()
	for key, u := range groups {
		if len(u.Pods) != expected || u.Version == "mixed" {
			u.Ready = false
		}
		groups[key] = u
	}
	return groups
}
func (l *Ledger) expectedPods() int {
	roles := l.Case.Input.Spec["template"].(map[string]interface{})["roles"].([]interface{})
	n := 0
	for _, r := range roles {
		role := r.(map[string]interface{})
		if l.Case.Expect.Mode == "Role" && role["name"] != "frontend" {
			continue
		}
		replicas := int(role["replicas"].(float64))
		if l.Case.Expect.Mode == "Role" {
			replicas = 1
		}
		n += replicas * (1 + int(role["workerReplicas"].(float64)))
	}
	return n
}
func NewLedger(c Case, owner string, pods, pgs map[string]*unstructured.Unstructured) (*Ledger, error) {
	l := &Ledger{Case: c, Owner: owner, Baseline: map[string]Unit{}, ProtectedUIDs: map[string]string{}, BaselinePG: map[string]string{}, PodUnit: map[string]string{}, Started: map[string]bool{}, PGStarted: map[string]bool{}, Released: map[string]bool{}}
	l.ReleasedUnits = map[string]bool{}
	l.Baseline = l.units(pods)
	if err := l.endpointIdentities(pods); err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}
	if len(l.Baseline) != c.Expect.Desired {
		return nil, fmt.Errorf("baseline unit count %d", len(l.Baseline))
	}
	for key, u := range l.Baseline {
		if !u.Ready || u.Version != "A" || u.Ordinal < 0 || u.Ordinal >= c.Expect.Desired {
			return nil, fmt.Errorf("invalid baseline %s", key)
		}
		for _, p := range u.Pods {
			l.PodUnit[string(p.UID)] = key
			if u.Ordinal < c.Expect.P {
				l.ProtectedUIDs[string(p.UID)] = p.Name
			}
		}
	}
	for _, o := range pods {
		role := o.GetLabels()[LabelRole]
		for _, name := range c.Expect.UnchangedRoles {
			if role == name {
				l.ProtectedUIDs[string(o.GetUID())] = o.GetName()
			}
		}
	}
	for _, o := range pgs {
		l.BaselinePG[o.GetName()] = string(o.GetUID())
	}
	return l, nil
}
func (l *Ledger) fail(msg string) {
	for _, v := range l.Violations {
		if v == msg {
			return
		}
	}
	l.Violations = append(l.Violations, msg)
}
func (l *Ledger) Observe(kind, event string, o *unstructured.Unstructured, pods map[string]*unstructured.Unstructured) {
	deleting := event == "DELETED" || o.GetDeletionTimestamp() != nil
	if kind == "pods" && deleting {
		uid := string(o.GetUID())
		if name, ok := l.ProtectedUIDs[uid]; ok {
			l.fail("PROTECTED_REPLACED: " + name)
		}
		if key, ok := l.PodUnit[uid]; ok && !l.Started[key] {
			l.Started[key] = true
			ord := l.Baseline[key].Ordinal
			l.StartSequence = append(l.StartSequence, ord)
			n := len(l.StartSequence) - 1
			if n >= len(l.Case.Expect.StartOrder) || ord != l.Case.Expect.StartOrder[n] {
				l.fail(fmt.Sprintf("ORDER_MISMATCH: observed %v expected %v", l.StartSequence, l.Case.Expect.StartOrder))
			}
		}
	}
	if kind == "podgroups" && deleting && l.BaselinePG[o.GetName()] == string(o.GetUID()) {
		if l.Case.Expect.Mode == "SG" {
			l.PGStarted[o.GetName()] = true
			if u, ok := l.Baseline[o.GetName()]; ok && u.Ordinal < l.Case.Expect.P {
				l.fail("PROTECTED_PODGROUP_REPLACED: " + o.GetName())
			}
			// Cross-resource Ready notifications can lag PG deletion. This bound uses
			// only the maximum capacity explicitly released by our workload driver.
			// Precise Ready-before-delete accounting is performed on the Pod stream.
			if len(l.PGStarted) > l.Case.Expect.U+len(l.ReleasedUnits) {
				l.fail("BUDGET_VIOLATION: PodGroup starts exceed even released-capacity ceiling")
			}
		} else {
			l.fail("UNEXPECTED_PODGROUP_REPLACED: " + o.GetName())
		}
	}
	l.Check(pods)
}
func (l *Ledger) Check(pods map[string]*unstructured.Unstructured) Metrics {
	m := Metrics{At: time.Now().UTC(), Starts: len(l.Started), PGStarts: len(l.PGStarted)}
	units := l.units(pods)
	for _, u := range units {
		if u.Active {
			m.Active++
		}
		for _, p := range u.Pods {
			if p.DeletionTimestamp != nil {
				m.Terminating++
			}
			// The test controls each target Pod's readiness by UID. A Ready
			// target before release invalidates the experiment even if its
			// capacity happens to keep the rolling budget within bounds.
			if podVersion(p) == "B" && podReady(p) && !l.Released[string(p.UID)] {
				l.fail("CONTROL_VIOLATION: target Ready before release: " + p.Name)
			}
		}
		if u.Version == "B" {
			m.New++
			if u.Ready {
				m.TargetReady++
			}
			if l.Case.Expect.FinalNew == 0 {
				l.fail("PROTECTED_SURGE_CREATED: B under full partition")
			}
		} else if u.Version == "A" {
			m.Old++
		}
	}
	// Keep the deleted old identity in Started even while its slot is absent.
	m.CommittedReady = l.Case.Expect.Desired - m.Starts + m.TargetReady
	if m.CommittedReady < max(l.Case.Expect.Desired-l.Case.Expect.U, 0) {
		l.fail(fmt.Sprintf("BUDGET_VIOLATION: K=%d T=%d committedReady=%d minimum=%d", m.Starts, m.TargetReady, m.CommittedReady, max(l.Case.Expect.Desired-l.Case.Expect.U, 0)))
	}
	if m.Active > l.Case.Expect.Desired+l.Case.Expect.S {
		l.fail(fmt.Sprintf("SURGE_VIOLATION: active=%d ceiling=%d", m.Active, l.Case.Expect.Desired+l.Case.Expect.S))
	}
	// Also verify physical availability: an unexpected Ready loss cannot be
	// disguised by counting every unstarted baseline slot as still healthy.
	available := 0
	for key, u := range units {
		if u.Ready && (u.Version == "B" || !l.Started[key]) {
			available++
		}
	}
	if available < max(l.Case.Expect.Desired-l.Case.Expect.U, 0) {
		l.fail(fmt.Sprintf("AVAILABILITY_VIOLATION: ready=%d minimum=%d", available, max(l.Case.Expect.Desired-l.Case.Expect.U, 0)))
	}
	comparable := m
	comparable.At = time.Time{}
	prev := l.last
	prev.At = time.Time{}
	if comparable != prev || len(l.History) == 0 {
		l.History = append(l.History, m)
		l.last = m
	}
	return m
}
func (l *Ledger) Candidates(pods map[string]*unstructured.Unstructured) []Unit {
	var out []Unit
	for _, u := range l.units(pods) {
		if u.Version != "B" || u.Ready || !u.Active || len(u.Pods) != l.expectedPods() {
			continue
		}
		good := true
		for _, p := range u.Pods {
			if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning || l.Released[string(p.UID)] {
				good = false
			}
		}
		if good {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ordinal > out[j].Ordinal })
	return out
}
