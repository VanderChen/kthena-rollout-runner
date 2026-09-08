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

func pluginRetryRule(id, caseID, namespace, owner string, old *corev1.Pod, cm *corev1.ConfigMap) (faultproxy.Rule, error) {
	if old == nil || old.UID == "" || old.Namespace != namespace || !owned(old, owner) || !podReady(old) || podVersion(old) != "A" || !podIsEntry(old) {
		return faultproxy.Rule{}, fmt.Errorf("plugin cleanup requires captured healthy old entry")
	}
	rule := faultproxy.Rule{ID: id, Namespace: namespace, Mode: "error", StatusCode: 503, Count: 2, DurationSeconds: 480}
	switch caseID {
	case "RUN-536", "RUN-538":
		// The W=0 source has no Service. OnRoleDelete still must perform this
		// live lookup and must retry its failure before accepting absence.
		// OnPodDelete is a no-op and cannot establish cleanup correctness.
		rule.Resource, rule.Methods, rule.Name = "services", []string{"GET"}, old.Name
	case "RUN-537", "RUN-539":
		if cm == nil || cm.UID == "" || cm.Namespace != namespace || cm.DeletionTimestamp != nil {
			return faultproxy.Rule{}, fmt.Errorf("ranktable cleanup requires actual owned ConfigMap UID")
		}
		if ref := metav1.GetControllerOf(cm); ref == nil || string(ref.UID) != owner {
			return faultproxy.Rule{}, fmt.Errorf("ranktable controller owner UID does not match")
		}
		for _, key := range []string{LabelGroup, LabelRole, LabelRoleID} {
			if cm.Labels[key] == "" || cm.Labels[key] != old.Labels[key] {
				return faultproxy.Rule{}, fmt.Errorf("ranktable is not associated with the selected old role")
			}
		}
		rule.Resource, rule.Methods, rule.Name, rule.UIDs = "configmaps", []string{"DELETE"}, cm.Name, []string{string(cm.UID)}
	default:
		return faultproxy.Rule{}, fmt.Errorf("unsupported plugin cleanup case")
	}
	return rule, nil
}

func (e *normalExecution) retryPluginError(ctx context.Context, p ScenarioStep, prefix string) (result error) {
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
		return fmt.Errorf("INCONCLUSIVE: accepted B changed before plugin fault")
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
		return fmt.Errorf("TRIGGER_MISSED: plugin fault requires three healthy old A and one blocked B")
	}
	members, err := deletionNotificationMembers(pods, e.l.Owner, e.l.Model.Mode)
	if err != nil {
		return err
	}
	old := members[0]
	if err = saveYAML(filepath.Join(e.dir, prefix+"-plugin-old-entry.yaml"), old); err != nil {
		return err
	}
	var cm *corev1.ConfigMap
	if e.c.ID == "RUN-536" || e.c.ID == "RUN-538" {
		service, getErr := e.r.kube.CoreV1().Services(e.namespace).Get(ctx, old.Name, metav1.GetOptions{})
		if getErr != nil && !apierrors.IsNotFound(getErr) {
			return getErr
		}
		if err = writeJSON(filepath.Join(e.dir, prefix+"-plugin-service-before.json"), map[string]interface{}{"received": time.Now().UTC(), "name": old.Name, "notFound": apierrors.IsNotFound(getErr), "object": service}); err != nil {
			return err
		}
		if !apierrors.IsNotFound(getErr) {
			return fmt.Errorf("TRIGGER_MISSED: source W=0 has an unexpected preexisting Service")
		}
	} else {
		name := fmt.Sprintf("model-%s-%s-ranktable", old.Labels[LabelGroup], old.Labels[LabelRoleID])
		cm, err = e.r.kube.CoreV1().ConfigMaps(e.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err = saveYAML(filepath.Join(e.dir, prefix+"-plugin-configmap-before.yaml"), cm); err != nil {
			return err
		}
	}
	id := fmt.Sprintf("%s-%s-%02d-plugin", e.r.opt.RunID, strings.ToLower(e.c.ID), e.phase)
	rule, err := pluginRetryRule(id, e.c.ID, e.namespace, e.l.Owner, old, cm)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		state, evidenceErr := e.r.faultState(cleanup)
		if evidenceErr == nil {
			evidenceErr = writeJSON(filepath.Join(e.dir, prefix+"-plugin-before-clear.json"), state)
			matched := false
			for _, saved := range state.Rules {
				if saved.ID == id {
					matched = saved.Hits == 2 && !saved.Active && saved.EndReason == "count-exhausted" && saved.Remaining == 0
				}
			}
			if !matched && evidenceErr == nil {
				evidenceErr = fmt.Errorf("INCONCLUSIVE: two actual cleanup-hook errors and automatic exhaustion not proved")
			}
		}
		if evidenceErr != nil {
			result = errors.Join(result, evidenceErr)
		}
		if cleanupErr := e.resumeRecovery([]string{id}, prefix+"-plugin"); cleanupErr != nil {
			result = errors.Join(result, cleanupErr)
		}
	}()
	var installed faultproxy.RuleStatus
	if err = e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-plugin-installed.json"), installed); err != nil {
		return err
	}
	if !reflect.DeepEqual(installed.Rule, rule) {
		return fmt.Errorf("INCONCLUSIVE: plugin cleanup rule was not acknowledged exactly")
	}
	if err = e.locked(func() error { e.l.Phase = p.Name; e.l.Expected = p.Expect; return nil }); err != nil {
		return err
	}
	// Existing resource orphans remain failures even after the fault expires.
	// Full normal final checks bind PodGroup, Service and ranktable membership.
	return e.finishStep(ctx, p, prefix)
}
