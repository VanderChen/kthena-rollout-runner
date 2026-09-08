// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import "testing"

func TestSingleChangedRoleHasNoSelfSkewButTwoChangedRolesRemainBounded(t *testing.T) {
	for _, backendChanged := range []bool{false, true} {
		l, objects := normalFixture(t, "RUN-143")
		spec := historyVersionSpec(l.Model.Spec, "B")
		for _, raw := range listValue(mapValue(spec, "template"), "roles") {
			role := raw.(map[string]interface{})
			if textValue(role, "name") == "frontend" {
				role["partition"] = float64(1)
				role["maxUnavailable"] = float64(1)
				role["maxSurge"] = float64(1)
			}
			if backendChanged && textValue(role, "name") == "backend" {
				for _, container := range listValue(mapValue(mapValue(role, "entryTemplate"), "spec"), "containers") {
					for _, env := range listValue(container.(map[string]interface{}), "env") {
						v := env.(map[string]interface{})
						if v["name"] == "ROLLOUT_VERSION" {
							v["value"] = "B"
						}
					}
				}
			}
		}
		mapValue(spec, "rolloutStrategy")["roleCoordination"] = map[string]interface{}{"maxSkew": "50%", "dependencies": []interface{}{map[string]interface{}{"role": "frontend", "dependsOn": []interface{}{"backend"}}}}
		if err := l.Transition(spec, "B", ScenarioExpectation{}, objects); err != nil {
			t.Fatal(err)
		}
		l.RevisionLayouts["fixture-B"] = l.Model
		surge := normalTestPod("Role", "frontend", 3, "B", true, "entry")
		objects["pods"][string(surge.GetUID())] = surge
		l.Released[string(surge.GetUID())] = true
		deleteNormal(l, objects, "frontend", 0, 2)
		if l.error() != nil {
			t.Fatal("first healthy deletion should be permitted", l.error())
		}
		deleteNormal(l, objects, "frontend", 0, 1)
		if backendChanged {
			requireNormalViolation(t, l, "SKEW_VIOLATION")
		} else if l.error() != nil {
			t.Fatal("single Role incorrectly gated by itself", l.error())
		}
	}
}
