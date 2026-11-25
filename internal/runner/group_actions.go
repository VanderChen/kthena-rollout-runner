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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const requiresGroupObjects = "servingGroupObjects"

// GroupDeleteAction is a user's deletion of one ServingGroup object.
type GroupDeleteAction struct {
	Group       int    `json:"group"`
	Propagation string `json:"propagation"`
}

func (e *normalExecution) rememberGroupSnapshot(name string) error {
	if !e.r.groupObjects {
		return nil
	}
	return e.locked(func() error {
		e.groupSnapshots[name] = groupSnapshots(e.o.objects)
		return nil
	})
}

// groupObjectFacts evaluates the declared ServingGroup and ModelServing facts
// of a step. It runs under the observer lock, next to the step predicate.
func (e *normalExecution) groupObjectFacts(expect ScenarioExpectation) (bool, string) {
	if reason := checkGroupExpectations(expect.ServingGroups, e.o.groupStream, e.o.objects, e.l.Owner, e.groupSnapshots); reason != "" {
		return false, reason
	}
	if len(expect.ServingGroups) > 0 {
		// Declared surge facts include the copy of the mark on the member Pods.
		if ok, reason := e.l.surgeMarksSettled(e.o.objects); !ok {
			return false, reason
		}
	}
	var model *unstructured.Unstructured
	for uid, observed := range e.o.objects["modelservings"] {
		if uid == e.l.Owner {
			model = observed
		}
	}
	if reason := conditionExpectations(expect.ModelServingConditions, model); reason != "" {
		return false, reason
	}
	return true, ""
}

func (e *normalExecution) groupMembers(ctx context.Context, name string) (*corev1.PodList, []*corev1.Pod, error) {
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	var members []*corev1.Pod
	for i := range pods.Items {
		if pod := &pods.Items[i]; pod.Labels[LabelGroup] == name && owned(pod, e.l.Owner) {
			members = append(members, pod)
		}
	}
	return pods, members, nil
}

// deleteServingGroup deletes one ServingGroup object the way a user would,
// with an explicit propagation policy and the exact UID. Foreground and
// Background destroy the member Pods: they become one finite recovery scope,
// so every other check of the ledger stays armed. Orphan keeps the Pods: each
// of them becomes a protected identity that must survive under the object
// that takes the group's place.
func (e *normalExecution) deleteServingGroup(ctx context.Context, p ScenarioStep, prefix string) error {
	name := fmt.Sprintf("model-%d", p.GroupDelete.Group)
	api := e.r.dynamic.Resource(SGGVR).Namespace(e.namespace)
	group, err := api.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: ServingGroup to delete: %w", err)
	}
	if group.GetDeletionTimestamp() != nil {
		return fmt.Errorf("INCONCLUSIVE: ServingGroup %s is already terminating", name)
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-servinggroup-before.yaml"), group.Object); err != nil {
		return err
	}
	pods, members, err := e.groupMembers(ctx, name)
	if err != nil {
		return err
	}
	if err = e.locked(func() error { return e.l.Transition(e.l.Model.Spec, p.Name, p.Expect, e.o.objects) }); err != nil {
		return err
	}
	if objectOwned(group, e.l.Owner) {
		if len(members) == 0 {
			return fmt.Errorf("INCONCLUSIVE: ServingGroup %s has no member Pod", name)
		}
		if p.GroupDelete.Propagation == "Orphan" {
			if err = e.locked(func() error {
				for _, pod := range members {
					e.l.Protected[string(pod.UID)] = pod.Name
				}
				// Removing only the object is not the destruction of the group:
				// its deletion event must not count as a start. The members stay
				// protected, so deleting any of them is still a violation.
				e.l.PGCommitted[string(group.GetUID())] = true
				return nil
			}); err != nil {
				return err
			}
		} else {
			scope, err := capturePodFaultScope(e.l.Owner, string(members[0].UID), "ServingGroupRecreate", pods.Items)
			if err != nil {
				return fmt.Errorf("INCONCLUSIVE: capture deleted group: %w", err)
			}
			if err = e.locked(func() error { return e.l.armRecovery(scope, e.o.objects) }); err != nil {
				return err
			}
			if err = writeJSON(filepath.Join(e.dir, prefix+"-deleted-scope.json"), scope); err != nil {
				return err
			}
		}
	}
	uid := group.GetUID()
	policy := metav1.DeletionPropagation(p.GroupDelete.Propagation)
	options := metav1.DeleteOptions{PropagationPolicy: &policy, Preconditions: &metav1.Preconditions{UID: &uid}}
	sent := time.Now().UTC()
	err = api.Delete(ctx, name, options)
	if saveErr := writeJSON(filepath.Join(e.dir, prefix+"-servinggroup-delete.json"), map[string]interface{}{"name": name, "uid": uid, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil}); saveErr != nil {
		return saveErr
	}
	return err
}

