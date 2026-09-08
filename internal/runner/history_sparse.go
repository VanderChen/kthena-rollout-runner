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

	"kthena.local/rollout-runner/internal/faultproxy"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// The fixture is built using actual controller-created A Pods. The caller must
// keep it separate from the source scenario until this function has established
// the exact sparse population and written a new observation boundary.
func sparseHistoryPods(pods []corev1.Pod, owner string, expected map[string]types.UID) error {
	return sparseSourcePods(pods, owner, expected, false)
}

func sparseSourcePods(pods []corev1.Pod, owner string, expected map[string]types.UID, servingGroup bool) error {
	seen := map[string]bool{}
	for i := range pods {
		p := &pods[i]
		if !owned(p, owner) {
			continue
		}
		role, ord := p.Labels[LabelRole], ordinal(p.Labels[LabelRoleID])
		valid := role == "frontend" && (ord == 0 || ord == 3 || ord == 4) || role == "backend" && ord >= 0 && ord <= 2
		group := ordinal(p.Labels[LabelGroup])
		if servingGroup {
			valid = role == "frontend" && ord == 0 && (group == 0 || group == 3 || group == 4)
		} else {
			valid = valid && group == 0
		}
		if !valid || !podIsEntry(p) || !podReady(p) || podVersion(p) != "A" {
			return fmt.Errorf("sparse fixture has unexpected member %s", p.Name)
		}
		key := fmt.Sprintf("%s/%d/%d", role, group, ord)
		if seen[key] || expected != nil && expected[p.Name] != p.UID {
			return fmt.Errorf("sparse fixture identity changed: %s", p.Name)
		}
		seen[key] = true
	}
	want := 6
	if servingGroup {
		want = 3
	}
	if len(seen) != want {
		return fmt.Errorf("sparse fixture has %d members, expected %d in the declared sparse mode", len(seen), want)
	}
	return nil
}

