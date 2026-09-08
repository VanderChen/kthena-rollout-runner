// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const ProductionCommit = "538b2825c06bc1e8c5392d18f18f84faee9fca95"

type Scenario struct {
	Source      map[string]interface{} `json:"source"`
	Profile     string                 `json:"profile"`
	InitialSpec map[string]interface{} `json:"initialSpec"`
	Steps       []ScenarioStep         `json:"steps"`
}
type ScenarioStep struct {
	ContainerRestart       *PodFaultAction        `json:"containerRestart,omitempty"`
	PodFault               *PodFaultAction        `json:"podFault,omitempty"`
	RequireLiveTerminating bool                   `json:"requireLiveTerminating,omitempty"`
	Name                   string                 `json:"name"`
	Action                 string                 `json:"action"`
	Spec                   map[string]interface{} `json:"spec,omitempty"`
	Patch                  map[string]interface{} `json:"patch,omitempty"`
	Until                  string                 `json:"until"`
	Conditions             []ScenarioCondition    `json:"conditions,omitempty"`
	Release                string                 `json:"release"`
	Exclude                []ScenarioCondition    `json:"exclude,omitempty"`
	HoldSeconds            int                    `json:"holdSeconds"`
	StableSeconds          int                    `json:"stableSeconds"`
	TimeoutSeconds         int                    `json:"timeoutSeconds,omitempty"`
	Expect                 ScenarioExpectation    `json:"expect"`
}
type ScenarioCondition struct {
	Kind    string `json:"kind"`
	Group   *int   `json:"group,omitempty"`
	Role    string `json:"role,omitempty"`
	Ordinal *int   `json:"ordinal,omitempty"`
	Version string `json:"version,omitempty"`
	Ready   *bool  `json:"ready,omitempty"`
	Count   int    `json:"count"`
}
type ScenarioExpectation struct {
	NoFullPromotion bool             `json:"noFullPromotion,omitempty"`
	NoReplacement   bool             `json:"noReplacement,omitempty"`
	NoNewRevision   bool             `json:"noNewRevision,omitempty"`
	Targets         []ScenarioTarget `json:"targets,omitempty"`
}
type ScenarioTarget struct {
	MinVersions map[string]int    `json:"minVersions,omitempty"`
	MinCount    int               `json:"minCount,omitempty"`
	MaxCount    int               `json:"maxCount,omitempty"`
	Scope       string            `json:"scope,omitempty"`
	Group       *int              `json:"group,omitempty"`
	Role        string            `json:"role"`
	Versions    map[string]int    `json:"versions"`
	Ordinals    map[string]string `json:"ordinals,omitempty"`
	Workers     map[string]int    `json:"workers,omitempty"`
}

