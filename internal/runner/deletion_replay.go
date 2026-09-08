// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kthena.local/rollout-runner/internal/faultproxy"
)

func replayCohort(pods *corev1.PodList, owner string) (PodFaultScope, []*corev1.Pod, error) {
	var target *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if owned(pod, owner) && ordinal(pod.Labels[LabelGroup]) == 0 && pod.Labels[LabelRole] == "frontend" && ordinal(pod.Labels[LabelRoleID]) == 0 && podIsEntry(pod) {
			if target != nil {
				return PodFaultScope{}, nil, fmt.Errorf("ambiguous protected replay entry")
			}
			target = pod
		}
	}
	if target == nil {
		return PodFaultScope{}, nil, fmt.Errorf("protected replay entry missing")
	}
	scope, err := capturePodFaultScope(owner, string(target.UID), "RoleRecreate", pods.Items)
	if err != nil {
		return scope, nil, err
	}
	var cohort []*corev1.Pod
	entries := 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !scope.contains(pod) {
			continue
		}
		if !podReady(pod) || podVersion(pod) != "A" {
			return scope, nil, fmt.Errorf("replay cohort is not complete original Ready A")
		}
		if podIsEntry(pod) {
			entries++
		}
		cohort = append(cohort, pod.DeepCopy())
	}
	if len(cohort) != 2 || entries != 1 {
		return scope, nil, fmt.Errorf("source replay requires exactly one entry and one worker")
	}
	sort.Slice(cohort, func(i, j int) bool { return cohort[i].Name < cohort[j].Name })
	return scope, cohort, nil
}

func oldReplayRequest(state faultproxy.State, id string, scope PodFaultScope) (faultproxy.ReplayRequest, bool) {
	for _, r := range state.Rules {
		if r.ID != id || !r.Active || r.Mode != "replay-deletion" || r.Hits != 2 || len(r.CaptureOrder) != 2 || len(r.Captured) != 2 || r.ReplayRequested || len(state.Errors) > 0 {
			continue
		}
		for _, uid := range r.CaptureOrder {
			if scope.RecoveryUIDs[uid] == "" || r.Captured[uid] == "" {
				return faultproxy.ReplayRequest{}, false
			}
		}
		streams := 0
		for _, stream := range state.ReplayStreams {
			if stream.RuleID == id && reflect.DeepEqual(stream.Forwarded, r.Captured) {
				streams++
			}
		}
		if streams != 1 {
			return faultproxy.ReplayRequest{}, false
		}
		return faultproxy.ReplayRequest{RuleID: id, Order: []string{r.CaptureOrder[1], r.CaptureOrder[0], r.CaptureOrder[1], r.CaptureOrder[0]}}, true
	}
	return faultproxy.ReplayRequest{}, false
}

