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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"kthena.local/rollout-runner/internal/faultproxy"
)

func historyTargets(model NormalModel, version string, sparse ...bool) ScenarioExpectation {
	initial := []int{0, 1, 2}
	if len(sparse) > 0 && sparse[0] {
		initial = []int{0, 3, 4}
	}
	protected := 0
	for _, ord := range initial {
		if ord < model.Roles["frontend"].P {
			protected++
		}
	}
	versions := map[string]int{}
	ordinals := map[string]string{}
	if protected > 0 {
		versions["A"] = protected
	}
	if protected < 3 {
		versions[version] = 3 - protected
	}
	for _, ord := range initial {
		if ord < model.Roles["frontend"].P {
			ordinals[fmt.Sprint(ord)] = "A"
		}
	}
	return ScenarioExpectation{Targets: []ScenarioTarget{
		{Role: "frontend", Versions: versions, Ordinals: ordinals},
		{Role: "backend", Versions: map[string]int{"A": 3}, Ordinals: map[string]string{"0": "A", "1": "A", "2": "A"}},
	}}
}

func historyVersionSpec(spec map[string]interface{}, version string) map[string]interface{} {
	out := cloneMap(spec)
	for _, raw := range listValue(mapValue(out, "template"), "roles") {
		role := raw.(map[string]interface{})
		if textValue(role, "name") != "frontend" {
			continue
		}
		for _, key := range []string{"entryTemplate", "workerTemplate"} {
			for _, raw := range listValue(mapValue(mapValue(role, key), "spec"), "containers") {
				for _, raw := range listValue(raw.(map[string]interface{}), "env") {
					env := raw.(map[string]interface{})
					if env["name"] == "ROLLOUT_VERSION" {
						env["value"] = version
					}
				}
			}
		}
	}
	return out
}

// Read actual Pod identities before fetching their histories. Recheck the exact
// Pod UID after a missing history so legitimate GC after its final reference
// disappears cannot be mistaken for a product failure across two API reads.
func (e *normalExecution) verifyHistoryReferences(ctx context.Context) (result error) {
	e.historyProbe++
	proof := map[string]interface{}{"started": time.Now().UTC(), "phase": e.l.Phase}
	defer func() {
		proof["completed"] = time.Now().UTC()
		if result != nil {
			proof["error"] = result.Error()
		}
		result = errors.Join(result, writeJSON(filepath.Join(e.dir, fmt.Sprintf("history-probe-%04d.json", e.historyProbe)), proof))
	}()
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: live history Pod references: %w", err)
	}
	proof["pods"] = pods
	histories := map[string]interface{}{}
	proof["histories"] = histories
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !owned(pod, e.l.Owner) {
			continue
		}
		name := "model-" + pod.Labels["modelserving.volcano.sh/revision"]
		if _, checked := histories[name]; checked {
			continue
		}
		cr, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			live, getErr := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if apierrors.IsNotFound(getErr) || getErr == nil && live.UID != pod.UID {
				continue
			}
			if getErr != nil {
				return fmt.Errorf("INCONCLUSIVE: confirm history reference UID: %w", getErr)
			}
			proof["confirmedLiveReference"] = live
			return e.historyFailure("LIVE_HISTORY_MISSING: " + name + " referenced by " + pod.Name + "/" + string(pod.UID))
		}
		if err != nil {
			return fmt.Errorf("INCONCLUSIVE: direct history read: %w", err)
		}
		histories[name] = cr
		object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cr)
		if err != nil {
			return err
		}
		u := &unstructured.Unstructured{Object: object}
		if !objectOwned(u, e.l.Owner) || cr.DeletionTimestamp != nil {
			return e.historyFailure("LIVE_HISTORY_INVALID: " + name)
		}
		data, _ := json.Marshal(u.Object["data"])
		if err = e.locked(func() error {
			if old, ok := e.l.RevisionData[string(cr.UID)]; ok && old != string(data) {
				e.l.fail("HISTORY_MUTATED: " + name)
			}
			return nil
		}); err != nil {
			return err
		}
		// Every live Pod referencing this history must carry the historical
		// template version, including terminating and surge instances.
		spec := cloneMap(e.l.Model.Spec)
		spec["template"] = map[string]interface{}{"roles": mapValue(u.Object, "data")["data"]}
		model, err := readModel(spec)
		if err != nil {
			return e.historyFailure("INVALID_HISTORY: " + name)
		}
		for j := range pods.Items {
			p := &pods.Items[j]
			if !owned(p, e.l.Owner) || "model-"+p.Labels["modelserving.volcano.sh/revision"] != name {
				continue
			}
			role, ok := model.Roles[p.Labels[LabelRole]]
			version := role.Worker
			if podIsEntry(p) {
				version = role.Entry
			}
			if !ok || version != podVersion(p) {
				return e.historyFailure("HISTORY_TEMPLATE_MISMATCH: " + p.Name)
			}
			if cr.CreationTimestamp.After(p.CreationTimestamp.Time) {
				return e.historyFailure("HISTORY_PERSISTED_AFTER_POD: " + p.Name)
			}
		}
	}
	return nil
}

