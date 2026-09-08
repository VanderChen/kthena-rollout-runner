// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

func workloadStatus(pod *corev1.Pod) (corev1.ContainerStatus, bool) {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "workload" {
			return status, true
		}
	}
	return corev1.ContainerStatus{}, false
}

// A successful exec acknowledgement alone does not establish a restart. Require
// the same Pod, a new container identity, and the fixture's actual process exit.
func containerRestartObserved(before, current *corev1.Pod, sent time.Time) (bool, error) {
	if current.UID != before.UID || current.DeletionTimestamp != nil {
		return false, fmt.Errorf("CONTAINER_RESTART_RECREATED_POD: %s", before.Name)
	}
	old, oldOK := workloadStatus(before)
	now, nowOK := workloadStatus(current)
	if !oldOK || !nowOK || now.RestartCount <= old.RestartCount || now.State.Running == nil {
		return false, nil
	}
	last := now.LastTerminationState.Terminated
	if last == nil || last.ExitCode != 42 || last.FinishedAt.Time.Before(sent.Truncate(time.Second)) || now.ContainerID == "" || now.ContainerID == old.ContainerID {
		return false, nil
	}
	return true, nil
}

func (e *normalExecution) restartContainer(ctx context.Context, p ScenarioStep, prefix string) error {
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	e.faultController = controller
	if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-before.yaml"), controller); err != nil {
		return err
	}
	if err = e.locked(func() error { return e.l.Transition(e.l.Model.Spec, p.Name, p.Expect, e.o.objects) }); err != nil {
		return err
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-restart-pods-before.yaml"), pods); err != nil {
		return err
	}
	var target *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		f := p.ContainerRestart
		if owned(pod, e.l.Owner) && ordinal(pod.Labels[LabelGroup]) == f.Group && pod.Labels[LabelRole] == f.Role && ordinal(pod.Labels[LabelRoleID]) == f.Ordinal && podIsEntry(pod) {
			if target != nil {
				return fmt.Errorf("INCONCLUSIVE: ambiguous container restart target")
			}
			target = pod
		}
	}
	if target == nil || !podReady(target) || target.DeletionTimestamp != nil || target.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		return fmt.Errorf("INCONCLUSIVE: restart target must be Ready with restartPolicy Always")
	}
	before, ok := workloadStatus(target)
	if !ok || before.State.Running == nil || before.ContainerID == "" {
		return fmt.Errorf("INCONCLUSIVE: restart target container not running")
	}
	grace := p.Action == "restart-container-grace"
	var workers []*corev1.Pod
	if grace {
		workers, err = e.armGraceRestart(ctx, target, pods, prefix)
		if err != nil {
			return err
		}
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-restart-target-before.yaml"), target); err != nil {
		return err
	}
	// Linux namespace PID 1 ignores signals without a handler. This fixture
	// installs USR1 -> exit 42, so the real PID 1 exits and kubelet restarts it.
	command := []string{"sh", "-c", "kill -USR1 1"}
	req := e.r.kube.CoreV1().RESTClient().Post().Resource("pods").Namespace(e.namespace).Name(target.Name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: "workload", Command: command, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(e.r.rest, "POST", req.URL())
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	started := time.Now()
	sent := started.UTC()
	window := 90 * time.Second
	if grace {
		window = 30 * time.Second
	}
	deadline, deadlineCancel := context.WithDeadline(ctx, started.Add(window))
	defer deadlineCancel()
	commandCtx, cancel := context.WithTimeout(deadline, 15*time.Second)
	execErr := executor.StreamWithContext(commandCtx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr})
	cancel()
	errorText := ""
	if execErr != nil {
		errorText = execErr.Error() // The process exit may close its exec stream.
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-restart-exec.json"), map[string]interface{}{"target": target.Name, "uid": target.UID, "containerID": before.ContainerID, "command": command, "sent": sent, "received": time.Now().UTC(), "stdout": stdout.String(), "stderr": stderr.String(), "error": errorText}); err != nil {
		return err
	}
	journal, err := os.OpenFile(filepath.Join(e.dir, prefix+"-restart-observations.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer journal.Close()
	enc := json.NewEncoder(journal)
	released := false
	for {
		current, err := e.r.kube.CoreV1().Pods(e.namespace).Get(deadline, target.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("CONTAINER_RESTART_RECREATED_POD: original Pod disappeared")
		}
		if err != nil {
			return err
		}
		if err = enc.Encode(map[string]interface{}{"received": time.Now().UTC(), "pod": current}); err != nil {
			return err
		}
		if err = e.locked(func() error { return nil }); err != nil {
			return err
		}
		if grace {
			if err = e.graceWorkersHealthy(deadline, workers); err != nil {
				return err
			}
		}
		observed, err := containerRestartObserved(target, current, sent)
		if err != nil {
			return err
		}
		if observed && !released {
			if err = journal.Sync(); err != nil {
				return err
			}
			if err = saveYAML(filepath.Join(e.dir, prefix+"-restart-target-after.yaml"), current); err != nil {
				return err
			}
			// The restarted container has a fresh writable layer; release only
			// this unchanged Pod after proving its new running container.
			e.res.Releases++
			if err = e.r.release(deadline, Unit{Key: current.Name, Pods: []*corev1.Pod{current}}, e.dir, e.res.Releases); err != nil {
				return err
			}
			if !grace {
				return nil
			}
			released = true
		}
		if grace && released && observed && podReady(current) {
			watchReady := false
			if err = e.locked(func() error {
				if obj := e.o.objects["pods"][string(target.UID)]; obj != nil {
					var pod corev1.Pod
					if convertPod(obj, &pod) == nil {
						witness, witnessErr := containerRestartObserved(target, &pod, sent)
						watchReady = witness && witnessErr == nil && podReady(&pod)
					}
				}
				return nil
			}); err != nil {
				return err
			}
			if watchReady {
				elapsed := time.Since(started)
				if elapsed > window {
					return fmt.Errorf("GRACE_RECOVERY_TIMEOUT: Ready not established within 30 seconds")
				}
				if err = journal.Sync(); err != nil {
					return err
				}
				if err = saveYAML(filepath.Join(e.dir, prefix+"-grace-ready.yaml"), current); err != nil {
					return err
				}
				return writeJSON(filepath.Join(e.dir, prefix+"-grace-recovery.json"), map[string]interface{}{"sent": sent, "readyReceived": time.Now().UTC(), "elapsedNanos": elapsed.Nanoseconds(), "graceSeconds": 30, "entryUID": target.UID, "healthyWorkers": workers, "apiAndWatchReady": true})
			}
		}
		select {
		case <-deadline.Done():
			if grace {
				return fmt.Errorf("GRACE_RECOVERY_TIMEOUT: same-UID restart and Ready within 30 seconds not established: %w", deadline.Err())
			}
			return fmt.Errorf("INCONCLUSIVE: actual fixture process exit and same-UID container restart not established: %w", deadline.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
