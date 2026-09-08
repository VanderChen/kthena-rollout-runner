// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Derive expected CPU/memory from the declared historical template and current
// minimum Role count. Fixed 5m/4Mi assumptions cannot verify a resource fault.
func gangFixtureResources(model NormalModel, minimums map[string]RoleLayout) (map[string]resource.Quantity, error) {
	total := map[string]resource.Quantity{"cpu": resource.MustParse("0"), "memory": resource.MustParse("0")}
	for _, raw := range listValue(mapValue(model.Spec, "template"), "roles") {
		role := raw.(map[string]interface{})
		layout := minimums[textValue(role, "name")]
		for field, members := range map[string]int{"entryTemplate": layout.R, "workerTemplate": layout.R * layout.W} {
			if members == 0 {
				continue
			}
			spec := mapValue(mapValue(role, field), "spec")
			if len(listValue(spec, "initContainers")) != 0 || len(mapValue(spec, "overhead")) != 0 {
				return nil, fmt.Errorf("INCONCLUSIVE: fixture gang resources require explicit init/overhead support")
			}
			for _, item := range listValue(spec, "containers") {
				requests := mapValue(mapValue(item.(map[string]interface{}), "resources"), "requests")
				for name, sum := range total {
					value := textValue(requests, name)
					if value == "" {
						continue
					}
					q, err := resource.ParseQuantity(value)
					if err != nil {
						return nil, err
					}
					q.Mul(int64(members))
					sum.Add(q)
					total[name] = sum
				}
			}
		}
	}
	return total, nil
}

func scheduledMilliCPU(pod *corev1.Pod) int64 {
	regular, sidecars, peak := int64(0), int64(0), int64(0)
	for _, container := range pod.Spec.Containers {
		regular += container.Resources.Requests.Cpu().MilliValue()
	}
	for _, container := range pod.Spec.InitContainers {
		request := container.Resources.Requests.Cpu().MilliValue()
		if container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			sidecars += request
			peak = max(peak, sidecars)
		} else {
			peak = max(peak, sidecars+request)
		}
	}
	return max(regular+sidecars, peak) + pod.Spec.Overhead.Cpu().MilliValue()
}

func (e *normalExecution) verifyResourceStop(ctx context.Context, prefix string) error {
	if e.capacityPod == nil {
		return fmt.Errorf("INCONCLUSIVE: resource holder not established")
	}
	nodes, err := e.r.kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(nodes.Items) != 1 {
		return fmt.Errorf("INCONCLUSIVE: resource proof requires single-node Kind")
	}
	node := nodes.Items[0]
	pods, err := e.r.kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	used := int64(0)
	var candidates []*corev1.Pod
	var allocations []map[string]interface{}
	holderPresent := false
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Spec.Resources != nil {
			return fmt.Errorf("INCONCLUSIVE: resource proof does not yet support Pod-level resource requests")
		}
		if owned(pod, e.l.Owner) && pod.Namespace == e.namespace && podVersion(pod) == "B" && pod.Spec.NodeName == "" && pod.Status.Phase == corev1.PodPending && pod.DeletionTimestamp == nil {
			candidates = append(candidates, pod)
		}
		if pod.Spec.NodeName != node.Name || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		request := scheduledMilliCPU(pod)
		used += request
		allocations = append(allocations, map[string]interface{}{"namespace": pod.Namespace, "name": pod.Name, "uid": pod.UID, "milliCPU": request})
		if pod.UID == e.capacityPod.UID && pod.Namespace == e.namespace && pod.Name == e.capacityPod.Name && pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning {
			holderPresent = true
		}
	}
	free := node.Status.Allocatable.Cpu().MilliValue() - used
	var blocked []*corev1.Pod
	for _, pod := range candidates {
		if scheduledMilliCPU(pod) > free {
			blocked = append(blocked, pod)
		}
	}
	proof := map[string]interface{}{"received": time.Now().UTC(), "nodeUID": node.UID, "allocatableMilliCPU": node.Status.Allocatable.Cpu().MilliValue(), "usedMilliCPU": used, "freeMilliCPU": free, "holderUID": e.capacityPod.UID, "holderRunning": holderPresent, "allocations": allocations, "blockedPods": blocked}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-resource-stop.json"), proof); err != nil {
		return err
	}
	if !holderPresent || len(blocked) == 0 {
		return fmt.Errorf("INCONCLUSIVE: real Pending Pod with insufficient node CPU not established")
	}
	return nil
}
