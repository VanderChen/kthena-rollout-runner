// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const Baseline = "e2578d01859bb98d9a85846bafbfb2c771a6f117"

type Case struct {
	Scenario *Scenario `json:"scenario,omitempty"`
	Format   string    `json:"format"`
	ID       string    `json:"id"`
	Baseline string    `json:"baseline"`
	Input    struct {
		Spec map[string]interface{} `json:"spec"`
	} `json:"input"`
	Update struct {
		Roles []string `json:"roles"`
		From  string   `json:"from"`
		To    string   `json:"to"`
	} `json:"update"`
	Expect  Expectation `json:"expect"`
	Process struct {
		HoldSeconds int    `json:"holdSeconds"`
		Release     string `json:"release"`
	} `json:"process"`
}
type Expectation struct {
	Mode           string   `json:"mode"`
	Desired        int      `json:"desired"`
	U              int      `json:"maxUnavailable"`
	S              int      `json:"maxSurge"`
	P              int      `json:"partition"`
	StartOrder     []int    `json:"startOrder"`
	Protected      []int    `json:"protectedOrdinals"`
	UnchangedRoles []string `json:"unchangedRoles"`
	FinalOld       int      `json:"finalOld"`
	FinalNew       int      `json:"finalNew"`
	InitialStarts  int      `json:"initialStarts"`
}

func LoadCases(dir string) ([]Case, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "RUN-*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []Case
	seen := map[string]bool{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var c Case
		if err = yaml.UnmarshalStrict(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("duplicate ID %s", c.ID)
		}
		seen[c.ID] = true
		if err = c.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no cases in %s", dir)
	}
	return out, nil
}
func (c Case) Validate() error {
	if c.Scenario != nil {
		return c.validateScenario()
	}
	e := c.Expect
	if len(c.ID) != 7 || !strings.HasPrefix(c.ID, "RUN-") {
		return fmt.Errorf("invalid case ID")
	}
	if n, err := strconv.Atoi(c.ID[4:]); err != nil || n < 1 || fmt.Sprintf("RUN-%03d", n) != c.ID {
		return fmt.Errorf("invalid case ID")
	}
	if c.Format != "rollout-runner/v1" || c.Baseline != Baseline || c.ID == "" {
		return fmt.Errorf("unsupported case format/baseline")
	}
	if e.Mode != "SG" && e.Mode != "Role" {
		return fmt.Errorf("unknown mode")
	}
	if e.Desired < 1 || e.U < 0 || e.S < 0 || e.P < 0 || e.P > e.Desired {
		return fmt.Errorf("invalid budget")
	}
	if e.FinalOld != e.P || e.FinalNew != e.Desired-e.P || e.InitialStarts != min(e.U, e.FinalNew) {
		return fmt.Errorf("inconsistent expectation")
	}
	if len(e.StartOrder) != e.FinalNew || len(e.Protected) != e.P {
		return fmt.Errorf("incomplete ordinal expectations")
	}
	for i, n := range e.StartOrder {
		if n != e.Desired-1-i {
			return fmt.Errorf("invalid start order")
		}
	}
	for i, n := range e.Protected {
		if n != i {
			return fmt.Errorf("invalid protected ordinal")
		}
	}
	if e.FinalNew > 0 && e.U+e.S == 0 {
		return fmt.Errorf("nonprogressing budget")
	}
	if c.Process.HoldSeconds < 1 || c.Process.Release != "one-unit-at-a-time" {
		return fmt.Errorf("unsupported process")
	}
	if len(c.Update.Roles) != 1 || c.Update.Roles[0] != "frontend" || c.Update.From != "A" || c.Update.To != "B" {
		return fmt.Errorf("unsupported update")
	}
	if c.Input.Spec == nil {
		return fmt.Errorf("missing input spec")
	}
	if err := c.checkSpec(c.Input.Spec, false); err != nil {
		return err
	}
	return nil
}