func (e *normalExecution) replayOldDeletions(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-before.yaml"), controller); err != nil {
		return err
	}
	ids, err := e.recoveryPause(ctx, prefix)
	id := fmt.Sprintf("%s-%s-replay", e.r.opt.RunID, strings.ToLower(e.c.ID))
	allIDs := append(append([]string(nil), ids...), id)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		state, err := e.r.faultState(cleanup)
		if err == nil {
			err = writeJSON(filepath.Join(e.dir, prefix+"-replay-before-clear.json"), state)
		}
		result = errors.Join(result, err, e.resumeRecovery(allIDs, prefix+"-replay"))
	}()
	if err != nil {
		return err
	}
	api := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace)
	current, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	next := objectForSpec(e.namespace, e.c.ID, e.materializeSpec(p.Spec))
	next.SetUID(current.GetUID())
	next.SetResourceVersion(current.GetResourceVersion())
	if err = e.locked(func() error { return e.l.Transition(mapValue(next.Object, "spec"), p.Name, p.Expect, e.o.objects) }); err != nil {
		return err
	}
	write, err := updateScenario(ctx, api, current, next, e.dir, prefix+"-submit-B", nil)
	if err != nil {
		return err
	}
	e.current = write.Object
	if err = saveYAML(filepath.Join(e.dir, prefix+"-B-server.yaml"), e.current.Object); err != nil {
		return err
	}
	server, err := readModel(mapValue(e.current.Object, "spec"))
	if err != nil || !sameEffectiveModel(e.l.Model, server) {
		return fmt.Errorf("DEFAULTING: replay target differs from accepted model: %v", err)
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-fault-pods-before.yaml"), pods); err != nil {
		return err
	}
	scope, cohort, err := replayCohort(pods, e.l.Owner)
	if err != nil {
		return fmt.Errorf("TRIGGER_MISSED: %w", err)
	}
	if err = e.locked(func() error {
		for _, old := range cohort {
			if e.l.Protected[string(old.UID)] != old.Name {
				return fmt.Errorf("TRIGGER_MISSED: replay target is not protected ordinal0")
			}
		}
		return e.l.armRecovery(scope, e.o.objects)
	}); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-fault-scope.json"), scope); err != nil {
		return err
	}
	rule := faultproxy.Rule{ID: id, Namespace: e.namespace, Resource: "pods", OwnerUID: e.l.Owner, UIDs: []string{string(cohort[0].UID), string(cohort[1].UID)}, Mode: "replay-deletion", Count: -1, DurationSeconds: 900}
	var installed faultproxy.RuleStatus
	if err = e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-replay-installed.json"), installed); err != nil {
		return err
	}
	if !reflect.DeepEqual(installed.Rule, rule) {
		return fmt.Errorf("INCONCLUSIVE: exact replay capture rule not acknowledged")
	}
	// Both externally deleted members are finite original identities. Native
	// concurrent DELETEs model the simultaneous fault without following names.
	receipts := make([]map[string]interface{}, len(cohort))
	deleteErrors := make([]error, len(cohort))
	var wg sync.WaitGroup
	for i, old := range cohort {
		wg.Add(1)
		go func(i int, old *corev1.Pod) {
			defer wg.Done()
			options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &old.UID}}
			sent := time.Now().UTC()
			err := e.r.kube.CoreV1().Pods(e.namespace).Delete(ctx, old.Name, options)
			r := map[string]interface{}{"name": old.Name, "uid": old.UID, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil}
			if err != nil {
				r["error"] = err.Error()
			}
			receipts[i], deleteErrors[i] = r, err
		}(i, old)
	}
	wg.Wait()
	if err = writeJSON(filepath.Join(e.dir, prefix+"-concurrent-deletes.json"), receipts); err != nil {
		return err
	}
	if err = errors.Join(deleteErrors...); err != nil {
		return fmt.Errorf("INCONCLUSIVE: simultaneous old deletion failed: %w", err)
	}
	deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for {
		actual, err := e.r.kube.CoreV1().Pods(e.namespace).List(deadline, metav1.ListOptions{})
		if err != nil {
			return err
		}
		gone := true
		for _, pod := range actual.Items {
			if scope.RecoveryUIDs[string(pod.UID)] != "" {
				gone = false
			}
		}
		if err = e.locked(func() error {
			for uid := range scope.RecoveryUIDs {
				if e.o.objects["pods"][uid] != nil {
					gone = false
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if gone {
			if err = saveYAML(filepath.Join(e.dir, prefix+"-old-absent.yaml"), actual); err != nil {
				return err
			}
			break
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: both old deletions not fully observed")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err = e.waitFaultState(ctx, prefix+"-before-controller-resume", ids, func(faultproxy.State) bool { return true }); err != nil {
		return err
	}
	for _, pause := range ids {
		if err = e.r.faultControl(ctx, "DELETE", "/v1/rules/"+pause, nil, nil); err != nil {
			return err
		}
	}
	zero, ready := 0, true
	held := ScenarioStep{Name: "protected-A-new-UIDs-ready-before-replay", Until: "conditions", Release: "one", HoldSeconds: 3, TimeoutSeconds: p.TimeoutSeconds, Conditions: []ScenarioCondition{{Kind: "unit", Group: &zero, Role: "frontend", Ordinal: &zero, Version: "A", Ready: &ready, Count: 1}}, Expect: p.Expect}
	if err = e.locked(func() error { e.l.Phase = held.Name; return nil }); err != nil {
		return err
	}
	if err = e.wait(ctx, held); err != nil {
		return err
	}
	var replacements []*corev1.Pod
	for _, old := range cohort {
		actual, err := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, old.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if actual.UID == old.UID || !owned(actual, e.l.Owner) || !podReady(actual) || podVersion(actual) != "A" {
			return fmt.Errorf("RECOVERY_WRONG_TARGET: protected replay member is not a new Ready A UID")
		}
		replacements = append(replacements, actual)
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-new-protected-before-replay.yaml"), replacements); err != nil {
		return err
	}
	var request faultproxy.ReplayRequest
	if err = e.waitFaultState(ctx, prefix+"-replay-ready", []string{id}, func(s faultproxy.State) bool { var ok bool; request, ok = oldReplayRequest(s, id, scope); return ok }); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-replay-request.json"), request); err != nil {
		return err
	}
	var receipt faultproxy.ReplayReceipt
	if err = e.r.faultControl(ctx, "POST", "/v1/replay", request, &receipt); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-replay-response.json"), receipt); err != nil {
		return err
	}
	if receipt.RuleID != id || receipt.Request == 0 || !reflect.DeepEqual(receipt.Order, request.Order) || len(receipt.PayloadSHA256) != 4 {
		return fmt.Errorf("INCONCLUSIVE: actual old-UID replay acknowledgement mismatch")
	}
	if err = e.locked(func() error { e.l.Phase = p.Name; return nil }); err != nil {
		return err
	}
	return e.finishStep(ctx, p, prefix)
}
