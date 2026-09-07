// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"encoding/json"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (e *normalExecution) settled(expect ScenarioExpectation) (bool, string) {
	l := e.l
	obj := e.o.objects
	roles := l.roleUnits(obj["pods"])
	groups := map[int]map[string][]NormalUnit{}
	for _, u := range roles {
		if !u.Ready || !u.Complete {
			return false, "members not complete/Ready: " + u.Key
		}
		if groups[u.Group] == nil {
			groups[u.Group] = map[string][]NormalUnit{}
		}
		groups[u.Group][u.Role] = append(groups[u.Group][u.Role], u)
	}
	if len(groups) != l.Model.N {
		return false, fmt.Sprintf("groups %d want %d", len(groups), l.Model.N)
	}
	for uid, name := range l.Protected {
		p, ok := obj["pods"][uid]
		if !ok || p.GetDeletionTimestamp() != nil {
			return false, "protected UID absent: " + name
		}
	}
	effective := map[int]NormalModel{}
	allTarget := true
	for group, byRole := range groups {
		candidates := []NormalModel{l.Model}
		if l.Model.Mode == "SG" && group < l.Model.P {
			for i := len(l.History) - 2; i >= 0; i-- {
				candidates = append(candidates, l.History[i])
			}
		}
		matched := false
		for _, candidate := range candidates {
			if len(candidate.Roles) != len(byRole) {
				continue
			}
			good := true
			for name, layout := range candidate.Roles {
				if current, ok := l.Model.Roles[name]; ok {
					layout.R = current.R
				}
				us := byRole[name]
				minCount, maxCount := layout.R, layout.R
				for _, t := range expect.Targets {
					if t.Role == name && (t.Group == nil || *t.Group == group) && t.MaxCount > 0 {
						minCount, maxCount = t.MinCount, t.MaxCount
					}
				}
				if len(us) < minCount || len(us) > maxCount {
					good = false
					break
				}
				for _, u := range us {
					valid := roleMatches(u, layout)
					if !valid && l.Model.Mode == "Role" {
						// A retained ordinal may use its own historical entry/worker layout.
						protected := false
						for _, p := range u.Pods {
							if _, ok := l.Protected[string(p.UID)]; ok {
								protected = true
							}
						}
						explicitOld := false
						for _, t := range expect.Targets {
							if t.Role == name && (t.Group == nil || *t.Group == group) && (t.Versions[u.Version] > 0 || t.MinVersions[u.Version] > 0) {
								explicitOld = true
							}
						}
						if protected || explicitOld {
							for _, h := range l.History {
								if r, ok := h.Roles[name]; ok && roleMatches(u, r) {
									valid = true
									break
								}
							}
						}
					}
					if !valid {
						good = false
						break
					}
					if !roleMatches(u, l.Model.Roles[name]) {
						allTarget = false
					}
				}
				if !good {
					break
				}
			}
			if good {
				effective[group] = candidate
				matched = true
				break
			}
		}
		if !matched {
			return false, fmt.Sprintf("group %d has wrong versions/layout", group)
		}
	}
	for _, t := range expect.Targets {
		if t.Scope == "SG" {
			var units []NormalUnit
			for _, u := range l.units(obj["pods"]) {
				units = append(units, u)
			}
			if ok, reason := targetFacts(units, t); !ok {
				return false, reason
			}
			continue
		}
		for group, byRole := range groups {
			if t.Group != nil && *t.Group != group {
				continue
			}
			if ok, reason := targetFacts(byRole[t.Role], t); !ok {
				return false, reason
			}
		}
	}
	if len(obj["podgroups"]) != len(groups) {
		return false, "PodGroup cleanup/count"
	}
	for _, pg := range obj["podgroups"] {
		g := ordinal(pg.GetName())
		model, ok := effective[g]
		if !ok || pg.GetDeletionTimestamp() != nil || !objectOwned(pg, l.Owner) {
			return false, "PodGroup ownership/lifecycle"
		}
		minMembers := 0
		expectedGangRoles := map[string]RoleLayout{}
		gang := mapValue(mapValue(l.Model.Spec, "template"), "gangPolicy")
		minimums := mapValue(gang, "minRoleReplicas")
		for name, r := range model.Roles {
			if latest, ok := l.Model.Roles[name]; ok {
				r.R = latest.R
			}
			minimum := r.R
			if minimums[name] != nil {
				minimum = intValue(minimums, name, r.R)
			}
			minMembers += minimum * (1 + r.W)
			r.R = minimum
			expectedGangRoles[name] = r
		}
		actual, _, _ := unstructured.NestedInt64(pg.Object, "spec", "minMember")
		if actual != int64(minMembers) {
			return false, fmt.Sprintf("PG %s minMember=%d want=%d", pg.GetName(), actual, minMembers)
		}
		if len(minimums) > 0 {
			if ok, reason := explicitGangRoleFacts(pg, e.current.GetName(), expectedGangRoles); !ok {
				return false, reason
			}
		}
		for key, want := range map[string]string{"cpu": fmt.Sprintf("%dm", 5*minMembers), "memory": fmt.Sprintf("%dMi", 4*minMembers)} {
			raw, _, _ := unstructured.NestedString(pg.Object, "spec", "minResources", key)
			q, err := resource.ParseQuantity(raw)
			if err != nil || q.Cmp(resource.MustParse(want)) != 0 {
				return false, "PG minResources: " + key
			}
		}
		for _, us := range groups[g] {
			for _, u := range us {
				for _, p := range u.Pods {
					if p.Annotations["scheduling.k8s.io/group-name"] != pg.GetName() {
						return false, "PodGroup association"
					}
				}
			}
		}
	}
	if ok, reason := e.pluginFacts(groups); !ok {
		return false, reason
	}
	ms := e.current
	for _, observed := range obj["modelservings"] {
		if observed.GetUID() == ms.GetUID() {
			ms = observed
			break
		}
	}
	observed, _, _ := unstructured.NestedInt64(ms.Object, "status", "observedGeneration")
	ready, _, _ := unstructured.NestedInt64(ms.Object, "status", "availableReplicas")
	replicas, _, _ := unstructured.NestedInt64(ms.Object, "status", "replicas")
	updated, _, _ := unstructured.NestedInt64(ms.Object, "status", "updatedReplicas")
	current, _, _ := unstructured.NestedString(ms.Object, "status", "currentRevision")
	target, _, _ := unstructured.NestedString(ms.Object, "status", "updateRevision")
	if observed < e.current.GetGeneration() || replicas != int64(l.Model.N) || (!expect.NoFullPromotion && ready != int64(l.Model.N)) || current == "" || target == "" {
		return false, "ModelServing status not converged"
	}
	if allTarget && current != target {
		return false, "currentRevision not promoted"
	}
	if expect.NoFullPromotion && current == target {
		return false, "dependency canary incorrectly promoted to full completion"
	}
	if expect.NoFullPromotion {
		blocked, inProgress := false, false
		for _, raw := range listValue(mapValue(ms.Object, "status"), "conditions") {
			c := raw.(map[string]interface{})
			blocked = blocked || (c["type"] == "CoordinatedRoleRolloutBlocked" && c["status"] == "True" && c["reason"] == "OldVersionDependencyPresent")
			inProgress = inProgress || (c["type"] == "UpdateInProgress" && c["status"] == "True")
		}
		if !blocked || !inProgress {
			return false, "dependency canary status does not report the old path"
		}
	}
	updatedGroups := 0
	for _, byRole := range groups {
		matches := len(byRole) == len(l.Model.Roles)
		for name, layout := range l.Model.Roles {
			if len(byRole[name]) != layout.R {
				matches = false
			}
			for _, u := range byRole[name] {
				matches = matches && roleMatches(u, layout)
			}
		}
		if matches {
			updatedGroups++
		}
	}
	if updated != int64(updatedGroups) {
		return false, "updatedReplicas not converged"
	}
	revisions := map[string]bool{}
	for _, cr := range obj["controllerrevisions"] {
		if objectOwned(cr, l.Owner) {
			revisions[cr.GetName()] = true
		}
	}
	if !revisions["model-"+current] || !revisions["model-"+target] {
		return false, "live ControllerRevision missing"
	}
	return true, ""
}

