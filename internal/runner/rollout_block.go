// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// recordBlockedOld freezes the exact healthy source UIDs at the fault stop.
// A later replacement with the same name cannot satisfy this guard.
func (e *normalExecution) recordBlockedOld(prefix string) error {
	var frozen map[string]string
	var starts []NormalStart
	var metrics []ScopeMetric
	if err := e.locked(func() error {
		frozen = make(map[string]string, len(e.l.BlockedOld))
		for uid, name := range e.l.BlockedOld {
			frozen[uid] = name
		}
		starts = append([]NormalStart(nil), e.l.Starts...)
		metrics = e.l.Metrics(e.o.objects["pods"])
		return nil
	}); err != nil {
		return err
	}
	if len(frozen) == 0 {
		return fmt.Errorf("TRIGGER_MISSED: no healthy old Pod UIDs at blocking stop")
	}
	return writeJSON(filepath.Join(e.dir, prefix+"-blocked-old.json"), map[string]interface{}{
		"observedAt": time.Now().UTC(), "uids": frozen, "startsBefore": starts, "metrics": metrics,
	})
}

func (e *normalExecution) verifyBlockedOld(ctx context.Context) error {
	var frozen map[string]string
	if err := e.locked(func() error {
		if len(e.l.BlockedOld) == 0 {
			return nil
		}
		frozen = make(map[string]string, len(e.l.BlockedOld))
		for uid, name := range e.l.BlockedOld {
			frozen[uid] = name
		}
		return nil
	}); err != nil {
		return err
	}
	for uid, name := range frozen {
		pod, err := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("ROLLOUT_BLOCK_VIOLATION: healthy old Pod disappeared during fault: %s (%s)", name, uid)
		}
		if err != nil {
			return fmt.Errorf("INCONCLUSIVE: direct blocked Pod GET %s: %w", name, err)
		}
		if string(pod.UID) != uid || pod.DeletionTimestamp != nil {
			return fmt.Errorf("ROLLOUT_BLOCK_VIOLATION: healthy old Pod replaced or terminating during fault: %s (%s)", name, uid)
		}
		if pod.Status.Phase != corev1.PodRunning || !podReady(pod) {
			return fmt.Errorf("INCONCLUSIVE: another healthy old Pod lost Ready during fault: %s (%s)", name, uid)
		}
	}
	return nil
}