func (r *Runner) testedCommit() string {
	if r.opt.ControllerCommit != "" {
		return r.opt.ControllerCommit
	}
	return Baseline
}
func (c Case) validateScenario() error {
	n, err := strconv.Atoi(strings.TrimPrefix(c.ID, "RUN-"))
	normal := n >= 61 && n <= 303 && c.Format == "rollout-runner/v2"
	recovery := n >= 304 && n <= 388 && c.Format == "rollout-runner/v3"
	restart := n >= 389 && n <= 400 && c.Format == "rollout-runner/v3"
	midRollout := n >= 401 && n <= 430 && c.Format == "rollout-runner/v3"
	graceRestart := n >= 431 && n <= 432 && c.Format == "rollout-runner/v3"
	controllerRestart := n >= 435 && n <= 440 && c.Format == "rollout-runner/v3"
	if err != nil || (!normal && !recovery && !restart && !midRollout && !graceRestart && !controllerRestart) || c.ID != fmt.Sprintf("RUN-%03d", n) || c.Baseline != ProductionCommit {
		return fmt.Errorf("invalid normal case identity/format/baseline")
	}
	s := c.Scenario
	if s.Source["id"] != c.ID {
		return fmt.Errorf("catalogue ID mismatch")
	}
	if s.Profile != "controlled" && s.Profile != "auto" {
		return fmt.Errorf("unsupported readiness profile %q", s.Profile)
	}
	if _, err := readModel(s.InitialSpec); err != nil {
		return fmt.Errorf("initial spec: %w", err)
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("missing executable steps")
	}
	for _, p := range s.Steps {
		if p.ContainerRestart != nil && p.Action != "restart-container" && p.Action != "restart-container-grace" {
			return fmt.Errorf("container restart attached to unrelated action")
		}
		if p.PodFault != nil && p.Action != "recover-pod" {
			return fmt.Errorf("Pod fault attached to unrelated action")
		}
		if p.Name == "" || p.HoldSeconds < 0 || p.StableSeconds < 0 || p.TimeoutSeconds < 0 {
			return fmt.Errorf("invalid step")
		}
		switch p.Action {
		case "resume-after-grace":
			if !graceRestart {
				return fmt.Errorf("grace resumption not declared by this case")
			}
		case "terminate-controller":
			if !controllerRestart {
				return fmt.Errorf("controller termination not declared by this case")
			}
		case "verify-resource-stop":
			if !midRollout || n > 424 || (n-401)%4 != 0 {
				return fmt.Errorf("resource fault not declared by this case")
			}
		case "drop-ready", "restore-ready":
			if !midRollout || n > 424 || (n-401)%4 != 2 {
				return fmt.Errorf("readiness fault not declared by this case")
			}
		case "restart-container":
			f := p.ContainerRestart
			if !restart || f == nil || f.Group != 0 || f.Role != "frontend" || f.Ordinal != 0 || f.Member != "entry" || len(p.Spec) != 0 || !p.Expect.NoReplacement || !p.Expect.NoNewRevision {
				return fmt.Errorf("invalid in-place container restart action")
			}
		case "restart-container-grace":
			f := p.ContainerRestart
			if !graceRestart || f == nil || f.Group != 0 || f.Role != "frontend" || f.Ordinal != 0 || f.Member != "entry" || len(p.Spec) != 0 || p.Expect.NoReplacement || !p.Expect.NoNewRevision || p.Release != "none" {
				return fmt.Errorf("invalid bounded grace restart action")
			}
		case "recover-pod":
			if !recovery || p.PodFault == nil || p.PodFault.Group != 0 || p.PodFault.Role != "frontend" || p.PodFault.Ordinal != 0 || (p.PodFault.Member != "entry" && p.PodFault.Member != "worker") {
				return fmt.Errorf("invalid typed recovery action")
			}
			if _, err := readModel(p.Spec); err != nil {
				return fmt.Errorf("recovery target: %w", err)
			}
		case "update":
			if _, err := readModel(p.Spec); err != nil {
				return fmt.Errorf("%s: %w", p.Name, err)
			}
		case "merge-patch":
			if len(p.Patch) == 0 {
				return fmt.Errorf("empty patch")
			}
		case "observe", "restart-controller", "pin", "unpin", "configmap", "restore-status", "block-resources", "restore-resources":
		default:
			return fmt.Errorf("unsupported action %q", p.Action)
		}
		switch p.Until {
		case "settled":
		case "conditions":
			if len(p.Conditions) == 0 {
				return fmt.Errorf("missing trigger conditions")
			}
		default:
			return fmt.Errorf("unsupported predicate %q", p.Until)
		}
		switch p.Release {
		case "none", "all", "one", "except":
		default:
			return fmt.Errorf("unsupported release %q", p.Release)
		}
		if s.Profile == "auto" && p.Release != "none" {
			return fmt.Errorf("automatic profile cannot use controlled readiness")
		}
		for _, cond := range p.Conditions {
			switch cond.Kind {
			case "unit", "started", "terminating", "pending", "unschedulable", "running-not-ready", "image-pull-backoff":
			default:
				return fmt.Errorf("unsupported condition %q", cond.Kind)
			}
			if cond.Count < 1 {
				return fmt.Errorf("condition requires positive count")
			}
		}
	}
	if recovery && (len(s.Steps) != 1 || s.Steps[0].Action != "recover-pod") {
		return fmt.Errorf("Pod recovery cases require their complete compound action")
	}
	if restart && (len(s.Steps) != 2 || s.Steps[0].Action != "restart-container" || s.Steps[1].Action != "update" || textValue(s.InitialSpec, "recoveryPolicy") != "None") {
		return fmt.Errorf("container restart cases require None, in-place restart then template update")
	}
	if graceRestart && (len(s.Steps) != 3 || s.Steps[0].Action != "update" || s.Steps[1].Action != "restart-container-grace" || s.Steps[2].Action != "resume-after-grace" || textValue(s.InitialSpec, "recoveryPolicy") != "RoleRecreate" || intValue(mapValue(s.InitialSpec, "template"), "restartGracePeriodSeconds", -1) != 30 || intValue(mapValue(s.Steps[0].Spec, "template"), "restartGracePeriodSeconds", -1) != 30) {
		return fmt.Errorf("grace cases require accepted template grace=30, B window, in-place restart then convergence")
	}
	if controllerRestart {
		terminations := 0
		for _, step := range s.Steps {
			if step.Action == "terminate-controller" {
				terminations++
			}
		}
		if terminations != 1 {
			return fmt.Errorf("controller recovery cases require exactly one declared termination")
		}
	}
	if s.Steps[len(s.Steps)-1].Until != "settled" {
		return fmt.Errorf("last step must verify a settled state")
	}
	return nil
}
func cloneMap(in map[string]interface{}) map[string]interface{} {
	b, _ := json.Marshal(in)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return out
}
func objectForSpec(ns, id string, spec map[string]interface{}) *unstructured.Unstructured {
	m := map[string]interface{}{"apiVersion": "workload.serving.volcano.sh/v1alpha1", "kind": "ModelServing", "metadata": map[string]interface{}{"name": "model", "namespace": ns, "labels": map[string]interface{}{"rollout-runner/case": id}}, "spec": spec}
	b, _ := json.Marshal(m)
	u := &unstructured.Unstructured{}
	_ = u.UnmarshalJSON(b)
	return u
}
func mapValue(m map[string]interface{}, k string) map[string]interface{} {
	v, _ := m[k].(map[string]interface{})
	return v
}
func listValue(m map[string]interface{}, k string) []interface{} {
	v, _ := m[k].([]interface{})
	return v
}
func intValue(m map[string]interface{}, k string, fallback int) int {
	if m[k] == nil {
		return fallback
	}
	return number(m, k, fallback)
}
func textValue(m map[string]interface{}, k string) string { v, _ := m[k].(string); return v }