// The explicit Role minimum must reach the scheduler as a Role constraint.
// A correct total minMember cannot detect swapped or missing Role minimums.
func explicitGangRoleFacts(pg *unstructured.Unstructured, modelName string, roles map[string]RoleLayout) (bool, string) {
	policies, found, err := unstructured.NestedSlice(pg.Object, "spec", "subGroupPolicy")
	if err != nil || !found || len(policies) != len(roles) {
		return false, "PodGroup explicit Gang Role policy count"
	}
	seen := map[string]bool{}
	for _, raw := range policies {
		policy, ok := raw.(map[string]interface{})
		if !ok {
			return false, "PodGroup invalid Gang Role policy"
		}
		name := textValue(policy, "name")
		role, ok := roles[name]
		if !ok || seen[name] || intValue(policy, "minSubGroups", -1) != role.R || intValue(policy, "subGroupSize", -1) != 1+role.W {
			return false, "PodGroup Gang Role minimum/layout: " + name
		}
		seen[name] = true
		selector := mapValue(policy, "labelSelector")
		labels := mapValue(selector, "matchLabels")
		keys := listValue(policy, "matchLabelKeys")
		if len(labels) != 2 || labels["modelserving.volcano.sh/name"] != modelName || labels[LabelRole] != name || len(listValue(selector, "matchExpressions")) != 0 || len(keys) != 1 || keys[0] != LabelRoleID {
			return false, "PodGroup Gang Role selector: " + name
		}
	}
	return true, ""
}

