// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"kthena.local/rollout-runner/internal/faultproxy"
)

// A declared partial expansion source is an interleaving fixture. Prevent the
// next ordinal's creation until the subsequent spec is accepted, so a legal
// controller batch cannot skip the source state. No product batch-size limit.
func partialScaleBoundary(step ScenarioStep) (int, bool) {
	if !step.SourceState || step.Action != "update" || step.Expect.Compound == nil {
		return 0, false
	}
	next, err := readModelForContract(step.Spec, true)
	if err != nil || step.Expect.Compound.Active >= next.N {
		return 0, false
	}
	n := step.Expect.Compound.Active
	if n < 1 || len(step.Expect.Compound.Groups) != n {
		return 0, false
	}
	seen := map[int]bool{}
	for _, g := range step.Expect.Compound.Groups {
		seen[g.Ordinal] = true
	}
	for i := 0; i < n; i++ {
		if !seen[i] {
			return 0, false
		}
	}
	return n, true
}
func needsPartialScaleBoundary(c Case) bool {
	if c.Format != "rollout-runner/compound-v2" || c.Scenario == nil {
		return false
	}
	for _, step := range c.Scenario.Steps {
		if _, ok := partialScaleBoundary(step); ok {
			return true
		}
	}
	return false
}
func (e *normalExecution) installPartialScaleBoundary(ctx context.Context, p ScenarioStep) error {
	ordinal, ok := partialScaleBoundary(p)
	if !e.l.CompoundV2 || !ok {
		return nil
	}
	id := fmt.Sprintf("%s-%s-scale-boundary-%d", e.r.opt.RunID, strings.ToLower(e.c.ID), e.phase)
	// Return a retryable error, rather than forwarding an old held POST after
	// the next spec. The controller must recompute creation from that new spec.
	rule := faultproxy.Rule{ID: id, Namespace: e.namespace, Resource: "podgroups", Name: fmt.Sprintf("model-%d", ordinal), Methods: []string{"POST"}, Mode: "error", StatusCode: 503, Count: -1, DurationSeconds: 300}
	e.partialScaleRule = id
	e.partialScalePhase = e.phase
	var installed faultproxy.RuleStatus
	if err := e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
		return err
	}
	return writeJSON(filepath.Join(e.dir, fmt.Sprintf("step-%02d-partial-scale-barrier.json", e.phase)), installed)
}
func (e *normalExecution) releasePartialScaleBoundary() error {
	if e.partialScaleRule == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	state, err := e.r.faultState(ctx)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(e.dir, fmt.Sprintf("step-%02d-partial-scale-release.json", e.phase)), state); err != nil {
		return err
	}
	if err := e.r.faultControl(ctx, "DELETE", "/v1/rules/"+e.partialScaleRule, nil, nil); err != nil {
		return err
	}
	e.partialScaleRule = ""
	return nil
}
