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
	Fixture     string                 `json:"fixture,omitempty"`
	Source      map[string]interface{} `json:"source"`
	Profile     string                 `json:"profile"`
	InitialSpec map[string]interface{} `json:"initialSpec"`
	Steps       []ScenarioStep         `json:"steps"`
}
type ScenarioStep struct {
	HistoryFault           string                 `json:"historyFault,omitempty"`
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
	RequireCompleted bool             `json:"requireCompleted,omitempty"`
	BlockedByBudget  bool             `json:"blockedByBudget,omitempty"`
	NoFullPromotion  bool             `json:"noFullPromotion,omitempty"`
	NoReplacement    bool             `json:"noReplacement,omitempty"`
	NoNewRevision    bool             `json:"noNewRevision,omitempty"`
	Targets          []ScenarioTarget `json:"targets,omitempty"`
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
	prefix := "RUN-"
	if strings.HasPrefix(c.ID, "DENY-") {
		prefix = "DENY-"
	}
	n, err := strconv.Atoi(strings.TrimPrefix(c.ID, prefix))
	rejection := prefix == "DENY-" && n >= 1 && n <= 97 && c.Format == "rollout-runner/v4"
	normal := n >= 61 && n <= 303 && c.Format == "rollout-runner/v2"
	recovery := n >= 304 && n <= 388 && c.Format == "rollout-runner/v3"
	restart := n >= 389 && n <= 400 && c.Format == "rollout-runner/v3"
	midRollout := n >= 401 && n <= 430 && c.Format == "rollout-runner/v3"
	graceRestart := n >= 431 && n <= 432 && c.Format == "rollout-runner/v3"
	controllerRestart := n >= 435 && n <= 440 && c.Format == "rollout-runner/v3"
	apiRetry := n >= 455 && n <= 462 && c.Format == "rollout-runner/v3"
	eviction := n >= 433 && n <= 434 && c.Format == "rollout-runner/v3"
	leaderSwitch := n >= 441 && n <= 442 && c.Format == "rollout-runner/v3"
	pluginRetry := n >= 536 && n <= 539 && c.Format == "rollout-runner/v3"
	deletionReplay := (n == 449 || n == 452) && c.Format == "rollout-runner/v3"
	initialSync := (n == 450 || n == 451 || n == 453 || n == 454) && c.Format == "rollout-runner/v3"
	lostDeletion := n >= 443 && n <= 448 && c.Format == "rollout-runner/v3"
	historyCreate := n >= 463 && n <= 522 && c.Format == "rollout-runner/v3"
	sparseBoundary := n >= 574 && n <= 603 && c.Format == "rollout-runner/v3"
	numericBoundary := n >= 540 && n <= 572 && c.Format == "rollout-runner/v2"
	identityBoundary := n == 611 && c.Format == "rollout-runner/v3"
	completionBoundary := n >= 604 && n <= 609 && c.Format == "rollout-runner/v3"
	dependencyBoundary := n == 573 && c.Format == "rollout-runner/v3"
	equalCollision := n == 610 && c.Format == "rollout-runner/v3"
	historyCollision := (n == 533 || n == 534) && c.Format == "rollout-runner/v3"
	historyObject := (n >= 523 && n <= 532 && n != 524 && n != 529) && c.Format == "rollout-runner/v3"
	historyRead := (n == 524 || n == 529) && c.Format == "rollout-runner/v3"
	historyGC := n == 535 && c.Format == "rollout-runner/v3"
	if err != nil || (!normal && !recovery && !restart && !midRollout && !graceRestart && !controllerRestart && !apiRetry && !lostDeletion && !pluginRetry && !leaderSwitch && !eviction && !initialSync && !deletionReplay && !historyCreate && !historyGC && !historyRead && !historyObject && !historyCollision && !numericBoundary && !rejection && !sparseBoundary && !equalCollision && !dependencyBoundary && !completionBoundary && !identityBoundary) || c.ID != fmt.Sprintf("%s%03d", prefix, n) || c.Baseline != ProductionCommit {
		return fmt.Errorf("invalid normal case identity/format/baseline")
	}
	s := c.Scenario
	sparseHistory := historyCreate && (n-463)%10 >= 5
	if sparseHistory && s.Fixture != "sparse-history-A" || sparseBoundary && s.Fixture != "sparse-boundary-A" || !sparseHistory && !sparseBoundary && s.Fixture != "" {
		return fmt.Errorf("scenario fixture is not enabled for this catalogue entry")
	}
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
		if p.Expect.RequireCompleted && !completionBoundary {
			return fmt.Errorf("explicit complete-state guard is restricted to sparse completion boundaries")
		}
		if p.Expect.BlockedByBudget && (!sparseBoundary || n != 592 && n != 598 || !p.Expect.NoReplacement || p.StableSeconds < 30) {
			return fmt.Errorf("budget-blocked stop is only declared by the two sparse zero-budget traps")
		}
		if p.HistoryFault != "" && p.Action != "history-object-recovery" {
			return fmt.Errorf("historical object fixture attached to unrelated action")
		}
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
		case "reject-update", "reject-merge-patch":
			if !rejection || p.Release != "none" || !p.Expect.NoNewRevision || (p.Action == "reject-update" && (len(p.Spec) == 0 || len(p.Patch) != 0)) || (p.Action == "reject-merge-patch" && (len(p.Patch) == 0 || len(p.Spec) != 0)) {
				return fmt.Errorf("rejection requires an actual raw API request and retained accepted history")
			}
		case "prepare-history-source":
			if !(historyRead || historyObject) || n < 528 || p.Name != "establish-protected-A-and-eligible-B" || p.StableSeconds != 10 || p.Release != "one" {
				return fmt.Errorf("source preparation only applies to the five Role historical source cases")
			}
			model, err := readModel(p.Spec)
			if err != nil || model.Mode != "Role" || model.N != 1 || model.Roles["frontend"].R != 3 || model.Roles["frontend"].W != 1 || model.Roles["frontend"].P != 1 {
				return fmt.Errorf("invalid mixed historical source")
			}
		case "new-identity-boundary":
			if !identityBoundary || p.Release != "one" || p.StableSeconds < 30 || p.Expect.NoReplacement || p.Expect.NoNewRevision {
				return fmt.Errorf("new identity requires actual old owner residue and new owned population")
			}
			if _, err := readModel(p.Spec); err != nil {
				return err
			}
		case "sparse-completion-boundary":
			if !p.Expect.RequireCompleted || !completionBoundary || p.Release != "none" || p.StableSeconds < 30 || !p.Expect.NoReplacement || !p.Expect.NoNewRevision {
				return fmt.Errorf("sparse completion must preserve actual B UIDs through automatic promotion before restart")
			}
			if _, err := readModel(p.Spec); err != nil {
				return err
			}
		case "stable-dependency-boundary":
			if !dependencyBoundary || p.Release != "one" || p.StableSeconds < 30 || p.Expect.NoReplacement || p.Expect.NoNewRevision {
				return fmt.Errorf("stable dependency boundary requires actual high surge and automatic completion")
			}
			if _, err := readModel(p.Spec); err != nil {
				return err
			}
		case "history-equal-collision":
			if !equalCollision || p.Release != "one" || p.StableSeconds < 30 || !p.Expect.NoNewRevision || p.Expect.NoReplacement || len(p.Spec) == 0 {
				return fmt.Errorf("equivalent collision requires actual AlreadyExists, same history UID and automatic B")
			}
			if _, err := readModel(p.Spec); err != nil {
				return err
			}
		case "history-collision-recovery":
			if !historyCollision || p.Release != "one" || p.StableSeconds < 30 || len(p.Spec) == 0 || p.Expect.NoReplacement || p.Expect.NoNewRevision {
				return fmt.Errorf("collision requires actual POST AlreadyExists and automatic allowed B recovery")
			}
		case "history-object-recovery":
			want := map[int]string{523: "missing", 525: "corrupt-data", 526: "missing-role", 527: "foreign-owner", 528: "missing", 530: "corrupt-data", 531: "missing-role", 532: "foreign-owner"}[n]
			if !historyObject || p.HistoryFault != want || !p.Expect.NoReplacement || p.Expect.NoNewRevision || p.Release != "one" || p.StableSeconds < 30 || len(p.Spec) != 0 {
				return fmt.Errorf("invalid exact historical object recovery fixture")
			}
		case "history-read-recovery":
			if !historyRead || !p.Expect.NoReplacement || !p.Expect.NoNewRevision || p.Release != "one" || p.StableSeconds < 30 || len(p.Spec) != 0 {
				return fmt.Errorf("history read recovery requires a bounded exact A read fault and protected restoration")
			}
		case "history-gc-list-error":
			if !historyGC || !p.Expect.NoReplacement || !p.Expect.NoNewRevision || p.StableSeconds < 30 || p.Release != "none" || p.TimeoutSeconds < 600 {
				return fmt.Errorf("history GC requires retained live references through the actual audit and retry window")
			}
		case "history-create-recovery":
			if !historyCreate || p.Release != "one" || p.StableSeconds < 30 {
				return fmt.Errorf("invalid history persistence recovery action")
			}
			if _, err := readModel(p.Spec); err != nil {
				return err
			}
		case "replay-old-deletions":
			if !deletionReplay || p.Release != "one" || p.StableSeconds < 30 {
				return fmt.Errorf("invalid protected old-UID deletion replay action")
			}
			if _, err := readModel(p.Spec); err != nil {
				return err
			}
		case "hold-initial-sync":
			if !initialSync || p.Release != "one" || p.StableSeconds < 30 || p.Expect.NoReplacement || p.Expect.NoNewRevision {
				return fmt.Errorf("initial-sync fault requires automatic B convergence after bounded hold")
			}
			if _, err := readModel(p.Spec); err != nil {
				return fmt.Errorf("initial-sync target: %w", err)
			}
		case "drop-old-deletions":
			if !lostDeletion || p.Release != "one" || p.TimeoutSeconds < 600 || p.TimeoutSeconds > 750 {
				return fmt.Errorf("lost-deletion cases require ordinary release and a bounded full audit window")
			}
			if _, err := readModel(p.Spec); err != nil {
				return fmt.Errorf("lost-deletion target: %w", err)
			}
		case "evict-ready-entry":
			if !eviction || len(p.Spec) != 0 || p.Release != "one" {
				return fmt.Errorf("invalid actual Eviction API action")
			}
		case "terminate-leader":
			if !leaderSwitch || len(p.Spec) != 0 || p.Release != "one" {
				return fmt.Errorf("invalid elected leader termination action")
			}
		case "retry-plugin-error":
			if !pluginRetry || len(p.Spec) != 0 || p.Release != "one" || p.Expect.NoReplacement {
				return fmt.Errorf("invalid plugin cleanup retry action")
			}
		case "retry-api-error":
			if !apiRetry || len(p.Spec) != 0 || p.Release != "one" || p.Expect.NoReplacement {
				return fmt.Errorf("invalid API retry action")
			}
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
	if identityBoundary && (len(s.Steps) != 1 || s.Steps[0].Action != "new-identity-boundary") {
		return fmt.Errorf("identity boundary cannot skip real old-owner residues")
	}
	if completionBoundary && (len(s.Steps) != 1 || s.Steps[0].Action != "sparse-completion-boundary") {
		return fmt.Errorf("sparse completion cannot skip automatic promotion before restart")
	}
	if dependencyBoundary && (len(s.Steps) != 1 || s.Steps[0].Action != "stable-dependency-boundary") {
		return fmt.Errorf("stable dependency boundary cannot skip its real source and post-clear gate")
	}
	if sparseBoundary && (len(s.Steps) != 1 || s.Steps[0].Action != "update" || s.Steps[0].StableSeconds < 30 || s.Steps[0].Expect.BlockedByBudget != (n == 592 || n == 598)) {
		return fmt.Errorf("sparse boundary requires its exact source and semantic target")
	}
	if rejection {
		active := n == 54 || n == 55 || n == 78 || n == 79 || n == 96 || n == 97
		index := 0
		if active || n == 94 || n == 95 {
			index = 1
		}
		want := index + 1
		if active {
			want++
		}
		if len(s.Steps) != want || (index == 1 && s.Steps[0].Action != "update") {
			return fmt.Errorf("rejection source/action sequence is incomplete")
		}
		p := s.Steps[index]
		method := "reject-update"
		if n == 85 || n == 86 || n == 92 || n == 93 {
			method = "reject-merge-patch"
		}
		if p.Action != method || p.Expect.NoReplacement == active || p.StableSeconds < 30 && !active || p.StableSeconds < 10 || !active && p.Until != "settled" || active && (p.Until != "conditions" || len(p.Conditions) == 0) {
			return fmt.Errorf("rejection request or atomic observation is missing")
		}
		if active && (s.Steps[2].Action != "observe" || s.Steps[2].Release != "one" || s.Steps[2].Until != "settled" || s.Steps[2].StableSeconds < 30 || s.Steps[2].Expect.NoReplacement) {
			return fmt.Errorf("in-flight rejection must continue the last accepted rollout")
		}
		if textValue(s.Source, "kind") != "拒绝" {
			return fmt.Errorf("rejection source mismatch")
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
	if eviction && (len(s.Steps) != 2 || s.Steps[0].Action != "update" || s.Steps[1].Action != "evict-ready-entry") {
		return fmt.Errorf("eviction requires actual A/B mixture followed by Eviction API")
	}
	if leaderSwitch && (len(s.Steps) != 2 || s.Steps[0].Action != "update" || s.Steps[1].Action != "terminate-leader") {
		return fmt.Errorf("leader switch requires actual A/B mixed rollout then elected leader deletion")
	}
	if pluginRetry && (len(s.Steps) != 2 || s.Steps[0].Action != "update" || s.Steps[0].Release != "none" || s.Steps[1].Action != "retry-plugin-error") {
		return fmt.Errorf("plugin cleanup faults require a blocked first B surge followed by two finite hook failures")
	}
	if apiRetry && (len(s.Steps) != 2 || s.Steps[0].Action != "update" || s.Steps[0].Release != "none" || s.Steps[1].Action != "retry-api-error") {
		return fmt.Errorf("API retry cases require an unfinished B window then exactly one finite failure")
	}
	if deletionReplay && (len(s.Steps) != 1 || s.Steps[0].Action != "replay-old-deletions") {
		return fmt.Errorf("deletion replay requires protected recovery followed by actual old event replay")
	}
	if initialSync && (len(s.Steps) != 1 || s.Steps[0].Action != "hold-initial-sync") {
		return fmt.Errorf("initial-sync cases require their complete startup fault and resumption")
	}
	if lostDeletion && (len(s.Steps) != 1 || s.Steps[0].Action != "drop-old-deletions") {
		return fmt.Errorf("lost-deletion cases require one uninterrupted controller run")
	}
	ordinals := "O={0,1,2}"
	if sparseHistory {
		ordinals = "O={0,3,4}"
	}
	if historyCreate && (len(s.Steps) != 1 || s.Steps[0].Action != "history-create-recovery" || !strings.Contains(textValue(s.Source, "initial"), ordinals)) {
		return fmt.Errorf("history persistence requires its full compound action and actual supported ordinal fixture")
	}
	boundarySteps := 1
	if n == 567 || n == 569 {
		boundarySteps = 2
	}
	if numericBoundary && (len(s.Steps) != boundarySteps || s.Steps[0].Action != "update" || s.Steps[0].StableSeconds < 30 || boundarySteps == 2 && (s.Steps[1].Action != "update" || s.Steps[1].StableSeconds < 30)) {
		return fmt.Errorf("numeric boundary requires literal A-to-B request and stable allowed target")
	}
	if equalCollision && (len(s.Steps) != 1 || s.Steps[0].Action != "history-equal-collision") {
		return fmt.Errorf("equivalent collision requires its complete native API race")
	}
	if historyCollision && (len(s.Steps) != 1 || s.Steps[0].Action != "history-collision-recovery") {
		return fmt.Errorf("collision requires precreated conflict plus native Create AlreadyExists sequence")
	}
	historySourceAction := "update"
	if (historyRead || historyObject) && n >= 528 {
		historySourceAction = "prepare-history-source"
	}
	if historyObject && (len(s.Steps) != 2 || s.Steps[0].Action != historySourceAction || s.Steps[1].Action != "history-object-recovery") {
		return fmt.Errorf("historical object fault requires actual protected A/B source and restoration")
	}
	if historyRead && (len(s.Steps) != 2 || s.Steps[0].Action != historySourceAction || s.Steps[1].Action != "history-read-recovery" || !strings.Contains(textValue(s.Source, "initial"), "API读取失败")) {
		return fmt.Errorf("historical read failure requires actual protected A/B stop then fault and restoration")
	}
	if historyGC && (len(s.Steps) != 2 || s.Steps[0].Action != "update" || s.Steps[1].Action != "history-gc-list-error" || intValue(s.InitialSpec, "revisionHistoryLimit", -1) != 0) {
		return fmt.Errorf("history GC requires limit zero, actual frontend B/backend A, then an exact live-reference List fault")
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
		} else if sg && d > 0 && p > 0 {
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