func targetFacts(units []NormalUnit, target ScenarioTarget) (bool, string) {
	counts := map[string]int{}
	ordinals := map[string]string{}
	for _, u := range units {
		counts[u.Version]++
		ordinals[strconv.Itoa(u.Ordinal)] = u.Version
		if want, ok := target.Workers[u.Version]; ok && u.Workers != want {
			return false, "target worker count: " + u.Key
		}
	}
	if target.MaxCount > 0 && (len(units) < target.MinCount || len(units) > target.MaxCount) {
		return false, "target count outside declared range"
	}
	for version, want := range target.MinVersions {
		if counts[version] < want {
			return false, "target minimum version count: " + version
		}
	}
	if len(target.Versions) > 0 && len(counts) != len(target.Versions) {
		return false, fmt.Sprintf("target versions %v want %v", counts, target.Versions)
	}
	for version, want := range target.Versions {
		if counts[version] != want {
			return false, fmt.Sprintf("target %s count %d want %d", version, counts[version], want)
		}
	}
	for ordinal, want := range target.Ordinals {
		if ordinals[ordinal] != want {
			return false, "target ordinal " + ordinal + " want " + want
		}
	}
	return true, ""
}
func objectOwned(o *unstructured.Unstructured, uid string) bool {
	for _, r := range o.GetOwnerReferences() {
		if string(r.UID) == uid {
			return true
		}
	}
	return false
}
func (e *normalExecution) pluginFacts(groups map[int]map[string][]NormalUnit) (bool, string) {
	enabled := map[string]bool{}
	for _, p := range listValue(e.l.Model.Spec, "plugins") {
		enabled[textValue(p.(map[string]interface{}), "name")] = true
	}
	wantedServices := map[string]map[string]string{}
	wantedTables := map[string]int{}
	for group, byRole := range groups {
		for role, us := range byRole {
			for _, u := range us {
				if enabled["headless-service"] && u.Workers > 0 {
					for _, p := range u.Pods {
						if podIsEntry(p) {
							wantedServices[p.Name] = map[string]string{LabelGroup: p.Labels[LabelGroup], LabelRole: role, LabelRoleID: p.Labels[LabelRoleID]}
						}
					}
				}
			}
			if enabled["ranktable"] {
				for _, u := range us {
					wantedTables[fmt.Sprintf("model-model-%d-%s-%d-ranktable", group, role, u.Ordinal)] = 1
					for _, p := range u.Pods {
						if !e.l.BornRanktable[string(p.UID)] {
							continue
						}
						mounted, volume := false, false
						for _, v := range p.Spec.Volumes {
							if v.Name == "ranktable" && v.ConfigMap != nil && v.ConfigMap.Name == fmt.Sprintf("model-model-%d-%s-%d-ranktable", group, role, u.Ordinal) {
								volume = true
							}
						}
						for _, c := range p.Spec.Containers {
							if c.Name == "workload" {
								for _, v := range c.VolumeMounts {
									if v.Name == "ranktable" && v.MountPath == "/etc/production020-ranktable" && v.ReadOnly {
										mounted = true
									}
								}
							}
						}
						if !volume || !mounted {
							return false, "Ranktable mount mismatch: " + p.Name
						}
					}
				}
			}
		}
	}
	actualServices := map[string]bool{}
	for _, s := range e.o.objects["services"] {
		if !objectOwned(s, e.l.Owner) {
			continue
		}
		if s.GetDeletionTimestamp() != nil {
			return false, "terminating Service"
		}
		selector, wanted := wantedServices[s.GetName()]
		if !wanted {
			return false, "unexpected Service " + s.GetName()
		}
		ip, _, _ := unstructured.NestedString(s.Object, "spec", "clusterIP")
		if ip != "None" {
			return false, "Service is not headless"
		}
		actualSelector, _, _ := unstructured.NestedStringMap(s.Object, "spec", "selector")
		if len(actualSelector) != len(selector) {
			return false, "headless Service selector size"
		}
		for k, v := range selector {
			if actualSelector[k] != v {
				return false, "headless Service selector: " + s.GetName()
			}
		}
		publish, _, _ := unstructured.NestedBool(s.Object, "spec", "publishNotReadyAddresses")
		if !publish {
			return false, "headless Service must publish NotReady members"
		}
		actualServices[s.GetName()] = true
	}
	if len(actualServices) != len(wantedServices) {
		return false, "headless Service count"
	}
	found := map[string]bool{}
	for _, cm := range e.o.objects["configmaps"] {
		if !objectOwned(cm, e.l.Owner) {
			continue
		}
		count, ok := wantedTables[cm.GetName()]
		if !ok {
			return false, "unexpected ConfigMap " + cm.GetName()
		}
		if cm.GetDeletionTimestamp() != nil {
			return false, "terminating Ranktable"
		}
		raw, _, _ := unstructured.NestedString(cm.Object, "data", "ranktable.json")
		var table map[string]interface{}
		if json.Unmarshal([]byte(raw), &table) != nil {
			return false, "invalid Ranktable JSON"
		}
		if table["version"] != e.tableVersion {
			return false, "Ranktable template version not reconciled"
		}
		if table["status"] != "Completed" {
			return false, "Ranktable membership not completed"
		}
		if fmt.Sprint(table["server_count"]) != strconv.Itoa(count) {
			return false, fmt.Sprintf("Ranktable count %s got=%v want=%d", cm.GetName(), table["server_count"], count)
		}
		found[cm.GetName()] = true
	}
	if len(found) != len(wantedTables) {
		return false, "Ranktable count"
	}
	return true, ""
}
