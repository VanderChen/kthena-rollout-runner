// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type coordinationProgress struct {
	Total, Started, Ready, Old                 int
	HasTarget, HasTargetReady, HasOld, Changed bool
}

func (l *NormalLedger) coordinationBefore(kind, event string, o *unstructured.Unstructured, objects Objects) {
	if !l.Armed || l.Model.Mode != "Role" || kind != "pods" {
		return
	}
	co := mapValue(mapValue(l.Model.Spec, "rolloutStrategy"), "roleCoordination")
	if co == nil {
		return
	}
	var pod corev1.Pod
	if convertPod(o, &pod) != nil || !owned(&pod, l.Owner) {
		return
	}
	role := pod.Labels[LabelRole]
	group := ordinal(pod.Labels[LabelGroup])
	ord := ordinal(pod.Labels[LabelRoleID])
	selected := map[string]bool{}
	for _, n := range listValue(co, "roles") {
		selected[n.(string)] = true
	}
	if len(selected) == 0 {
		for n := range l.Model.Roles {
			selected[n] = true
		}
	}
	if !selected[role] || l.ScaleGroups[group] || l.ScaleUIDs[string(pod.UID)] {
		return
	}
	deps := map[string][]string{}
	callers := map[string][]string{}
	for _, raw := range listValue(co, "dependencies") {
		d := raw.(map[string]interface{})
		from := textValue(d, "role")
		for _, v := range listValue(d, "dependsOn") {
			to := v.(string)
			deps[from] = append(deps[from], to)
			callers[to] = append(callers[to], from)
		}
	}
	states := map[string]*coordinationProgress{}
	ru := l.roleUnits(objects["pods"])
	for n := range selected {
		layout := l.Model.Roles[n]
		base := l.Base.Roles[n]
		end := min(base.R, layout.R)
		s := &coordinationProgress{Total: max(end-layout.P, 0), Changed: !sameRole(base, layout)}
		oldStable := 0
		for _, u := range ru {
			if u.Group != group || u.Role != n {
				continue
			}
			target := roleMatches(u, layout)
			committed := false
			terminating := false
			for _, p := range u.Pods {
				committed = committed || l.Committed[string(p.UID)]
				terminating = terminating || p.DeletionTimestamp != nil
			}
			stable := u.Ordinal >= layout.P && u.Ordinal < end
			if !target {
				s.HasOld = true
				if !committed && !terminating {
					if stable {
						oldStable++
					}
					s.Old++
				}
			} else if !committed && !terminating && u.Ordinal < layout.R {
				s.HasTarget = true
				if u.Ready {
					s.HasTargetReady = true
					if stable {
						s.Ready++
					}
				}
			}
		}
		s.Started = max(s.Total-oldStable, 0)
		s.Ready = min(s.Ready, s.Started)
		states[n] = s
	}
	state := states[role]
	if state == nil || !state.Changed {
		return
	}
	deleting := event == "DELETED" || o.GetDeletionTimestamp() != nil
	if deleting && l.Committed[string(o.GetUID())] {
		return
	}
	newTarget := !deleting && event == "ADDED" && podVersion(&pod) == l.Model.Roles[role].Entry && podIsEntry(&pod)
	if newTarget && !state.HasTarget && state.Started == 0 && len(callers[role]) == 0 {
		visited := map[string]bool{}
		var walk func(string)
		walk = func(n string) {
			for _, dep := range deps[n] {
				if visited[dep] {
					continue
				}
				visited[dep] = true
				if s := states[dep]; s != nil && s.Changed && !s.HasTargetReady {
					l.fail(fmt.Sprintf("DEPENDENCY_VIOLATION: group=%d root=%s dependency=%s has no stable target Ready", group, role, dep))
				}
				walk(dep)
			}
		}
		walk(role)
	}
	if !deleting {
		return
	}
	u, ok := ru[roleUnitKey(&pod)]
	if !ok || roleMatches(u, l.Model.Roles[role]) {
		return
	}
	if state.Old <= 1 {
		for _, caller := range callers[role] {
			if s := states[caller]; s != nil && s.Changed && s.HasOld {
				l.fail(fmt.Sprintf("OLD_PATH_VIOLATION: group=%d last old %s deleted while old caller %s exists", group, role, caller))
			}
		}
	}
	layout := l.Model.Roles[role]
	end := min(l.Base.Roles[role].R, layout.R)
	if ord < layout.P || ord >= end {
		return
	}
	k, err := strconv.Atoi(strings.TrimSuffix(textValue(co, "maxSkew"), "%"))
	if err != nil {
		l.fail("INVALID_COORDINATION: maxSkew")
		return
	}
	allowance := state.Total
	for _, s := range states {
		if !s.Changed || s.Total == 0 || s.Ready >= s.Total {
			continue
		}
		numerator := state.Total * (100*s.Ready + k*s.Total)
		denominator := 100 * s.Total
		allowance = min(allowance, (numerator+denominator-1)/denominator)
	}
	if state.Started+1 > allowance {
		l.fail(fmt.Sprintf("SKEW_VIOLATION: group=%d role=%s starts=%d allowance=%d", group, role, state.Started+1, allowance))
	}
}
