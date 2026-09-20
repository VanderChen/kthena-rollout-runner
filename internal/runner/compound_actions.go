// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (e *normalExecution) setCompoundDeletionCosts(ctx context.Context, costs map[string]int, prefix string) error {
	list, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	seen := map[string]int{}
	var receipts []map[string]interface{}
	for _, item := range list.Items {
		group := item.Labels[LabelGroup]
		value, ok := costs[strconv.Itoa(ordinal(group))]
		if !ok || !owned(&item, e.l.Owner) {
			continue
		}
		pod, err := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, item.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if pod.UID != item.UID || pod.DeletionTimestamp != nil {
			return fmt.Errorf("INCONCLUSIVE: deletion-cost target changed: %s", item.Name)
		}
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		pod.Annotations[corev1.PodDeletionCost] = strconv.Itoa(value)
		updated, err := e.r.kube.CoreV1().Pods(e.namespace).Update(ctx, pod, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		seen[strconv.Itoa(ordinal(group))]++
		receipts = append(receipts, map[string]interface{}{"group": group, "pod": updated.Name, "uid": updated.UID, "cost": value, "resourceVersion": updated.ResourceVersion})
	}
	for group := range costs {
		if seen[group] == 0 {
			return fmt.Errorf("INCONCLUSIVE: no owned deletion-cost target for group %s", group)
		}
	}
	return writeJSON(filepath.Join(e.dir, prefix+"-deletion-costs.json"), receipts)
}
