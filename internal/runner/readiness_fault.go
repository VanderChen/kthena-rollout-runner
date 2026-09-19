// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

func actualContainerFault(pod *corev1.Pod, kind string) bool {
	if pod.DeletionTimestamp != nil || podReady(pod) {
		return false
	}
	status, ok := workloadStatus(pod)
	if !ok {
		return false
	}
	switch kind {
	case "running-not-ready":
		return pod.Status.Phase == corev1.PodRunning && status.State.Running != nil && !status.Ready
	case "image-pull-backoff":
		return status.State.Waiting != nil && status.State.Waiting.Reason == "ImagePullBackOff"
	}
	return false
}

func (e *normalExecution) restoreOrReplaceReadiness(ctx context.Context, prefix string) error {
	if e.readinessPod == nil {
		return fmt.Errorf("INCONCLUSIVE: exact readiness fault UID not recorded")
	}
	target := e.readinessPod
	current, err := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, target.Name, metav1.GetOptions{})
	mode := "controller-replaced-faulted-old"
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("INCONCLUSIVE: faulted old Pod GET: %w", err)
	}
	if err == nil && current.UID == target.UID && current.DeletionTimestamp == nil {
		if podReady(current) {
			return fmt.Errorf("INCONCLUSIVE: faulted old Pod recovered without declared release")
		}
		mode = "same-uid-readiness-restore"
		e.res.Releases++
		if err := e.r.release(ctx, Unit{Key: target.Name, Pods: []*corev1.Pod{target}}, e.dir, e.res.Releases); err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(e.dir, prefix+"-fault-recovery-choice.json"), map[string]interface{}{
		"at": time.Now().UTC(), "mode": mode, "faultedUID": target.UID,
		"currentUID": func() string {
			if current == nil {
				return ""
			}
			return string(current.UID)
		}(),
	})
}

func (e *normalExecution) dropReady(ctx context.Context, step ScenarioStep, prefix string) error {
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-readiness-before.yaml"), pods); err != nil {
		return err
	}
	var target *corev1.Pod
	healthyOld := 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !owned(pod, e.l.Owner) || pod.Labels[LabelRole] != "frontend" || !podIsEntry(pod) || pod.DeletionTimestamp != nil || !podReady(pod) {
			continue
		}
		if podVersion(pod) == "A" {
			healthyOld++
		}
		if step.ReadinessTarget != nil {
			t := step.ReadinessTarget
			if podVersion(pod) != t.Version || ordinal(pod.Labels[LabelGroup]) != *t.Group || ordinal(pod.Labels[LabelRoleID]) != *t.Ordinal {
				continue
			}
			if target != nil {
				return fmt.Errorf("TRIGGER_MISSED: old readiness target is ambiguous")
			}
			target = pod
			continue
		}
		if podVersion(pod) == "B" {
			if target != nil {
				return fmt.Errorf("TRIGGER_MISSED: more than one B was Ready before readiness withdrawal")
			}
			target = pod
		}
	}
	if target == nil || healthyOld == 0 {
		return fmt.Errorf("TRIGGER_MISSED: Ready fault target and healthy old A must coexist")
	}
	e.readinessPod = target.DeepCopy()
	command := []string{"rm", "-f", "/tmp/ready"}
	req := e.r.kube.CoreV1().RESTClient().Post().Resource("pods").Namespace(e.namespace).Name(target.Name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: "workload", Command: command, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(e.r.rest, "POST", req.URL())
	if err != nil {
		return err
	}
	var out, stderr bytes.Buffer
	sent := time.Now().UTC()
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = executor.StreamWithContext(commandCtx, remotecommand.StreamOptions{Stdout: &out, Stderr: &stderr})
	cancel()
	errorText := ""
	if err != nil {
		errorText = err.Error()
	}
	if saveErr := writeJSON(filepath.Join(e.dir, prefix+"-readiness-exec.json"), map[string]interface{}{"name": target.Name, "uid": target.UID, "sent": sent, "received": time.Now().UTC(), "command": command, "stdout": out.String(), "stderr": stderr.String(), "error": errorText}); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: readiness withdrawal exec: %w", err)
	}
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		current, err := e.r.kube.CoreV1().Pods(e.namespace).Get(deadline, target.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if current.UID != target.UID || current.DeletionTimestamp != nil {
			return fmt.Errorf("READINESS_FAULT_REPLACED_POD: %s", target.Name)
		}
		observed := false
		if err = e.locked(func() error {
			if object := e.o.objects["pods"][string(target.UID)]; object != nil {
				var pod corev1.Pod
				if convertPod(object, &pod) == nil {
					observed = actualContainerFault(&pod, "running-not-ready")
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if observed && actualContainerFault(current, "running-not-ready") {
			return saveYAML(filepath.Join(e.dir, prefix+"-readiness-withdrawn.yaml"), current)
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: actual same-UID readiness withdrawal not established")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
