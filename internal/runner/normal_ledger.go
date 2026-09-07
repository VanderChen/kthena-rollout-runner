// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Objects = map[string]map[string]*unstructured.Unstructured

type NormalUnit struct {
	Key      string        `json:"key"`
	Scope    string        `json:"scope"`
	Group    int           `json:"group"`
	Role     string        `json:"role,omitempty"`
	Ordinal  int           `json:"ordinal"`
	Version  string        `json:"version"`
	Workers  int           `json:"workers"`
	Ready    bool          `json:"ready"`
	Complete bool          `json:"complete"`
	Active   bool          `json:"active"`
	Pods     []*corev1.Pod `json:"-"`
}
type NormalStart struct {
	At          time.Time `json:"at"`
	Phase       string    `json:"phase"`
	Key         string    `json:"key"`
	Scope       string    `json:"scope"`
	Group       int       `json:"group"`
	Role        string    `json:"role"`
	Ordinal     int       `json:"ordinal"`
	Version     string    `json:"version"`
	Reason      string    `json:"reason"`
	ReadyBefore int       `json:"readyBefore"`
	Minimum     int       `json:"minimum"`
	UIDs        []string  `json:"uids"`
}
type ScopeMetric struct {
	Scope       string `json:"scope"`
	Desired     int    `json:"desired"`
	Unavailable int    `json:"maxUnavailable"`
	Surge       int    `json:"maxSurge"`
	Partition   int    `json:"partition"`
	Active      int    `json:"active"`
	Ready       int    `json:"ready"`
}
type NormalLedger struct {
	PGScale              map[string]bool        `json:"podGroupScaleIntentUIDs"`
	PGPhase              map[string]string      `json:"podGroupIntentPhase"`
	BornRanktable        map[string]bool        `json:"ranktableAtPodCreation"`
	Served               map[string]bool        `json:"previouslyReadyUIDs"`
	GroupShrinkRemaining int                    `json:"groupShrinkRemaining"`
	RoleShrinkRemaining  map[string]int         `json:"roleShrinkRemaining"`
	RevisionLayouts      map[string]NormalModel `json:"-"`
	PGCommitted          map[string]bool        `json:"podGroupCommittedUIDs"`
	PGPods               map[string]bool        `json:"podGroupCommittedPodUIDs"`
	RevisionData         map[string]string      `json:"revisionData"`
	Base                 NormalModel            `json:"-"`
	Owner                string                 `json:"owner"`
	Profile              string                 `json:"profile"`
	Phase                string                 `json:"phase"`
	Model                NormalModel            `json:"-"`
	History              []NormalModel          `json:"-"`
	Released             map[string]bool        `json:"released"`
	Committed            map[string]bool        `json:"committedUIDs"`
	Protected            map[string]string      `json:"protectedUIDs"`
	Starts               []NormalStart          `json:"starts"`
	Violations           []string               `json:"violations"`
	ScaleUIDs            map[string]bool        `json:"scaleUIDs"`
	RoleScaleIntents     map[string]string      `json:"roleScaleIntentPodUIDs,omitempty"`
	ScaleGroups          map[int]bool           `json:"scaleGroups"`
	Ceiling              map[string]int         `json:"ceiling"`
	Armed                bool                   `json:"armed"`
	NoNewRevision        bool                   `json:"noNewRevision"`
	Revisions            map[string]bool        `json:"revisions"`
	Expected             ScenarioExpectation    `json:"-"`
}

