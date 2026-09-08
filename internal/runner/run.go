// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

type Options struct {
	FaultProxyControl, FaultProxyAPI, FaultProxyTokenFile       string
	CaseDir, OutDir, Kubeconfig, RunID, Select, ControllerImage string
	ControllerCommit                                            string
	Hold                                                        time.Duration
	Timeout                                                     time.Duration
}
type Result struct {
	NormalStarts  []NormalStart `json:"normalStarts,omitempty"`
	NormalMetrics []ScopeMetric `json:"normalMetrics,omitempty"`
	ID            string        `json:"id"`
	Status        string        `json:"status"`
	Error         string        `json:"error,omitempty"`
	CleanupError  string        `json:"cleanupError,omitempty"`
	Namespace     string        `json:"namespace"`
	Started       time.Time     `json:"started"`
	Duration      float64       `json:"durationSeconds"`
	HoldSeconds   float64       `json:"holdSeconds"`
	Sequence      []int         `json:"startSequence"`
	Metrics       []Metrics     `json:"timeline"`
	Violations    []string      `json:"violations,omitempty"`
	Checkpoints   int           `json:"checkpoints"`
	Releases      int           `json:"releases"`
}
type Summary struct {
	RunID     string   `json:"runID"`
	Baseline  string   `json:"kthenaBaseline"`
	Available int      `json:"availableCases"`
	Selected  int      `json:"selected"`
	Passed    int      `json:"passed"`
	Results   []Result `json:"results"`
}
type Runner struct {
	opt     Options
	rest    *rest.Config
	kube    kubernetes.Interface
	dynamic dynamic.Interface
	root    string
}