// Resolve percentages from desired capacity, never from the number of observed
// Pods or surge objects. SG positive U has the documented minimum of one.
func budget(m map[string]interface{}, k string, d int, sg bool) (int, error) {
	fallback := 0
	if k == "maxUnavailable" {
		fallback = 1
	}
	if m[k] == nil {
		return fallback, nil
	}
	if s, ok := m[k].(string); ok {
		if !strings.HasSuffix(s, "%") {
			return 0, fmt.Errorf("invalid %s=%q", k, s)
		}
		p, err := strconv.Atoi(strings.TrimSuffix(s, "%"))
		if err != nil || p < 0 {
			return 0, fmt.Errorf("invalid percentage")
		}
		v := d * p / 100
		if k != "maxUnavailable" {
			v = (d*p + 99) / 100
		} else if sg && p > 0 {
			v = max(v, 1)
		}
		return v, nil
	}
	v := number(m, k, -1)
	if v < 0 {
		return 0, fmt.Errorf("invalid %s", k)
	}
	return v, nil
}

type RoleLayout struct {
	Name          string
	R, W, U, S, P int
	Entry, Worker string
}
type NormalModel struct {
	Mode       string
	N, U, S, P int
	Roles      map[string]RoleLayout
	Spec       map[string]interface{}
}

func templateVersion(t map[string]interface{}) string {
	for _, raw := range listValue(mapValue(t, "spec"), "containers") {
		c := raw.(map[string]interface{})
		for _, raw := range listValue(c, "env") {
			v := raw.(map[string]interface{})
			if v["name"] == "ROLLOUT_VERSION" {
				return textValue(v, "value")
			}
		}
	}
	return ""
}
func readModel(spec map[string]interface{}) (NormalModel, error) {
	m := NormalModel{Mode: "SG", N: intValue(spec, "replicas", 1), Roles: map[string]RoleLayout{}, Spec: cloneMap(spec)}
	if spec == nil || m.N < 0 {
		return m, fmt.Errorf("invalid spec/replicas")
	}
	strategy := mapValue(spec, "rolloutStrategy")
	if textValue(strategy, "type") == "RoleRollingUpdate" {
		m.Mode = "Role"
	}
	var err error
	if m.Mode == "SG" {
		b := mapValue(strategy, "rollingUpdateConfiguration")
		m.U, err = budget(b, "maxUnavailable", m.N, true)
		if err != nil {
			return m, err
		}
		m.S, err = budget(b, "maxSurge", m.N, true)
		if err != nil {
			return m, err
		}
		m.P, err = budget(b, "partition", m.N, true)
		if err != nil {
			return m, err
		}
	}
	roles := listValue(mapValue(spec, "template"), "roles")
	if len(roles) == 0 {
		return m, fmt.Errorf("no roles")
	}
	for _, raw := range roles {
		r, ok := raw.(map[string]interface{})
		if !ok {
			return m, fmt.Errorf("invalid role")
		}
		l := RoleLayout{Name: textValue(r, "name"), R: intValue(r, "replicas", 1), W: intValue(r, "workerReplicas", -1), Entry: templateVersion(mapValue(r, "entryTemplate")), Worker: templateVersion(mapValue(r, "workerTemplate"))}
		if l.Name == "" || l.R < 0 || l.W < 0 || l.Entry == "" || (l.W > 0 && l.Worker == "") {
			return m, fmt.Errorf("invalid layout %s", l.Name)
		}
		if _, ok := m.Roles[l.Name]; ok {
			return m, fmt.Errorf("duplicate role")
		}
		if m.Mode == "Role" {
			l.U, err = budget(r, "maxUnavailable", l.R, false)
			if err != nil {
				return m, err
			}
			l.S, err = budget(r, "maxSurge", l.R, false)
			if err != nil {
				return m, err
			}
			l.P, err = budget(r, "partition", l.R, false)
			if err != nil {
				return m, err
			}
		}
		m.Roles[l.Name] = l
	}
	return m, nil
}
func sameRole(a, b RoleLayout) bool {
	return a.Name == b.Name && a.W == b.W && a.Entry == b.Entry && a.Worker == b.Worker
}
func sameTemplates(a, b NormalModel) bool {
	if len(a.Roles) != len(b.Roles) {
		return false
	}
	for k, v := range a.Roles {
		if !sameRole(v, b.Roles[k]) {
			return false
		}
	}
	return true
}