// createForeignGroup occupies the name of a future ordinal with a ServingGroup
// that does not belong to the ModelServing under test. Its spec is copied from
// an existing group so that it is a valid object; it has no owner.
func (e *normalExecution) createForeignGroup(ctx context.Context, p ScenarioStep, prefix string) error {
	api := e.r.dynamic.Resource(SGGVR).Namespace(e.namespace)
	source, err := api.Get(ctx, "model-0", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: source of the foreign ServingGroup: %w", err)
	}
	name := fmt.Sprintf("model-%d", *p.ForeignGroup)
	foreign := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": source.GetAPIVersion(), "kind": source.GetKind(),
		"metadata": map[string]interface{}{"name": name, "namespace": e.namespace, "labels": map[string]interface{}{"rollout-runner/foreign": "true"}},
		"spec":     cloneMap(mapValue(source.Object, "spec")),
	}}
	if err = e.locked(func() error { e.l.Phase = p.Name; e.l.Expected = p.Expect; return nil }); err != nil {
		return err
	}
	created, err := api.Create(ctx, foreign, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: create foreign ServingGroup: %w", err)
	}
	if len(created.GetOwnerReferences()) != 0 {
		return fmt.Errorf("INCONCLUSIVE: foreign ServingGroup was created with an owner")
	}
	return saveYAML(filepath.Join(e.dir, prefix+"-foreign-servinggroup.yaml"), created.Object)
}

// faultMember makes one member faulty without deleting it: the workload
// process exits, the kubelet restarts the container and the fresh container is
// not Ready (it is never released). The members of the recovery scope of the
// current recoveryPolicy are captured first, so that the recovery may replace
// exactly them and nothing else.
func (e *normalExecution) faultMember(ctx context.Context, p ScenarioStep, prefix string) error {
	if err := e.locked(func() error { return e.l.Transition(e.l.Model.Spec, p.Name, p.Expect, e.o.objects) }); err != nil {
		return err
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-fault-pods-before.yaml"), pods); err != nil {
		return err
	}
	f := p.ContainerRestart
	var target *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if owned(pod, e.l.Owner) && ordinal(pod.Labels[LabelGroup]) == f.Group && pod.Labels[LabelRole] == f.Role && ordinal(pod.Labels[LabelRoleID]) == f.Ordinal && podIsEntry(pod) == (f.Member == "entry") {
			if target != nil {
				return fmt.Errorf("INCONCLUSIVE: ambiguous faulty member")
			}
			target = pod
		}
	}
	if target == nil || !podReady(target) || target.DeletionTimestamp != nil || target.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		return fmt.Errorf("INCONCLUSIVE: faulty member must be Ready with restartPolicy Always")
	}
	before, ok := workloadStatus(target)
	if !ok || before.State.Running == nil {
		return fmt.Errorf("INCONCLUSIVE: faulty member container not running")
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
	command := []string{"sh", "-c", "kill -USR1 1"}
	req := e.r.kube.CoreV1().RESTClient().Post().Resource("pods").Namespace(e.namespace).Name(target.Name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: "workload", Command: command, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(e.r.rest, "POST", req.URL())
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	sent := time.Now().UTC()
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	execErr := executor.StreamWithContext(commandCtx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr})
	cancel()
	errorText := ""
	if execErr != nil {
		errorText = execErr.Error() // The process exit may close its exec stream.
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-fault-exec.json"), map[string]interface{}{"target": target.Name, "uid": target.UID, "command": command, "sent": sent, "received": time.Now().UTC(), "stdout": stdout.String(), "stderr": stderr.String(), "error": errorText}); err != nil {
		return err
	}
	// The fault is real once the same Pod shows a restarted container, or once
	// the controller has already begun to delete that Pod because of it.
	deadline, cancelDeadline := context.WithTimeout(ctx, 60*time.Second)
	defer cancelDeadline()
	for {
		current, getErr := e.r.kube.CoreV1().Pods(e.namespace).Get(deadline, target.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) || getErr == nil && (current.UID != target.UID || current.DeletionTimestamp != nil) {
			break
		}
		if getErr != nil {
			return getErr
		}
		if status, found := workloadStatus(current); found && status.RestartCount > before.RestartCount {
			if err = saveYAML(filepath.Join(e.dir, prefix+"-fault-target-after.yaml"), current); err != nil {
				return err
			}
			break
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: container exit of the faulty member not established")
		case <-time.After(200 * time.Millisecond):
		}
	}
	if err = e.finishStep(ctx, p, prefix); err != nil {
		return err
	}
	if !p.Expect.TwoStepGroupRecovery {
		return nil
	}
	return e.locked(func() error {
		if err := writeJSON(filepath.Join(e.dir, prefix+"-pod-timeline.json"), e.o.podTimeline); err != nil {
			return err
		}
		return checkTwoStepGroupRecovery(e.o.podTimeline, string(target.UID), scope.RecoveryUIDs)
	})
}
