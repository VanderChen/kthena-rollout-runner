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

// Unknown historical templates never grant permission to render B with an A
// identity or replace healthy Pods outside the captured recovery cohort.
func (e *normalExecution) historyReadRecovery(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	if err := e.locked(func() error {
		if ok, why := e.settled(p.Expect); !ok {
			return fmt.Errorf("INCONCLUSIVE: protected A/B source not established: %s", why)
		}
		return e.l.Transition(e.l.Model.Spec, "historical-A-read-unavailable", p.Expect, e.o.objects)
	}); err != nil {
		return err
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-before-fault-pods.yaml"), pods); err != nil {
		return err
	}
	var target *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if owned(pod, e.l.Owner) && pod.Labels[LabelRole] == "frontend" && ordinal(pod.Labels[LabelGroup]) == 0 && ordinal(pod.Labels[LabelRoleID]) == 0 && podIsEntry(pod) {
			if target != nil {
				return fmt.Errorf("INCONCLUSIVE: ambiguous protected A entry")
			}
			target = pod
		}
	}
	if target == nil || podVersion(target) != "A" || !podReady(target) {
		return fmt.Errorf("INCONCLUSIVE: protected source A entry missing")
	}
	historyName := "model-" + target.Labels["modelserving.volcano.sh/revision"]
	history, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, historyName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-original-A-history.yaml"), history); err != nil {
		return err
	}
	scope, err := capturePodFaultScope(e.l.Owner, string(target.UID), textValue(mapValue(e.current.Object, "spec"), "recoveryPolicy"), pods.Items)
	if err != nil {
		return err
	}
	if err = e.locked(func() error {
		if e.l.Protected[string(target.UID)] == "" {
			return fmt.Errorf("INCONCLUSIVE: source A was not protected")
		}
		return e.l.armRecovery(scope, e.o.objects)
	}); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-fault-scope.json"), scope); err != nil {
		return err
	}
	objectFault := p.HistoryFault != ""
	e.historyReferences = !objectFault
	if !objectFault {
		if err = e.verifyHistoryReferences(ctx); err != nil {
			return err
		}
	}
	id := fmt.Sprintf("%s-%s-history-read", e.r.opt.RunID, strings.ToLower(e.c.ID))
	cleared := objectFault
	var objectFixture *historyObjectFault
	defer func() {
		if !cleared {
			result = errors.Join(result, e.resumeRecovery([]string{id}, prefix+"-cleanup"))
		}
	}()
	if objectFault {
		objectFixture, err = e.installHistoryObjectFault(ctx, history, p.HistoryFault, prefix)
		if err != nil {
			return err
		}
	} else {
		rule := faultproxy.Rule{ID: id, Namespace: e.namespace, Resource: "controllerrevisions", Name: historyName, Methods: []string{"GET"}, Mode: "error", StatusCode: 503, Count: -1, DurationSeconds: 180}
		var installed faultproxy.RuleStatus
		if err = e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(e.dir, prefix+"-read-installed.json"), installed); err != nil {
			return err
		}
		if !reflect.DeepEqual(rule, installed.Rule) {
			return fmt.Errorf("INCONCLUSIVE: historical read rule changed")
		}
	}
	options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &target.UID}}
	sent := time.Now().UTC()
	err = e.r.kube.CoreV1().Pods(e.namespace).Delete(ctx, target.Name, options)
	if saveErr := writeJSON(filepath.Join(e.dir, prefix+"-external-delete.json"), map[string]interface{}{"name": target.Name, "uid": target.UID, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil}); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return err
	}
	if !objectFault {
		if err = e.waitFaultState(ctx, prefix+"-actual-read-error", []string{id}, func(state faultproxy.State) bool {
			for _, r := range state.Rules {
				if r.ID == id {
					return r.Hits > 0
				}
			}
			return false
		}); err != nil {
			return err
		}
	}
	// No readiness release is needed while history is unknown. The armed ledger
	// continues checking all new Pod versions, old healthy UIDs and budgets.
	e.waitDeadline = time.Time{}
	hold := ScenarioStep{Name: "actual-A-read-errors-safe-recovery-boundary", Until: "conditions", Release: "none", HoldSeconds: 30, TimeoutSeconds: 60}
	if err = e.wait(ctx, hold); err != nil {
		return err
	}
	if err = e.snapshot(prefix + "-unknown-boundary"); err != nil {
		return err
	}
	if !objectFault {
		if err = e.waitFaultState(ctx, prefix+"-before-read-clear", []string{id}, func(state faultproxy.State) bool {
			for _, r := range state.Rules {
				if r.ID == id {
					return r.Hits >= 2
				}
			}
			return false
		}); err != nil {
			return err
		}
	}
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	logs, err := e.r.kube.CoreV1().Pods(controller.Namespace).GetLogs(controller.Name, &corev1.PodLogOptions{Timestamps: true}).DoRaw(ctx)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(e.dir, prefix+"-unknown-history-controller.log"), logs, 0644); err != nil {
		return err
	}
	diagnostic := false
	for _, line := range strings.Split(string(logs), "\n") {
		if strings.Contains(line, e.namespace) && strings.Contains(line, strings.TrimPrefix(historyName, "model-")) && (strings.Contains(line, "injected external controller API failure") || strings.Contains(line, "failed to get ControllerRevision") || objectFault && (strings.Contains(line, "Cannot resolve") || strings.Contains(line, "not found") || strings.Contains(line, "failed to parse") || strings.Contains(line, "no Role") || strings.Contains(line, "failed to read") || strings.Contains(line, "not controlled"))) {
			diagnostic = true
		}
	}
	if !diagnostic {
		return fmt.Errorf("INCONCLUSIVE: actual historical read failure lacks scoped controller diagnostic")
	}
	if objectFault {
		restored, restoreErr := e.restoreHistoryObject(ctx, objectFixture, prefix)
		if restoreErr != nil {
			return restoreErr
		}
		if err = saveYAML(filepath.Join(e.dir, prefix+"-restored-A-history-before-recovery.yaml"), restored); err != nil {
			return err
		}
	} else {
		if err = e.resumeRecovery([]string{id}, prefix+"-read"); err != nil {
			return err
		}
		cleared = true
	}
	e.waitDeadline = time.Time{}
	p.Name = "protected-A-history-read-restored"
	if err = e.finishStep(ctx, p, prefix+"-recovered"); err != nil {
		return err
	}
	current, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, historyName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if (!objectFault && current.UID != history.UID) || !reflect.DeepEqual(current.Data, history.Data) {
		return e.historyFailure("HISTORY_CHANGED_DURING_READ_FAILURE: " + historyName)
	}
	return saveYAML(filepath.Join(e.dir, prefix+"-restored-A-history.yaml"), current)
}