// The preparation barrier rejects mutations but never queues informer events.
// A new controller therefore starts from the actual sparse R=3 snapshot without
// replaying a previous R=5 scale-down decision after the barrier is released.
func (e *normalExecution) sparseMutationBarrier(ctx context.Context) ([]string, error) {
	resources := []struct{ resource, subresource string }{{"modelservings", ""}, {"modelservings", "status"}, {"pods", ""}, {"pods", "status"}, {"controllerrevisions", ""}, {"services", ""}, {"configmaps", ""}, {"podgroups", ""}, {"podgroups", "status"}}
	var ids []string
	for i, resource := range resources {
		id := fmt.Sprintf("%s-%s-sparse-%d", e.r.opt.RunID, strings.ToLower(e.c.ID), i)
		rule := faultproxy.Rule{ID: id, Namespace: e.namespace, Resource: resource.resource, Subresource: resource.subresource, Mode: "error", Methods: []string{"POST", "PUT", "PATCH", "DELETE"}, StatusCode: 503, Count: -1, DurationSeconds: 180}
		ids = append(ids, id)
		var installed faultproxy.RuleStatus
		if err := e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
			return ids, err
		}
		if err := writeJSON(filepath.Join(e.dir, fmt.Sprintf("sparse-fault-rule-%d.json", i)), installed); err != nil {
			return ids, err
		}
	}
	return ids, e.waitFaultState(ctx, "sparse-mutation-barrier", ids, func(s faultproxy.State) bool { return s.InFlightAllowed == 0 })
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
	initialModel, modelErr := readModel(originalSpec)
	if modelErr != nil {
		return modelErr
	}
	servingGroup := initialModel.Mode == "SG"
	retainedCount := 6
	if servingGroup {
		retainedCount = 3
	}
	expanded := cloneMap(originalSpec)
	if servingGroup {
		expanded["replicas"] = float64(5)
	}
	for _, raw := range listValue(mapValue(expanded, "template"), "roles") {
		role := raw.(map[string]interface{})
		if !servingGroup && textValue(role, "name") == "frontend" {
			role["replicas"] = float64(5)
		}
	}
	// Expanding the zero-budget trap would itself be invalid. Use a finite
	// legal preparation budget; restore the exact source request before its boundary.
	if e.c.Scenario.Fixture == "sparse-boundary-A" {
		if servingGroup && initialModel.U == 0 && initialModel.S == 0 {
			cfg := mapValue(mapValue(expanded, "rolloutStrategy"), "rollingUpdateConfiguration")
			cfg["maxUnavailable"] = float64(1)
		} else if !servingGroup && initialModel.Roles["frontend"].U == 0 && initialModel.Roles["frontend"].S == 0 {
			for _, raw := range listValue(mapValue(expanded, "template"), "roles") {
				role := raw.(map[string]interface{})
				if textValue(role, "name") == "frontend" {
					role["maxUnavailable"] = float64(1)
				}
			}
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
	deletedGroups := map[string]bool{}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !owned(pod, e.l.Owner) {
			continue
		}
		if !podReady(pod) || podVersion(pod) != "A" || !podIsEntry(pod) {
			return fmt.Errorf("INCONCLUSIVE: expanded source A not Ready")
		}
		ord := ordinal(pod.Labels[LabelRoleID])
		if servingGroup {
			ord = ordinal(pod.Labels[LabelGroup])
		}
		if pod.Labels[LabelRole] == "frontend" && (ord == 1 || ord == 2) {
			deleted[pod.Name] = pod.UID
			if servingGroup {
				deletedGroups[pod.Labels[LabelGroup]] = true
			}
		} else {
			retained[pod.Name] = pod.UID
		}
	}
	if len(deleted) != 2 || len(retained) != retainedCount {
		return fmt.Errorf("INCONCLUSIVE: expanded A fixture identity count")
	}
	e.phase = 1
	ids, err := e.sparseMutationBarrier(ctx)
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
			if err = sparseSourcePods(actual.Items, e.l.Owner, retained, servingGroup); err != nil {
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
	// All source reads/events remain live. Keep mutation failures in place
	// until the old process is gone and its replacement has read the real R=3
	// population. No hold rule or pending write is released into the new process.
	state, err := e.r.faultState(ctx)
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, "mutation-barrier-before-restart.json"), state); err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, "source-generation-before-restart.yaml"), e.current.Object); err != nil {
		return err
	}
	if err = e.terminateController(ctx, "fixture-restart"); err != nil {
		return err
	}
	if err = e.waitFaultState(ctx, "mutation-barrier-after-restart", ids, func(s faultproxy.State) bool {
		hits := 0
		for _, rule := range s.Rules {
			for _, id := range ids {
				if rule.ID == id {
					hits += rule.Hits
				}
			}
		}
		return hits > 0 && s.InFlightAllowed == 0
	}); err != nil {
		return err
	}
	if err = e.resumeRecovery(ids, "sparse"); err != nil {
		return err
	}
	resumed = true
	pods, err = e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = sparseSourcePods(pods.Items, e.l.Owner, retained, servingGroup); err != nil {
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
		matches := cm.Labels[LabelRole] == "frontend" && (ord == 1 || ord == 2) && ordinal(cm.Labels[LabelGroup]) == 0
		if servingGroup {
			matches = deletedGroups[cm.Labels[LabelGroup]] && cm.Labels[LabelRole] == "frontend" && ord == 0
		}
		if !ownedConfigMap(&cm, e.l.Owner) || !matches {
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
	if servingGroup {
		groups, err := e.r.dynamic.Resource(PGGVR).Namespace(e.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		var receipts []map[string]interface{}
		for i := range groups.Items {
			pg := &groups.Items[i]
			if !objectOwned(pg, e.l.Owner) || !deletedGroups[pg.GetName()] {
				continue
			}
			actual, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				return err
			}
			if err = sparseSourcePods(actual.Items, e.l.Owner, retained, true); err != nil {
				return err
			}
			if err = e.locked(func() error {
				for _, pod := range e.o.objects["pods"] {
					if objectOwned(pod, e.l.Owner) && pod.GetLabels()[LabelGroup] == pg.GetName() {
						return fmt.Errorf("INCONCLUSIVE: preparation PodGroup still has members")
					}
				}
				return nil
			}); err != nil {
				return err
			}
			if err = saveYAML(filepath.Join(e.dir, "orphan-preparation-podgroup-"+pg.GetName()+".yaml"), pg.Object); err != nil {
				return err
			}
			uid := pg.GetUID()
			options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}
			sent := time.Now().UTC()
			err = e.r.dynamic.Resource(PGGVR).Namespace(e.namespace).Delete(ctx, pg.GetName(), options)
			receipts = append(receipts, map[string]interface{}{"name": pg.GetName(), "uid": uid, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil})
			if saveErr := writeJSON(filepath.Join(e.dir, "preparation-podgroup-cleanup.json"), receipts); saveErr != nil {
				return saveErr
			}
			if err != nil {
				return err
			}
		}
	}
	guard := ScenarioExpectation{NoReplacement: true, NoNewRevision: true, Targets: []ScenarioTarget{{Role: "frontend", Versions: map[string]int{"A": 3}, Ordinals: map[string]string{"0": "A", "3": "A", "4": "A"}}, {Role: "backend", Versions: map[string]int{"A": 3}, Ordinals: map[string]string{"0": "A", "1": "A", "2": "A"}}}}
	if servingGroup {
		guard.Targets = []ScenarioTarget{{Scope: "SG", Versions: map[string]int{"A": 3}, Ordinals: map[string]string{"0": "A", "3": "A", "4": "A"}}}
	}
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
				above := o.GetLabels()[LabelRole] == "frontend" && ordinal(o.GetLabels()[LabelRoleID]) >= sourceModel.Roles["frontend"].R
				if servingGroup {
					above = ordinal(o.GetLabels()[LabelGroup]) >= sourceModel.N
				}
				if above {
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
		if err = writeJSON(filepath.Join(originalDir, "source-boundary.json"), map[string]interface{}{"at": time.Now().UTC(), "lastPreparationSequence": e.o.seq, "generation": ms.GetGeneration(), "retainedUIDs": retained, "deletedPreparationUIDs": deleted, "frontendOrdinals": []int{0, 3, 4}, "preparationControllerRestart": true, "fixtureBarrier": "mutation-errors-until-fresh-initial-sync"}); err != nil {
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
