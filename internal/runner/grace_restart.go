// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func graceRestartMembers(target *corev1.Pod, pods *corev1.PodList, owner string) ([]*corev1.Pod, error) {
	if podVersion(target) != "A" || !podReady(target) {
		return nil, fmt.Errorf("TRIGGER_MISSED: grace target must be an old Ready A entry")
	}
	var workers []*corev1.Pod
	newBlocked := false
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !owned(pod, owner) {
			continue
		}
		if podVersion(pod) == "B" && actualContainerFault(pod, "running-not-ready") {
			newBlocked = true
		}
		if pod.Labels[LabelGroup] == target.Labels[LabelGroup] && pod.Labels[LabelRole] == target.Labels[LabelRole] && pod.Labels[LabelRoleID] == target.Labels[LabelRoleID] && !podIsEntry(pod) {
			if !podReady(pod) || podVersion(pod) != "A" {
				return nil, fmt.Errorf("TRIGGER_MISSED: old worker must remain healthy")
			}
			workers = append(workers, pod.DeepCopy())
		}
	}
	if !newBlocked || len(workers) != 1 {
		return nil, fmt.Errorf("TRIGGER_MISSED: ongoing B rollout and one healthy old worker required")
	}
	return workers, nil
}

func (e *normalExecution) armGraceRestart(ctx context.Context, target *corev1.Pod, pods *corev1.PodList, prefix string) ([]*corev1.Pod, error) {
	ms, err := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace).Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	spec := mapValue(ms.Object, "spec")
	if intValue(mapValue(spec, "template"), "restartGracePeriodSeconds", -1) != 30 || textValue(spec, "recoveryPolicy") != "RoleRecreate" {
		return nil, fmt.Errorf("INCONCLUSIVE: actual admitted spec must have template restart grace=30 and RoleRecreate")
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-grace-server.yaml"), ms.Object); err != nil {
		return nil, err
	}
	workers, err := graceRestartMembers(target, pods, e.l.Owner)
	if err != nil {
		return nil, err
	}
	// Only the actual old entry and its healthy worker are preserved during
	// this bounded restart. Other instances continue ordinary rollout checks.
	// The next explicit phase transition retires this temporary protection.
	err = e.locked(func() error {
		for _, pod := range append([]*corev1.Pod{target}, workers...) {
			uid := string(pod.UID)
			if e.l.Committed[uid] || e.l.PGPods[uid] || e.l.RoleScaleIntents[uid] != "" {
				return fmt.Errorf("TRIGGER_MISSED: grace target already committed for deletion: %s", pod.Name)
			}
			e.l.Protected[uid] = pod.Name
		}
		return nil
	})
	return workers, err
}

func (e *normalExecution) graceWorkersHealthy(ctx context.Context, workers []*corev1.Pod) error {
	for _, before := range workers {
		current, err := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, before.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("GRACE_WORKER_LOST: %s: %w", before.Name, err)
		}
		if current.UID != before.UID || !podReady(current) {
			return fmt.Errorf("GRACE_WORKER_CHANGED: %s", before.Name)
		}
	}
	return nil
}
