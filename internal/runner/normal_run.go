// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

type normalExecution struct {
	readinessPod    *corev1.Pod
	faultController *corev1.Pod
	capacityPod     *corev1.Pod
	templateCM      *corev1.ConfigMap
	tableVersion    string
	baselineStatus  map[string]interface{}
	pinned          map[string]types.UID
	r               *Runner
	c               Case
	o               *Observer
	l               *NormalLedger
	namespace, dir  string
	current         *unstructured.Unstructured
	res             *Result
	phase           int
	waitDeadline    time.Time
}

func (r *Runner) runNormalCase(ctx context.Context, c Case) (res Result) {
	retryTrigger := c.Scenario.Source["executionProfile"] == "AUTO_READY_INTERLEAVE"
	for _, step := range c.Scenario.Steps {
		retryTrigger = retryTrigger || step.RequireLiveTerminating
	}
	if !retryTrigger {
		return r.runNormalAttempt(ctx, c, 0)
	}
	dir := filepath.Join(r.root, c.ID)
	if err := os.Mkdir(dir, 0755); err != nil {
		return Result{ID: c.ID, Status: "ERROR", Error: err.Error()}
	}
	started := time.Now()
	var attempts []Result
	for attempt := 1; attempt <= 3; attempt++ {
		res = r.runNormalAttempt(ctx, c, attempt)
		attempts = append(attempts, res)
		if res.Status != "TRIGGER_MISSED" || res.CleanupError != "" || ctx.Err() != nil {
			break
		}
	}
	res.Duration = time.Since(started).Seconds()
	if err := writeJSON(filepath.Join(dir, "attempts.json"), attempts); err != nil {
		res.Status = "ERROR"
		res.Error = err.Error()
	}
	if err := writeJSON(filepath.Join(dir, "result.json"), res); err != nil {
		res.Status = "ERROR"
		res.Error = err.Error()
	}
	return res
}
func (r *Runner) runNormalAttempt(ctx context.Context, c Case, attempt int) (res Result) {
	start := time.Now()
	res = Result{ID: c.ID, Status: "ERROR", Started: start.UTC(), Namespace: "rr-" + r.opt.RunID + "-" + strings.ToLower(c.ID)}
	dir := filepath.Join(r.root, c.ID)
	if attempt > 0 {
		dir = filepath.Join(dir, fmt.Sprintf("attempt-%d", attempt))
		res.Namespace += fmt.Sprintf("-a%d", attempt)
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		res.Error = err.Error()
		return
	}
	defer func() {
		res.Duration = time.Since(start).Seconds()
		if err := writeJSON(filepath.Join(dir, "result.json"), res); err != nil {
			res.Status = "ERROR"
			res.Error += "; " + err.Error()
		}
	}()
	if r.testedCommit() != ProductionCommit {
		res.Error = "normal suite requires --controller-commit=" + ProductionCommit
		return
	}
	if err := saveYAML(filepath.Join(dir, "case.yaml"), c); err != nil {
		res.Error = err.Error()
		return
	}
	ns, err := r.kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: res.Namespace, Labels: map[string]string{"rollout-runner/run": r.opt.RunID, "rollout-runner/case": c.ID}}}, metav1.CreateOptions{})
	if err != nil {
		res.Error = err.Error()
		return
	}
	defer func() {
		if err := r.cleanupNormalNamespace(res.Namespace, ns.UID); err != nil {
			res.Status = "ERROR"
			res.CleanupError = err.Error()
			res.Error += "; cleanup: " + err.Error()
		}
	}()
	o, err := newObserver(ctx, r.dynamic, res.Namespace, dir, true)
	if err != nil {
		res.Error = err.Error()
		return
	}
	e := &normalExecution{pinned: map[string]types.UID{}, tableVersion: "1.0", r: r, c: c, o: o, namespace: res.Namespace, dir: dir, res: &res}
	defer e.finalizeRecoveryEvidence()
	defer func() {
		// Cancel and drain all already-delivered events before freezing the verdict.
		o.cancel()
		o.wg.Wait()
		o.mu.Lock()
		defer o.mu.Unlock()
		if err := saveYAML(filepath.Join(dir, "final-resources.yaml"), o.objects); err != nil {
			res.Error += "; " + err.Error()
			res.Status = "ERROR"
		}
		if o.normal != nil {
			res.NormalStarts = append([]NormalStart{}, o.normal.Starts...)
			res.NormalMetrics = o.normal.Metrics(o.objects["pods"])
			for _, start := range o.normal.Starts {
				if start.Reason == "rollout" {
					res.Sequence = append(res.Sequence, start.Ordinal)
				}
			}
			res.Violations = append([]string{}, o.normal.Violations...)
			if err := writeJSON(filepath.Join(dir, "ledger.json"), o.normal); err != nil {
				res.Error += "; " + err.Error()
				res.Status = "ERROR"
			}
		}
		if o.err != nil {
			res.Status = "INCONCLUSIVE"
			res.Error += "; " + o.err.Error()
		}
		if err := o.journal.Close(); err != nil {
			res.Status = "ERROR"
			res.Error += "; " + err.Error()
		}
		res.enforceLatchedViolations()
	}()
	defer func() {
		if e.templateCM != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			uid := e.templateCM.UID
			if err := r.kube.CoreV1().ConfigMaps("kthena-system").Delete(cleanupCtx, e.templateCM.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
				res.Status = "ERROR"
				res.Error += "; isolated template cleanup: " + err.Error()
			}
		}
	}()
	if err = e.execute(ctx); err != nil {
		res.Error = err.Error()
		res.Status = "FAIL"
		if strings.Contains(res.Error, "INCONCLUSIVE") {
			res.Status = "INCONCLUSIVE"
		}
		if strings.Contains(res.Error, "TRIGGER_MISSED") {
			res.Status = "TRIGGER_MISSED"
		}
		return
	}
	res.Status = "PASS"
	return
}
func (r *Runner) cleanupNormalNamespace(ns string, uid types.UID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// Finalizers belong only to Pods in the exact namespace this attempt created.
	list, err := r.kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, p := range list.Items {
			for _, f := range p.Finalizers {
				if f == "rollout-runner/hold" {
					p.Finalizers = removeString(p.Finalizers, f)
					if _, e := r.kube.CoreV1().Pods(ns).Update(ctx, &p, metav1.UpdateOptions{}); e != nil && !apierrors.IsNotFound(e) {
						return e
					}
				}
			}
		}
	}
	if err = r.kube.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		_, err = r.kube.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
