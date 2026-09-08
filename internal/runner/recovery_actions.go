// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"kthena.local/rollout-runner/internal/faultproxy"
)

type PodFaultAction struct {
	Group   int    `json:"group"`
	Role    string `json:"role"`
	Ordinal int    `json:"ordinal"`
	Member  string `json:"member"`
}

func (r *Runner) faultControl(ctx context.Context, method, endpoint string, value interface{}, out interface{}) error {
	token, err := os.ReadFile(r.opt.FaultProxyTokenFile)
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: fault control token unavailable")
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.opt.FaultProxyControl, "/")+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: fault control request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if method == "DELETE" && resp.StatusCode == http.StatusNotFound {
		return nil // Also clears an install whose acknowledgement was uncertain.
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("INCONCLUSIVE: fault control %s %s status=%d", method, endpoint, resp.StatusCode)
	}
	if out != nil {
		if err = json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("INCONCLUSIVE: invalid fault control response: %w", err)
		}
	}
	return nil
}

func (r *Runner) faultState(ctx context.Context) (faultproxy.State, error) {
	var state faultproxy.State
	err := r.faultControl(ctx, "GET", "/v1/state", nil, &state)
	if err == nil && len(state.Errors) > 0 {
		err = fmt.Errorf("INCONCLUSIVE: fault proxy evidence errors: %v", state.Errors)
	}
	return state, err
}