func newNormalLedger(spec map[string]interface{}, owner, profile string) (*NormalLedger, error) {
	m, e := readModel(spec)
	if e != nil {
		return nil, e
	}
	return &NormalLedger{PGScale: map[string]bool{}, PGPhase: map[string]string{}, BornRanktable: map[string]bool{}, Served: map[string]bool{}, RevisionLayouts: map[string]NormalModel{}, PGCommitted: map[string]bool{}, PGPods: map[string]bool{}, RevisionData: map[string]string{}, Owner: owner, Profile: profile, Model: m, Base: m, History: []NormalModel{m}, Released: map[string]bool{}, Committed: map[string]bool{}, Protected: map[string]string{}, ScaleUIDs: map[string]bool{}, ScaleGroups: map[int]bool{}, Ceiling: map[string]int{}, Revisions: map[string]bool{}}, nil
}
func (l *NormalLedger) fail(s string) {
	for _, v := range l.Violations {
		if s == v {
			return
		}
	}
	l.Violations = append(l.Violations, l.Phase+": "+s)
}
func podIsEntry(p *corev1.Pod) bool { return p.Labels[LabelEntry] == "true" }
func podHasEntryIdentity(p *corev1.Pod) bool {
	marker := p.Labels[LabelEntry]
	// Production labels entries explicitly and leaves the marker absent on
	// workers, whose generated names end in a positive Pod index.
	return marker == "true" || marker == "false" || (marker == "" && ordinal(p.Name) > 0)
}
func roleUnitKey(p *corev1.Pod) string {
	return p.Labels[LabelGroup] + "/" + p.Labels[LabelRole] + "/" + p.Labels[LabelRoleID]
}
func (l *NormalLedger) roleUnits(objects map[string]*unstructured.Unstructured) map[string]NormalUnit {
	out := map[string]NormalUnit{}
	for _, o := range objects {
		var p corev1.Pod
		if convertPod(o, &p) != nil || !owned(&p, l.Owner) {
			continue
		}
		key := roleUnitKey(&p)
		u, exists := out[key]
		if !exists {
			u = NormalUnit{Key: key, Scope: p.Labels[LabelGroup] + "/" + p.Labels[LabelRole], Group: ordinal(p.Labels[LabelGroup]), Role: p.Labels[LabelRole], Ordinal: ordinal(p.Labels[LabelRoleID]), Ready: true}
		}
		u.Pods = append(u.Pods, &p)
		u.Active = u.Active || p.DeletionTimestamp == nil
		u.Ready = u.Ready && podReady(&p) && !l.Committed[string(p.UID)]
		if podIsEntry(&p) {
			u.Version = podVersion(&p)
		} else {
			u.Workers++
		}
		out[key] = u
	}
	for key, u := range out {
		layouts := l.History
		for _, p := range u.Pods {
			if podIsEntry(p) && p.Labels["modelserving.volcano.sh/revision"] != "" {
				layouts = nil
				if m, ok := l.RevisionLayouts[p.Labels["modelserving.volcano.sh/revision"]]; ok {
					layouts = []NormalModel{m}
				}
				break
			}
		}
		for i := len(layouts) - 1; i >= 0; i-- {
			if r, ok := layouts[i].Roles[u.Role]; ok && roleMatches(u, r) {
				u.Complete = true
				break
			}
		}
		u.Ready = u.Ready && u.Complete
		out[key] = u
	}
	return out
}
func roleMatches(u NormalUnit, r RoleLayout) bool {
	if len(u.Pods) != 1+r.W {
		return false
	}
	entries := 0
	workers := 0
	for _, p := range u.Pods {
		if podIsEntry(p) {
			entries++
			if podVersion(p) != r.Entry {
				return false
			}
		} else {
			workers++
			if podVersion(p) != r.Worker {
				return false
			}
		}
	}
	return entries == 1 && workers == r.W
}
func (l *NormalLedger) units(objects map[string]*unstructured.Unstructured) map[string]NormalUnit {
	roles := l.roleUnits(objects)
	if l.Model.Mode == "Role" {
		return roles
	}
	out := map[string]NormalUnit{}
	for _, r := range roles {
		key := fmt.Sprintf("model-%d", r.Group)
		u, ok := out[key]
		if !ok {
			u = NormalUnit{Key: key, Scope: "SG", Group: r.Group, Ordinal: r.Group, Ready: true}
		}
		u.Pods = append(u.Pods, r.Pods...)
		u.Ready = u.Ready && r.Ready
		u.Active = u.Active || r.Active
		if r.Role == "frontend" || u.Version == "" {
			u.Version = r.Version
		}
		out[key] = u
	}
	for k, u := range out {
		u.Ready = false
		for i := len(l.History) - 1; i >= 0; i-- {
			candidate := l.History[i]
			expected := 0
			matched := true
			cohortReady := true
			for name, layout := range candidate.Roles {
				readyMinimum := layout.R
				if baseline, ok := l.Base.Roles[name]; ok {
					readyMinimum = baseline.R
				}
				if current, ok := l.Model.Roles[name]; ok {
					readyMinimum = min(readyMinimum, current.R)
					layout.R = current.R
				}
				expected += layout.R
				count, readyCount := 0, 0
				for _, r := range roles {
					if r.Group == u.Group && r.Role == name {
						count++
						// Availability requires every Role's own historical layout
						// to be complete. Replica expansion can legitimately mix A
						// and B Role instances before the enclosing SG is replaced.
						if !r.Complete {
							matched = false
						}
						if r.Ready {
							readyCount++
						}
					}
				}
				if count != layout.R {
					matched = false
				}
				cohortReady = cohortReady && readyCount >= readyMinimum
			}
			// Newly added Roles/replicas can coexist with the old membership.
			// Pending additions do not remove the old cohort's serving capacity;
			// an explicit reduction only requires its smaller remaining capacity.
			// Exact target membership remains a separate final-state predicate.
			u.Ready = u.Ready || cohortReady
			actual := 0
			for _, r := range roles {
				if r.Group == u.Group {
					actual++
				}
			}
			if matched && actual == expected {
				u.Complete = true
			}
		}
		out[k] = u
	}
	return out
}
func (l *NormalLedger) scopeBudget(u NormalUnit) (d, v, s, p int) {
	if l.Model.Mode == "SG" {
		return l.Model.N, l.Model.U, l.Model.S, l.Model.P
	}
	r := l.Model.Roles[u.Role]
	return r.R, r.U, r.S, r.P
}
func (l *NormalLedger) Metrics(objects map[string]*unstructured.Unstructured) []ScopeMetric {
	scopes := map[string]ScopeMetric{}
	for _, u := range l.units(objects) {
		m := scopes[u.Scope]
		m.Scope = u.Scope
		m.Desired, m.Unavailable, m.Surge, m.Partition = l.scopeBudget(u)
		if u.Active {
			m.Active++
		}
		if u.Ready {
			m.Ready++
		}
		scopes[u.Scope] = m
	}
	var out []ScopeMetric
	for _, m := range scopes {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope < out[j].Scope })
	return out
}
func (l *NormalLedger) Transition(spec map[string]interface{}, phase string, e ScenarioExpectation, objects Objects) error {
	next, err := readModel(spec)
	if err != nil {
		return err
	}
	before := l.units(objects["pods"])
	l.rememberReady(before)
	old := l.Model
	l.Phase = phase
	l.Expected = e
	l.NoNewRevision = e.NoNewRevision
	l.Revisions = map[string]bool{}
	for uid := range objects["controllerrevisions"] {
		l.Revisions[uid] = true
	}
	l.ScaleUIDs = map[string]bool{}
	l.ScaleGroups = map[int]bool{}
	l.GroupShrinkRemaining = 0
	l.RoleShrinkRemaining = map[string]int{}
	// A scale request grants a finite, explicit set of deletions. Select before
	// changing layout so that expanding W/R cannot make healthy old units appear
	// unhealthy and thereby exempt arbitrary deletion.
	groups := map[int]NormalUnit{}
	for _, u := range before {
		g, ok := groups[u.Group]
		if !ok {
			g = NormalUnit{Group: u.Group, Ordinal: u.Group, Ready: true}
		}
		g.Pods = append(g.Pods, u.Pods...)
		g.Ready = g.Ready && u.Ready
		g.Active = g.Active || u.Active
		groups[u.Group] = g
	}
	var gs []NormalUnit
	for _, g := range groups {
		if g.Active {
			gs = append(gs, g)
		}
	}
	sort.Slice(gs, func(i, j int) bool {
		pi, pj := gs[i].Ordinal < next.P, gs[j].Ordinal < next.P
		if old.Mode == "SG" && pi != pj {
			return !pi
		}
		if gs[i].Ready != gs[j].Ready {
			return !gs[i].Ready
		}
		return gs[i].Ordinal > gs[j].Ordinal
	})
	groupDeletes := 0
	if next.N < old.N {
		groupDeletes = max(len(gs)-next.N, 0)
	}
	if l.Profile == "auto" {
		l.GroupShrinkRemaining = groupDeletes
		groupDeletes = 0
	}
	for i := 0; i < groupDeletes && i < len(gs); i++ {
		l.ScaleGroups[gs[i].Group] = true
		for _, p := range gs[i].Pods {
			l.ScaleUIDs[string(p.UID)] = true
		}
	}
	ru := l.roleUnits(objects["pods"])
	byScope := map[string][]NormalUnit{}
	for _, u := range ru {
		byScope[u.Scope] = append(byScope[u.Scope], u)
	}
	for _, us := range byScope {
		if len(us) == 0 {
			continue
		}
		r := next.Roles[us[0].Role]
		if _, exists := next.Roles[us[0].Role]; !exists && old.Mode == "SG" {
			continue
		}
		oldR := old.Roles[us[0].Role].R
		sort.Slice(us, func(i, j int) bool {
			pi, pj := us[i].Ordinal < r.P, us[j].Ordinal < r.P
			if old.Mode == "Role" && pi != pj {
				return !pi
			}
			if us[i].Ready != us[j].Ready {
				return !us[i].Ready
			}
			return us[i].Ordinal > us[j].Ordinal
		})
		roleDeletes := 0
		if r.R < oldR {
			roleDeletes = max(len(us)-r.R, 0)
		}
		if l.Profile == "auto" {
			l.RoleShrinkRemaining[us[0].Scope] = roleDeletes
			roleDeletes = 0
		}
		for i := 0; i < roleDeletes && i < len(us); i++ {
			for _, p := range us[i].Pods {
				l.ScaleUIDs[string(p.UID)] = true
			}
		}
	}
	l.Model = next
	l.History = append(l.History, next)
	l.Protected = map[string]string{}
	for _, u := range before {
		_, _, _, p := l.scopeBudget(u)
		unchanged := sameTemplates(old, next)
		if old.Mode == "Role" && next.Mode == "Role" {
			unchanged = sameRole(old.Roles[u.Role], next.Roles[u.Role])
		}
		d, _, _, _ := l.scopeBudget(u)
		active := 0
		for _, other := range before {
			if other.Scope == u.Scope && other.Active {
				active++
			}
		}
		temporaryTarget := u.Ordinal >= d && active > d && l.unitTarget(u)
		// Already-issued deletion commitments survive new partition/budget values.
		for _, pod := range u.Pods {
			uid := string(pod.UID)
			if l.Committed[uid] || l.PGPods[uid] || l.ScaleUIDs[uid] || l.RoleScaleIntents[uid] != "" || l.ScaleGroups[u.Group] {
				continue
			}
			if e.NoReplacement || (!temporaryTarget && ((unchanged && l.unitTarget(u)) || u.Ordinal < p)) {
				l.Protected[uid] = pod.Name
			}
		}
	}
	l.Ceiling = map[string]int{}
	for _, m := range l.Metrics(objects["pods"]) {
		limit := m.Desired
		if !e.NoReplacement {
			limit += m.Surge
		}
		l.Ceiling[m.Scope] = max(limit, m.Active)
	}
	l.Armed = true
	return nil
}
func (l *NormalLedger) Before(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if !l.Armed {
		return
	}
	l.dynamicScaleBefore(kind, event, o, objects)
	l.roleScaleBefore(kind, event, o, objects)
	l.coordinationBefore(kind, event, o, objects)
	l.podGroupBefore(kind, event, o, objects)
	if kind != "pods" || (event != "DELETED" && o.GetDeletionTimestamp() == nil) {
		return
	}
	uid := string(o.GetUID())
	if l.Committed[uid] {
		return
	}
	var p corev1.Pod
	if convertPod(o, &p) != nil || !owned(&p, l.Owner) {
		return
	}
	units := l.units(objects["pods"])
	key := roleUnitKey(&p)
	if l.Model.Mode == "SG" {
		key = p.Labels[LabelGroup]
	}
	u, exists := units[key]
	if !exists {
		l.fail("INCONCLUSIVE: deletion without observed unit " + p.Name)
		return
	}
	reason := "rollout"
	reservedEarlier := l.PGPods[uid] && l.PGPhase[uid] != l.Phase
	if reservedEarlier {
		reason = "rollout-in-flight"
	}
	scale := l.ScaleUIDs[uid] || l.ScaleGroups[u.Group] || l.PGScale[uid] || l.RoleScaleIntents[uid] != ""
	if scale {
		reason = "scale"
	}
	if name, ok := l.Protected[uid]; ok && !scale {
		l.fail("PROTECTED_REPLACED: " + name)
	}
	ready, active := 0, 0
	for _, other := range units {
		if other.Scope == u.Scope {
			if other.Ready {
				ready++
			}
			if other.Active {
				active++
			}
		}
	}
	d, v, _, _ := l.scopeBudget(u)
	// Only healthy loss consumes additional availability. A commitment marks
	// every member immediately; later worker deletion cannot spend it twice.
	readyAfter := ready
	if u.Ready {
		readyAfter--
	}
	if !scale && !reservedEarlier && u.Ready && readyAfter < max(d-v, 0) {
		l.fail(fmt.Sprintf("BUDGET_VIOLATION: %s ready=%d delete=%s minimum=%d", u.Scope, ready, key, max(d-v, 0)))
	}
	if !scale && active > d && l.unitTarget(u) {
		reason = "surge-cleanup"
	}
	if !scale && !reservedEarlier && active <= d && u.Ready && l.unitTarget(u) {
		l.fail("UNEXPECTED_TARGET_REPLACED: " + key)
	}
	// Already unavailable old units may be replaced before healthy old units.
	// Descending healthy replacement order applies when consuming capacity.
	if !scale && !reservedEarlier && u.Ready && !l.unitTarget(u) {
		_, _, _, partition := l.scopeBudget(u)
		for _, other := range units {
			if other.Scope != u.Scope || other.Ordinal <= u.Ordinal || other.Ordinal < partition || other.Ordinal >= d || !other.Active || l.unitTarget(other) {
				continue
			}
			available := true
			for _, p := range other.Pods {
				if l.Committed[string(p.UID)] || l.ScaleUIDs[string(p.UID)] || p.DeletionTimestamp != nil {
					available = false
				}
			}
			if available {
				l.fail(fmt.Sprintf("ORDER_MISMATCH: %s started %d before eligible %d", u.Scope, u.Ordinal, other.Ordinal))
			}
		}
	}
	start := NormalStart{At: time.Now().UTC(), Phase: l.Phase, Key: key, Scope: u.Scope, Group: u.Group, Role: u.Role, Ordinal: u.Ordinal, Version: u.Version, Reason: reason, ReadyBefore: ready, Minimum: max(d-v, 0)}
	if scale && l.Model.Mode == "SG" && !l.ScaleGroups[u.Group] && !l.PGScale[uid] {
		// In SG mode an explicit Role scale-down removes only that Role instance;
		// it is not permission to replace all other members of the ServingGroup.
		for _, pod := range u.Pods {
			if roleUnitKey(pod) == roleUnitKey(&p) && (l.ScaleUIDs[string(pod.UID)] || l.RoleScaleIntents[string(pod.UID)] != "") {
				l.Committed[string(pod.UID)] = true
				start.UIDs = append(start.UIDs, string(pod.UID))
			}
		}
	} else {
		for _, pod := range u.Pods {
			if reservedEarlier && !l.PGPods[string(pod.UID)] {
				continue
			}
			l.Committed[string(pod.UID)] = true
			start.UIDs = append(start.UIDs, string(pod.UID))
		}
	}
	l.Starts = append(l.Starts, start)
}

