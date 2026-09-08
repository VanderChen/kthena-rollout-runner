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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// The fixture is built using actual controller-created A Pods. The caller must
// keep it separate from the source scenario until this function has established
// the exact sparse population and written a new observation boundary.
func sparseHistoryPods(pods []corev1.Pod, owner string, expected map[string]types.UID) error {
	seen := map[string]bool{}
	for i := range pods {
		p := &pods[i]
		if !owned(p, owner) {
			continue
		}
		role, ord := p.Labels[LabelRole], ordinal(p.Labels[LabelRoleID])
		valid := role == "frontend" && (ord == 0 || ord == 3 || ord == 4) || role == "backend" && ord >= 0 && ord <= 2
		if !valid || !podIsEntry(p) || !podReady(p) || podVersion(p) != "A" || ordinal(p.Labels[LabelGroup]) != 0 {
			return fmt.Errorf("sparse fixture has unexpected member %s", p.Name)
		}
		key := fmt.Sprintf("%s/%d", role, ord)
		if seen[key] || expected != nil && expected[p.Name] != p.UID {
			return fmt.Errorf("sparse fixture identity changed: %s", p.Name)
		}
		seen[key] = true
	}
	if len(seen) != 6 {
		return fmt.Errorf("sparse fixture has %d members, expected frontend{0,3,4}/backend{0,1,2}", len(seen))
	}
	return nil
}

