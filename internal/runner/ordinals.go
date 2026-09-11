// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const NormalOrdinalContract = "normal-endpoints/ordinals-0-to-replicas-minus-1/v1"

// Normal flows require canonical identities at the baseline and each settled
// endpoint, including partitioned A/B endpoints. Fault fixtures have separate
// source/stop contracts; they are not silently redefined by this check.
func (c Case) normalFlow() bool {
	n, err := strconv.Atoi(strings.TrimPrefix(c.ID, "RUN-"))
	return err == nil && strings.HasPrefix(c.ID, "RUN-") && n >= 1 && n <= 303
}

// canonicalOrdinalFacts is an endpoint predicate, never a Watch-event rule.
// Group and Role identities come from owned Pods. Workers share a Role ID and
// do not count as additional Role replicas. Distinct labels with the same
// numeric suffix must not conceal duplicate ordinal identities.
func canonicalOrdinalFacts(n int, layouts map[int]map[string]int, owner string, pods map[string]*unstructured.Unstructured) error {
	groups := map[string]map[string]map[string]int{}
	for _, pod := range pods {
		if !objectOwned(pod, owner) {
			continue
		}
		labels := pod.GetLabels()
		group, role, instance := labels[LabelGroup], labels[LabelRole], labels[LabelRoleID]
		if groups[group] == nil {
			groups[group] = map[string]map[string]int{}
		}
		if groups[group][role] == nil {
			groups[group][role] = map[string]int{}
		}
		if _, ok := groups[group][role][instance]; !ok {
			groups[group][role][instance] = 0
		}
		if labels[LabelEntry] == "true" {
			groups[group][role][instance]++
		}
	}
	var actual []int
	var names []string
	for group := range groups {
		actual = append(actual, ordinal(group))
		names = append(names, group)
	}
	if err := canonicalOrdinalSet("SG", n, actual); err != nil {
		return err
	}
	sort.Strings(names)
	for _, group := range names {
		wanted, ok := layouts[ordinal(group)]
		if !ok {
			return fmt.Errorf("FINAL_ORDINAL_MISMATCH: missing desired Role layout for %s", group)
		}
		var roles []string
		for role := range wanted {
			roles = append(roles, role)
		}
		for role := range groups[group] {
			if _, ok := wanted[role]; !ok {
				return fmt.Errorf("FINAL_ORDINAL_MISMATCH: unexpected Role %s/%s", group, role)
			}
		}
		sort.Strings(roles)
		for _, role := range roles {
			actual = nil
			for instance, entries := range groups[group][role] {
				if entries != 1 {
					return fmt.Errorf("FINAL_ORDINAL_MISMATCH: %s/%s/%s has %d entries, want 1", group, role, instance, entries)
				}
				actual = append(actual, ordinal(instance))
			}
			if err := canonicalOrdinalSet(group+"/"+role, wanted[role], actual); err != nil {
				return err
			}
		}
	}
	return nil
}

func canonicalOrdinalSet(scope string, desired int, actual []int) error {
	sort.Ints(actual)
	valid := len(actual) == desired && desired >= 0
	for i, n := range actual {
		valid = valid && i == n
	}
	if !valid {
		return fmt.Errorf("FINAL_ORDINAL_MISMATCH: %s actual=%v want=0..%d (replicas=%d)", scope, actual, desired-1, desired)
	}
	return nil
}

func fixedOrdinalLayouts(spec map[string]interface{}) (int, map[int]map[string]int) {
	n := intValue(spec, "replicas", 1)
	roles := map[string]int{}
	for _, raw := range listValue(mapValue(spec, "template"), "roles") {
		role := raw.(map[string]interface{})
		roles[textValue(role, "name")] = intValue(role, "replicas", 1)
	}
	layouts := map[int]map[string]int{}
	for g := 0; g < n; g++ {
		layouts[g] = roles
	}
	return n, layouts
}

func (l *Ledger) canonicalOrdinals(pods map[string]*unstructured.Unstructured) error {
	n, layouts := fixedOrdinalLayouts(l.Case.Input.Spec)
	return canonicalOrdinalFacts(n, layouts, l.Owner, pods)
}