func (r *Runner) preflightRecovery(ctx context.Context) error {
	if r.opt.FaultProxyControl == "" || r.opt.FaultProxyTokenFile == "" || !strings.HasPrefix(r.opt.FaultProxyAPI, "https://") {
		return fmt.Errorf("recovery requires explicit fault proxy API, control URL and token file")
	}
	state, err := r.faultState(ctx)
	if err != nil {
		return err
	}
	for _, rule := range state.Rules {
		if rule.Active {
			return fmt.Errorf("fault proxy already has an active rule: %s", rule.ID)
		}
	}
	dep, err := r.kube.AppsV1().Deployments("kthena-system").Get(ctx, "kthena-controller-manager", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(dep.Spec.Template.Spec.Containers) != 1 {
		return fmt.Errorf("unexpected controller container layout")
	}
	c := dep.Spec.Template.Spec.Containers[0]
	kubeconfig := ""
	for _, arg := range c.Args {
		if strings.HasPrefix(arg, "--kubeconfig=") {
			kubeconfig = strings.TrimPrefix(arg, "--kubeconfig=")
		}
		if strings.HasPrefix(arg, "--master") {
			return fmt.Errorf("controller master override would invalidate proxy route proof")
		}
	}
	for _, mount := range c.VolumeMounts {
		if path.Dir(kubeconfig) != mount.MountPath {
			continue
		}
		for _, volume := range dep.Spec.Template.Spec.Volumes {
			if volume.Name != mount.Name || volume.ConfigMap == nil {
				continue
			}
			cm, err := r.kube.CoreV1().ConfigMaps(dep.Namespace).Get(ctx, volume.ConfigMap.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			config, err := clientcmd.Load([]byte(cm.Data[path.Base(kubeconfig)]))
			if err != nil {
				return err
			}
			current := config.Contexts[config.CurrentContext]
			if current == nil || config.Clusters[current.Cluster] == nil || config.AuthInfos[current.AuthInfo] == nil {
				return fmt.Errorf("invalid controller proxy kubeconfig")
			}
			cluster, auth := config.Clusters[current.Cluster], config.AuthInfos[current.AuthInfo]
			if cluster.Server != r.opt.FaultProxyAPI || cluster.InsecureSkipTLSVerify || len(cluster.CertificateAuthorityData) == 0 || auth.TokenFile != "/var/run/secrets/kubernetes.io/serviceaccount/token" {
				return fmt.Errorf("controller proxy route/TLS/tokenFile mismatch")
			}
			return writeJSON(filepath.Join(r.root, "fault-proxy-preflight.json"), map[string]interface{}{"controllerUID": dep.UID, "controllerGeneration": dep.Generation, "configMapUID": cm.UID, "api": cluster.Server, "tokenFile": auth.TokenFile, "initialState": state})
		}
	}
	return fmt.Errorf("controller does not use the declared fault proxy kubeconfig")
}

func (e *normalExecution) recoveryPause(ctx context.Context, prefix string) ([]string, error) {
	resources := []struct{ resource, subresource string }{{"modelservings", ""}, {"modelservings", "status"}, {"pods", ""}, {"pods", "status"}, {"controllerrevisions", ""}, {"services", ""}, {"configmaps", ""}, {"podgroups", ""}, {"podgroups", "status"}}
	var ids []string
	for i, resource := range resources {
		id := fmt.Sprintf("%s-%s-%02d-%d", e.r.opt.RunID, strings.ToLower(e.c.ID), e.phase, i)
		rule := faultproxy.Rule{ID: id, Namespace: e.namespace, Resource: resource.resource, Subresource: resource.subresource, Mode: "hold", Count: -1, DurationSeconds: 90}
		// Keep the identity before sending, including an uncertain response, so
		// cleanup can still clear a rule whose install acknowledgement was lost.
		ids = append(ids, id)
		var installed faultproxy.RuleStatus
		if err := e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
			return ids, err
		}
		if err := writeJSON(filepath.Join(e.dir, fmt.Sprintf("%s-fault-rule-%d.json", prefix, i)), installed); err != nil {
			return ids, err
		}
	}
	return ids, e.waitFaultState(ctx, prefix+"-barrier", ids, func(s faultproxy.State) bool { return s.InFlightAllowed == 0 })
}

func (e *normalExecution) waitFaultState(ctx context.Context, prefix string, ids []string, predicate func(faultproxy.State) bool) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		state, err := e.r.faultState(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			found := false
			for _, rule := range state.Rules {
				if rule.ID == id {
					found = true
					if !rule.Active {
						return fmt.Errorf("INCONCLUSIVE: pause rule ended before explicit resume: %s (%s)", id, rule.EndReason)
					}
				}
			}
			if !found {
				return fmt.Errorf("INCONCLUSIVE: installed pause rule missing: %s", id)
			}
		}
		if predicate(state) {
			return writeJSON(filepath.Join(e.dir, prefix+".json"), state)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("INCONCLUSIVE: pause state not established: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (e *normalExecution) resumeRecovery(ids []string, prefix string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var errors []string
	for _, id := range ids {
		if err := e.r.faultControl(ctx, "DELETE", "/v1/rules/"+id, nil, nil); err != nil {
			errors = append(errors, err.Error())
		}
	}
	state, err := e.r.faultState(ctx)
	if err != nil {
		errors = append(errors, err.Error())
	} else {
		for _, rule := range state.Rules {
			if rule.Active {
				errors = append(errors, "active rule remains: "+rule.ID)
			}
		}
		if err := writeJSON(filepath.Join(e.dir, prefix+"-resume.json"), state); err != nil {
			errors = append(errors, err.Error())
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("INCONCLUSIVE: recovery pause cleanup failed: %s", strings.Join(errors, "; "))
	}
	return nil
}

func (e *normalExecution) recoverPod(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	e.faultController = controller
	if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-before.yaml"), controller); err != nil {
		return err
	}
	ids, err := e.recoveryPause(ctx, prefix)
	resumed := false
	defer func() {
		if !resumed {
			if err := e.resumeRecovery(ids, prefix+"-cleanup"); err != nil {
				e.res.CleanupError = err.Error()
				result = fmt.Errorf("%v; %w", result, err)
			}
		}
	}()
	if err != nil {
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
	if err = e.locked(func() error { return e.l.Transition(mapValue(next.Object, "spec"), p.Name, p.Expect, e.o.objects) }); err != nil {
		return err
	}
	write, err := updateScenario(ctx, api, current, next, e.dir, prefix+"-submit-B", nil)
	if err != nil {
		return err
	}
	e.current = write.Object
	if err = saveYAML(filepath.Join(e.dir, prefix+"-B-server.yaml"), e.current.Object); err != nil {
		return err
	}
	serverModel, err := readModel(mapValue(e.current.Object, "spec"))
	if err != nil || !sameEffectiveModel(e.l.Model, serverModel) {
		return fmt.Errorf("DEFAULTING: recovery target differs from accepted model: %v", err)
	}
	if err = e.waitFaultState(ctx, prefix+"-B-held", ids, func(s faultproxy.State) bool {
		for _, rule := range s.Rules {
			if rule.ID == ids[0] && rule.Hits > 0 {
				return true
			}
		}
		return false
	}); err != nil {
		return err
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-fault-pods-before.yaml"), pods); err != nil {
		return err
	}
	var target *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if owned(pod, e.l.Owner) && ordinal(pod.Labels[LabelGroup]) == p.PodFault.Group && pod.Labels[LabelRole] == p.PodFault.Role && ordinal(pod.Labels[LabelRoleID]) == p.PodFault.Ordinal && podIsEntry(pod) == (p.PodFault.Member == "entry") {
			if target != nil {
				return fmt.Errorf("INCONCLUSIVE: ambiguous fault target")
			}
			target = pod
		}
	}
	if target == nil {
		return fmt.Errorf("INCONCLUSIVE: exact fault target missing")
	}
	scope, err := capturePodFaultScope(e.l.Owner, string(target.UID), textValue(mapValue(e.current.Object, "spec"), "recoveryPolicy"), pods.Items)
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: capture fault scope: %w", err)
	}
	if err = e.locked(func() error { return e.l.armRecovery(scope, e.o.objects) }); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-fault-scope.json"), scope); err != nil {
		return err
	}
	options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &target.UID}}
	sent := time.Now().UTC()
	err = e.r.kube.CoreV1().Pods(e.namespace).Delete(ctx, target.Name, options)
	received := time.Now().UTC()
	if saveErr := writeJSON(filepath.Join(e.dir, prefix+"-fault-delete.json"), map[string]interface{}{"target": target.Name, "uid": target.UID, "options": options, "sent": sent, "received": received, "accepted": err == nil}); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return err
	}
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		_, err := e.r.kube.CoreV1().Pods(e.namespace).Get(deadline, target.Name, metav1.GetOptions{})
		observed := false
		if lockErr := e.locked(func() error { observed = e.l.Recoveries[len(e.l.Recoveries)-1].Deleted[string(target.UID)]; return nil }); lockErr != nil {
			return lockErr
		}
		if apierrors.IsNotFound(err) && observed {
			break
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: external deletion not fully observed")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err = e.snapshot(prefix + "-before-resume"); err != nil {
		return err
	}
	if err = e.waitFaultState(ctx, prefix+"-before-clear", ids, func(faultproxy.State) bool { return true }); err != nil {
		return err
	}
	if err = e.resumeRecovery(ids, prefix); err != nil {
		return err
	}
	resumed = true
	return nil
}

