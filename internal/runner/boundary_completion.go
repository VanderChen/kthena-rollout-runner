// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kthena.local/rollout-runner/internal/faultproxy"
)

func sparseCompletionPods(pods []corev1.Pod, owner string, expected map[string]types.UID) error {
	seen := map[string]bool{}
	for i := range pods {
		p := &pods[i]
		if !owned(p, owner) {
			continue
		}
		role, ord := p.Labels[LabelRole], ordinal(p.Labels[LabelRoleID])
		if (role != "frontend" && role != "backend") || (ord != 1 && ord != 2) || ordinal(p.Labels[LabelGroup]) != 0 || !podIsEntry(p) || !podReady(p) || podVersion(p) != "B" {
			return fmt.Errorf("unexpected sparse completion member %s", p.Name)
		}
		key := fmt.Sprintf("%s/%d", role, ord)
		if seen[key] || expected != nil && expected[p.Name] != p.UID {
			return fmt.Errorf("sparse completion UID changed %s", p.Name)
		}
		seen[key] = true
	}
	if len(seen) != 4 {
		return fmt.Errorf("sparse completion needs four actual Ready B members, got %d", len(seen))
	}
	return nil
}

// All preparation restarts and finite cleanup precede the old-current status
// write. After that accepted write, completion must be automatic; the final
// controller restart is only allowed after a stable completed observation.
func (e *normalExecution) sparseCompletionBoundary(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	originalDir, originalPhase := e.dir, e.phase
	prep := filepath.Join(e.dir, "completion-preparation")
	if err := os.Mkdir(prep, 0755); err != nil {
		return err
	}
	e.dir = prep
	defer func() { e.dir, e.phase, e.waitDeadline = originalDir, originalPhase, time.Time{} }()
	originalStatus := cloneMap(e.baselineStatus)
	if textValue(originalStatus, "currentRevision") == "" {
		return fmt.Errorf("INCONCLUSIVE: original A status absent")
	}
	if err := writeJSON(filepath.Join(prep, "original-A-status.json"), originalStatus); err != nil {
		return err
	}
	expanded := cloneMap(e.c.Scenario.InitialSpec)
	delete(mapValue(expanded, "rolloutStrategy"), "roleCoordination")
	for _, raw := range listValue(mapValue(expanded, "template"), "roles") {
		role := raw.(map[string]interface{})
		role["replicas"] = float64(3)
		role["maxUnavailable"] = float64(1)
		role["maxSurge"] = float64(0)
		role["partition"] = float64(0)
	}
	e.phase = 0
	e.waitDeadline = time.Time{}
	if err := e.step(ctx, ScenarioStep{Name: "prepare-three-A-per-role", Action: "update", Spec: expanded, Until: "settled", Release: "one", StableSeconds: 1, TimeoutSeconds: 180, Expect: ScenarioExpectation{NoReplacement: true, NoNewRevision: true}}); err != nil {
		return fmt.Errorf("INCONCLUSIVE: completion expansion: %w", err)
	}
	b := cloneMap(expanded)
	for _, raw := range listValue(mapValue(b, "template"), "roles") {
		role := raw.(map[string]interface{})
		for _, key := range []string{"entryTemplate", "workerTemplate"} {
			for _, container := range listValue(mapValue(mapValue(role, key), "spec"), "containers") {
				for _, rawEnv := range listValue(container.(map[string]interface{}), "env") {
					env := rawEnv.(map[string]interface{})
					if textValue(env, "name") == "ROLLOUT_VERSION" {
						env["value"] = "B"
					}
				}
			}
		}
	}
	e.phase = 1
	e.waitDeadline = time.Time{}
	if err := e.step(ctx, ScenarioStep{Name: "prepare-six-actual-B-members", Action: "update", Spec: b, Until: "settled", Release: "one", TimeoutSeconds: 300, Expect: ScenarioExpectation{Targets: []ScenarioTarget{{Role: "frontend", Versions: map[string]int{"B": 3}}, {Role: "backend", Versions: map[string]int{"B": 3}}}}}); err != nil {
		return fmt.Errorf("INCONCLUSIVE: completion B source: %w", err)
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(prep, "six-B-pods.yaml"), pods); err != nil {
		return err
	}
	kept, removed := map[string]types.UID{}, map[string]types.UID{}
	for _, pod := range pods.Items {
		if !owned(&pod, e.l.Owner) {
			continue
		}
		if !podReady(&pod) || podVersion(&pod) != "B" {
			return fmt.Errorf("INCONCLUSIVE: B preparation not Ready")
		}
		if ordinal(pod.Labels[LabelRoleID]) == 0 {
			removed[pod.Name] = pod.UID
		} else {
			kept[pod.Name] = pod.UID
		}
	}
	if len(kept) != 4 || len(removed) != 2 {
		return fmt.Errorf("INCONCLUSIVE: B source count")
	}
	ids, err := e.sparseMutationBarrier(ctx)
	resumed := false
	defer func() {
		if !resumed {
			result = errors.Join(result, e.resumeRecovery(ids, "completion-preparation-cleanup"))
		}
	}()
	if err != nil {
		return err
	}
	if err = e.locked(func() error { e.l.Armed = false; return nil }); err != nil {
		return err
	}
	zero := int64(0)
	var deleted []map[string]interface{}
	for name, uid := range removed {
		options := metav1.DeleteOptions{GracePeriodSeconds: &zero, Preconditions: &metav1.Preconditions{UID: &uid}}
		sent := time.Now().UTC()
		err = e.r.kube.CoreV1().Pods(e.namespace).Delete(ctx, name, options)
		deleted = append(deleted, map[string]interface{}{"name": name, "uid": uid, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil})
		if saveErr := writeJSON(filepath.Join(prep, "external-zero-deletes.json"), deleted); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return err
		}
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		pods, err = e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		gone := sparseCompletionPods(pods.Items, e.l.Owner, kept) == nil
		if err = e.locked(func() error {
			for _, uid := range removed {
				if e.o.objects["pods"][string(uid)] != nil {
					gone = false
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if gone {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("INCONCLUSIVE: removed B0 UIDs did not disappear from API and Watch")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err = saveYAML(filepath.Join(prep, "actual-B12-before-restore.yaml"), pods); err != nil {
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
	write, err := updateScenario(ctx, api, current, next, prep, "restore-original-budget-B2", nil)
	if err != nil {
		return err
	}
	e.current = write.Object
	model, err := readModel(mapValue(e.current.Object, "spec"))
	if err != nil {
		return err
	}
	if err = e.locked(func() error {
		e.l.Model = model
		e.l.Base = model
		e.l.History = append(e.l.History, model)
		return nil
	}); err != nil {
		return err
	}
	if err = e.terminateController(ctx, "preparation-controller"); err != nil {
		return err
	}
	if err = e.waitFaultState(ctx, "barrier-after-fresh-controller", ids, func(s faultproxy.State) bool {
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
		return hits > 0 && s.InFlightAllowed == 0
	}); err != nil {
		return err
	}
	if err = e.resumeRecovery(ids, "completion-source"); err != nil {
		return err
	}
	resumed = true
	// Only orphan cohort zero was deliberately removed. Never delete a ranktable
	// whose actual API or Watch cohort still contains a member.
	cms, err := e.r.kube.CoreV1().ConfigMaps(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	var cleanup []map[string]interface{}
	for _, cm := range cms.Items {
		role := cm.Labels[LabelRole]
		if !ownedConfigMap(&cm, e.l.Owner) || (role != "frontend" && role != "backend") || ordinal(cm.Labels[LabelGroup]) != 0 || ordinal(cm.Labels[LabelRoleID]) != 0 {
			continue
		}
		actual, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		if err = sparseCompletionPods(actual.Items, e.l.Owner, kept); err != nil {
			return fmt.Errorf("INCONCLUSIVE: %w", err)
		}
		if err = saveYAML(filepath.Join(prep, "cleanup-members-"+cm.Name+".yaml"), actual); err != nil {
			return err
		}
		if err = e.locked(func() error {
			for _, o := range e.o.objects["pods"] {
				if objectOwned(o, e.l.Owner) && o.GetLabels()[LabelRole] == role && ordinal(o.GetLabels()[LabelRoleID]) == 0 {
					return fmt.Errorf("INCONCLUSIVE: orphan cleanup still has watched member")
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if err = saveYAML(filepath.Join(prep, "orphan-zero-"+cm.Name+".yaml"), &cm); err != nil {
			return err
		}
		uid := cm.UID
		options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}
		sent := time.Now().UTC()
		err = e.r.kube.CoreV1().ConfigMaps(e.namespace).Delete(ctx, cm.Name, options)
		cleanup = append(cleanup, map[string]interface{}{"name": cm.Name, "uid": uid, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil})
		if saveErr := writeJSON(filepath.Join(prep, "orphan-cleanup.json"), cleanup); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return err
		}
	}
	guard := p.Expect
	guard.RequireCompleted = false
	if err = e.locked(func() error {
		fresh, err := completionSourceLedger(model, e.l)
		if err != nil {
			return err
		}
		for _, uid := range kept {
			if !e.l.Released[string(uid)] {
				return fmt.Errorf("INCONCLUSIVE: B UID readiness was not actually released")
			}
			fresh.Released[string(uid)] = true
		}
		for _, kind := range []string{"controllerrevisions", "pods"} {
			for _, o := range e.o.objects[kind] {
				fresh.After(kind, "LIST", o, e.o.objects)
			}
		}
		if err = fresh.error(); err != nil {
			return err
		}
		if err = fresh.Transition(model.Spec, "actual-sparse-B12-source-stable", guard, e.o.objects); err != nil {
			return err
		}
		e.l, e.o.normal = fresh, fresh
		return nil
	}); err != nil {
		return err
	}
	e.waitDeadline = time.Time{}
	e.preparingCompletionSource = true
	e.historyReferences = true
	defer func() { e.preparingCompletionSource = false }()
	if err = e.finishStep(ctx, ScenarioStep{Name: "actual-sparse-B12-source-stable", Until: "settled", Release: "none", StableSeconds: 10, TimeoutSeconds: 180, Expect: guard}, "source"); err != nil {
		return fmt.Errorf("INCONCLUSIVE: sparse B source preparation: %w", err)
	}
	e.preparingCompletionSource = false
	e.dir, e.phase = originalDir, originalPhase
	if err = e.snapshot(prefix + "-actual-B12-source"); err != nil {
		return err
	}
	// The accepted status write is the semantic boundary. No controller restart
	// or preparation cleanup follows until automatic promotion has passed.
	for attempt := 1; attempt <= 5; attempt++ {
		current, err = api.Get(ctx, "model", metav1.GetOptions{})
		if err != nil {
			return err
		}
		current.Object["status"] = cloneMap(originalStatus)
		label := fmt.Sprintf("%s-old-current-%02d", prefix, attempt)
		if err = saveYAML(filepath.Join(e.dir, label+"-request.yaml"), current.Object); err != nil {
			return err
		}
		sent := time.Now().UTC()
		updated, writeErr := api.UpdateStatus(ctx, current, metav1.UpdateOptions{})
		receipt := map[string]interface{}{"sent": sent, "received": time.Now().UTC(), "accepted": writeErr == nil, "uid": current.GetUID(), "generation": current.GetGeneration()}
		if writeErr != nil {
			receipt["error"] = writeErr.Error()
		}
		if err = writeJSON(filepath.Join(e.dir, label+"-receipt.json"), receipt); err != nil {
			return err
		}
		if apierrors.IsConflict(writeErr) && attempt < 5 {
			continue
		}
		if writeErr != nil {
			return writeErr
		}
		e.current = updated
		if err = saveYAML(filepath.Join(e.dir, label+"-server.yaml"), updated.Object); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(e.dir, prefix+"-source-boundary.json"), map[string]interface{}{"at": time.Now().UTC(), "statusWrite": label, "generation": updated.GetGeneration(), "ownerUID": e.l.Owner, "retainedUIDs": kept, "removedPreparationUIDs": removed, "oldCurrentRevision": textValue(originalStatus, "currentRevision")}); err != nil {
			return err
		}
		break
	}
	e.completionStatusProbe = true
	e.historyReferences = true
	p.Name = "sparse-B12-automatic-completion-before-restart"
	e.waitDeadline = time.Time{}
	if err = e.locked(func() error { e.l.Phase = p.Name; e.l.Expected = p.Expect; return nil }); err != nil {
		return err
	}
	if err = e.finishStep(ctx, p, prefix+"-automatic-completed"); err != nil {
		return err
	}
	if err = e.terminateController(ctx, prefix+"-after-completion"); err != nil {
		return err
	}
	p.Name = "sparse-B12-equivalent-completion-after-restart"
	e.waitDeadline = time.Time{}
	if err = e.locked(func() error { e.l.Phase = p.Name; return nil }); err != nil {
		return err
	}
	return e.finishStep(ctx, p, prefix+"-final")
}

func completionSourceLedger(model NormalModel, prior *NormalLedger) (*NormalLedger, error) {
	fresh, err := newNormalLedger(model.Spec, prior.Owner, prior.Profile)
	if err != nil {
		return nil, err
	}
	// The source resets Pod commitments, not the real A/B template history.
	// Old current A must remain readable for the subsequent status boundary.
	fresh.History = append([]NormalModel{}, prior.History...)
	fresh.History = append(fresh.History, model)
	return fresh, nil
}

func (e *normalExecution) probeCompletionStatus(ctx context.Context) error {
	started := time.Now().UTC()
	ms, err := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace).Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	e.completionStatusProbes++
	return writeJSON(filepath.Join(e.dir, fmt.Sprintf("completion-status-%04d.json", e.completionStatusProbes)), map[string]interface{}{"started": started, "completed": time.Now().UTC(), "phase": e.l.Phase, "object": ms.Object})
}

func completedBoundaryStatus(status map[string]interface{}) error {
	current, target := textValue(status, "currentRevision"), textValue(status, "updateRevision")
	if current == "" || current != target {
		return fmt.Errorf("sparse target currentRevision not automatically promoted")
	}
	for _, raw := range listValue(status, "conditions") {
		c := raw.(map[string]interface{})
		if (textValue(c, "type") == "UpdateInProgress" || textValue(c, "type") == "CoordinatedRoleRolloutBlocked") && textValue(c, "status") == "True" {
			return fmt.Errorf("sparse target still reports incomplete rollout")
		}
	}
	return nil
}