func (e *normalExecution) historyFailure(reason string) error {
	return e.locked(func() error { e.l.fail(reason); return nil })
}

func (e *normalExecution) historyCreateRecovery(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	config := mapValue(e.c.Scenario.Source, "config")
	server := mapValue(e.current.Object, "spec")
	want, declared := config["revisionHistoryLimit"]
	_, present := server["revisionHistoryLimit"]
	if declared && want != "omitted" {
		if !present || intValue(server, "revisionHistoryLimit", -1) != intValue(config, "revisionHistoryLimit", -2) {
			return fmt.Errorf("DEFAULTING: source revisionHistoryLimit not preserved")
		}
	} else if intValue(server, "revisionHistoryLimit", 10) != 10 {
		return fmt.Errorf("DEFAULTING: omitted history limit did not default to 10")
	}
	e.historyReferences = true
	if err := e.verifyHistoryReferences(ctx); err != nil {
		return err
	}
	id := fmt.Sprintf("%s-%s-history-create", e.r.opt.RunID, strings.ToLower(e.c.ID))
	cleared := false
	defer func() {
		if !cleared {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			state, err := e.r.faultState(cleanup)
			if err == nil {
				err = writeJSON(filepath.Join(e.dir, prefix+"-create-failure-state.json"), state)
			}
			result = errors.Join(result, err, e.resumeRecovery([]string{id}, prefix+"-cleanup"))
		}
	}()
	rule := faultproxy.Rule{ID: id, Namespace: e.namespace, Resource: "controllerrevisions", OwnerUID: e.l.Owner, Methods: []string{"POST"}, Mode: "error", StatusCode: 503, Count: -1, DurationSeconds: 180}
	var installed faultproxy.RuleStatus
	if err := e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(e.dir, prefix+"-create-installed.json"), installed); err != nil {
		return err
	}
	if !reflect.DeepEqual(rule, installed.Rule) {
		return fmt.Errorf("INCONCLUSIVE: exact CR creation fault not acknowledged")
	}
	ready := true
	held := ScenarioStep{Name: "B-accepted-history-create-blocked-A-retained", Action: "update", Spec: p.Spec, Until: "conditions", Release: "none", Conditions: []ScenarioCondition{{Kind: "unit", Role: "frontend", Version: "A", Ready: &ready, Count: 3}}, Expect: ScenarioExpectation{NoReplacement: true, NoNewRevision: true}, TimeoutSeconds: p.TimeoutSeconds}
	if err := e.step(ctx, held); err != nil {
		return err
	}
	if err := e.waitFaultState(ctx, prefix+"-create-hit", []string{id}, func(state faultproxy.State) bool {
		for _, rule := range state.Rules {
			if rule.ID == id && rule.Hits > 0 && len(state.Errors) == 0 {
				return true
			}
		}
		return false
	}); err != nil {
		return err
	}
	held.Name, held.HoldSeconds = "actual-CR-create-failure-no-template-actions", 10
	if err := e.wait(ctx, held); err != nil {
		return err
	}
	if err := e.snapshot(prefix + "-create-blocked"); err != nil {
		return err
	}
	if err := e.waitFaultState(ctx, prefix+"-before-create-clear", []string{id}, func(state faultproxy.State) bool { return len(state.Errors) == 0 }); err != nil {
		return err
	}
	finishB := p
	finishB.Action, finishB.Name, finishB.Expect = "observe", "B-allowed-target-after-history-recovery", historyTargets(e.l.Model, "B", e.c.Scenario.Fixture == "sparse-history-A")
	finishB.StableSeconds = 10
	if err := e.locked(func() error { return e.l.Transition(e.l.Model.Spec, finishB.Name, finishB.Expect, e.o.objects) }); err != nil {
		return err
	}
	if err := e.resumeRecovery([]string{id}, prefix+"-create"); err != nil {
		return err
	}
	cleared = true
	e.waitDeadline = time.Time{}
	if err := e.finishStep(ctx, finishB, prefix+"-B-allowed"); err != nil {
		return err
	}
	// Pin a real eligible Ready B before C. P=3 on {0,1,2} produces no
	// eligible old termination, so that conditional trigger is explicitly absent.
	sparse := e.c.Scenario.Fixture == "sparse-history-A"
	eligible := e.l.Model.Roles["frontend"].P < 3 || sparse
	pinOrdinal := 2
	if sparse {
		pinOrdinal = -1
		if err := e.locked(func() error {
			for _, unit := range e.l.roleUnits(e.o.objects["pods"]) {
				if unit.Role == "frontend" && unit.Ready && unit.Version == "B" && unit.Ordinal >= e.l.Model.Roles["frontend"].P {
					pinOrdinal = max(pinOrdinal, unit.Ordinal)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if pinOrdinal < 0 {
			return fmt.Errorf("INCONCLUSIVE: sparse B target has no actual eligible Ready instance to pin")
		}
	}
	if eligible {
		ordinal := pinOrdinal
		pin := ScenarioStep{Action: "pin", Conditions: []ScenarioCondition{{Kind: "unit", Role: "frontend", Ordinal: &ordinal, Version: "B", Ready: &ready, Count: 1}}}
		if err := e.specialAction(ctx, pin, prefix+"-before-C"); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(e.dir, prefix+"-terminating-trigger.json"), map[string]interface{}{"eligible": eligible, "pinnedUIDs": e.pinned, "reason": "actual source ordinals and partition", "fixture": e.c.Scenario.Fixture, "ordinal": pinOrdinal}); err != nil {
		return err
	}
	next := p
	next.Action, next.Name, next.Spec = "update", "C-after-allowed-B", historyVersionSpec(p.Spec, "C")
	next.Expect = historyTargets(e.l.Model, "C", e.c.Scenario.Fixture == "sparse-history-A")
	if eligible {
		next.Until, next.HoldSeconds, next.StableSeconds = "conditions", 10, 0
		ordinal := pinOrdinal
		next.Conditions = []ScenarioCondition{{Kind: "terminating", Role: "frontend", Ordinal: &ordinal, Version: "B", Count: 1}}
	}
	// Nested requests have their own phase number to preserve B request files.
	e.phase++
	e.waitDeadline = time.Time{}
	if err := e.step(ctx, next); err != nil {
		return err
	}
	if eligible {
		if _, err := e.liveTerminating(ctx, prefix+"-C-live-old", nil, false); err != nil {
			return err
		}
		if err := e.verifyHistoryReferences(ctx); err != nil {
			return err
		}
		if err := e.specialAction(ctx, ScenarioStep{Action: "unpin"}, prefix+"-release-old"); err != nil {
			return err
		}
	}
	next.Action, next.Until, next.HoldSeconds, next.StableSeconds = "observe", "settled", 0, 10
	next.Name = "C-allowed-target-after-old-release"
	e.waitDeadline = time.Time{}
	if err := e.finishStep(ctx, next, prefix+"-C-allowed"); err != nil {
		return err
	}
	if err := e.locked(func() error {
		return e.l.Transition(e.l.Model.Spec, "C-stable-across-controller-restart", ScenarioExpectation{NoReplacement: true, NoNewRevision: true}, e.o.objects)
	}); err != nil {
		return err
	}
	if err := e.terminateController(ctx, prefix+"-restart"); err != nil {
		return err
	}
	next.Name, next.StableSeconds = "C-retained-after-controller-restart", p.StableSeconds
	e.waitDeadline = time.Time{}
	return e.finishStep(ctx, next, prefix+"-final")
}
