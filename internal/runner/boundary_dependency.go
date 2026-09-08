// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kthena.local/rollout-runner/internal/faultproxy"
)

// Build the actual extra B Ready backend while retaining stable A slots. The
// decisive gate observation occurs AFTER clearing every preparation DELETE
// hold, with a stable B replacement deliberately still NotReady.
func (e *normalExecution) stableDependencyBoundary(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-original-pods.yaml"), pods); err != nil {
		return err
	}
	var ids []string
	cleared := false
	defer func() {
		if !cleared {
			result = errors.Join(result, e.resumeRecovery(ids, prefix+"-preparation-cleanup"))
		}
	}()
	for _, pod := range pods.Items {
		if !owned(&pod, e.l.Owner) || pod.Labels[LabelRole] != "backend" {
			continue
		}
		if !podReady(&pod) || podVersion(&pod) != "A" || ordinal(pod.Labels[LabelRoleID]) >= 3 {
			return fmt.Errorf("INCONCLUSIVE: invalid original stable dependency source")
		}
		rule := faultproxy.Rule{ID: e.r.opt.RunID + "-" + strings.ToLower(e.c.ID) + "-old-backend-" + pod.Name, Namespace: e.namespace, Resource: "pods", Name: pod.Name, Methods: []string{"DELETE"}, Mode: "hold", Count: -1, DurationSeconds: 120}
		ids = append(ids, rule.ID)
		var actual faultproxy.RuleStatus
		if err = e.r.faultControl(ctx, "POST", "/v1/rules", rule, &actual); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(e.dir, fmt.Sprintf("%s-dependency-rule-%d.json", prefix, len(ids)-1)), actual); err != nil {
			return err
		}
		if !reflect.DeepEqual(actual.Rule, rule) {
			return fmt.Errorf("INCONCLUSIVE: dependency preparation rule mismatch")
		}
	}
	if len(ids) != 3 {
		return fmt.Errorf("INCONCLUSIVE: dependency source requires three actual backend slots")
	}
	ready, notReady := true, false
	surge, stable := 3, 2
	conditions := []ScenarioCondition{{Kind: "unit", Role: "frontend", Version: "A", Ready: &ready, Count: 3}, {Kind: "unit", Role: "backend", Version: "A", Ready: &ready, Count: 3}, {Kind: "unit", Role: "backend", Ordinal: &surge, Version: "B", Ready: &notReady, Count: 1}}
	submit := ScenarioStep{Name: "dependency-both-B-accepted", Action: "update", Spec: p.Spec, Until: "conditions", Release: "none", Conditions: conditions, TimeoutSeconds: 60}
	if err = e.step(ctx, submit); err != nil {
		return err
	}
	conditions[2].Ready = &ready
	source := ScenarioStep{Name: "actual-high-backend-B-Ready-only", Until: "conditions", Release: "one", Exclude: []ScenarioCondition{{Kind: "unit", Role: "frontend"}}, Conditions: conditions, StableSeconds: 5, TimeoutSeconds: 60}
	e.waitDeadline = time.Time{}
	if err = e.wait(ctx, source); err != nil {
		return err
	}
	if err = e.waitFaultState(ctx, prefix+"-actual-old-backend-delete-held", ids, func(s faultproxy.State) bool {
		hits := 0
		for _, r := range s.Rules {
			for _, id := range ids {
				if r.ID == id {
					if !r.Active || r.Released != 0 {
						return false
					}
					hits += r.Hits
				}
			}
		}
		return hits > 0
	}); err != nil {
		return err
	}
	pods, err = e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-source-pods.yaml"), pods); err != nil {
		return err
	}
	if err = e.snapshot(prefix + "-high-only-source"); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-source-boundary.json"), map[string]interface{}{"at": time.Now().UTC(), "generation": e.current.GetGeneration(), "ownerUID": e.l.Owner, "preparationRules": ids}); err != nil {
		return err
	}
	if err = e.resumeRecovery(ids, prefix+"-preparation"); err != nil {
		return err
	}
	cleared = true
	// No hold is active during this gate. Even with a Ready surge, frontend
	// cannot start before a stable backend B slot is Ready.
	conditions[1].Count = 2
	conditions = append(conditions, ScenarioCondition{Kind: "unit", Role: "backend", Ordinal: &stable, Version: "B", Ready: &notReady, Count: 1})
	gate := ScenarioStep{Name: "cleared-proxy-stable-backend-B-not-ready-gate", Until: "conditions", Release: "none", Conditions: conditions, StableSeconds: 10, TimeoutSeconds: 60}
	e.waitDeadline = time.Time{}
	if err = e.wait(ctx, gate); err != nil {
		return err
	}
	if err = e.snapshot(prefix + "-stable-slot-not-ready"); err != nil {
		return err
	}
	p.Name = "stable-backend-ready-then-automatic-frontend"
	e.historyReferences = true
	e.waitDeadline = time.Time{}
	return e.finishStep(ctx, p, prefix+"-final")
}
