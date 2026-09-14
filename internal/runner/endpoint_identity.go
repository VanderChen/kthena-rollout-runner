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

const LegacyOrdinalContract = "normal-endpoints/unique-identities-without-contiguous-ordinals/v1"

// Normal cases advertise the legacy endpoint contract; fault-source identity
// contracts remain explicit and independent.
func (c Case) normalFlow() bool {
	n, err := strconv.Atoi(strings.TrimPrefix(c.ID, "RUN-"))
	return err == nil && strings.HasPrefix(c.ID, "RUN-") && n >= 1 && n <= 303
}

// Endpoint identities may be sparse or shifted. Count, nonnegative unique
// indices, expected Role membership and exactly one entry per Role still apply.
// Workers share their Role identity and never provide extra replica credit.
func endpointIdentityFacts(n int, layouts map[int]map[string]int, owner string, pods map[string]*unstructured.Unstructured) error {
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
	if err := endpointIdentitySet("SG", n, actual); err != nil {
		return err
	}
	sort.Strings(names)
	for _, group := range names {
		wanted, ok := layouts[ordinal(group)]
		if !ok {
			return fmt.Errorf("FINAL_IDENTITY_MISMATCH: missing desired Role layout for %s", group)
		}
		var roles []string
		for role := range wanted {
			roles = append(roles, role)
		}
		for role := range groups[group] {
			if _, ok := wanted[role]; !ok {
				return fmt.Errorf("FINAL_IDENTITY_MISMATCH: unexpected Role %s/%s", group, role)
			}
		}
		sort.Strings(roles)
		for _, role := range roles {
			actual = nil
			for instance, entries := range groups[group][role] {
				if entries != 1 {
					return fmt.Errorf("FINAL_IDENTITY_MISMATCH: %s/%s/%s has %d entries, want 1", group, role, instance, entries)
				}
				actual = append(actual, ordinal(instance))
			}
			if err := endpointIdentitySet(group+"/"+role, wanted[role], actual); err != nil {
				return err
			}
		}
	}
	return nil
}

func endpointIdentitySet(scope string, desired int, actual []int) error {
	sort.Ints(actual)
	valid := len(actual) == desired && desired >= 0
	for i, n := range actual {
		valid = valid && n >= 0 && (i == 0 || actual[i-1] != n)
	}
	if !valid {
		return fmt.Errorf("FINAL_IDENTITY_MISMATCH: %s actual=%v want %d distinct nonnegative identities", scope, actual, desired)
	}
	return nil
}

func endpointLayouts(spec map[string]interface{}, owner string, pods map[string]*unstructured.Unstructured) (int, map[int]map[string]int) {
	n := intValue(spec, "replicas", 1)
	roles := map[string]int{}
	for _, raw := range listValue(mapValue(spec, "template"), "roles") {
		role := raw.(map[string]interface{})
		roles[textValue(role, "name")] = intValue(role, "replicas", 1)
	}
	layouts := map[int]map[string]int{}
	for _, pod := range pods {
		if objectOwned(pod, owner) {
			layouts[ordinal(pod.GetLabels()[LabelGroup])] = roles
		}
	}
	return n, layouts
}

func (l *Ledger) endpointIdentities(pods map[string]*unstructured.Unstructured) error {
	n, layouts := endpointLayouts(l.Case.Input.Spec, l.Owner, pods)
	return endpointIdentityFacts(n, layouts, l.Owner, pods)
}
