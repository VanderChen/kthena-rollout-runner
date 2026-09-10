// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// Only a real admission/schema rejection proves the required behavior. Transport
// errors, optimistic concurrency and RBAC failures do not satisfy the oracle.
func admissionRejection(err error) bool {
	status, ok := err.(apierrors.APIStatus)
	if !ok {
		return false
	}
	s := status.Status()
	switch s.Code {
	case 400, 422:
		if s.Reason == metav1.StatusReasonBadRequest || s.Reason == metav1.StatusReasonInvalid {
			return true
		}
		// AdmissionResponse.Result may omit Reason while its native API Status
		// still explicitly records a webhook denial. Keep transport/RBAC errors
		// inconclusive and require the actual denial envelope in this case.
		return s.Reason == "" && s.Status == metav1.StatusFailure && strings.HasPrefix(s.Message, "admission webhook \"") && strings.Contains(s.Message, "\" denied the request:")
	case 403:
		return strings.Contains(s.Message, "admission webhook") && strings.Contains(s.Message, "denied")
	}
	return false
}

func unchangedRejectedSpec(expected, actual *unstructured.Unstructured) error {
	if actual.GetUID() != expected.GetUID() || actual.GetGeneration() != expected.GetGeneration() || !reflect.DeepEqual(actual.Object["spec"], expected.Object["spec"]) {
		return fmt.Errorf("REJECTION_ATOMICITY: accepted spec/generation/UID changed")
	}
	return nil
}

// Refresh only optimistic-concurrency metadata. Invalid fields must reach the
// API verbatim: do not parse them as a NormalModel or arm a scale transition.
func submitRejectedRequest(ctx context.Context, api dynamic.ResourceInterface, current, desired *unstructured.Unstructured, patch map[string]interface{}, dir, prefix string, before func(string) error) error {
	latest := current.DeepCopy()
	for attempt := 1; attempt <= 5; attempt++ {
		if attempt > 1 {
			var err error
			latest, err = api.Get(ctx, current.GetName(), metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("INCONCLUSIVE: rejection conflict refresh: %w", err)
			}
			if err = unchangedRejectedSpec(current, latest); err != nil {
				return err
			}
		}
		key := fmt.Sprintf("%s-reject-%02d", prefix, attempt)
		if err := saveYAML(filepath.Join(dir, key+"-before.yaml"), latest.Object); err != nil {
			return err
		}
		if before != nil {
			if err := before(key); err != nil {
				return err
			}
		}
		var response *unstructured.Unstructured
		var err error
		var sent time.Time
		method := "PUT"
		if patch == nil {
			request := desired.DeepCopy()
			request.SetUID(latest.GetUID())
			request.SetResourceVersion(latest.GetResourceVersion())
			if err = saveYAML(filepath.Join(dir, key+"-request.yaml"), request.Object); err != nil {
				return err
			}
			sent = time.Now().UTC()
			response, err = api.Update(ctx, request, metav1.UpdateOptions{})
		} else {
			// resourceVersion is only a concurrency precondition, not a mutation
			// of the catalogue's intentionally null/missing/invalid fields.
			request := runtime.DeepCopyJSON(patch)
			metadata, _ := request["metadata"].(map[string]interface{})
			if metadata == nil {
				metadata = map[string]interface{}{}
				request["metadata"] = metadata
			}
			metadata["resourceVersion"] = latest.GetResourceVersion()
			metadata["uid"] = string(latest.GetUID())
			if err = saveYAML(filepath.Join(dir, key+"-merge-patch.yaml"), request); err != nil {
				return err
			}
			raw, marshalErr := json.Marshal(request)
			if marshalErr != nil {
				return marshalErr
			}
			method = "PATCH"
			sent = time.Now().UTC()
			response, err = api.Patch(ctx, current.GetName(), types.MergePatchType, raw, metav1.PatchOptions{})
		}
		received := time.Now().UTC()
		record := map[string]interface{}{"sent": sent, "received": received, "method": method, "uid": latest.GetUID(), "generation": latest.GetGeneration(), "requestResourceVersion": latest.GetResourceVersion(), "accepted": err == nil}
		if err != nil {
			record["error"] = err.Error()
			if status, ok := err.(apierrors.APIStatus); ok {
				record["status"] = status.Status()
			}
		}
		if saveErr := writeJSON(filepath.Join(dir, key+"-receipt.json"), record); saveErr != nil {
			return saveErr
		}
		if err == nil {
			if saveErr := saveYAML(filepath.Join(dir, key+"-unexpected-accepted.yaml"), response.Object); saveErr != nil {
				return saveErr
			}
			return fmt.Errorf("ADMISSION_UNEXPECTED_ACCEPT: invalid catalogue request was accepted")
		}
		if apierrors.IsConflict(err) {
			continue
		}
		if !admissionRejection(err) {
			return fmt.Errorf("INCONCLUSIVE: request did not reach a conclusive admission rejection: %w", err)
		}
		actual, getErr := api.Get(ctx, current.GetName(), metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("INCONCLUSIVE: read after rejected request: %w", getErr)
		}
		if saveErr := saveYAML(filepath.Join(dir, key+"-after.yaml"), actual.Object); saveErr != nil {
			return saveErr
		}
		if checkErr := unchangedRejectedSpec(current, actual); checkErr != nil {
			return checkErr
		}
		return writeJSON(filepath.Join(dir, prefix+"-rejection.json"), map[string]interface{}{"attemptPrefix": key, "method": method, "sent": sent, "received": received, "status": err.(apierrors.APIStatus).Status(), "uid": current.GetUID(), "generation": current.GetGeneration()})
	}
	return fmt.Errorf("INCONCLUSIVE: five optimistic concurrency conflicts, no admission decision")
}

