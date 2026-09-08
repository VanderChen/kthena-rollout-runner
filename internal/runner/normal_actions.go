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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func (e *normalExecution) specialAction(ctx context.Context, p ScenarioStep, prefix string) error {
	switch p.Action {
	case "verify-resource-stop":
		return e.verifyResourceStop(ctx, prefix)
	case "drop-ready":
		return e.dropReady(ctx, prefix)
	case "restore-ready":
		if e.readinessPod == nil {
			return fmt.Errorf("INCONCLUSIVE: exact readiness fault UID not recorded")
		}
		e.res.Releases++
		return e.r.release(ctx, Unit{Key: e.readinessPod.Name, Pods: []*corev1.Pod{e.readinessPod}}, e.dir, e.res.Releases)
	case "block-resources":
		return e.blockResources(ctx, prefix)
	case "restore-resources":
		if e.capacityPod == nil {
			return fmt.Errorf("no capacity holder")
		}
		uid := e.capacityPod.UID
		return e.r.kube.CoreV1().Pods(e.namespace).Delete(ctx, e.capacityPod.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})

	case "configmap":
		if e.templateCM == nil {
			return fmt.Errorf("isolated template ConfigMap missing")
		}
		cm, err := e.r.kube.CoreV1().ConfigMaps("kthena-system").Get(ctx, e.templateCM.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if cm.UID != e.templateCM.UID {
			return fmt.Errorf("template UID changed")
		}
		if err = saveYAML(filepath.Join(e.dir, prefix+"-configmap-before.yaml"), cm); err != nil {
			return err
		}
		cm.Data["ranktable-template"] = `{"version":"2.0","server_count":"{{ .ServerCount }}","status":"{{ .Status }}"}`
		cm, err = e.r.kube.CoreV1().ConfigMaps("kthena-system").Update(ctx, cm, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		e.tableVersion = "2.0"
		return saveYAML(filepath.Join(e.dir, prefix+"-configmap-after.yaml"), cm)

	case "restart-controller":
		dep, err := e.r.kube.AppsV1().Deployments("kthena-system").Get(ctx, "kthena-controller-manager", metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(dep.Spec.Template.Spec.Containers) != 1 || dep.Spec.Template.Spec.Containers[0].Image != e.r.opt.ControllerImage {
			return fmt.Errorf("controller changed before restart")
		}
		if err = saveYAML(filepath.Join(e.dir, prefix+"-controller-before.yaml"), dep); err != nil {
			return err
		}
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = map[string]string{}
		}
		dep.Spec.Template.Annotations["rollout-runner/restartedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		dep, err = e.r.kube.AppsV1().Deployments(dep.Namespace).Update(ctx, dep, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		generation := dep.Generation
		timeout, cancel := context.WithTimeout(ctx, e.r.opt.Timeout)
		defer cancel()
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			dep, err = e.r.kube.AppsV1().Deployments(dep.Namespace).Get(timeout, dep.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if dep.Status.ObservedGeneration >= generation && dep.Status.UpdatedReplicas == *dep.Spec.Replicas && dep.Status.AvailableReplicas == *dep.Spec.Replicas && dep.Status.Replicas == *dep.Spec.Replicas {
				break
			}
			if err = e.locked(func() error { return nil }); err != nil {
				return err
			}
			select {
			case <-timeout.Done():
				return timeout.Err()
			case <-tick.C:
			}
		}
		return saveYAML(filepath.Join(e.dir, prefix+"-controller-after.yaml"), dep)
	case "restore-status":
		api := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace)
		ms, err := api.Get(ctx, "model", metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(e.baselineStatus) == 0 {
			return fmt.Errorf("baseline status not captured")
		}
		ms.Object["status"] = cloneMap(e.baselineStatus)
		if err = saveYAML(filepath.Join(e.dir, prefix+"-status-request.yaml"), ms.Object); err != nil {
			return err
		}
		ms, err = api.UpdateStatus(ctx, ms, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		e.current = ms
		return saveYAML(filepath.Join(e.dir, prefix+"-status-server.yaml"), ms.Object)
	case "pin":
		var selected []struct {
			Name string
			UID  types.UID
		}
		if err := e.locked(func() error {
			for _, u := range e.l.units(e.o.objects["pods"]) {
				for _, c := range p.Conditions {
					if conditionMatches(c, u) {
						for _, pod := range u.Pods {
							selected = append(selected, struct {
								Name string
								UID  types.UID
							}{pod.Name, pod.UID})
						}
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if len(selected) == 0 {
			return fmt.Errorf("finalizer target was not observed")
		}
		for _, s := range selected {
			pod, err := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, s.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if pod.UID != s.UID || pod.DeletionTimestamp != nil {
				return fmt.Errorf("finalizer UID changed")
			}
			pod.Finalizers = append(pod.Finalizers, "rollout-runner/hold")
			pod, err = e.r.kube.CoreV1().Pods(e.namespace).Update(ctx, pod, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			e.pinned[pod.Name] = pod.UID
			if err = saveYAML(filepath.Join(e.dir, prefix+"-pinned-"+pod.Name+".yaml"), pod); err != nil {
				return err
			}
		}
		return nil
	case "unpin":
		for name, uid := range e.pinned {
			pod, err := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if pod.UID != uid {
				return fmt.Errorf("finalizer target UID changed: %s", name)
			}
			pod.Finalizers = removeString(pod.Finalizers, "rollout-runner/hold")
			if _, err = e.r.kube.CoreV1().Pods(e.namespace).Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
				return err
			}
			delete(e.pinned, name)
		}
		return nil
	default:
		return fmt.Errorf("unsupported special action %s", p.Action)
	}
}

// A queued Watch event alone is insufficient evidence of real overlap. Both
// live lists surround the accepted replicas request and must still contain an
// eligible old member of the original rollout, using its original partition.
func (e *normalExecution) liveOverlap(ctx context.Context, original NormalModel, name string) error {
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE overlap live list: %w", err)
	}
	if err = saveYAML(filepath.Join(e.dir, name+".yaml"), pods); err != nil {
		return err
	}
	unfinished := false
	for _, p := range pods.Items {
		r, ok := original.Roles[p.Labels[LabelRole]]
		if !ok {
			continue
		}
		partition := r.P
		ordinalValue := ordinal(p.Labels[LabelRoleID])
		if original.Mode == "SG" {
			partition = original.P
			ordinalValue = ordinal(p.Labels[LabelGroup])
		}
		target := r.Entry
		if !podIsEntry(&p) {
			target = r.Worker
		}
		if owned(&p, e.l.Owner) && ordinalValue >= partition && podVersion(&p) != target {
			unfinished = true
		}
	}
	if !unfinished {
		return fmt.Errorf("TRIGGER_MISSED: no eligible old member in %s", name)
	}
	return writeJSON(filepath.Join(e.dir, name+"-proof.json"), map[string]interface{}{"at": time.Now().UTC(), "resourceVersion": pods.ResourceVersion, "eligibleOldMemberPresent": true})
}

func (e *normalExecution) liveTerminating(ctx context.Context, name string, previous map[string]bool, fence bool) (map[string]bool, error) {
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("INCONCLUSIVE terminating live list: %w", err)
	}
	if err = saveYAML(filepath.Join(e.dir, name+".yaml"), pods); err != nil {
		return nil, err
	}
	matched := map[string]bool{}
	for _, p := range pods.Items {
		if !owned(&p, e.l.Owner) || p.DeletionTimestamp == nil || (previous != nil && !previous[string(p.UID)]) {
			continue
		}
		if len(e.pinned) > 0 && e.pinned[p.Name] != p.UID {
			continue
		}
		matched[string(p.UID)] = true
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("TRIGGER_MISSED: exact terminating UID absent in %s", name)
	}
	if fence {
		groups, err := e.r.dynamic.Resource(PGGVR).Namespace(e.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("INCONCLUSIVE PodGroup deletion fence: %w", err)
		}
		if err = saveYAML(filepath.Join(e.dir, name+"-podgroups.yaml"), groups); err != nil {
			return nil, err
		}
		live := map[string]bool{}
		for _, g := range groups.Items {
			if g.GetDeletionTimestamp() == nil {
				live[string(g.GetUID())] = true
			}
		}
		// A live deletionTimestamp proves the API accepted deletion before the
		// next scale request, even when that Pod's Watch event is still queued.
		// Commit it under the previous budget without replacing the Watch cache.
		if err = e.locked(func() error {
			for uid, g := range e.o.objects["podgroups"] {
				if !live[uid] && objectOwned(g, e.l.Owner) {
					e.l.Before("podgroups", "DELETED", g, e.o.objects)
				}
			}
			for _, p := range pods.Items {
				if !owned(&p, e.l.Owner) || p.DeletionTimestamp == nil {
					continue
				}
				object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&p)
				if err != nil {
					return err
				}
				e.l.Before("pods", "MODIFIED", &unstructured.Unstructured{Object: object}, e.o.objects)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	err = writeJSON(filepath.Join(e.dir, name+"-proof.json"), map[string]interface{}{"at": time.Now().UTC(), "resourceVersion": pods.ResourceVersion, "terminatingUIDs": matched})
	return matched, err
}

func (e *normalExecution) prepareTemplate(ctx context.Context) error {
	needs := false
	for _, p := range e.c.Scenario.Steps {
		for _, raw := range listValue(p.Spec, "plugins") {
			if textValue(mapValue(raw.(map[string]interface{}), "config"), "template") == "${CASE_TEMPLATE}" {
				needs = true
			}
		}
	}
	if !needs {
		return nil
	}
	base, err := e.r.kube.CoreV1().ConfigMaps("kthena-system").Get(ctx, "production020-ranktable-template", metav1.GetOptions{})
	if err != nil {
		return err
	}
	labels := map[string]string{}
	for k, v := range base.Labels {
		labels[k] = v
	}
	labels["rollout-runner/run"] = e.r.opt.RunID
	labels["rollout-runner/case"] = e.c.ID
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: e.namespace + "-template", Namespace: "kthena-system", Labels: labels}, Data: base.Data}
	cm, err = e.r.kube.CoreV1().ConfigMaps("kthena-system").Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	e.templateCM = cm
	return saveYAML(filepath.Join(e.dir, "plugin-template-initial.yaml"), cm)
}
func (e *normalExecution) materializeSpec(spec map[string]interface{}) map[string]interface{} {
	out := cloneMap(spec)
	for _, raw := range listValue(out, "plugins") {
		config := mapValue(raw.(map[string]interface{}), "config")
		if textValue(config, "template") == "${CASE_TEMPLATE}" && e.templateCM != nil {
			config["template"] = e.templateCM.Name
		}
	}
	return out
}

func (e *normalExecution) blockResources(ctx context.Context, prefix string) error {
	nodes, err := e.r.kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(nodes.Items) != 1 {
		return fmt.Errorf("resource-gate fixture requires the verified single-node Kind cluster")
	}
	node := nodes.Items[0]
	pods, err := e.r.kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	used := int64(0)
	for _, p := range pods.Items {
		if p.Spec.NodeName != node.Name || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		regular := int64(0)
		for _, c := range p.Spec.Containers {
			regular += c.Resources.Requests.Cpu().MilliValue()
		}
		initial := int64(0)
		for _, c := range p.Spec.InitContainers {
			initial = max(initial, c.Resources.Requests.Cpu().MilliValue())
		}
		used += max(regular, initial) + p.Spec.Overhead.Cpu().MilliValue()
	}
	free := node.Status.Allocatable.Cpu().MilliValue() - used
	if free < 1 {
		return fmt.Errorf("cannot establish resource gate: free CPU %dm", free)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "capacity-holder", Namespace: e.namespace}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, SchedulerName: "default-scheduler", NodeSelector: map[string]string{"kubernetes.io/hostname": node.Labels["kubernetes.io/hostname"]}, Containers: []corev1.Container{{Name: "holder", Image: "busybox:1.36", ImagePullPolicy: corev1.PullIfNotPresent, Command: []string{"sleep", "3600"}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(free, resource.DecimalSI)}}}}}}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-resource-gate-request.yaml"), pod); err != nil {
		return err
	}
	pod, err = e.r.kube.CoreV1().Pods(e.namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	e.capacityPod = pod
	deadline, cancel := context.WithTimeout(ctx, e.r.opt.Timeout)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		pod, err = e.r.kube.CoreV1().Pods(e.namespace).Get(deadline, pod.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if pod.Spec.NodeName == node.Name && pod.Status.Phase == corev1.PodRunning {
			break
		}
		select {
		case <-deadline.Done():
			return deadline.Err()
		case <-tick.C:
		}
	}
	return saveYAML(filepath.Join(e.dir, prefix+"-resource-gate-server.yaml"), pod)
}