// checkSpec is deliberately independent of Kthena controller budget helpers.
// This first suite accepts integer budgets and the explicit core fixture shape.
func (c Case) checkSpec(spec map[string]interface{}, defaulted bool) error {
	strategy, ok := spec["rolloutStrategy"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing strategy")
	}
	wantType := "ServingGroupRollingUpdate"
	wantN := c.Expect.Desired
	if c.Expect.Mode == "Role" {
		wantType = "RoleRollingUpdate"
		wantN = 1
	}
	if strategy["type"] != wantType || number(spec, "replicas", -1) != wantN {
		return fmt.Errorf("input mode/replicas do not match expectation")
	}
	tmpl, ok := spec["template"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing template")
	}
	roles, ok := tmpl["roles"].([]interface{})
	if !ok {
		return fmt.Errorf("missing roles")
	}
	wantRoles := 1
	if c.Expect.Mode == "Role" {
		wantRoles = 2
	}
	if len(roles) != wantRoles {
		return fmt.Errorf("unsupported core Role layout")
	}
	seen := map[string]bool{}
	var front map[string]interface{}
	for _, raw := range roles {
		role, ok := raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid role")
		}
		name, ok := role["name"].(string)
		if !ok || seen[name] {
			return fmt.Errorf("invalid/duplicate Role name")
		}
		seen[name] = true
		if name != "frontend" && (name != "backend" || c.Expect.Mode != "Role") {
			return fmt.Errorf("unsupported Role")
		}
		r := 1
		if c.Expect.Mode == "Role" {
			r = c.Expect.Desired
		}
		if number(role, "replicas", -1) != r || number(role, "workerReplicas", -1) < 0 {
			return fmt.Errorf("unsupported Role replicas/worker layout")
		}
		if defaulted {
			if _, ok := role["maxUnavailable"]; !ok {
				return fmt.Errorf("API did not default Role.maxUnavailable")
			}
		}
		if name == "frontend" {
			front = role
		}
	}
	if front == nil {
		return fmt.Errorf("missing frontend")
	}
	budget := front
	if c.Expect.Mode == "SG" {
		budget, _ = strategy["rollingUpdateConfiguration"].(map[string]interface{})
	}
	if number(budget, "maxUnavailable", 1) != c.Expect.U || number(budget, "maxSurge", 0) != c.Expect.S || number(budget, "partition", 0) != c.Expect.P {
		return fmt.Errorf("effective U/S/P differs from reviewed expectation")
	}
	return nil
}
func number(m map[string]interface{}, key string, fallback int) int {
	v, ok := m[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case int64:
		return int(n)
	case float64:
		if n == float64(int(n)) {
			return int(n)
		}
	}
	return -999999
}
func (c Case) Manifest(ns string, version string) (*unstructured.Unstructured, error) {
	// JSON round-trip converts numeric types and never fills optional rolling fields.
	b, err := json.Marshal(c.Input.Spec)
	if err != nil {
		return nil, err
	}
	var spec map[string]interface{}
	if err = json.Unmarshal(b, &spec); err != nil {
		return nil, err
	}
	spec["schedulerName"] = "volcano"
	spec["plugins"] = []interface{}{
		map[string]interface{}{"name": "headless-service", "type": "BuiltIn"},
		map[string]interface{}{"name": "ranktable", "type": "BuiltIn", "config": map[string]interface{}{"template": "production020-ranktable-template"}},
	}
	tmpl, ok := spec["template"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing template")
	}
	roles, ok := tmpl["roles"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("missing roles")
	}
	for _, raw := range roles {
		role := raw.(map[string]interface{})
		name := role["name"].(string)
		v := "A"
		if name == "frontend" {
			v = version
		}
		role["entryTemplate"] = podTemplate(name, "entry", v)
		if w, ok := role["workerReplicas"].(float64); ok && w > 0 {
			role["workerTemplate"] = podTemplate(name, "worker", v)
		}
	}
	obj := map[string]interface{}{
		"apiVersion": "workload.serving.volcano.sh/v1alpha1", "kind": "ModelServing",
		"metadata": map[string]interface{}{"name": "model", "namespace": ns, "labels": map[string]interface{}{"rollout-runner/case": c.ID}},
		"spec":     spec,
	}
	// Unstructured requires int64 for integer-valued API fields, not Go int.
	b, err = json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{}
	err = u.UnmarshalJSON(b)
	return u, err
}
func podTemplate(role, member, version string) map[string]interface{} {
	return map[string]interface{}{
		"metadata": map[string]interface{}{"annotations": map[string]interface{}{
			"production.kthena.io/ranktable": fmt.Sprintf("{\"pod_name\":%q,\"server_id\":%q}", role+"-"+member, role),
		}},
		"spec": map[string]interface{}{
			"terminationGracePeriodSeconds": int64(1),
			"containers": []interface{}{map[string]interface{}{
				"name": "workload", "image": "busybox:1.36", "imagePullPolicy": "IfNotPresent",
				"command": []interface{}{"sh", "-c", `if [ "$ROLLOUT_VERSION" = A ]; then touch /tmp/ready; fi; sleep 3600`},
				"env":     []interface{}{map[string]interface{}{"name": "ROLLOUT_VERSION", "value": version}},
				"readinessProbe": map[string]interface{}{
					"exec":          map[string]interface{}{"command": []interface{}{"test", "-f", "/tmp/ready"}},
					"periodSeconds": int64(1), "failureThreshold": int64(1),
				},
				"resources": map[string]interface{}{"requests": map[string]interface{}{"cpu": "5m", "memory": "4Mi"}},
			}},
		},
	}
}
func saveYAML(path string, v interface{}) error {
	b, e := yaml.Marshal(v)
	if e != nil {
		return e
	}
	return os.WriteFile(path, b, 0644)
}
