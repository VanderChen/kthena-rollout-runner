// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kthena.local/rollout-runner/internal/faultproxy"
)

func apiRetryRule(id, namespace, owner string, generation int64, caseID string) (faultproxy.Rule, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(caseID, "RUN-"))
	if err != nil || n < 455 || n > 462 || generation < 2 {
		return faultproxy.Rule{}, fmt.Errorf("invalid API retry identity or accepted B generation")
	}
	rule := faultproxy.Rule{ID: id, Namespace: namespace, Mode: "error", StatusCode: 503, Count: 1, DurationSeconds: 480}
	switch (n - 455) % 4 {
	case 0:
		rule.Resource, rule.Methods, rule.OwnerUID = "pods", []string{"POST"}, owner
	case 1:
		// Production's normal Role/SG rollout calls DeleteCollection, not
		// a named Pod Delete. Keep its real label selector in proxy evidence.
		rule.Resource, rule.Methods, rule.CollectionOnly = "pods", []string{"DELETE"}, true
	case 2:
		rule.Resource, rule.Subresource, rule.Name = "modelservings", "status", "model"
		rule.Methods, rule.OwnerUID, rule.Generation = []string{"PUT"}, owner, generation
	case 3:
		rule.Resource, rule.Methods, rule.CollectionOnly = "controllerrevisions", []string{"GET"}, true
	}
	return rule, nil
}

func apiRetryWindow(pods *corev1.PodList, owner string) bool {
	old, blocked := 0, 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !owned(pod, owner) || !podIsEntry(pod) || pod.Labels[LabelRole] != "frontend" {
			continue
		}
		if podVersion(pod) == "A" && podReady(pod) {
			old++
		}
		if podVersion(pod) == "B" && actualContainerFault(pod, "running-not-ready") {
			blocked++
		}
	}
	return old == 3 && blocked == 1
}

func (e *normalExecution) retryAPIError(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-before.yaml"), controller); err != nil {
		return err
	}
	ms, err := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace).Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	actual, err := readModel(mapValue(ms.Object, "spec"))
	if err != nil || !sameEffectiveModel(e.l.Model, actual) {
		return fmt.Errorf("INCONCLUSIVE: accepted B changed before API fault")
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-fault-server.yaml"), ms.Object); err != nil {
		return err
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-fault-pods-before.yaml"), pods); err != nil {
		return err
	}
	if !apiRetryWindow(pods, e.l.Owner) {
		return fmt.Errorf("TRIGGER_MISSED: API fault requires three healthy old A and one blocked B")
	}
	if err = e.locked(func() error { e.l.Phase = p.Name; e.l.Expected = p.Expect; return nil }); err != nil {
		return err
	}
	id := fmt.Sprintf("%s-%s-%02d-api", e.r.opt.RunID, strings.ToLower(e.c.ID), e.phase)
	rule, err := apiRetryRule(id, e.namespace, e.l.Owner, ms.GetGeneration(), e.c.ID)
	if err != nil {
		return err
	}
	// Install acknowledgements may be lost. Cleanup retains the exact rule ID
	// before sending and runs even when rollout itself finds a product failure.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		state, evidenceErr := e.r.faultState(cleanup)
		if evidenceErr == nil {
			evidenceErr = writeJSON(filepath.Join(e.dir, prefix+"-api-fault-before-clear.json"), state)
			matched := false
			for _, saved := range state.Rules {
				if saved.ID == id {
					matched = saved.Hits == 1 && !saved.Active && saved.EndReason == "count-exhausted" && saved.Remaining == 0
				}
			}
			if !matched && evidenceErr == nil {
				evidenceErr = fmt.Errorf("INCONCLUSIVE: exactly one actual API error and automatic count exhaustion not proved")
			}
		}
		if evidenceErr != nil {
			result = errors.Join(result, fmt.Errorf("API fault evidence: %w", evidenceErr))
		}
		if cleanupErr := e.resumeRecovery([]string{id}, prefix+"-api-fault"); cleanupErr != nil {
			result = errors.Join(result, cleanupErr)
		}
	}()
	var installed faultproxy.RuleStatus
	if err = e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-api-fault-installed.json"), installed); err != nil {
		return err
	}
	if !reflect.DeepEqual(installed.Rule, rule) {
		return fmt.Errorf("INCONCLUSIVE: proxy did not acknowledge exact collection/owner/generation fault semantics")
	}
	return e.finishStep(ctx, p, prefix)
}