func (e *normalExecution) recoveryController(ctx context.Context) (*corev1.Pod, error) {
	pods, err := e.r.kube.CoreV1().Pods("kthena-system").List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/component=kthena-controller-manager,app.kubernetes.io/instance=kthena,app.kubernetes.io/name=workload"})
	if err != nil {
		return nil, err
	}
	var current *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		if current != nil || len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != e.r.opt.ControllerImage || len(pod.Status.ContainerStatuses) != 1 || !pod.Status.ContainerStatuses[0].Ready {
			return nil, fmt.Errorf("CONTROLLER_STATE: expected one Ready production controller")
		}
		current = pod
	}
	if current == nil {
		return nil, fmt.Errorf("CONTROLLER_STATE: production controller absent")
	}
	return current, nil
}

func (e *normalExecution) finalizeRecoveryEvidence() {
	if e.c.Format != "rollout-runner/v3" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	state, err := e.r.faultState(ctx)
	if saveErr := writeJSON(filepath.Join(e.dir, "fault-proxy-final.json"), state); saveErr != nil {
		err = saveErr
	}
	for _, rule := range state.Rules {
		if rule.Active {
			err = fmt.Errorf("active fault remains at case end: %s", rule.ID)
			e.res.CleanupError = err.Error()
		}
	}
	if err != nil {
		e.res.Status = "INCONCLUSIVE"
		e.res.Error += "; fault facility: " + err.Error()
		return
	}
	if e.faultController == nil {
		return
	}
	controller, err := e.recoveryController(ctx)
	if err == nil {
		err = saveYAML(filepath.Join(e.dir, "fault-controller-after.yaml"), controller)
	}
	if err == nil && (controller.UID != e.faultController.UID || controller.Status.ContainerStatuses[0].RestartCount != e.faultController.Status.ContainerStatuses[0].RestartCount) {
		err = fmt.Errorf("controller UID/restartCount changed during Pod fault")
	}
	if err != nil {
		e.res.Status = "FAIL"
		e.res.Error += "; CONTROLLER_STATE: " + err.Error()
	}
}
