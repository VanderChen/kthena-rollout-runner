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

	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const controllerLeaseName = "lease.kthena.controller-manager"

func selectElectedController(pods *corev1.PodList, lease *coordinationv1.Lease, image string) (*corev1.Pod, *corev1.Pod, error) {
	if lease == nil || lease.UID == "" || lease.Spec.HolderIdentity == nil || lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil || *lease.Spec.LeaseDurationSeconds != 15 {
		return nil, nil, fmt.Errorf("CONTROLLER_STATE: actual default-duration leader Lease missing")
	}
	holder, identity, ok := strings.Cut(*lease.Spec.HolderIdentity, "_")
	if !ok || identity == "" {
		return nil, nil, fmt.Errorf("CONTROLLER_STATE: malformed leader holder identity")
	}
	var leader, standby *corev1.Pod
	count := 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		count++
		if !podReady(pod) || len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != image || len(pod.Status.ContainerStatuses) != 1 || !pod.Status.ContainerStatuses[0].Ready {
			return nil, nil, fmt.Errorf("CONTROLLER_STATE: two Ready production controllers required")
		}
		flags := 0
		for _, arg := range pod.Spec.Containers[0].Args {
			if strings.HasPrefix(arg, "--leader-elect") {
				if arg != "--leader-elect=true" {
					return nil, nil, fmt.Errorf("CONTROLLER_STATE: leader election must be explicitly enabled")
				}
				flags++
			}
		}
		if flags != 1 {
			return nil, nil, fmt.Errorf("CONTROLLER_STATE: unambiguous leader election flag required")
		}
		if pod.Name == holder {
			leader = pod
		} else {
			standby = pod
		}
	}
	if count != 2 || leader == nil || standby == nil || leader.UID == standby.UID {
		return nil, nil, fmt.Errorf("CONTROLLER_STATE: Lease holder must identify one of two distinct live Pods")
	}
	return leader.DeepCopy(), standby.DeepCopy(), nil
}

