// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Prepare only the catalogue's already-mixed initial state. Earlier recorded
// cleanup regressions remain product findings. No fixture cleanup is permitted
// after the explicit boundary or during the subsequent historical fault.
func (e *normalExecution) prepareHistorySource(ctx context.Context, p ScenarioStep, prefix string) error {
	initial, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	old := map[string]types.UID{}
	oldCohorts := map[[3]string]bool{}
	for _, pod := range initial.Items {
		if owned(&pod, e.l.Owner) && pod.Labels[LabelRole] == "frontend" && ordinal(pod.Labels[LabelRoleID]) > 0 {
			old[pod.Name] = pod.UID
			oldCohorts[[3]string{pod.Labels[LabelGroup], pod.Labels[LabelRole], pod.Labels[LabelRoleID]}] = true
		}
	}
	if len(old) != 4 {
		return fmt.Errorf("INCONCLUSIVE: expected two original frontend Role cohorts")
	}
	prep := filepath.Join(e.dir, "source-preparation")
	if err = os.Mkdir(prep, 0755); err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(prep, "original-A-pods.yaml"), initial); err != nil {
		return err
	}
	warmup := p
	warmup.Action = "update"
	warmup.Name = "prepare-first-actual-A-B-stop"
	warmup.StableSeconds = 0
	if err = e.step(ctx, warmup); err != nil {
		return fmt.Errorf("INCONCLUSIVE: mixed source preparation: %w", err)
	}
	if err = e.snapshot(prefix + "-first-stop"); err != nil {
		return err
	}
	current, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	retained := map[string]types.UID{}
	for _, pod := range current.Items {
		if !owned(&pod, e.l.Owner) {
			continue
		}
		if !podReady(&pod) || old[pod.Name] == pod.UID {
			return fmt.Errorf("INCONCLUSIVE: incomplete mixed source")
		}
		retained[pod.Name] = pod.UID
	}
	if len(retained) != 9 {
		return fmt.Errorf("INCONCLUSIVE: mixed source capacity")
	}
	if err = saveYAML(filepath.Join(prep, "retained-pods.yaml"), current); err != nil {
		return err
	}
	originalDir := e.dir
	e.dir = prep
	defer func() { e.dir = originalDir }()
	if err = e.terminateController(ctx, "controller"); err != nil {
		return err
	}
	cms, err := e.r.kube.CoreV1().ConfigMaps(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	var cleanup []map[string]interface{}
	for _, cm := range cms.Items {
		ord := ordinal(cm.Labels[LabelRoleID])
		if !ownedConfigMap(&cm, e.l.Owner) || cm.Labels[LabelRole] != "frontend" || ord < 1 || ord > 2 || !oldCohorts[[3]string{cm.Labels[LabelGroup], cm.Labels[LabelRole], cm.Labels[LabelRoleID]}] {
			continue
		}
		actual, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		member := false
		for _, pod := range actual.Items {
			if owned(&pod, e.l.Owner) && pod.Labels[LabelGroup] == cm.Labels[LabelGroup] && pod.Labels[LabelRole] == cm.Labels[LabelRole] && pod.Labels[LabelRoleID] == cm.Labels[LabelRoleID] {
				member = true
			}
			if old[pod.Name] == pod.UID {
				return fmt.Errorf("INCONCLUSIVE: original preparation Pod still present")
			}
		}
		if err = e.locked(func() error {
			for _, o := range e.o.objects["pods"] {
				if objectOwned(o, e.l.Owner) && o.GetLabels()[LabelGroup] == cm.Labels[LabelGroup] && o.GetLabels()[LabelRole] == cm.Labels[LabelRole] && o.GetLabels()[LabelRoleID] == cm.Labels[LabelRoleID] {
					member = true
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if member {
			continue
		}
		if err = saveYAML(filepath.Join(prep, "before-cleanup-"+cm.Name+"-pods.yaml"), actual); err != nil {
			return err
		}
		if err = saveYAML(filepath.Join(prep, "orphan-"+cm.Name+".yaml"), &cm); err != nil {
			return err
		}
		uid := cm.UID
		options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}
		sent := time.Now().UTC()
		err = e.r.kube.CoreV1().ConfigMaps(e.namespace).Delete(ctx, cm.Name, options)
		cleanup = append(cleanup, map[string]interface{}{"name": cm.Name, "uid": uid, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil})
		if saveErr := writeJSON(filepath.Join(prep, "preparation-cleanup.json"), cleanup); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return err
		}
	}
	e.dir = originalDir
	guard := p
	guard.Name = "mixed-history-source-established"
	guard.Action = "observe"
	guard.Expect.NoReplacement = true
	guard.Expect.NoNewRevision = true
	guard.StableSeconds = 10
	guard.TimeoutSeconds = 180
	if err = e.locked(func() error { return e.l.Transition(e.l.Model.Spec, guard.Name, guard.Expect, e.o.objects) }); err != nil {
		return err
	}
	e.waitDeadline = time.Time{}
	if err = e.finishStep(ctx, guard, prefix); err != nil {
		return fmt.Errorf("INCONCLUSIVE: mixed source stability: %w", err)
	}
	current, err = e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	count := 0
	for _, pod := range current.Items {
		if !owned(&pod, e.l.Owner) {
			continue
		}
		count++
		if retained[pod.Name] != pod.UID || !podReady(&pod) {
			return fmt.Errorf("INCONCLUSIVE: mixed source identity or readiness changed")
		}
	}
	if count != len(retained) {
		return fmt.Errorf("INCONCLUSIVE: mixed source lost retained capacity")
	}
	if err = saveYAML(filepath.Join(prep, "boundary-pods.yaml"), current); err != nil {
		return err
	}
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	return e.locked(func() error {
		return writeJSON(filepath.Join(originalDir, "source-history-boundary.json"), map[string]interface{}{"at": time.Now().UTC(), "lastPreparationSequence": e.o.seq, "generation": e.current.GetGeneration(), "retainedUIDs": retained, "oldPreparationUIDs": old, "controllerUID": controller.UID, "preparationRestart": true})
	})
}
