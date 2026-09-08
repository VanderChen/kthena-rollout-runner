// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kthena.local/rollout-runner/internal/faultproxy"
)

func historyGCListRule(id, namespace string) faultproxy.Rule {
	zero := int64(0)
	return faultproxy.Rule{ID: id, Namespace: namespace, Resource: "pods", Methods: []string{"GET"}, CollectionOnly: true, Mode: "error", StatusCode: 503, Count: 1, DurationSeconds: 660, LabelSelector: "modelserving.volcano.sh/name=model", ListLimit: &zero}
}

func (e *normalExecution) historyGCListError(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	if intValue(mapValue(e.current.Object, "spec"), "revisionHistoryLimit", -1) != 0 {
		return fmt.Errorf("DEFAULTING: explicit history limit zero not accepted")
	}
	if err := e.locked(func() error {
		if ok, reason := e.settled(p.Expect); !ok {
			return fmt.Errorf("TRIGGER_MISSED: live A/B fixture not settled: %s", reason)
		}
		versions := map[string]bool{}
		revisions := map[string]bool{}
		for _, u := range e.l.roleUnits(e.o.objects["pods"]) {
			versions[u.Version] = true
			for _, pod := range u.Pods {
				revisions[pod.Labels["modelserving.volcano.sh/revision"]] = true
			}
		}
		if !versions["A"] || !versions["B"] || len(revisions) != 2 {
			return fmt.Errorf("TRIGGER_MISSED: A/B histories must both have live Pod references")
		}
		return e.l.Transition(e.l.Model.Spec, p.Name, p.Expect, e.o.objects)
	}); err != nil {
		return err
	}
	e.historyReferences = true
	if err := e.verifyHistoryReferences(ctx); err != nil {
		return err
	}
	if err := e.snapshot(prefix + "-live-A-B-before-fault"); err != nil {
		return err
	}
	id := fmt.Sprintf("%s-%s-gc-list", e.r.opt.RunID, strings.ToLower(e.c.ID))
	rule := historyGCListRule(id, e.namespace)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		state, err := e.r.faultState(cleanup)
		if err == nil {
			err = writeJSON(filepath.Join(e.dir, prefix+"-gc-before-clear.json"), state)
		}
		result = errors.Join(result, err, e.resumeRecovery([]string{id}, prefix+"-gc"))
	}()
	var installed faultproxy.RuleStatus
	if err := e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(e.dir, prefix+"-gc-installed.json"), installed); err != nil {
		return err
	}
	if !reflect.DeepEqual(rule, installed.Rule) {
		return fmt.Errorf("INCONCLUSIVE: exact GC List filter not acknowledged")
	}
	// The production default audit runs every 300 seconds. Spec-identical
	// metadata updates are not a reliable reconciliation trigger, so wait for
	// real controller traffic without changing the model or controller flags.
	deadline, cancel := context.WithTimeout(ctx, 600*time.Second)
	defer cancel()
	for {
		state, err := e.r.faultState(deadline)
		if err != nil {
			return err
		}
		if len(state.Errors) > 0 {
			return fmt.Errorf("INCONCLUSIVE: GC proxy evidence incomplete")
		}
		hit := false
		for _, r := range state.Rules {
			if r.ID == id {
				hit = r.Hits == 1 && !r.Active && r.EndReason == "count-exhausted"
				if !r.Active && !hit {
					return fmt.Errorf("INCONCLUSIVE: GC List fault expired or changed without its one actual hit")
				}
			}
		}
		if hit {
			if err = writeJSON(filepath.Join(e.dir, prefix+"-gc-hit.json"), state); err != nil {
				return err
			}
			break
		}
		if err = e.locked(func() error { return nil }); err != nil {
			return err
		}
		if time.Since(e.historyChecked) >= time.Second {
			if err = e.verifyHistoryReferences(deadline); err != nil {
				return err
			}
			e.historyChecked = time.Now()
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: exact GC List fault did not hit within two default audit periods")
		case <-time.After(100 * time.Millisecond):
		}
	}
	e.waitDeadline = time.Time{}
	if err := e.finishStep(ctx, p, prefix+"-recovered"); err != nil {
		return err
	}
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	since := metav1.NewTime(installed.Installed)
	logs, err := e.r.kube.CoreV1().Pods(controller.Namespace).GetLogs(controller.Name, &corev1.PodLogOptions{Timestamps: true, SinceTime: &since}).DoRaw(ctx)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(e.dir, prefix+"-gc-controller.log"), logs, 0644); err != nil {
		return err
	}
	matched := false
	for _, line := range strings.Split(string(logs), "\n") {
		matched = matched || strings.Contains(line, e.namespace) && strings.Contains(line, "list Pods before cleaning ControllerRevisions")
	}
	if !matched {
		return fmt.Errorf("INCONCLUSIVE: actual history cleanup read error not diagnosed by this controller")
	}
	return nil
}
