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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kthena.local/rollout-runner/internal/faultproxy"
)

func requireDefaultAudit(args []string) error {
	for i, arg := range args {
		for flag, expected := range map[string]time.Duration{"--modelserving-audit-period": 5 * time.Minute, "--modelserving-audit-timeout": 30 * time.Second} {
			value := ""
			if arg == flag {
				if i+1 == len(args) {
					return fmt.Errorf("INCONCLUSIVE: missing audit flag value")
				}
				value = args[i+1]
			} else if strings.HasPrefix(arg, flag+"=") {
				value = strings.TrimPrefix(arg, flag+"=")
			}
			if value != "" {
				actual, err := time.ParseDuration(value)
				if err != nil || actual != expected {
					return fmt.Errorf("INCONCLUSIVE: this case requires production default %s=%s", flag, expected)
				}
			}
		}
	}
	return nil
}

func deletionNotificationMembers(pods *corev1.PodList, owner, mode string) ([]*corev1.Pod, error) {
	var members []*corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !owned(pod, owner) || pod.Labels[LabelRole] != "frontend" {
			continue
		}
		selected := ordinal(pod.Labels[LabelGroup]) == 2 && ordinal(pod.Labels[LabelRoleID]) == 0
		if mode == "Role" {
			selected = ordinal(pod.Labels[LabelGroup]) == 0 && ordinal(pod.Labels[LabelRoleID]) == 2
		}
		if selected {
			if !podReady(pod) || podVersion(pod) != "A" {
				return nil, fmt.Errorf("TRIGGER_MISSED: notification target must be complete old Ready A")
			}
			members = append(members, pod.DeepCopy())
		}
	}
	// All six source fixtures declare W=0 and exactly one selected entry.
	if len(members) != 1 || !podIsEntry(members[0]) {
		return nil, fmt.Errorf("TRIGGER_MISSED: exact first old frontend instance not found")
	}
	return members, nil
}

func (e *normalExecution) dropOldDeletions(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	if err = requireDefaultAudit(controller.Spec.Containers[0].Args); err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-before.yaml"), controller); err != nil {
		return err
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-notification-pods-before.yaml"), pods); err != nil {
		return err
	}
	members, err := deletionNotificationMembers(pods, e.l.Owner, e.l.Model.Mode)
	if err != nil {
		return err
	}
	var uids []string
	for _, pod := range members {
		uids = append(uids, string(pod.UID))
	}
	if err = e.locked(func() error {
		for _, uid := range uids {
			if e.o.objects["pods"][uid] == nil {
				return fmt.Errorf("INCONCLUSIVE: direct Watch has not observed old target UID")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	id := fmt.Sprintf("%s-%s-%02d-drop", e.r.opt.RunID, strings.ToLower(e.c.ID), e.phase)
	rule := faultproxy.Rule{ID: id, Namespace: e.namespace, Resource: "pods", OwnerUID: e.l.Owner, UIDs: uids, Mode: "drop-deletion", Count: -1, DurationSeconds: 900}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-notification-scope.json"), map[string]interface{}{"ownerUID": e.l.Owner, "members": members, "controllerUID": controller.UID, "auditPeriodSeconds": 300, "auditTimeoutSeconds": 30, "runnerWindowSeconds": p.TimeoutSeconds, "faultUIDs": uids}); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		state, evidenceErr := e.r.faultState(cleanup)
		if evidenceErr == nil {
			evidenceErr = writeJSON(filepath.Join(e.dir, prefix+"-notification-before-clear.json"), state)
			matched := false
			for _, saved := range state.Rules {
				if saved.ID == id {
					matched = saved.Active && saved.EndReason == "" && saved.Hits >= 2
				}
			}
			if !matched && evidenceErr == nil {
				evidenceErr = fmt.Errorf("INCONCLUSIVE: live deletion-notification rule with actual hits not established")
			}
		}
		if evidenceErr != nil {
			result = errors.Join(result, evidenceErr)
		}
		if cleanupErr := e.resumeRecovery([]string{id}, prefix+"-notification"); cleanupErr != nil {
			result = errors.Join(result, cleanupErr)
		}
	}()
	var installed faultproxy.RuleStatus
	if err = e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-notification-installed.json"), installed); err != nil {
		return err
	}
	if !reflect.DeepEqual(installed.Rule, rule) {
		return fmt.Errorf("INCONCLUSIVE: exact old-UID notification fault not acknowledged")
	}
	// Ordinary rollout accounting remains armed. Only the controller's receipt
	// of old Pod deletion frames is changed; the observer sees the real API.
	update := p
	update.Action = "update"
	if err = e.step(ctx, update); err != nil {
		return err
	}
	var gone []map[string]interface{}
	for _, old := range members {
		current, getErr := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, old.Name, metav1.GetOptions{})
		if getErr != nil && !apierrors.IsNotFound(getErr) {
			return getErr
		}
		if getErr == nil && current.UID == old.UID {
			return fmt.Errorf("DELETION_NOT_COMPLETE: old UID remains in real API")
		}
		if err = e.locked(func() error {
			if e.o.objects["pods"][string(old.UID)] != nil {
				return fmt.Errorf("INCONCLUSIVE: direct Watch has not observed old UID deletion")
			}
			return nil
		}); err != nil {
			return err
		}
		newUID := ""
		if current != nil {
			newUID = string(current.UID)
		}
		gone = append(gone, map[string]interface{}{"name": old.Name, "oldUID": old.UID, "notFound": apierrors.IsNotFound(getErr), "currentUID": newUID, "directWatchAbsent": true})
	}
	return writeJSON(filepath.Join(e.dir, prefix+"-notification-real-api-after.json"), map[string]interface{}{"received": time.Now().UTC(), "oldMembers": gone})
}