func removeString(ss []string, x string) []string {
	var out []string
	for _, s := range ss {
		if s != x {
			out = append(out, s)
		}
	}
	return out
}
func (e *normalExecution) locked(f func() error) error {
	e.o.mu.Lock()
	defer e.o.mu.Unlock()
	if e.o.err != nil {
		return e.o.err
	}
	if e.l != nil {
		if err := e.l.error(); err != nil {
			return err
		}
	}
	err := f()
	if err != nil {
		return err
	}
	if e.l != nil {
		return e.l.error()
	}
	return nil
}
func (e *normalExecution) snapshot(name string) error {
	return e.locked(func() error { return saveYAML(filepath.Join(e.dir, name+"-resources.yaml"), e.o.objects) })
}
func (e *normalExecution) execute(ctx context.Context) error {
	s := e.c.Scenario
	if e.c.Format == "rollout-runner/v3" {
		controller, err := e.recoveryController(ctx)
		if err != nil {
			return err
		}
		e.faultController = controller
		if err = saveYAML(filepath.Join(e.dir, "case-controller-before.yaml"), controller); err != nil {
			return err
		}
	}
	if err := e.prepareTemplate(ctx); err != nil {
		return err
	}
	before := objectForSpec(e.namespace, e.c.ID, s.InitialSpec)
	if err := saveYAML(filepath.Join(e.dir, "before.yaml"), before.Object); err != nil {
		return err
	}
	created, err := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace).Create(ctx, before, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	e.current = created
	if err = saveYAML(filepath.Join(e.dir, "before-server.yaml"), created.Object); err != nil {
		return err
	}
	l, err := newNormalLedger(s.InitialSpec, string(created.GetUID()), s.Profile)
	if err != nil {
		return err
	}
	e.l = l
	// Account for any readiness event that arrived between Create and arming.
	if err = e.locked(func() error {
		e.o.normal = l
		for _, kind := range []string{"controllerrevisions", "pods"} {
			for _, object := range e.o.objects[kind] {
				l.After(kind, "LIST", object, e.o.objects)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	baseline := ScenarioStep{Name: "baseline", Action: "observe", Until: "settled", Release: "all", StableSeconds: 1}
	if s.Profile == "auto" {
		baseline.Release = "none"
	}
	if err = e.wait(ctx, baseline); err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	if err = e.locked(func() error {
		for _, ms := range e.o.objects["modelservings"] {
			e.baselineStatus = cloneMap(mapValue(ms.Object, "status"))
		}
		return nil
	}); err != nil {
		return err
	}
	if err = e.snapshot("baseline"); err != nil {
		return err
	}
	for i, p := range s.Steps {
		e.phase = i + 1
		e.waitDeadline = time.Time{}
		fmt.Printf("PHASE %s %02d %s\n", e.c.ID, e.phase, p.Name)
		if err = e.step(ctx, p); err != nil {
			if p.RequireLiveTerminating && e.resolvePriorRoleIntent(ctx, p) {
				err = e.finishStep(ctx, p, fmt.Sprintf("step-%02d", e.phase))
			}
		}
		if err != nil {
			return fmt.Errorf("step %02d %s: %w", e.phase, p.Name, err)
		}
	}
	return nil
}
func (e *normalExecution) step(ctx context.Context, p ScenarioStep) error {
	prefix := fmt.Sprintf("step-%02d", e.phase)
	api := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace)
	switch p.Action {
	case "retry-api-error":
		return e.retryAPIError(ctx, p, prefix)
	case "resume-after-grace":
		if err := e.locked(func() error { return e.l.Transition(e.l.Model.Spec, p.Name, p.Expect, e.o.objects) }); err != nil {
			return err
		}
	case "restart-container", "restart-container-grace":
		if err := e.restartContainer(ctx, p, prefix); err != nil {
			return err
		}
	case "recover-pod":
		if err := e.recoverPod(ctx, p, prefix); err != nil {
			return err
		}
	case "update", "merge-patch":
		current, err := api.Get(ctx, "model", metav1.GetOptions{})
		if err != nil {
			return err
		}
		var next *unstructured.Unstructured
		if p.Action == "update" {
			next = objectForSpec(e.namespace, e.c.ID, e.materializeSpec(p.Spec))
			next.SetResourceVersion(current.GetResourceVersion())
			next.SetUID(current.GetUID())
		} else {
			obj := cloneMap(current.Object)
			mergeInto(obj, p.Patch)
			next = &unstructured.Unstructured{Object: obj}
		}
		var terminatingUIDs map[string]bool
		if p.RequireLiveTerminating {
			terminatingUIDs, err = e.liveTerminating(ctx, prefix+"-terminating-before", nil, true)
			if err != nil {
				return err
			}
		}
		if err = e.locked(func() error { return e.l.Transition(mapValue(next.Object, "spec"), p.Name, p.Expect, e.o.objects) }); err != nil {
			return err
		}
		interleave := e.c.Scenario.Source["executionProfile"] == "AUTO_READY_INTERLEAVE" && e.phase == 2
		originalModel := e.l.Model
		if interleave {
			// The previous accepted request is the original rollout; retain its
			// budget/partition when proving a later scale request overlapped.
			var err error
			originalModel, err = readModel(mapValue(current.Object, "spec"))
			if err != nil {
				return err
			}
		}
		var requestAt, receivedAt time.Time
		if err = saveYAML(filepath.Join(e.dir, prefix+"-request.yaml"), next.Object); err != nil {
			return err
		}
		if p.Action == "update" {
			beforeAttempt := func(attemptPrefix string) error {
				if p.RequireLiveTerminating {
					if _, err := e.liveTerminating(ctx, attemptPrefix+"-terminating-before", terminatingUIDs, false); err != nil {
						return err
					}
				}
				if interleave {
					return e.liveOverlap(ctx, originalModel, attemptPrefix+"-trigger-before")
				}
				return nil
			}
			write, writeErr := updateScenario(ctx, api, current, next, e.dir, prefix, beforeAttempt)
			if writeErr != nil {
				return writeErr
			}
			next, requestAt, receivedAt = write.Object, write.Sent, write.Received
			if err = saveYAML(filepath.Join(e.dir, prefix+"-request.yaml"), write.Request.Object); err != nil {
				return err
			}
			// Canonical evidence always describes the successful request. Failed
			// write attempts and their actual trigger proofs remain separate.
			var suffixes []string
			if p.RequireLiveTerminating {
				suffixes = append(suffixes, "-terminating-before.yaml", "-terminating-before-proof.json")
			}
			if interleave {
				suffixes = append(suffixes, "-trigger-before.yaml", "-trigger-before-proof.json")
			}
			for _, suffix := range suffixes {
				data, readErr := os.ReadFile(filepath.Join(e.dir, write.Prefix+suffix))
				if readErr != nil {
					return readErr
				}
				if err = os.WriteFile(filepath.Join(e.dir, prefix+suffix), data, 0644); err != nil {
					return err
				}
			}
		} else {
			if interleave {
				if err = e.liveOverlap(ctx, originalModel, prefix+"-trigger-before"); err != nil {
					return err
				}
			}
			if err = saveYAML(filepath.Join(e.dir, prefix+"-merge-patch.yaml"), p.Patch); err != nil {
				return err
			}
			b, _ := json.Marshal(p.Patch)
			requestAt = time.Now().UTC()
			next, err = api.Patch(ctx, "model", types.MergePatchType, b, metav1.PatchOptions{})
			receivedAt = time.Now().UTC()
		}
		if err != nil {
			return err
		}
		e.current = next
		if err = saveYAML(filepath.Join(e.dir, prefix+"-server.yaml"), next.Object); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(e.dir, prefix+"-request-time.json"), map[string]interface{}{"sent": requestAt, "received": receivedAt, "generation": next.GetGeneration(), "resourceVersion": next.GetResourceVersion()}); err != nil {
			return err
		}
		if p.RequireLiveTerminating {
			if _, err = e.liveTerminating(ctx, prefix+"-terminating-after", terminatingUIDs, false); err != nil {
				return err
			}
		}
		if interleave {
			if err = e.liveOverlap(ctx, originalModel, prefix+"-trigger-after"); err != nil {
				return err
			}
		}
		server, err := readModel(mapValue(next.Object, "spec"))
		if err != nil {
			return err
		}
		if err = e.locked(func() error {
			if !sameEffectiveModel(e.l.Model, server) {
				return fmt.Errorf("DEFAULTING: effective server model differs from submitted expectation")
			}
			return nil
		}); err != nil {
			return err
		}
	case "observe":
		if err := e.locked(func() error { e.l.Phase = p.Name; e.l.Expected = p.Expect; return nil }); err != nil {
			return err
		}
	default:
		if err := e.locked(func() error { return e.l.Transition(e.l.Model.Spec, p.Name, p.Expect, e.o.objects) }); err != nil {
			return err
		}
		if err := e.specialAction(ctx, p, prefix); err != nil {
			return err
		}
	}
	return e.finishStep(ctx, p, prefix)
}
func (e *normalExecution) finishStep(ctx context.Context, p ScenarioStep, prefix string) error {
	if err := e.wait(ctx, p); err != nil {
		return err
	}
	if p.Until == "settled" {
		if err := e.locked(func() error { e.l.Base = e.l.Model; return nil }); err != nil {
			return err
		}
	}
	return e.snapshot(prefix)
}
func sameEffectiveModel(a, b NormalModel) bool {
	if a.Mode != b.Mode || a.N != b.N || a.U != b.U || a.S != b.S || a.P != b.P || len(a.Roles) != len(b.Roles) {
		return false
	}
	for n, r := range a.Roles {
		if r != b.Roles[n] {
			return false
		}
	}
	return true
}
func mergeInto(dst, patch map[string]interface{}) {
	for k, v := range patch {
		if v == nil {
			delete(dst, k)
			continue
		}
		if m, ok := v.(map[string]interface{}); ok {
			d, ok := dst[k].(map[string]interface{})
			if !ok {
				d = map[string]interface{}{}
				dst[k] = d
			}
			mergeInto(d, m)
		} else {
			dst[k] = v
		}
	}
}
func (e *normalExecution) wait(ctx context.Context, p ScenarioStep) error {
	timeout := e.r.opt.Timeout
	if p.TimeoutSeconds > 0 {
		timeout = time.Duration(p.TimeoutSeconds) * time.Second
	}
	if e.waitDeadline.IsZero() {
		e.waitDeadline = time.Now().Add(timeout)
	}
	deadline := time.NewTimer(time.Until(e.waitDeadline))
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	var stableAt time.Time
	var releaseAfter time.Time
	lastReason := ""
	for {
		var candidates []NormalUnit
		done := false
		err := e.locked(func() error {
			var reason string
			if p.Until == "conditions" {
				done = e.l.conditions(p.Conditions, e.o.objects)
				reason = "waiting for declared conditions"
			} else {
				done, reason = e.settled(p.Expect)
			}
			lastReason = reason
			if !done && p.Release != "none" && time.Now().After(releaseAfter) {
				candidates = e.l.releaseCandidates(e.o.objects, p.Exclude)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if done {
			if stableAt.IsZero() {
				stableAt = time.Now()
			}
			if time.Since(stableAt) >= time.Duration(p.StableSeconds+p.HoldSeconds)*time.Second {
				e.res.Checkpoints++
				return e.locked(func() error {
					return writeJSON(filepath.Join(e.dir, fmt.Sprintf("checkpoint-%03d.json", e.res.Checkpoints)), map[string]interface{}{"phase": p.Name, "stableSince": stableAt.UTC(), "completed": time.Now().UTC(), "elapsedStableNanos": time.Since(stableAt).Nanoseconds(), "holdSeconds": p.HoldSeconds, "stableSeconds": p.StableSeconds, "metrics": e.l.Metrics(e.o.objects["pods"]), "starts": e.l.Starts})
				})
			}
		} else if !stableAt.IsZero() {
			return fmt.Errorf("STABILITY_VIOLATION: settled predicate regressed: %s", lastReason)
		}
		if len(candidates) > 0 {
			if p.Release == "one" {
				candidates = candidates[:1]
			}
			for _, u := range candidates {
				if err = e.locked(func() error {
					for _, pod := range u.Pods {
						e.l.Released[string(pod.UID)] = true
					}
					return nil
				}); err != nil {
					return err
				}
				e.res.Releases++
				if err = e.r.release(ctx, Unit{Key: u.Key, Pods: u.Pods}, e.dir, e.res.Releases); err != nil {
					return err
				}
			}
			if p.Release == "one" {
				releaseAfter = time.Now().Add(10 * time.Second)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("TIMEOUT: %s (%s)", p.Name, lastReason)
		case <-tick.C:
		}
	}
}