func Run(ctx context.Context, opt Options) error {
	cases, err := LoadCases(opt.CaseDir)
	if err != nil {
		return err
	}
	if opt.RunID == "" {
		opt.RunID = time.Now().UTC().Format("20060102-150405")
	}
	if len(opt.RunID) > 30 || strings.Trim(opt.RunID, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return fmt.Errorf("invalid run ID")
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 180 * time.Second
	}
	var cfg *rest.Config
	if opt.Kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", opt.Kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return err
	}
	cfg.QPS = 30
	cfg.Burst = 60
	cfg.UserAgent = "kthena-rollout-runner"
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}
	root := filepath.Join(opt.OutDir, opt.RunID)
	if err = os.MkdirAll(opt.OutDir, 0755); err != nil {
		return err
	}
	if err = os.Mkdir(root, 0755); err != nil {
		return fmt.Errorf("refuse to overwrite attempt: %w", err)
	}
	r := &Runner{opt: opt, rest: cfg, kube: kube, dynamic: dyn, root: root}
	if err = r.preflight(ctx); err != nil {
		return err
	}
	for _, c := range cases {
		if c.Format == "rollout-runner/v3" {
			if err = r.preflightRecovery(ctx); err != nil {
				return err
			}
			break
		}
	}
	selected := map[string]bool{}
	if opt.Select != "" {
		for _, id := range strings.Split(opt.Select, ",") {
			selected[id] = true
		}
	}
	summary := Summary{RunID: opt.RunID, Baseline: r.testedCommit(), Available: len(cases)}
	for _, c := range cases {
		if len(selected) > 0 && !selected[c.ID] {
			continue
		}
		summary.Selected++
	}
	if summary.Selected == 0 || len(selected) > 0 && summary.Selected != len(selected) {
		return fmt.Errorf("unknown/empty case selection")
	}
	for index, c := range cases {
		if len(selected) > 0 && !selected[c.ID] {
			continue
		}
		fmt.Printf("CASE %s START run=%s\n", c.ID, opt.RunID)
		var result Result
		if c.Scenario != nil {
			result = r.runNormalCase(ctx, c)
		} else {
			result = r.runCase(ctx, c)
		}
		summary.Results = append(summary.Results, result)
		if result.Status == "PASS" {
			summary.Passed++
		}
		if result.CleanupError != "" {
			for _, pending := range cases[index+1:] {
				if len(selected) > 0 && !selected[pending.ID] {
					continue
				}
				summary.Results = append(summary.Results, Result{ID: pending.ID, Status: "NOT_RUN", Error: "previous case cleanup failed"})
			}
		}
		if err = writeJSON(filepath.Join(root, "summary.json"), summary); err != nil {
			return err
		}
		if err = writeJUnit(filepath.Join(root, "junit.xml"), summary); err != nil {
			return err
		}
		fmt.Printf("CASE %s %s %.1fs starts=%v holds=%d releases=%d %s\n", c.ID, result.Status, result.Duration, result.Sequence, result.Checkpoints, result.Releases, result.Error)
		if result.CleanupError != "" {
			return fmt.Errorf("stopped at baseline cleanup barrier: %s", result.CleanupError)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	fmt.Printf("SUMMARY selected=%d passed=%d failed=%d artifacts=%s\n", summary.Selected, summary.Passed, summary.Selected-summary.Passed, root)
	if summary.Passed != summary.Selected {
		return fmt.Errorf("%d/%d cases did not pass", summary.Selected-summary.Passed, summary.Selected)
	}
	return nil
}
func (r *Runner) preflight(ctx context.Context) error {
	version, err := r.kube.Discovery().ServerVersion()
	if err != nil {
		return err
	}
	nodes, err := r.kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	deployment, err := r.kube.AppsV1().Deployments("kthena-system").Get(ctx, "kthena-controller-manager", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if r.opt.ControllerImage == "" {
		return fmt.Errorf("--controller-image is required to pin the tested deployment")
	}
	found := false
	for _, c := range deployment.Spec.Template.Spec.Containers {
		if c.Image == r.opt.ControllerImage {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("controller image mismatch: expected %s", r.opt.ControllerImage)
	}
	if deployment.Status.AvailableReplicas < 1 {
		return fmt.Errorf("controller unavailable")
	}
	pgs, err := r.dynamic.Resource(PGGVR).Namespace("default").List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return err
	}
	_ = pgs
	for _, name := range []string{"production020-ranktable-parser", "production020-ranktable-template"} {
		if _, err = r.kube.CoreV1().ConfigMaps("kthena-system").Get(ctx, name, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("apply deploy/production-fixture.yaml first: %w", err)
		}
	}
	crdGVR := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	crd, err := r.dynamic.Resource(crdGVR).Get(ctx, "modelservings.workload.serving.volcano.sh", metav1.GetOptions{})
	if err != nil {
		return err
	}
	deps, err := r.kube.AppsV1().Deployments("volcano-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	controllerPods, err := r.kube.CoreV1().Pods("kthena-system").List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/component=kthena-controller-manager"})
	if err != nil {
		return err
	}
	provenance := map[string]interface{}{}
	if path, err := os.Executable(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			provenance["binarySHA256"] = fmt.Sprintf("%x", sha256.Sum256(data))
		}
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		provenance["goBuildInfo"] = info
	}
	inputs := map[string]string{}
	paths, err := filepath.Glob(filepath.Join(r.opt.CaseDir, "RUN-*.yaml"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		inputs[filepath.Base(path)] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	provenance["caseSHA256"] = inputs
	if r.opt.Kubeconfig == "" {
		ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return err
		}
		name, err := os.Hostname()
		if err != nil {
			return err
		}
		pod, err := r.kube.CoreV1().Pods(strings.TrimSpace(string(ns))).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		provenance["runnerPod"] = pod
	}
	return writeJSON(filepath.Join(r.root, "environment.json"), map[string]interface{}{
		"baseline": r.testedCommit(), "kubernetes": version, "nodes": nodes.Items, "controller": deployment, "controllerPods": controllerPods.Items, "volcano": deps.Items, "modelServingCRD": crd.Object, "options": r.opt, "runner": provenance,
	})
}
func (r *Runner) runCase(ctx context.Context, c Case) (res Result) {
	started := time.Now()
	res = Result{ID: c.ID, Status: "ERROR", Started: started.UTC(), Namespace: "rr-" + r.opt.RunID + "-" + strings.ToLower(c.ID)}
	dir := filepath.Join(r.root, c.ID)
	if err := os.Mkdir(dir, 0755); err != nil {
		res.Error = err.Error()
		return
	}
	defer func() {
		res.Duration = time.Since(started).Seconds()
		if err := writeJSON(filepath.Join(dir, "result.json"), res); err != nil {
			res.Status = "ERROR"
			res.Error += "; result write: " + err.Error()
		}
	}()
	fail := func(err error) {
		res.Error = err.Error()
		res.Status = "FAIL"
		if strings.Contains(res.Error, "INCONCLUSIVE") {
			res.Status = "INCONCLUSIVE"
		}
	}
	if err := saveYAML(filepath.Join(dir, "case.yaml"), c); err != nil {
		res.Error = err.Error()
		return
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: res.Namespace, Labels: map[string]string{"rollout-runner/run": r.opt.RunID, "rollout-runner/case": c.ID}}}
	createdNS, err := r.kube.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		res.Error = err.Error()
		return
	}
	// Cleanup only the exact namespace this attempt successfully created.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		uid := types.UID(createdNS.UID)
		err := r.kube.CoreV1().Namespaces().Delete(cleanupCtx, res.Namespace, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if err == nil || apierrors.IsNotFound(err) {
			tick := time.NewTicker(200 * time.Millisecond)
			defer tick.Stop()
			for {
				_, err = r.kube.CoreV1().Namespaces().Get(cleanupCtx, res.Namespace, metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					err = nil
					break
				}
				if err != nil {
					break
				}
				select {
				case <-cleanupCtx.Done():
					err = cleanupCtx.Err()
				case <-tick.C:
				}
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			res.Status = "ERROR"
			res.Error += "; cleanup: " + err.Error()
			res.CleanupError = err.Error()
		}
	}()
	obs, err := NewObserver(ctx, r.dynamic, res.Namespace, dir)
	if err != nil {
		res.Error = err.Error()
		return
	}
	defer func() {
		if err := obs.Snapshot(filepath.Join(dir, "final-resources.yaml")); err != nil {
			res.Status = "ERROR"
			res.Error += "; snapshot: " + err.Error()
		}
		var observationErr error
		res.Metrics, res.Sequence, res.Violations, observationErr = obs.Finalize()
		if observationErr != nil {
			res.Status = "INCONCLUSIVE"
			res.Error += "; " + observationErr.Error()
		}
		if err := obs.Close(); err != nil {
			res.Status = "ERROR"
			res.Error += "; journal close: " + err.Error()
		}
		// A last Watch event can arrive between the completion check and the
		// atomic ledger freeze. Never let an already latched violation pass.
		res.enforceLatchedViolations()
	}()
	api := r.dynamic.Resource(MSGVR).Namespace(res.Namespace)
	a, err := c.Manifest(res.Namespace, "A")
	if err != nil {
		res.Error = err.Error()
		return
	}
	if err = saveYAML(filepath.Join(dir, "before.yaml"), a.Object); err != nil {
		res.Error = err.Error()
		return
	}
	created, err := api.Create(ctx, a, metav1.CreateOptions{})
	if err != nil {
		res.Error = err.Error()
		return
	}
	if err = saveYAML(filepath.Join(dir, "before-server.yaml"), created.Object); err != nil {
		res.Error = err.Error()
		return
	}
	if err = c.checkSpec(created.Object["spec"].(map[string]interface{}), true); err != nil {
		fail(fmt.Errorf("DEFAULTING: %w", err))
		return
	}
	owner := string(created.GetUID())
	if err = r.await(ctx, obs, "baseline", func(_ *Ledger, obj map[string]map[string]*unstructured.Unstructured) (bool, error) {
		for _, p := range obj["pods"] {
			var pod corev1.Pod
			if err = convertPod(p, &pod); err != nil {
				return false, err
			}
			if !podReady(&pod) {
				return false, nil
			}
		}
		l, e := NewLedger(c, owner, obj["pods"], obj["podgroups"])
		if e != nil {
			return false, nil
		}
		if len(obj["podgroups"]) != int(c.Input.Spec["replicas"].(float64)) {
			return false, nil
		}
		_ = l
		for _, m := range obj["modelservings"] {
			n, _, _ := unstructured.NestedInt64(m.Object, "status", "availableReplicas")
			if n == int64(c.Input.Spec["replicas"].(float64)) {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		fail(err)
		return
	}
	if err = obs.Snapshot(filepath.Join(dir, "baseline-resources.yaml")); err != nil {
		res.Error = err.Error()
		return
	}
	if err = obs.Arm(c, owner); err != nil {
		res.Error = err.Error()
		return
	}
	b, err := c.Manifest(res.Namespace, "B")
	if err != nil {
		res.Error = err.Error()
		return
	}
	current, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		res.Error = err.Error()
		return
	}
	b.SetResourceVersion(current.GetResourceVersion())
	if err = saveYAML(filepath.Join(dir, "after.yaml"), b.Object); err != nil {
		res.Error = err.Error()
		return
	}
	changed, err := api.Update(ctx, b, metav1.UpdateOptions{})
	if err != nil {
		res.Error = err.Error()
		return
	}
	if err = saveYAML(filepath.Join(dir, "after-server.yaml"), changed.Object); err != nil {
		res.Error = err.Error()
		return
	}
	if err = c.checkSpec(changed.Object["spec"].(map[string]interface{}), true); err != nil {
		fail(fmt.Errorf("DEFAULTING: %w", err))
		return
	}
	if err = r.await(ctx, obs, "initial starts", func(l *Ledger, _ map[string]map[string]*unstructured.Unstructured) (bool, error) {
		return len(l.Started) == c.Expect.InitialStarts, nil
	}); err != nil {
		fail(err)
		return
	}
	hold := time.Duration(c.Process.HoldSeconds) * time.Second
	if r.opt.Hold > 0 {
		hold = r.opt.Hold
	}
	res.HoldSeconds = hold.Seconds()
	// Each release is separated by a continuously observed hold. Do not use
	// sleeps that stop event processing or inspect only the ending snapshot.
	for round := 0; round < 20; round++ {
		if err = r.hold(ctx, obs, hold); err != nil {
			fail(err)
			return
		}
		res.Checkpoints++
		_, err = obs.Inspect(func(l *Ledger, obj map[string]map[string]*unstructured.Unstructured) (bool, error) {
			return true, writeJSON(filepath.Join(dir, fmt.Sprintf("checkpoint-%02d.json", res.Checkpoints)), map[string]interface{}{"holdSeconds": hold.Seconds(), "metrics": l.Check(obj["pods"]), "starts": l.StartSequence, "units": l.units(obj["pods"])})
		})
		if err != nil {
			fail(err)
			return
		}
		done, err := obs.Inspect(func(l *Ledger, obj map[string]map[string]*unstructured.Unstructured) (bool, error) {
			return finalFacts(l, obj, changed.GetGeneration()), nil
		})
		if err != nil {
			fail(err)
			return
		}
		if done {
			res.Status = "PASS"
			return
		}
		var next Unit
		err = r.await(ctx, obs, "next blocked target or completion", func(l *Ledger, obj map[string]map[string]*unstructured.Unstructured) (bool, error) {
			if finalFacts(l, obj, changed.GetGeneration()) {
				return true, nil
			}
			candidates := l.Candidates(obj["pods"])
			if len(candidates) == 0 {
				return false, nil
			}
			next = candidates[0]
			return true, nil
		})
		if err != nil {
			fail(err)
			return
		}
		if next.Key == "" {
			res.Status = "PASS"
			return
		}
		beforeStarts := 0
		_, err = obs.Inspect(func(l *Ledger, _ map[string]map[string]*unstructured.Unstructured) (bool, error) {
			beforeStarts = len(l.Started)
			for _, p := range next.Pods {
				l.Released[string(p.UID)] = true
			}
			l.ReleasedUnits[next.Key+"/"+string(next.Pods[0].UID)] = true
			return true, nil
		})
		if err != nil {
			fail(err)
			return
		}
		if err = r.release(ctx, next, dir, res.Releases+1); err != nil {
			res.Error = err.Error()
			return
		}
		res.Releases++
		if err = r.await(ctx, obs, "released target Ready", func(l *Ledger, obj map[string]map[string]*unstructured.Unstructured) (bool, error) {
			unit, ok := l.units(obj["pods"])[next.Key]
			if !ok {
				return false, nil
			}
			if unit.Ready && unit.Version == "B" {
				return true, nil
			}
			return false, nil
		}); err != nil {
			fail(err)
			return
		}
		if beforeStarts < c.Expect.FinalNew {
			if err = r.await(ctx, obs, "advance after one Ready credit", func(l *Ledger, _ map[string]map[string]*unstructured.Unstructured) (bool, error) {
				return len(l.Started) >= beforeStarts+1, nil
			}); err != nil {
				fail(err)
				return
			}
		}
	}
	fail(fmt.Errorf("process exceeded finite release steps"))
	return
}
func (r *Runner) await(ctx context.Context, o *Observer, name string, check func(*Ledger, map[string]map[string]*unstructured.Unstructured) (bool, error)) error {
	deadline := time.NewTimer(r.opt.Timeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		ok, err := o.Inspect(check)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("TIMEOUT: %s", name)
		case <-tick.C:
		}
	}
}
func (r *Runner) hold(ctx context.Context, o *Observer, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := o.Inspect(func(l *Ledger, obj map[string]map[string]*unstructured.Unstructured) (bool, error) {
			l.Check(obj["pods"])
			return true, nil
		}); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-tick.C:
		}
	}
}
func (r *Runner) release(ctx context.Context, u Unit, dir string, index int) error {
	for _, pod := range u.Pods {
		current, err := r.kube.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if current.UID != pod.UID || current.DeletionTimestamp != nil {
			return fmt.Errorf("release UID changed: %s", pod.Name)
		}
		req := r.kube.CoreV1().RESTClient().Post().Resource("pods").Namespace(pod.Namespace).Name(pod.Name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: "workload", Command: []string{"touch", "/tmp/ready"}, Stdout: true, Stderr: true}, scheme.ParameterCodec)
		executor, err := remotecommand.NewSPDYExecutor(r.rest, "POST", req.URL())
		if err != nil {
			return err
		}
		var out, stderr bytes.Buffer
		commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = executor.StreamWithContext(commandCtx, remotecommand.StreamOptions{Stdout: &out, Stderr: &stderr})
		cancel()
		if err != nil {
			return fmt.Errorf("release %s: %w %s", pod.Name, err, stderr.String())
		}
		current, err = r.kube.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if current.UID != pod.UID {
			return fmt.Errorf("release UID raced: %s", pod.Name)
		}
	}
	return writeJSON(filepath.Join(dir, fmt.Sprintf("release-%02d.json", index)), map[string]interface{}{"at": time.Now().UTC(), "unit": u.Key, "pods": u.Pods})
}
func finalFacts(l *Ledger, obj map[string]map[string]*unstructured.Unstructured, generation int64) bool {
	e := l.Case.Expect
	units := l.units(obj["pods"])
	if len(units) != e.Desired || len(l.Started) != e.FinalNew {
		return false
	}
	old, newCount := 0, 0
	for _, u := range units {
		if !u.Ready {
			return false
		}
		if u.Version == "A" {
			old++
		} else if u.Version == "B" {
			newCount++
		} else {
			return false
		}
	}
	if old != e.FinalOld || newCount != e.FinalNew {
		return false
	}
	for uid := range l.ProtectedUIDs {
		p, ok := obj["pods"][uid]
		if !ok || p.GetDeletionTimestamp() != nil {
			return false
		}
	}
	groupN := int(l.Case.Input.Spec["replicas"].(float64))
	if len(obj["podgroups"]) != groupN {
		return false
	}
	totalPods := 0
	for _, raw := range l.Case.Input.Spec["template"].(map[string]interface{})["roles"].([]interface{}) {
		r := raw.(map[string]interface{})
		totalPods += int(r["replicas"].(float64)) * (1 + int(r["workerReplicas"].(float64)))
	}
	pgNames := map[string]bool{}
	for _, pg := range obj["podgroups"] {
		n, _, _ := unstructured.NestedInt64(pg.Object, "spec", "minMember")
		if n != int64(totalPods) || pg.GetDeletionTimestamp() != nil {
			return false
		}
		for key, want := range map[string]string{"cpu": fmt.Sprintf("%dm", 5*totalPods), "memory": fmt.Sprintf("%dMi", 4*totalPods)} {
			got, _, _ := unstructured.NestedString(pg.Object, "spec", "minResources", key)
			q, err := resource.ParseQuantity(got)
			if err != nil || q.Cmp(resource.MustParse(want)) != 0 {
				return false
			}
		}
		ownerOK := false
		for _, ref := range pg.GetOwnerReferences() {
			if string(ref.UID) == l.Owner {
				ownerOK = true
			}
		}
		if !ownerOK {
			return false
		}
		pgNames[pg.GetName()] = true
	}
	for _, p := range obj["pods"] {
		if p.GetDeletionTimestamp() != nil || !pgNames[p.GetAnnotations()["scheduling.k8s.io/group-name"]] {
			return false
		}
	}
	if len(obj["pods"]) != groupN*totalPods {
		return false
	}
	for _, m := range obj["modelservings"] {
		observed, _, _ := unstructured.NestedInt64(m.Object, "status", "observedGeneration")
		ready, _, _ := unstructured.NestedInt64(m.Object, "status", "availableReplicas")
		replicas, _, _ := unstructured.NestedInt64(m.Object, "status", "replicas")
		updated, _, _ := unstructured.NestedInt64(m.Object, "status", "updatedReplicas")
		current, _, _ := unstructured.NestedString(m.Object, "status", "currentRevision")
		update, _, _ := unstructured.NestedString(m.Object, "status", "updateRevision")
		if observed < generation || ready != int64(groupN) || replicas != int64(groupN) || update == "" {
			return false
		}
		expectedUpdated := e.FinalNew
		if e.Mode == "Role" {
			expectedUpdated = 0
			if e.P == 0 {
				expectedUpdated = groupN
			}
		}
		if updated != int64(expectedUpdated) {
			return false
		}
		if e.P == 0 && current != update {
			return false
		}
		revisions := map[string]bool{}
		for _, cr := range obj["controllerrevisions"] {
			for _, ref := range cr.GetOwnerReferences() {
				if string(ref.UID) == l.Owner {
					revisions[cr.GetName()] = true
				}
			}
		}
		// ModelServing status contains the hash, while the persisted CR is
		// named <ModelServing name>-<hash> (the fixture name is short).
		if !revisions[m.GetName()+"-"+current] || !revisions[m.GetName()+"-"+update] {
			return false
		}
		return true
	}
	return false
}
func convertPod(o *unstructured.Unstructured, p *corev1.Pod) error {
	b, e := o.MarshalJSON()
	if e != nil {
		return e
	}
	return json.Unmarshal(b, p)
}
func (r *Result) enforceLatchedViolations() {
	if len(r.Violations) > 0 {
		r.Status = "FAIL"
		r.Error += "; process violations: " + strings.Join(r.Violations, "; ")
	}
}
func writeJSON(path string, v interface{}) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}

type junitFailure struct {
	Message string `xml:"message,attr"`
}
type junitCase struct {
	Name    string        `xml:"name,attr"`
	Time    float64       `xml:"time,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
}
type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Cases    []junitCase `xml:"testcase"`
}

func writeJUnit(path string, s Summary) error {
	suite := junitSuite{Name: s.RunID, Tests: len(s.Results)}
	for _, r := range s.Results {
		c := junitCase{Name: r.ID, Time: r.Duration}
		if r.Status != "PASS" {
			c.Failure = &junitFailure{Message: r.Status + ": " + r.Error}
			suite.Failures++
		}
		suite.Cases = append(suite.Cases, c)
	}
	b, e := xml.MarshalIndent(suite, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append([]byte(xml.Header), b...), 0644)
}
