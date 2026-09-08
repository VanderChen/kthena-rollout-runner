// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Use real old-owner objects. A runner finalizer retains terminating residues
// for a bounded identity window; it is then removed to let ordinary GC free
// occupied names. A deliberately held finalizer is not a product cleanup bug.
func (e *normalExecution) identityBoundary(ctx context.Context, p ScenarioStep, prefix string) error {
	api := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace)
	old, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	oldOwner := string(old.GetUID())
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	oldPods := map[string]types.UID{}
	forbidden := map[string]string{}
	for _, pod := range pods.Items {
		if !owned(&pod, oldOwner) {
			continue
		}
		if !podReady(&pod) || podVersion(&pod) != "A" {
			return fmt.Errorf("INCONCLUSIVE: old identity A source not Ready")
		}
		oldPods[pod.Name] = pod.UID
		forbidden[string(pod.UID)] = oldOwner
		pod.Finalizers = append(pod.Finalizers, "rollout-runner/hold")
		actual, err := e.r.kube.CoreV1().Pods(e.namespace).Update(ctx, &pod, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		if err = saveYAML(filepath.Join(e.dir, prefix+"-old-pinned-"+pod.Name+".yaml"), actual); err != nil {
			return err
		}
	}
	if len(oldPods) != 6 {
		return fmt.Errorf("INCONCLUSIVE: old identity requires six actual Pods")
	}
	histories, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(histories.Items) != 1 {
		return fmt.Errorf("INCONCLUSIVE: old identity requires one original history")
	}
	oldHistory := histories.Items[0].DeepCopy()
	if !identityOwned(oldHistory, oldOwner) {
		return fmt.Errorf("INCONCLUSIVE: history owner differs from original ModelServing")
	}
	forbidden[string(oldHistory.UID)] = oldOwner
	oldHistory.Finalizers = append(oldHistory.Finalizers, "rollout-runner/hold")
	oldHistory, err = e.r.kube.AppsV1().ControllerRevisions(e.namespace).Update(ctx, oldHistory, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-old-pinned-history.yaml"), oldHistory); err != nil {
		return err
	}
	if err = e.locked(func() error { e.l.Armed = false; return nil }); err != nil {
		return err
	}
	policy := metav1.DeletePropagationBackground
	uid := old.GetUID()
	options := metav1.DeleteOptions{PropagationPolicy: &policy, Preconditions: &metav1.Preconditions{UID: &uid}}
	sent := time.Now().UTC()
	deleteErr := api.Delete(ctx, "model", options)
	if err = writeJSON(filepath.Join(e.dir, prefix+"-old-model-delete.json"), map[string]interface{}{"uid": uid, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": deleteErr == nil}); err != nil {
		return err
	}
	if deleteErr != nil {
		return deleteErr
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err = api.Get(ctx, "model", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			pg, pgerr := e.r.dynamic.Resource(PGGVR).Namespace(e.namespace).List(ctx, metav1.ListOptions{})
			if pgerr != nil {
				return pgerr
			}
			cms, cmerr := e.r.kube.CoreV1().ConfigMaps(e.namespace).List(ctx, metav1.ListOptions{})
			if cmerr != nil {
				return cmerr
			}
			if len(pg.Items) == 0 && len(cms.Items) == 0 {
				break
			}
		} else if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("INCONCLUSIVE: old ModelServing and unpinned plugin siblings not deleted")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Save the actual old UID objects after the owner has really disappeared.
	oldLive, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(oldLive.Items) != 6 {
		return fmt.Errorf("INCONCLUSIVE: retained old Pod fixture missing")
	}
	for _, pod := range oldLive.Items {
		if oldPods[pod.Name] != pod.UID || !owned(&pod, oldOwner) || pod.DeletionTimestamp == nil {
			return fmt.Errorf("INCONCLUSIVE: old residue identity/termination mismatch")
		}
	}
	actualHistory, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, oldHistory.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if actualHistory.UID != oldHistory.UID || !identityOwned(actualHistory, oldOwner) || actualHistory.DeletionTimestamp == nil || !reflect.DeepEqual(actualHistory.Data, oldHistory.Data) {
		return fmt.Errorf("INCONCLUSIVE: old history residue changed")
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-old-residue-pods.yaml"), oldLive); err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-old-residue-history.yaml"), actualHistory); err != nil {
		return err
	}
	request := objectForSpec(e.namespace, e.c.ID, e.materializeSpec(p.Spec))
	if err = saveYAML(filepath.Join(e.dir, prefix+"-new-model-request.yaml"), request.Object); err != nil {
		return err
	}
	sent = time.Now().UTC()
	created, createErr := api.Create(ctx, request, metav1.CreateOptions{})
	if err = writeJSON(filepath.Join(e.dir, prefix+"-new-model-create.json"), map[string]interface{}{"sent": sent, "received": time.Now().UTC(), "accepted": createErr == nil, "oldOwnerUID": oldOwner}); err != nil {
		return err
	}
	if createErr != nil {
		return createErr
	}
	if string(created.GetUID()) == oldOwner {
		return fmt.Errorf("IDENTITY_REUSED: same-named ModelServing retained old UID")
	}
	e.current = created
	if err = saveYAML(filepath.Join(e.dir, prefix+"-new-model-server.yaml"), created.Object); err != nil {
		return err
	}
	if err = e.locked(func() error {
		fresh, err := newNormalLedger(mapValue(created.Object, "spec"), string(created.GetUID()), e.l.Profile)
		if err != nil {
			return err
		}
		fresh.ForeignResidueUIDs = forbidden
		for _, kind := range []string{"controllerrevisions", "pods"} {
			for _, o := range e.o.objects[kind] {
				fresh.After(kind, "LIST", o, e.o.objects)
			}
		}
		e.l, e.o.normal = fresh, fresh
		return fresh.error()
	}); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-source-boundary.json"), map[string]interface{}{"at": time.Now().UTC(), "oldOwnerUID": oldOwner, "newOwnerUID": created.GetUID(), "retainedOldPods": oldPods, "retainedOldHistoryUID": oldHistory.UID, "residueState": "terminating with runner-only finalizer; original old owner UID retained"}); err != nil {
		return err
	}
	start := time.Now()
	for probe := 1; ; probe++ {
		started := time.Now().UTC()
		ms, err := api.Get(ctx, "model", metav1.GetOptions{})
		if err != nil {
			return err
		}
		live, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		cr, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, oldHistory.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		proof := map[string]interface{}{"started": started, "completed": time.Now().UTC(), "model": ms.Object, "pods": live, "oldHistory": cr}
		if err = writeJSON(filepath.Join(e.dir, fmt.Sprintf("%s-identity-window-%03d.json", prefix, probe)), proof); err != nil {
			return err
		}
		if ms.GetUID() != created.GetUID() {
			return fmt.Errorf("IDENTITY_REUSED: new ModelServing UID changed")
		}
		if err = identityCapacity(ms.Object, live.Items, string(created.GetUID()), oldOwner, oldPods); err != nil {
			return err
		}
		if cr.UID != oldHistory.UID || !identityOwned(cr, oldOwner) || !reflect.DeepEqual(cr.Data, oldHistory.Data) {
			return fmt.Errorf("FOREIGN_OWNER_ADOPTED: old ControllerRevision identity/data changed")
		}
		if err = identityHistoryEvidence(ms.Object, oldHistory.Name); err != nil {
			return err
		}
		if err = e.locked(func() error { return nil }); err != nil {
			return err
		}
		if time.Since(start) >= 10*time.Second {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if err = e.snapshot(prefix + "-identity-window-completed"); err != nil {
		return err
	}
	// Release only finalizers installed on the finite old UID set. No explicit
	// deletion of another object's resources, no new template update or restart.
	var released []map[string]interface{}
	for name, uid := range oldPods {
		pod, err := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if pod.UID != uid || !owned(pod, oldOwner) {
			return fmt.Errorf("FOREIGN_OWNER_ADOPTED: old Pod changed before fixture release")
		}
		pod.Finalizers = removeString(pod.Finalizers, "rollout-runner/hold")
		sent = time.Now().UTC()
		_, writeErr := e.r.kube.CoreV1().Pods(e.namespace).Update(ctx, pod, metav1.UpdateOptions{})
		released = append(released, map[string]interface{}{"kind": "Pod", "name": name, "uid": uid, "sent": sent, "received": time.Now().UTC(), "accepted": writeErr == nil})
		if err = writeJSON(filepath.Join(e.dir, prefix+"-fixture-releases.json"), released); err != nil {
			return err
		}
		if writeErr != nil {
			return writeErr
		}
	}
	actualHistory, err = e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, oldHistory.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if actualHistory.UID != oldHistory.UID || !identityOwned(actualHistory, oldOwner) {
		return fmt.Errorf("FOREIGN_OWNER_ADOPTED: old history changed before fixture release")
	}
	actualHistory.Finalizers = removeString(actualHistory.Finalizers, "rollout-runner/hold")
	sent = time.Now().UTC()
	_, writeErr := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Update(ctx, actualHistory, metav1.UpdateOptions{})
	released = append(released, map[string]interface{}{"kind": "ControllerRevision", "name": actualHistory.Name, "uid": actualHistory.UID, "sent": sent, "received": time.Now().UTC(), "accepted": writeErr == nil})
	if err = writeJSON(filepath.Join(e.dir, prefix+"-fixture-releases.json"), released); err != nil {
		return err
	}
	if writeErr != nil {
		return writeErr
	}
	e.historyReferences = true
	e.waitDeadline = time.Time{}
	p.Name = "new-owner-automatic-population-after-fixture-release"
	if err = e.locked(func() error { e.l.Phase = p.Name; return nil }); err != nil {
		return err
	}
	return e.finishStep(ctx, p, prefix+"-final")
}

func identityCapacity(ms map[string]interface{}, pods []corev1.Pod, newOwner, oldOwner string, oldPods map[string]types.UID) error {
	newGroups := map[string]bool{}
	seen := map[string]bool{}
	for i := range pods {
		p := &pods[i]
		if uid, ok := oldPods[p.Name]; ok && p.UID == uid {
			if !owned(p, oldOwner) || owned(p, newOwner) {
				return fmt.Errorf("FOREIGN_OWNER_ADOPTED: old Pod %s", p.Name)
			}
			seen[p.Name] = true
		}
		if owned(p, newOwner) {
			newGroups[p.Labels[LabelGroup]] = true
			if podReady(p) {
				return fmt.Errorf("CONTROL_VIOLATION: new Pod Ready before release")
			}
		}
	}
	if len(seen) != len(oldPods) {
		return fmt.Errorf("INCONCLUSIVE: retained identity source lost a pinned old Pod")
	}
	status := mapValue(ms, "status")
	if intValue(status, "availableReplicas", 0) > 0 || intValue(status, "replicas", 0) > len(newGroups) {
		return fmt.Errorf("FOREIGN_CAPACITY_COUNTED: status includes capacity/Ready without new owned Pod identities")
	}
	return nil
}

func identityOwned(object metav1.Object, owner string) bool {
	for _, ref := range object.GetOwnerReferences() {
		if string(ref.UID) == owner {
			return true
		}
	}
	return false
}

func identityHistoryEvidence(ms map[string]interface{}, oldName string) error {
	status := mapValue(ms, "status")
	for _, field := range []string{"currentRevision", "updateRevision"} {
		revision := textValue(status, field)
		if revision != "" && "model-"+revision == oldName {
			return fmt.Errorf("FOREIGN_HISTORY_REFERENCED: new ModelServing %s references retained old-owner history", field)
		}
	}
	return nil
}
