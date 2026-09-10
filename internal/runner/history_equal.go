// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kthena.local/rollout-runner/internal/faultproxy"
)

// Keep one actual equivalent owned history throughout the collision. A single
// controller-only stale NotFound read opens the real Create race; the held POST
// is forwarded to the API server, which must return its native AlreadyExists.
func (e *normalExecution) equalHistoryCollision(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	api := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace)
	before, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	next := objectForSpec(e.namespace, e.c.ID, e.materializeSpec(p.Spec))
	next.SetUID(before.GetUID())
	next.SetResourceVersion(before.GetResourceVersion())
	admitted, err := api.Update(ctx, next, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-B-dry-run-admitted.yaml"), admitted.Object); err != nil {
		return err
	}
	target, err := verifyObservedCollisionTarget(mapValue(admitted.Object, "spec"))
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-target-provenance.json"), target); err != nil {
		return err
	}
	current, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err = unchangedRejectedSpec(before, current); err != nil {
		return fmt.Errorf("INCONCLUSIVE: dry-run changed source: %w", err)
	}
	original, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, "model-"+textValue(mapValue(current.Object, "status"), "currentRevision"), metav1.GetOptions{})
	if err != nil {
		return err
	}
	data, err := json.Marshal(target.Data)
	if err != nil {
		return err
	}
	model, err := readModel(mapValue(admitted.Object, "spec"))
	if err != nil {
		return err
	}
	if err = e.locked(func() error { e.l.History = append(e.l.History, model); return nil }); err != nil {
		return err
	}
	request := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: target.Name, Namespace: e.namespace, Labels: map[string]string{"modelserving.volcano.sh/name": "model", "modelserving.volcano.sh/revision": strings.TrimPrefix(target.Name, "model-")}, OwnerReferences: original.OwnerReferences}, Revision: 1, Data: runtime.RawExtension{Raw: data}}
	existing, err := e.createHistoryCollision(ctx, request, prefix+"-equivalent-precreated")
	if err != nil {
		return err
	}
	observed := false
	for attempts := 0; attempts < 100; attempts++ {
		if err = e.locked(func() error { observed = e.o.objects["controllerrevisions"][string(existing.UID)] != nil; return nil }); err != nil {
			return err
		}
		if observed {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !observed {
		return fmt.Errorf("INCONCLUSIVE: precreated equivalent history not observed")
	}
	if err = e.snapshot(prefix + "-source-equivalent"); err != nil {
		return err
	}
	stem := e.r.opt.RunID + "-" + strings.ToLower(e.c.ID) + "-equal"
	postID, readID := stem+"-post", stem+"-stale-read"
	rules := []faultproxy.Rule{
		{ID: postID, Namespace: e.namespace, Resource: "controllerrevisions", Name: target.Name, Methods: []string{"POST"}, Mode: "hold", Count: -1, DurationSeconds: 90},
		{ID: readID, Namespace: e.namespace, Resource: "controllerrevisions", Name: target.Name, Methods: []string{"GET"}, Mode: "error", StatusCode: 404, Count: 1, DurationSeconds: 90},
	}
	var installedIDs []string
	cleared := false
	defer func() {
		if !cleared {
			result = errors.Join(result, e.resumeRecovery(installedIDs, prefix+"-cleanup"))
		}
	}()
	for i, rule := range rules {
		installedIDs = append(installedIDs, rule.ID)
		var installed faultproxy.RuleStatus
		if err = e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(e.dir, fmt.Sprintf("%s-collision-rule-%d.json", prefix, i)), installed); err != nil {
			return err
		}
		if !reflect.DeepEqual(rule, installed.Rule) {
			return fmt.Errorf("INCONCLUSIVE: equivalent collision rule changed")
		}
	}
	ready := true
	submit := ScenarioStep{Name: "equivalent-history-B-request", Action: "update", Spec: p.Spec, Until: "conditions", Release: "none", TimeoutSeconds: 60, Conditions: []ScenarioCondition{{Kind: "unit", Role: "frontend", Version: "A", Ready: &ready, Count: 3}}, Expect: ScenarioExpectation{NoNewRevision: true}}
	if err = e.step(ctx, submit); err != nil {
		return err
	}
	// The single stale read must exhaust; only the POST pause must stay active.
	if err = e.waitFaultState(ctx, prefix+"-native-post-held", []string{postID}, func(state faultproxy.State) bool {
		return equivalentCollisionHeld(state, readID, postID)
	}); err != nil {
		return err
	}
	if err = e.resumeRecovery(installedIDs, prefix+"-native-race"); err != nil {
		return err
	}
	cleared = true
	submit.Name = "equivalent-native-race-delivered"
	submit.StableSeconds = 1
	e.waitDeadline = time.Time{}
	if err = e.wait(ctx, submit); err != nil {
		return err
	}
	if err = e.captureNativeCollision(ctx, postID, target.Name, prefix); err != nil {
		return err
	}
	if err = e.locked(func() error {
		return e.l.Transition(e.l.Model.Spec, "equivalent-history-reused-automatic-B", p.Expect, e.o.objects)
	}); err != nil {
		return err
	}
	e.historyReferences = true
	e.waitDeadline = time.Time{}
	p.Name = "equivalent-history-reused-automatic-B"
	if err = e.finishStep(ctx, p, prefix+"-final"); err != nil {
		return err
	}
	actual, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, existing.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if actual.UID != existing.UID || !reflect.DeepEqual(actual.Data, existing.Data) || !reflect.DeepEqual(actual.OwnerReferences, existing.OwnerReferences) {
		return fmt.Errorf("HISTORY_COLLISION_OVERWRITTEN: equivalent source history replaced or mutated")
	}
	return saveYAML(filepath.Join(e.dir, prefix+"-equivalent-retained.yaml"), actual)
}

func equivalentCollisionHeld(state faultproxy.State, readID, postID string) bool {
	read, post := false, false
	for _, r := range state.Rules {
		if r.ID == readID {
			read = r.Mode == "error" && r.StatusCode == 404 && r.Count == 1 && r.Hits == 1 && !r.Active && r.EndReason == "count-exhausted"
		}
		if r.ID == postID {
			post = r.Mode == "hold" && r.Active && r.Hits > 0 && r.Released == 0
		}
	}
	return read && post
}