// Natural readiness can change between the replicas request and deletion. Keep
// a finite capacity reduction, then choose from the observed healthy/cost/order
// ranking at the destructive event, rather than freezing a request-time UID.
func (l *NormalLedger) dynamicScaleBefore(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if l.Profile != "auto" || (kind != "pods" && kind != "podgroups") || (event != "DELETED" && o.GetDeletionTimestamp() == nil) || !objectOwned(o, l.Owner) {
		return
	}
	if l.Committed[string(o.GetUID())] {
		return
	}
	group := ordinal(o.GetName())
	role, roleKey := "", ""
	if kind == "pods" {
		var p corev1.Pod
		if convertPod(o, &p) != nil {
			return
		}
		group = ordinal(p.Labels[LabelGroup])
		role = p.Labels[LabelRole]
		roleKey = roleUnitKey(&p)
	}
	if l.ScaleGroups[group] || l.ScaleUIDs[string(o.GetUID())] || l.RoleScaleIntents[string(o.GetUID())] != "" {
		return
	}
	ru := l.roleUnits(objects["pods"])
	if l.GroupShrinkRemaining > 0 {
		gs := map[int]NormalUnit{}
		for _, u := range ru {
			g, ok := gs[u.Group]
			if !ok {
				g = NormalUnit{Group: u.Group, Ordinal: u.Group, Ready: true}
			}
			g.Pods = append(g.Pods, u.Pods...)
			g.Active = g.Active || u.Active
			g.Ready = g.Ready && u.Ready
			gs[u.Group] = g
		}
		var candidates []NormalUnit
		for _, g := range gs {
			if g.Active && !l.ScaleGroups[g.Group] {
				candidates = append(candidates, g)
			}
		}
		sort.Slice(candidates, func(i, j int) bool { return l.scaleLess(candidates[i], candidates[j], l.Model.P, l.Model.Mode == "SG") })
		if len(candidates) > 0 && candidates[0].Group == group {
			l.GroupShrinkRemaining--
			l.ScaleGroups[group] = true
			for _, p := range candidates[0].Pods {
				l.ScaleUIDs[string(p.UID)] = true
				delete(l.Protected, string(p.UID))
			}
			return
		}
	}
	if roleKey == "" {
		return
	}
	u, ok := ru[roleKey]
	if !ok || l.RoleShrinkRemaining[u.Scope] <= 0 {
		return
	}
	var candidates []NormalUnit
	for _, other := range ru {
		if other.Scope == u.Scope && other.Active {
			selected := false
			for _, p := range other.Pods {
				selected = selected || l.ScaleUIDs[string(p.UID)]
			}
			if !selected {
				candidates = append(candidates, other)
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return l.scaleLess(candidates[i], candidates[j], l.Model.Roles[role].P, l.Model.Mode == "Role")
	})
	if len(candidates) > 0 && candidates[0].Key == roleKey {
		l.RoleShrinkRemaining[u.Scope]--
		for _, p := range u.Pods {
			l.ScaleUIDs[string(p.UID)] = true
			delete(l.Protected, string(p.UID))
		}
	}
}
func (l *NormalLedger) scaleLess(a, b NormalUnit, partition int, protect bool) bool {
	if protect && (a.Ordinal < partition) != (b.Ordinal < partition) {
		return a.Ordinal >= partition
	}
	if a.Ready != b.Ready {
		return !a.Ready
	}
	if len(a.Pods) != len(b.Pods) {
		return len(a.Pods) < len(b.Pods)
	}
	return a.Ordinal > b.Ordinal
}
func (l *NormalLedger) unitTarget(u NormalUnit) bool {
	if l.Model.Mode == "Role" {
		return roleMatches(u, l.Model.Roles[u.Role])
	}
	groups := map[string]NormalUnit{}
	for _, p := range u.Pods {
		k := roleUnitKey(p)
		r := groups[k]
		r.Pods = append(r.Pods, p)
		r.Role = p.Labels[LabelRole]
		groups[k] = r
	}
	if len(groups) == 0 {
		return false
	}
	for _, r := range groups {
		layout, ok := l.Model.Roles[r.Role]
		if !ok || !roleMatches(r, layout) {
			return false
		}
	}
	for name, layout := range l.Model.Roles {
		count := 0
		for _, r := range groups {
			if r.Role == name {
				count++
			}
		}
		if count != layout.R {
			return false
		}
	}
	return true
}
func (l *NormalLedger) After(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if kind == "controllerrevisions" && event != "DELETED" && objectOwned(o, l.Owner) {
		data, _ := json.Marshal(o.Object["data"])
		uid := string(o.GetUID())
		if old, ok := l.RevisionData[uid]; ok && old != string(data) {
			l.fail("HISTORY_MUTATED: " + o.GetName())
		}
		l.RevisionData[uid] = string(data)
		spec := cloneMap(l.Model.Spec)
		spec["template"] = map[string]interface{}{"roles": mapValue(o.Object, "data")["data"]}
		if m, err := readModel(spec); err == nil {
			known := false
			for _, h := range l.History {
				if sameTemplates(h, m) {
					known = true
					break
				}
			}
			if !known {
				l.fail("UNEXPECTED_HISTORY_TEMPLATE: " + o.GetName())
			}
			l.RevisionLayouts[o.GetLabels()["modelserving.volcano.sh/revision"]] = m
		} else {
			l.fail("INVALID_HISTORY: " + o.GetName())
		}
	}
	if kind == "pods" && event != "DELETED" {
		var p corev1.Pod
		if convertPod(o, &p) == nil && owned(&p, l.Owner) {
			if _, seen := l.BornRanktable[string(p.UID)]; !seen {
				enabled := false
				for _, raw := range listValue(l.Model.Spec, "plugins") {
					if textValue(raw.(map[string]interface{}), "name") == "ranktable" {
						enabled = true
					}
				}
				l.BornRanktable[string(p.UID)] = enabled
			}
		}
		if convertPod(o, &p) == nil && owned(&p, l.Owner) && p.Namespace != "" && (p.Labels["modelserving.volcano.sh/revision"] == "" || p.Labels[LabelRoleID] == "" || !podHasEntryIdentity(&p)) {
			l.fail("IDENTITY_MISSING: " + p.Name)
		}
		if convertPod(o, &p) == nil && owned(&p, l.Owner) && l.Profile == "controlled" && podReady(&p) && !l.Released[string(p.UID)] {
			l.fail("CONTROL_VIOLATION: Ready before UID release " + p.Name)
		}
	}
	l.rememberReady(l.units(objects["pods"]))
	if !l.Armed {
		return
	}
	if kind == "controllerrevisions" && event == "ADDED" && l.NoNewRevision && !l.Revisions[string(o.GetUID())] {
		l.fail("UNEXPECTED_REVISION: " + o.GetName())
	}
	for _, m := range l.Metrics(objects["pods"]) {
		limit := m.Desired + m.Surge
		if l.Expected.NoReplacement {
			limit = m.Desired
		}
		if prev, ok := l.Ceiling[m.Scope]; ok {
			limit = max(limit, prev)
		}
		if m.Active > limit {
			l.fail(fmt.Sprintf("SURGE_VIOLATION: %s active=%d ceiling=%d", m.Scope, m.Active, limit))
		}
		floor := m.Desired + m.Surge
		if l.Expected.NoReplacement {
			floor = m.Desired
		}
		l.Ceiling[m.Scope] = max(floor, min(limit, m.Active))
	}
}
func (l *NormalLedger) rememberReady(units map[string]NormalUnit) {
	for _, u := range units {
		if u.Ready {
			for _, p := range u.Pods {
				if podReady(p) {
					l.Served[string(p.UID)] = true
				}
			}
		}
	}
}

// A published PodGroup minimum reduction confirms that the controller has
// accepted this group's Role scale-down. Its finite, already selected Pod UIDs
// may finish deletion after an immediate replicas restore. Preserve only that
// selection, not a group-wide exemption or permissions for future Pod UIDs.
func (l *NormalLedger) roleScaleBefore(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if kind != "podgroups" || event != "MODIFIED" || o.GetDeletionTimestamp() != nil || !objectOwned(o, l.Owner) || len(l.ScaleUIDs) == 0 {
		return
	}
	previous := objects["podgroups"][string(o.GetUID())]
	if previous == nil || previous.GetName() != o.GetName() || !objectOwned(previous, l.Owner) {
		return
	}
	oldTotal, _, oldErr := unstructured.NestedInt64(previous.Object, "spec", "minMember")
	newTotal, _, newErr := unstructured.NestedInt64(o.Object, "spec", "minMember")
	if oldErr != nil || newErr != nil || newTotal < 0 || oldTotal <= newTotal {
		return
	}
	type minimum struct{ count, size int64 }
	policies := func(pg *unstructured.Unstructured) map[string]minimum {
		values, _, err := unstructured.NestedSlice(pg.Object, "spec", "subGroupPolicy")
		if err != nil {
			return nil
		}
		out := map[string]minimum{}
		for _, value := range values {
			policy, ok := value.(map[string]interface{})
			if !ok {
				return nil
			}
			name, _, _ := unstructured.NestedString(policy, "name")
			role, _, _ := unstructured.NestedString(policy, "labelSelector", "matchLabels", LabelRole)
			keys, _, _ := unstructured.NestedStringSlice(policy, "matchLabelKeys")
			count, foundCount, countErr := unstructured.NestedInt64(policy, "minSubGroups")
			size, foundSize, sizeErr := unstructured.NestedInt64(policy, "subGroupSize")
			if _, duplicate := out[name]; duplicate || name == "" || role != name || len(keys) != 1 || keys[0] != LabelRoleID || !foundCount || !foundSize || countErr != nil || sizeErr != nil || count < 0 || size < 1 {
				return nil
			}
			out[name] = minimum{count, size}
		}
		return out
	}
	oldPolicies, newPolicies := policies(previous), policies(o)
	for role, reduced := range newPolicies {
		old, existed := oldPolicies[role]
		desired, known := l.Model.Roles[role]
		if !existed || !known || old.count <= reduced.count || reduced.count != int64(desired.R) || old.size != reduced.size || reduced.size != int64(desired.W+1) {
			continue
		}
		for uid, pod := range objects["pods"] {
			if !l.ScaleUIDs[uid] || !objectOwned(pod, l.Owner) || pod.GetLabels()[LabelGroup] != o.GetName() || pod.GetLabels()[LabelRole] != role {
				continue
			}
			if l.RoleScaleIntents == nil {
				l.RoleScaleIntents = map[string]string{}
			}
			l.RoleScaleIntents[uid] = string(o.GetUID())
		}
	}
}

// PodGroup deletion is an independent destructive signal. Cross-resource watch
// ordering can lag Pod readiness, so this check uses an optimistic upper bound
// from explicit releases. The Pod stream additionally checks actual Ready.
func (l *NormalLedger) podGroupBefore(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if kind != "podgroups" || (event != "DELETED" && o.GetDeletionTimestamp() == nil) || !objectOwned(o, l.Owner) || l.PGCommitted[string(o.GetUID())] {
		return
	}
	l.PGCommitted[string(o.GetUID())] = true
	group := ordinal(o.GetName())
	if l.ScaleGroups[group] {
		// The first selected PG deletion starts the finite SG shrink batch.
		// A rapid replicas restore can arrive while later PGs in that same
		// batch are still being deleted. Retain only its selected Pod UIDs.
		for uid, p := range objects["pods"] {
			if l.ScaleUIDs[uid] && l.ScaleGroups[ordinal(p.GetLabels()[LabelGroup])] && objectOwned(p, l.Owner) {
				if !l.PGPods[uid] {
					l.PGPhase[uid] = l.Phase
				}
				l.PGPods[uid] = true
				l.PGScale[uid] = true
			}
		}
		return
	}
	selected, unselected := false, false
	for uid, p := range objects["pods"] {
		if objectOwned(p, l.Owner) && p.GetLabels()[LabelGroup] == o.GetName() {
			selected = selected || l.PGScale[uid]
			unselected = unselected || !l.PGScale[uid]
		}
	}
	if selected && !unselected {
		return
	}
	if l.Model.Mode == "Role" {
		l.fail("UNEXPECTED_PODGROUP_REPLACED: " + o.GetName())
		return
	}
	units := l.units(objects["pods"])
	u, ok := units[o.GetName()]
	if !ok {
		return
	} // A PG may be collected after its last observed Pod.
	optimistic := func(u NormalUnit) bool {
		if !u.Complete {
			return false
		}
		for _, p := range u.Pods {
			if p.DeletionTimestamp != nil || l.Committed[string(p.UID)] || l.PGPods[string(p.UID)] || (l.Profile == "controlled" && !l.Released[string(p.UID)]) {
				return false
			}
		}
		return true
	}
	ready := 0
	for _, other := range units {
		if optimistic(other) {
			ready++
		}
	}
	if optimistic(u) && ready-1 < max(l.Model.N-l.Model.U, 0) {
		l.fail("BUDGET_VIOLATION: PodGroup deletion exceeds released capacity: " + o.GetName())
	}
	for _, p := range u.Pods {
		if _, protected := l.Protected[string(p.UID)]; protected {
			l.fail("PROTECTED_PODGROUP_REPLACED: " + o.GetName())
		}
		l.PGPods[string(p.UID)] = true
		l.PGPhase[string(p.UID)] = l.Phase
	}
}
func conditionMatches(c ScenarioCondition, u NormalUnit) bool {
	return (c.Group == nil || *c.Group == u.Group) && (c.Role == "" || c.Role == u.Role) && (c.Ordinal == nil || *c.Ordinal == u.Ordinal) && (c.Version == "" || c.Version == u.Version) && (c.Ready == nil || *c.Ready == u.Ready)
}
func (l *NormalLedger) conditions(cs []ScenarioCondition, objects Objects) bool {
	for _, c := range cs {
		count := 0
		switch c.Kind {
		case "started":
			for _, s := range l.Starts {
				u := NormalUnit{Group: s.Group, Role: s.Role, Ordinal: s.Ordinal, Version: s.Version}
				if conditionMatches(c, u) {
					count++
				}
			}
		case "unit":
			units := l.units(objects["pods"])
			if c.Role != "" {
				units = l.roleUnits(objects["pods"])
			}
			for _, u := range units {
				if u.Complete && u.Active && conditionMatches(c, u) {
					count++
				}
			}
		case "terminating", "pending", "unschedulable":
			for _, o := range objects["pods"] {
				var p corev1.Pod
				if convertPod(o, &p) != nil {
					continue
				}
				u := NormalUnit{Group: ordinal(p.Labels[LabelGroup]), Role: p.Labels[LabelRole], Ordinal: ordinal(p.Labels[LabelRoleID]), Version: podVersion(&p)}
				if l.Model.Mode == "SG" && c.Role == "" {
					u.Ordinal = u.Group
				}
				unschedulable := false
				for _, condition := range p.Status.Conditions {
					if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse && condition.Reason == "Unschedulable" && strings.Contains(condition.Message, "Insufficient cpu") {
						unschedulable = true
					}
				}
				if conditionMatches(c, u) && ((c.Kind == "terminating" && p.DeletionTimestamp != nil) || (c.Kind == "pending" && p.Status.Phase == corev1.PodPending) || (c.Kind == "unschedulable" && unschedulable)) {
					count++
				}
			}
		}
		if count < c.Count {
			return false
		}
	}
	return true
}
func (l *NormalLedger) releaseCandidates(objects Objects, exclude []ScenarioCondition) []NormalUnit {
	var out []NormalUnit
	for _, u := range l.units(objects["pods"]) {
		if !u.Complete || !u.Active {
			continue
		}
		good := true
		needed := false
		for _, p := range u.Pods {
			if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning {
				good = false
			}
			if !l.Released[string(p.UID)] {
				needed = true
			}
		}
		for _, c := range exclude {
			if conditionMatches(c, u) {
				good = false
			}
		}
		if good && needed {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].Ordinal > out[j].Ordinal
	})
	return out
}
func (l *NormalLedger) error() error {
	if len(l.Violations) > 0 {
		return fmt.Errorf("%s", strings.Join(l.Violations, "; "))
	}
	return nil
}