func (e *normalExecution) prepareSparseHistoryFixture(ctx context.Context) (result error) {
	originalDir, originalPhase := e.dir, e.phase
	e.dir = filepath.Join(originalDir, "fixture-preparation")
	if err := os.Mkdir(e.dir, 0755); err != nil {
		e.dir = originalDir
		return err
	}
	defer func() { e.dir, e.phase, e.waitDeadline = originalDir, originalPhase, time.Time{} }()
	originalSpec := cloneMap(e.c.Scenario.InitialSpec)
	expanded := cloneMap(originalSpec)
	for _, raw := range listValue(mapValue(expanded, "template"), "roles") {
		role := raw.(map[string]interface{})
		if textValue(role, "name") == "frontend" {
			role["replicas"] = float64(5)
		}
	}
	e.phase = 0
	expand := ScenarioStep{Name: "fixture-controller-creates-five-A", Action: "update", Spec: expanded, Until: "settled", Release: "one", StableSeconds: 1, TimeoutSeconds: 180, Expect: ScenarioExpectation{NoReplacement: true, NoNewRevision: true}}
	e.waitDeadline = time.Time{}
	if err := e.step(ctx, expand); err != nil {
		return fmt.Errorf("INCONCLUSIVE: sparse fixture expansion: %w", err)
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, "expanded-pods.yaml"), pods); err != nil {
		return err
	}
	retained, deleted := map[string]types.UID{}, map[string]types.UID{}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !owned(pod, e.l.Owner) {
			continue
		}
		if !podReady(pod) || podVersion(pod) != "A" || !podIsEntry(pod) {
			return fmt.Errorf("INCONCLUSIVE: expanded source A not Ready")
		}
		ord := ordinal(pod.Labels[LabelRoleID])
		if pod.Labels[LabelRole] == "frontend" && (ord == 1 || ord == 2) {
			deleted[pod.Name] = pod.UID
		} else {
			retained[pod.Name] = pod.UID
		}
	}
	if len(deleted) != 2 || len(retained) != 6 {
		return fmt.Errorf("INCONCLUSIVE: expanded A fixture identity count")
	}
	e.phase = 1
	ids, err := e.recoveryPause(ctx, "sparse")
	resumed := false
	defer func() {
		if !resumed {
			result = errors.Join(result, e.resumeRecovery(ids, "sparse-cleanup"))
		}
	}()
	if err != nil {
		return err
	}
	if err = e.locked(func() error { e.l.Armed = false; e.l.Phase = "fixture-external-A1-A2-removal"; return nil }); err != nil {
		return err
	}
	zero := int64(0)
	var receipts []map[string]interface{}
	for name, uid := range deleted {
		options := metav1.DeleteOptions{GracePeriodSeconds: &zero, Preconditions: &metav1.Preconditions{UID: &uid}}
		sent := time.Now().UTC()
		err := e.r.kube.CoreV1().Pods(e.namespace).Delete(ctx, name, options)
		receipts = append(receipts, map[string]interface{}{"name": name, "uid": uid, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil})
		if saveErr := writeJSON(filepath.Join(e.dir, "external-deletes.json"), receipts); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return err
		}
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
			if deleted[pod.Name] == pod.UID {
				gone = false
			}
		}
		if err = e.locked(func() error {
			for _, uid := range deleted {
				if e.o.objects["pods"][string(uid)] != nil {
					gone = false
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if gone {
			if err = sparseHistoryPods(actual.Items, e.l.Owner, retained); err != nil {
				return fmt.Errorf("INCONCLUSIVE: %w", err)
			}
			if err = saveYAML(filepath.Join(e.dir, "old-absent-sparse-A.yaml"), actual); err != nil {
				return err
			}
			break
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: fixture deleted UIDs not absent from API and Watch")
		case <-time.After(50 * time.Millisecond):
		}
	}
	api := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace)
	current, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	next := objectForSpec(e.namespace, e.c.ID, originalSpec)
	next.SetUID(current.GetUID())
	next.SetResourceVersion(current.GetResourceVersion())
	write, err := updateScenario(ctx, api, current, next, e.dir, "restore-source-R3", nil)
	if err != nil {
		return err
	}
	e.current = write.Object
	sourceModel, err := readModel(originalSpec)
	if err != nil {
		return err
	}
	if err = e.locked(func() error {
		e.l.Model, e.l.Base = sourceModel, sourceModel
		e.l.History = append(e.l.History, sourceModel)
		return nil
	}); err != nil {
		return err
	}
	if err = e.resumeRecovery(ids, "sparse"); err != nil {
		return err
	}
	resumed = true
	// A controller replacement must never close a stream with queued fixture
	// events. Check every rule's delivered counters and a real published source
	// generation, then retain that fully drained state before terminating it.
	flushCtx, flushCancel := context.WithTimeout(ctx, 30*time.Second)
	defer flushCancel()
	var stableSince time.Time
	for {
		state, err := e.r.faultState(flushCtx)
		if err != nil {
			return err
		}
		if len(state.Errors) > 0 {
			return fmt.Errorf("INCONCLUSIVE: sparse fixture proxy errors")
		}
		ready := true
		for _, id := range ids {
			found := false
			for _, r := range state.Rules {
				if r.ID == id {
					found = true
					ready = ready && !r.Active && r.Hits == r.Released
				}
			}
			ready = ready && found
		}
		ms, err := api.Get(flushCtx, "model", metav1.GetOptions{})
		if err != nil {
			return err
		}
		ready = ready && ms.GetUID() == e.current.GetUID() && intValue(mapValue(ms.Object, "status"), "observedGeneration", -1) >= int(e.current.GetGeneration())
		if ready {
			if stableSince.IsZero() {
				stableSince = time.Now()
			}
			if time.Since(stableSince) >= 2*time.Second {
				if err = writeJSON(filepath.Join(e.dir, "fully-delivered-before-restart.json"), state); err != nil {
					return err
				}
				if err = saveYAML(filepath.Join(e.dir, "source-generation-before-restart.yaml"), ms.Object); err != nil {
					return err
				}
				break
			}
		} else {
			stableSince = time.Time{}
		}
		select {
		case <-flushCtx.Done():
			return fmt.Errorf("INCONCLUSIVE: fixture events/source generation not fully delivered before restart")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err = e.terminateController(ctx, "fixture-restart"); err != nil {
		return err
	}
	pods, err = e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = sparseHistoryPods(pods.Items, e.l.Owner, retained); err != nil {
		return fmt.Errorf("INCONCLUSIVE: %w", err)
	}
	// Clean only the two deliberately removed preparation Role identities.
	// No resource cleanup of this kind is permitted after the source boundary.
	cms, err := e.r.kube.CoreV1().ConfigMaps(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	var cleanup []map[string]interface{}
	for _, cm := range cms.Items {
		ord := ordinal(cm.Labels[LabelRoleID])
		if !ownedConfigMap(&cm, e.l.Owner) || cm.Labels[LabelRole] != "frontend" || (ord != 1 && ord != 2) || ordinal(cm.Labels[LabelGroup]) != 0 {
			continue
		}
		uid := cm.UID
		options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}
		if err = saveYAML(filepath.Join(e.dir, "orphan-preparation-"+cm.Name+".yaml"), &cm); err != nil {
			return err
		}
		sent := time.Now().UTC()
		err = e.r.kube.CoreV1().ConfigMaps(e.namespace).Delete(ctx, cm.Name, options)
		cleanup = append(cleanup, map[string]interface{}{"name": cm.Name, "uid": uid, "sent": sent, "received": time.Now().UTC(), "options": options, "accepted": err == nil})
		if saveErr := writeJSON(filepath.Join(e.dir, "preparation-resource-cleanup.json"), cleanup); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return err
		}
	}
	guard := ScenarioExpectation{NoReplacement: true, NoNewRevision: true, Targets: []ScenarioTarget{{Role: "frontend", Versions: map[string]int{"A": 3}, Ordinals: map[string]string{"0": "A", "3": "A", "4": "A"}}, {Role: "backend", Versions: map[string]int{"A": 3}, Ordinals: map[string]string{"0": "A", "1": "A", "2": "A"}}}}
	if err = e.locked(func() error { return e.l.Transition(originalSpec, "sparse-source-A-established", guard, e.o.objects) }); err != nil {
		return err
	}
	e.waitDeadline = time.Time{}
	if err = e.finishStep(ctx, ScenarioStep{Name: "sparse-source-A-established", Until: "settled", Release: "none", StableSeconds: 10, TimeoutSeconds: 180, Expect: guard}, "source-established"); err != nil {
		return fmt.Errorf("INCONCLUSIVE: sparse source fixture not established: %w", err)
	}
	ms, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	model, err := readModel(mapValue(ms.Object, "spec"))
	if err != nil || !sameEffectiveModel(model, sourceModel) {
		return fmt.Errorf("INCONCLUSIVE: sparse source model changed")
	}
	if err = saveYAML(filepath.Join(originalDir, "source-initial-server.yaml"), ms.Object); err != nil {
		return err
	}
	return e.locked(func() error {
		if err := writeJSON(filepath.Join(e.dir, "preparation-ledger.json"), e.l); err != nil {
			return err
		}
		fresh, err := newNormalLedger(originalSpec, e.l.Owner, e.l.Profile)
		if err != nil {
			return err
		}
		for uid, o := range e.o.objects["pods"] {
			if objectOwned(o, e.l.Owner) {
				if !e.l.Released[uid] || retained[o.GetName()] != o.GetUID() {
					return fmt.Errorf("INCONCLUSIVE: unverified source Ready UID")
				}
				fresh.Released[uid] = true
				if o.GetLabels()[LabelRole] == "frontend" && ordinal(o.GetLabels()[LabelRoleID]) >= sourceModel.Roles["frontend"].R {
					if fresh.SourceAboveDesiredUIDs == nil {
						fresh.SourceAboveDesiredUIDs = map[string]bool{}
					}
					fresh.SourceAboveDesiredUIDs[uid] = true
				}
			}
		}
		for _, kind := range []string{"controllerrevisions", "pods"} {
			for _, o := range e.o.objects[kind] {
				fresh.After(kind, "LIST", o, e.o.objects)
			}
		}
		if err = fresh.error(); err != nil {
			return err
		}
		if !reflect.DeepEqual(e.current.Object["spec"], ms.Object["spec"]) {
			return fmt.Errorf("INCONCLUSIVE: source spec changed at boundary")
		}
		if err = writeJSON(filepath.Join(originalDir, "source-boundary.json"), map[string]interface{}{"at": time.Now().UTC(), "lastPreparationSequence": e.o.seq, "generation": ms.GetGeneration(), "retainedUIDs": retained, "deletedPreparationUIDs": deleted, "frontendOrdinals": []int{0, 3, 4}, "preparationControllerRestart": true}); err != nil {
			return err
		}
		e.l, e.o.normal, e.current = fresh, fresh, ms
		return nil
	})
}

func ownedConfigMap(cm *corev1.ConfigMap, owner string) bool {
	for _, ref := range cm.OwnerReferences {
		if string(ref.UID) == owner {
			return true
		}
	}
	return false
}
