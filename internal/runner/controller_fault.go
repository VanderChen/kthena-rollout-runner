// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (e *normalExecution) terminateController(ctx context.Context, prefix string) (result error) {
	before, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	dep, err := e.r.kube.AppsV1().Deployments(before.Namespace).Get(ctx, "kthena-controller-manager", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-terminated.yaml"), before); err != nil {
		return err
	}
	if len(e.pinned) > 0 {
		for name, uid := range e.pinned {
			pod, err := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if pod.UID != uid || pod.DeletionTimestamp == nil {
				return fmt.Errorf("TRIGGER_MISSED: pinned old instance not actually terminating")
			}
			if err = saveYAML(filepath.Join(e.dir, prefix+"-live-terminating-"+name+".yaml"), pod); err != nil {
				return err
			}
		}
	}
	zero := int64(0)
	options := metav1.DeleteOptions{GracePeriodSeconds: &zero, Preconditions: &metav1.Preconditions{UID: &before.UID}}
	sent := time.Now().UTC()
	err = e.r.kube.CoreV1().Pods(before.Namespace).Delete(ctx, before.Name, options)
	if saveErr := writeJSON(filepath.Join(e.dir, prefix+"-controller-delete.json"), map[string]interface{}{"name": before.Name, "uid": before.UID, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil}); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return err
	}
	// Exactly this declared termination may change the process identity. Keep
	// the new process as the expected identity for all subsequent observations.
	e.faultController = nil
	defer func() {
		if result != nil {
			e.faultController = before
		}
	}()
	deadline, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for {
		current, err := e.recoveryController(deadline)
		if err == nil && current.UID != before.UID {
			logs, logErr := e.r.kube.CoreV1().Pods(current.Namespace).GetLogs(current.Name, &corev1.PodLogOptions{Timestamps: true}).DoRaw(deadline)
			if logErr == nil && strings.Contains(string(logs), "initial sync has been done") {
				after, err := e.r.kube.AppsV1().Deployments(dep.Namespace).Get(deadline, dep.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if after.UID != dep.UID || !reflect.DeepEqual(after.Spec, dep.Spec) {
					return fmt.Errorf("CONTROLLER_STATE: Deployment spec changed during process replacement")
				}
				if err = os.WriteFile(filepath.Join(e.dir, prefix+"-new-controller.log"), logs, 0644); err != nil {
					return err
				}
				if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-replacement.yaml"), current); err != nil {
					return err
				}
				if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-deployment.yaml"), after); err != nil {
					return err
				}
				e.faultController = current
				return nil
			}
		}
		if err = e.locked(func() error { return nil }); err != nil {
			return err
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: replacement controller did not complete initial sync: %w", deadline.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
