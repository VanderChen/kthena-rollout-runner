// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ContractConflicts identifies known catalogue/contract disagreements before a
// cluster is touched. This is a migration gate, not an admission verdict: only
// the API contract executor's real server responses establish admission PASS.
func ContractConflicts(c Case) []string {
	var conflicts []string
	add := func(where, reason string) { conflicts = append(conflicts, where+": "+reason) }
	check := func(where string, spec map[string]interface{}) {
		if spec == nil {
			return
		}
		if intValue(mapValue(spec, "template"), "restartGracePeriodSeconds", 0) < -1 {
			add(where, "API 6: grace below -1")
		}
		scope := func(label string, values map[string]interface{}, n int) {
			u, e1 := budget(values, "maxUnavailable", n, false)
			s, e2 := budget(values, "maxSurge", n, false)
			p, e3 := budget(values, "partition", n, false)
			if e1 != nil || e2 != nil || e3 != nil {
				add(where, label+": invalid budget format")
				return
			}
			if u == 0 && s == 0 {
				add(where, label+": resolved U/S=0/0 (API 1.2)")
			}
			if u > n && !(n == 0 && u == 1 && (values["maxUnavailable"] == nil || !isString(values["maxUnavailable"]))) {
				add(where, label+": U exceeds replicas (API 3/4)")
			}
			if p > n {
				add(where, label+": P exceeds replicas (API 3/4)")
			}
			if s > 2147483647-n {
				add(where, label+": replicas+S overflows int32 (API 3/4)")
			}
		}
		strategy := mapValue(spec, "rolloutStrategy")
		if textValue(strategy, "type") != "RoleRollingUpdate" {
			scope("SG", mapValue(strategy, "rollingUpdateConfiguration"), intValue(spec, "replicas", 1))
		} else {
			for _, raw := range listValue(mapValue(spec, "template"), "roles") {
				role, ok := raw.(map[string]interface{})
				if ok {
					scope(textValue(role, "name"), role, intValue(role, "replicas", 1))
				}
			}
		}
	}
	if c.Scenario == nil {
		check("input", c.Input.Spec)
		return conflicts
	}
	previous := c.Scenario.InitialSpec
	check("initial", previous)
	for _, step := range c.Scenario.Steps {
		if strings.HasPrefix(step.Action, "reject-") {
			continue
		}
		next := step.Spec
		if step.Action == "merge-patch" {
			next = mergeContractMap(previous, mapValue(step.Patch, "spec"))
		}
		if len(next) == 0 {
			continue
		}
		check(step.Name, next)
		a, b := mapValue(previous, "template"), mapValue(next, "template")
		if !reflect.DeepEqual(contractRoleNames(a), contractRoleNames(b)) {
			add(step.Name, "API 1.3: immutable Role-name set changes")
		}
		if !reflect.DeepEqual(a["gangPolicy"], b["gangPolicy"]) {
			add(step.Name, "API 8: immutable gangPolicy changes")
		}
		ac, bc := mapValue(previous, "rolloutStrategy")["roleCoordination"], mapValue(next, "rolloutStrategy")["roleCoordination"]
		if contractSetJSON(ac) != contractSetJSON(bc) {
			add(step.Name, "API 5: immutable roleCoordination changes")
		}
		previous = next
	}
	// These five historical DENY recipes consist solely of the formerly forbidden
	// inactive budgets or >100% surge. Their real current ALLOW requests are in
	// cases/api-contract; do not turn a correct admission into a product failure.
	if c.Format == "rollout-runner/v4" {
		switch c.ID {
		case "DENY-020", "DENY-035":
			add("rejection", "API 11: surge above 100% is allowed within int32; use api-contract")
		case "DENY-044", "DENY-045", "DENY-046":
			add("rejection", "API 11: inactive budgets are allowed but ignored; use api-contract")
		}
	}
	return conflicts
}
func isString(v interface{}) bool { _, ok := v.(string); return ok }
func contractRoleNames(t map[string]interface{}) []string {
	var names []string
	for _, raw := range listValue(t, "roles") {
		if r, ok := raw.(map[string]interface{}); ok {
			names = append(names, textValue(r, "name"))
		}
	}
	sort.Strings(names)
	return names
}
func contractSetJSON(v interface{}) string {
	switch value := v.(type) {
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, contractSetJSON(item))
		}
		sort.Strings(parts)
		raw, _ := json.Marshal(parts)
		return string(raw)
	case map[string]interface{}:
		m := map[string]string{}
		for k, item := range value {
			m[k] = contractSetJSON(item)
		}
		raw, _ := json.Marshal(m)
		return string(raw)
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}
func mergeContractMap(base, patch map[string]interface{}) map[string]interface{} {
	out := cloneMap(base)
	if out == nil {
		out = map[string]interface{}{}
	}
	for key, value := range patch {
		if value == nil {
			delete(out, key)
			continue
		}
		if nested, ok := value.(map[string]interface{}); ok {
			out[key] = mergeContractMap(mapValue(out, key), nested)
		} else {
			out[key] = value
		}
	}
	if out == nil {
		return map[string]interface{}{}
	}
	return out
}
func currentContractError(cases []Case, selected map[string]bool) error {
	var reasons []string
	for _, c := range cases {
		if len(selected) > 0 && !selected[c.ID] {
			continue
		}
		for _, reason := range ContractConflicts(c) {
			reasons = append(reasons, c.ID+": "+reason)
		}
	}
	if len(reasons) > 0 {
		return fmt.Errorf("CATALOGUE_CONTRACT_CONFLICT (not a product verdict):\n%s\nSee docs/CONTRACT_MIGRATION.md and cases/api-contract", strings.Join(reasons, "\n"))
	}
	return nil
}