func (e *normalExecution) leaderSnapshot(ctx context.Context) (*corev1.Pod, *corev1.Pod, *coordinationv1.Lease, *appsv1.Deployment, error) {
	dep, err := e.r.kube.AppsV1().Deployments("kthena-system").Get(ctx, "kthena-controller-manager", metav1.GetOptions{})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 2 {
		return nil, nil, nil, nil, fmt.Errorf("CONTROLLER_STATE: source requires two controller replicas")
	}
	pods, err := e.r.kube.CoreV1().Pods(dep.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/component=kthena-controller-manager,app.kubernetes.io/instance=kthena,app.kubernetes.io/name=workload"})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	lease, err := e.r.kube.CoordinationV1().Leases(dep.Namespace).Get(ctx, controllerLeaseName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	leader, standby, err := selectElectedController(pods, lease, e.r.opt.ControllerImage)
	if err == nil && time.Since(lease.Spec.RenewTime.Time) > 20*time.Second {
		err = fmt.Errorf("CONTROLLER_STATE: observed leader Lease is stale")
	}
	return leader, standby, lease, dep, err
}

func (e *normalExecution) electedController(ctx context.Context) (*corev1.Pod, error) {
	leader, _, _, _, err := e.leaderSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if before := e.faultController; before != nil && (before.UID != leader.UID || before.Status.ContainerStatuses[0].RestartCount != leader.Status.ContainerStatuses[0].RestartCount) {
		return nil, fmt.Errorf("CONTROLLER_STATE: unrequested leader identity change")
	}
	return leader, nil
}

func (e *normalExecution) terminateLeader(ctx context.Context, prefix string) (result error) {
	leader, standby, lease, dep, err := e.leaderSnapshot(ctx)
	if err != nil {
		return err
	}
	if e.faultController == nil || e.faultController.UID != leader.UID {
		return fmt.Errorf("CONTROLLER_STATE: leader changed before declared termination")
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	oldReady, newReady := 0, 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		if owned(pod, e.l.Owner) && pod.Labels[LabelRole] == "frontend" && podReady(pod) {
			if podVersion(pod) == "A" {
				oldReady++
			}
			if podVersion(pod) == "B" {
				newReady++
			}
		}
	}
	if oldReady < 1 || newReady < 1 {
		return fmt.Errorf("TRIGGER_MISSED: elected leader termination requires actual healthy A/B mixture")
	}
	for suffix, object := range map[string]interface{}{"-leader-before.yaml": leader, "-standby-before.yaml": standby, "-lease-before.yaml": lease, "-deployment-before.yaml": dep, "-workload-pods-before.yaml": pods} {
		if err = saveYAML(filepath.Join(e.dir, prefix+suffix), object); err != nil {
			return err
		}
	}
	currentLease, err := e.r.kube.CoordinationV1().Leases(dep.Namespace).Get(ctx, controllerLeaseName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if currentLease.UID != lease.UID || currentLease.Spec.HolderIdentity == nil || *currentLease.Spec.HolderIdentity != *lease.Spec.HolderIdentity {
		return fmt.Errorf("TRIGGER_MISSED: Lease holder changed before termination request")
	}
	zero := int64(0)
	options := metav1.DeleteOptions{GracePeriodSeconds: &zero, Preconditions: &metav1.Preconditions{UID: &leader.UID}}
	sent := time.Now().UTC()
	err = e.r.kube.CoreV1().Pods(leader.Namespace).Delete(ctx, leader.Name, options)
	if saveErr := writeJSON(filepath.Join(e.dir, prefix+"-leader-delete.json"), map[string]interface{}{"name": leader.Name, "uid": leader.UID, "holderIdentity": *currentLease.Spec.HolderIdentity, "leaseUID": currentLease.UID, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil}); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return err
	}
	e.faultController = nil
	defer func() {
		if result != nil {
			e.faultController = leader
		}
	}()
	deadline, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for {
		current, replacement, newLease, newDep, pollErr := e.leaderSnapshot(deadline)
		if pollErr == nil && current.UID != leader.UID {
			if current.UID != standby.UID || current.Status.ContainerStatuses[0].RestartCount != standby.Status.ContainerStatuses[0].RestartCount {
				return fmt.Errorf("INCONCLUSIVE: the preexisting standby did not take over on the same process")
			}
			if newLease.UID != lease.UID || newLease.Spec.LeaseTransitions == nil || lease.Spec.LeaseTransitions == nil || *newLease.Spec.LeaseTransitions != *lease.Spec.LeaseTransitions+1 {
				return fmt.Errorf("CONTROLLER_STATE: exactly one real Lease transition not proved")
			}
			if newDep.UID != dep.UID || !reflect.DeepEqual(newDep.Spec, dep.Spec) {
				return fmt.Errorf("CONTROLLER_STATE: Deployment changed during leader failover")
			}
			logs, logErr := e.r.kube.CoreV1().Pods(current.Namespace).GetLogs(current.Name, &corev1.PodLogOptions{Timestamps: true}).DoRaw(deadline)
			if logErr == nil {
				synced := false
				for _, line := range strings.Split(string(logs), "\n") {
					stamp, _, _ := strings.Cut(line, " ")
					at, parseErr := time.Parse(time.RFC3339Nano, stamp)
					if parseErr == nil && at.After(sent) && strings.Contains(line, "initial sync has been done") {
						synced = true
					}
				}
				if synced {
					if err = os.WriteFile(filepath.Join(e.dir, prefix+"-new-leader.log"), logs, 0644); err != nil {
						return err
					}
					for suffix, object := range map[string]interface{}{"-leader-after.yaml": current, "-new-standby.yaml": replacement, "-lease-after.yaml": newLease, "-deployment-after.yaml": newDep} {
						if err = saveYAML(filepath.Join(e.dir, prefix+suffix), object); err != nil {
							return err
						}
					}
					e.faultController = current
					return nil
				}
			}
		}
		if err = e.locked(func() error { return nil }); err != nil {
			return err
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: preexisting standby did not become synced leader: %w", deadline.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
