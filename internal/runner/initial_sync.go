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

func initialSyncResource(id string) (string, error) {
	switch id {
	case "RUN-450", "RUN-453":
		return "modelservings", nil
	case "RUN-451", "RUN-454":
		return "pods", nil
	default:
		return "", fmt.Errorf("initial-sync fault is not declared for %s", id)
	}
}

func requireInitialHold(state faultproxy.State, id string) error {
	if len(state.Errors) > 0 {
		return fmt.Errorf("INCONCLUSIVE: initial-sync proxy errors")
	}
	for _, rule := range state.Rules {
		if rule.ID == id && rule.Active && rule.InitialSync && rule.Hits > 0 && rule.Released == 0 && rule.EndReason == "" {
			return nil
		}
	}
	return fmt.Errorf("INCONCLUSIVE: actual unreleased initial-sync request not established")
}

func (e *normalExecution) holdInitialSync(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	resource, err := initialSyncResource(e.c.ID)
	if err != nil {
		return err
	}
	before, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	dep, err := e.r.kube.AppsV1().Deployments(before.Namespace).Get(ctx, "kthena-controller-manager", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-terminated.yaml"), before); err != nil {
		return err
	}
	// Only the new process startup request is held. No Watch frames are
	// queued on the old process when its declared replacement closes streams.
	id := fmt.Sprintf("%s-%s-initial", e.r.opt.RunID, strings.ToLower(e.c.ID))
	ids := []string{id}
	resumed := false
	defer func() {
		if !resumed {
			result = errors.Join(result, e.resumeRecovery(ids, prefix+"-cleanup"))
		}
	}()
	rule := faultproxy.Rule{ID: id, Resource: resource, Methods: []string{"GET"}, Mode: "hold", InitialSync: true, Count: -1, DurationSeconds: 90}
	var installed faultproxy.RuleStatus
	if err = e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-initial-installed.json"), installed); err != nil {
		return err
	}
	if !reflect.DeepEqual(installed.Rule, rule) {
		return fmt.Errorf("INCONCLUSIVE: initial-sync rule acknowledgement mismatch")
	}
	guard := ScenarioExpectation{NoReplacement: true, NoNewRevision: true}
	if err = e.locked(func() error {
		return e.l.Transition(e.l.Model.Spec, "A-retained-during-controller-replacement", guard, e.o.objects)
	}); err != nil {
		return err
	}
	zero := int64(0)
	options := metav1.DeleteOptions{GracePeriodSeconds: &zero, Preconditions: &metav1.Preconditions{UID: &before.UID}}
	sent := time.Now().UTC()
	err = e.r.kube.CoreV1().Pods(before.Namespace).Delete(ctx, before.Name, options)
	if saveErr := writeJSON(filepath.Join(e.dir, prefix+"-controller-delete.json"), map[string]interface{}{"name": before.Name, "uid": before.UID, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil}); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return err
	}
	e.faultController = nil
	deadline, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var replacement *corev1.Pod
	for {
		current, currentErr := e.recoveryController(deadline)
		if currentErr == nil && current.UID != before.UID {
			state, stateErr := e.r.faultState(deadline)
			if stateErr != nil {
				return stateErr
			}
			if requireInitialHold(state, id) == nil {
				replacement = current
				if err = writeJSON(filepath.Join(e.dir, prefix+"-initial-hit.json"), state); err != nil {
					return err
				}
				break
			}
		}
		if err = e.locked(func() error { return nil }); err != nil {
			return err
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: replacement initial-sync request was not held")
		case <-time.After(100 * time.Millisecond):
		}
	}
	e.faultController = replacement
	if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-replacement.yaml"), replacement); err != nil {
		return err
	}
	// The new process serves actual admission independently of its blocked
	// controller informer. Warm up that endpoint with real dry-run updates.
	if err = e.awaitInitialSyncAdmission(ctx, prefix); err != nil {
		return err
	}
	ready := true
	held := ScenarioStep{Name: "B-accepted-while-informer-unsynced", Action: "update", Spec: p.Spec, Until: "conditions", Conditions: []ScenarioCondition{{Kind: "unit", Version: "A", Ready: &ready, Count: 3}}, Release: "none", TimeoutSeconds: p.TimeoutSeconds, Expect: guard}
	if e.l.Model.Mode == "Role" {
		held.Conditions[0].Role = "frontend"
	}
	if err = e.step(ctx, held); err != nil {
		return err
	}
	held.Name = "actual-selected-informer-unsynced-with-A-retained"
	held.HoldSeconds = 10
	if err = e.locked(func() error { e.l.Phase = held.Name; return nil }); err != nil {
		return err
	}
	if err = e.wait(ctx, held); err != nil {
		return err
	}
	state, err := e.r.faultState(ctx)
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-initial-before-clear.json"), state); err != nil {
		return err
	}
	if err = requireInitialHold(state, id); err != nil {
		return err
	}
	for _, r := range state.Rules {
		if r.ID != id && r.Active {
			return fmt.Errorf("INCONCLUSIVE: unrelated barrier still active during initial-sync hold")
		}
	}
	logs, err := e.r.kube.CoreV1().Pods(replacement.Namespace).GetLogs(replacement.Name, &corev1.PodLogOptions{Timestamps: true}).DoRaw(ctx)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(e.dir, prefix+"-controller-unsynced.log"), logs, 0644); err != nil {
		return err
	}
	if strings.Contains(string(logs), "initial sync has been done") {
		return fmt.Errorf("INCONCLUSIVE: initial-sync completed before fault release")
	}
	if err = e.snapshot(prefix + "-unsynced"); err != nil {
		return err
	}
	// Retire the temporary preservation constraint immediately before releasing
	// cache startup. Ordinary B rollout budgets and protected roles now apply.
	if err = e.locked(func() error { return e.l.Transition(e.l.Model.Spec, p.Name, p.Expect, e.o.objects) }); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-initial-release-time.json"), map[string]interface{}{"sent": time.Now().UTC(), "id": id, "controllerUID": replacement.UID}); err != nil {
		return err
	}
	if err = e.resumeRecovery(ids, prefix+"-initial"); err != nil {
		return err
	}
	resumed = true
	if err = e.finishStep(ctx, p, prefix+"-recovered"); err != nil {
		return err
	}
	current, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	logs, err = e.r.kube.CoreV1().Pods(current.Namespace).GetLogs(current.Name, &corev1.PodLogOptions{Timestamps: true}).DoRaw(ctx)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(e.dir, prefix+"-controller-synced.log"), logs, 0644); err != nil {
		return err
	}
	if !strings.Contains(string(logs), "initial sync has been done") {
		return fmt.Errorf("INCONCLUSIVE: replacement initial-sync completion not witnessed")
	}
	after, err := e.r.kube.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if after.UID != dep.UID || !reflect.DeepEqual(after.Spec, dep.Spec) {
		return fmt.Errorf("CONTROLLER_STATE: Deployment changed during initial-sync scenario")
	}
	return saveYAML(filepath.Join(e.dir, prefix+"-controller-deployment.yaml"), after)
}

// Dry-run the unchanged live A object: admission health checking must not
// submit B or advance the generation before the recorded scenario request.
func (e *normalExecution) awaitInitialSyncAdmission(ctx context.Context, prefix string) (result error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var attempts []map[string]interface{}
	defer func() {
		result = errors.Join(result, writeJSON(filepath.Join(e.dir, prefix+"-admission-warmup.json"), attempts))
	}()
	api := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace)
	for {
		current, err := api.Get(ctx, "model", metav1.GetOptions{})
		if err != nil {
			return err
		}
		sent := time.Now().UTC()
		response, err := api.Update(ctx, current, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}})
		attempt := map[string]interface{}{"sent": sent, "received": time.Now().UTC(), "accepted": err == nil, "requestUID": current.GetUID(), "dryRun": "All", "requestGeneration": current.GetGeneration()}
		if err != nil {
			attempt["error"] = err.Error()
		} else {
			attempt["responseUID"] = response.GetUID()
			attempt["responseGeneration"] = response.GetGeneration()
		}
		attempts = append(attempts, attempt)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("INCONCLUSIVE: replacement actual admission did not become ready: %w", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