func (e *normalExecution) rejectionTrigger(ctx context.Context, p ScenarioStep, prefix string) error {
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-pods.yaml"), pods); err != nil {
		return err
	}
	objects := Objects{"pods": {}}
	for i := range pods.Items {
		pod := &pods.Items[i]
		value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
		if err != nil {
			return err
		}
		objects["pods"][string(pod.UID)] = &unstructured.Unstructured{Object: value}
		if p.Expect.NoReplacement && owned(pod, e.l.Owner) && !podReady(pod) {
			return fmt.Errorf("INCONCLUSIVE: stable rejection source contains an unhealthy Pod")
		}
	}
	return e.locked(func() error {
		if len(p.Conditions) > 0 && (!e.l.conditions(p.Conditions, objects) || !e.l.conditions(p.Conditions, e.o.objects)) {
			return fmt.Errorf("TRIGGER_MISSED: rejection source not actually in declared in-flight state")
		}
		return nil
	})
}

func (e *normalExecution) rejectRequest(ctx context.Context, p ScenarioStep, prefix string) error {
	api := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace)
	current, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if current.GetUID() != e.current.GetUID() {
		return fmt.Errorf("INCONCLUSIVE: rejection source UID changed")
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-accepted-before.yaml"), current.Object); err != nil {
		return err
	}
	if err = e.locked(func() error {
		// Stable sources need a read-only baseline transition to protect every
		// UID. In-flight sources must retain their original commitments/budgets.
		if p.Expect.NoReplacement {
			if err := e.l.Transition(e.l.Model.Spec, p.Name, p.Expect, e.o.objects); err != nil {
				return err
			}
		}
		e.l.Phase = p.Name
		e.l.Expected = p.Expect
		e.l.NoNewRevision = true
		e.l.Revisions = map[string]bool{}
		for uid, o := range e.o.objects["controllerrevisions"] {
			if objectOwned(o, e.l.Owner) {
				e.l.Revisions[uid] = true
			}
		}
		e.l.RejectedSpec = runtime.DeepCopyJSON(current.Object["spec"].(map[string]interface{}))
		e.l.RejectedGeneration = current.GetGeneration()
		return nil
	}); err != nil {
		return err
	}
	e.rejectionExpected = current.DeepCopy()
	var desired *unstructured.Unstructured
	if p.Action == "reject-update" {
		desired = objectForSpec(e.namespace, e.c.ID, e.materializeSpec(p.Spec))
	}
	if err = submitRejectedRequest(ctx, api, current, desired, p.Patch, e.dir, prefix, func(key string) error { return e.rejectionTrigger(ctx, p, key+"-trigger") }); err != nil {
		return err
	}
	if err = e.probeRejectedSpec(ctx); err != nil {
		return err
	}
	e.rejectionChecked = time.Now()
	return e.finishStep(ctx, p, prefix)
}

func (e *normalExecution) probeRejectedSpec(ctx context.Context) error {
	current, err := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace).Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: rejected spec verification: %w", err)
	}
	e.rejectionProbes++
	if err = writeJSON(filepath.Join(e.dir, fmt.Sprintf("rejection-state-%03d.json", e.rejectionProbes)), map[string]interface{}{"at": time.Now().UTC(), "object": current.Object}); err != nil {
		return err
	}
	return unchangedRejectedSpec(e.rejectionExpected, current)
}
