// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const evictionWebhookName = "runner-eviction-022"
const evictionTrackerName = "kthena-eviction-tracker-model"

func validEvictionTracker(cm *unstructured.Unstructured, owner, uid string) bool {
	if uid == "" || string(cm.GetUID()) != uid || !objectOwned(cm, owner) || cm.GetName() != evictionTrackerName || cm.GetDeletionTimestamp() != nil || cm.GetLabels()["modelserving.volcano.sh/name"] != "model" {
		return false
	}
	raw, _, _ := unstructured.NestedString(cm.Object, "data", "entries")
	var entries map[string]struct {
		ExpiresAt string `json:"expiresAt"`
		UID       string `json:"triggerPodUID"`
		Name      string `json:"triggerPodName"`
	}
	if json.Unmarshal([]byte(raw), &entries) != nil || entries == nil {
		return false
	}
	for key, entry := range entries {
		if key == "" || entry.UID == "" || entry.Name == "" {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, entry.ExpiresAt); err != nil {
			return false
		}
	}
	return true
}

func (e *normalExecution) evictionAdmission(ctx context.Context, prefix string) error {
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	flags := 0
	for _, arg := range controller.Spec.Containers[0].Args {
		if strings.HasPrefix(arg, "--enable-eviction-webhook") {
			if arg != "--enable-eviction-webhook=true" {
				return fmt.Errorf("INCONCLUSIVE: actual eviction webhook is disabled")
			}
			flags++
		}
	}
	if flags != 1 {
		return fmt.Errorf("INCONCLUSIVE: explicit eviction webhook flag missing")
	}
	webhook, err := e.r.kube.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, evictionWebhookName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(webhook.Webhooks) != 1 {
		return fmt.Errorf("INCONCLUSIVE: unexpected eviction admission fixture")
	}
	w := webhook.Webhooks[0]
	if w.FailurePolicy == nil || *w.FailurePolicy != admissionv1.Fail || w.NamespaceSelector == nil || len(w.NamespaceSelector.MatchExpressions) != 0 || len(w.NamespaceSelector.MatchLabels) != 1 || w.NamespaceSelector.MatchLabels["rollout-runner/run"] != e.r.opt.RunID || len(w.ClientConfig.CABundle) == 0 {
		return fmt.Errorf("INCONCLUSIVE: eviction admission must be Fail and scoped to this run")
	}
	svc := w.ClientConfig.Service
	if svc == nil || svc.Name != "kthena-controller-manager-webhook" || svc.Namespace != "kthena-system" || svc.Path == nil || *svc.Path != "/validate-eviction" || svc.Port == nil || *svc.Port != 443 || len(w.Rules) != 1 {
		return fmt.Errorf("INCONCLUSIVE: actual eviction admission service route mismatch")
	}
	rule := w.Rules[0]
	if len(rule.Operations) != 1 || rule.Operations[0] != admissionv1.Create || len(rule.APIGroups) != 1 || rule.APIGroups[0] != "" || len(rule.APIVersions) != 1 || rule.APIVersions[0] != "v1" || len(rule.Resources) != 1 || rule.Resources[0] != "pods/eviction" {
		return fmt.Errorf("INCONCLUSIVE: actual pods/eviction admission rule missing")
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-eviction-webhook.yaml"), webhook); err != nil {
		return err
	}
	return saveYAML(filepath.Join(e.dir, prefix+"-controller-before.yaml"), controller)
}

func (e *normalExecution) evictReadyEntry(ctx context.Context, p ScenarioStep, prefix string) error {
	if err := e.evictionAdmission(ctx, prefix); err != nil {
		return err
	}
	ms, err := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace).Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-eviction-server.yaml"), ms.Object); err != nil {
		return err
	}
	eviction := mapValue(mapValue(mapValue(ms.Object, "spec"), "rolloutStrategy"), "evictionStrategy")
	if textValue(eviction, "protectionLevel") != "Role" || intValue(mapValue(eviction, "roleMinAvailable"), "frontend", -1) != 1 {
		return fmt.Errorf("TRIGGER_MISSED: actual role eviction minimum must equal one")
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-fault-pods-before.yaml"), pods); err != nil {
		return err
	}
	var target *corev1.Pod
	a, b := 0, 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !owned(pod, e.l.Owner) || pod.Labels[LabelRole] != "frontend" || !podReady(pod) {
			continue
		}
		if podVersion(pod) == "A" {
			a++
		}
		if podVersion(pod) == "B" {
			b++
		}
		if ordinal(pod.Labels[LabelGroup]) == 0 && ordinal(pod.Labels[LabelRoleID]) == 0 && podIsEntry(pod) && podVersion(pod) == "A" {
			if target != nil {
				return fmt.Errorf("TRIGGER_MISSED: ambiguous eviction target")
			}
			target = pod
		}
	}
	if a == 0 || b == 0 || target == nil {
		return fmt.Errorf("TRIGGER_MISSED: eviction requires real Ready A/B mixture and original ordinal0 entry")
	}
	scope, err := capturePodFaultScope(e.l.Owner, string(target.UID), textValue(mapValue(ms.Object, "spec"), "recoveryPolicy"), pods.Items)
	if err != nil {
		return err
	}
	if scope.Recovery != "RoleRecreate" || len(scope.RecoveryUIDs) != 1 {
		return fmt.Errorf("INCONCLUSIVE: source W=0 eviction must capture only the one old Role entry")
	}
	if err = e.locked(func() error {
		e.l.Phase = p.Name
		e.l.Expected = p.Expect
		if armErr := e.l.armRecovery(scope, e.o.objects); armErr != nil {
			return armErr
		}
		// This source requires eventual B, without prescribing the first
		// transient replacement version. Only old-UID disruption permission
		// is granted; subsequent real Pods retain ordinary rollout checks.
		e.l.Recoveries[len(e.l.Recoveries)-1].WantVersions = map[string]string{}
		return nil
	}); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-fault-scope.json"), scope); err != nil {
		return err
	}
	request := &policyv1.Eviction{TypeMeta: metav1.TypeMeta{APIVersion: "policy/v1", Kind: "Eviction"}, ObjectMeta: metav1.ObjectMeta{Name: target.Name, Namespace: e.namespace}, DeleteOptions: &metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &target.UID}}}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-eviction-request.yaml"), request); err != nil {
		return err
	}
	sent := time.Now().UTC()
	status := 0
	body, requestErr := e.r.kube.CoreV1().RESTClient().Post().Namespace(e.namespace).Resource("pods").Name(target.Name).SubResource("eviction").Body(request).Do(ctx).StatusCode(&status).Raw()
	received := time.Now().UTC()
	errText := ""
	if requestErr != nil {
		errText = requestErr.Error()
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-eviction-response.json"), map[string]interface{}{"sent": sent, "received": received, "target": target.Name, "uid": target.UID, "httpStatus": status, "body": string(body), "accepted": requestErr == nil, "error": errText}); err != nil {
		return err
	}
	if requestErr != nil {
		return fmt.Errorf("EVICTION_NOT_ACCEPTED: %w", requestErr)
	}
	cm, err := e.r.kube.CoreV1().ConfigMaps(e.namespace).Get(ctx, evictionTrackerName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: real eviction admission tracker missing: %w", err)
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-eviction-tracker.yaml"), cm); err != nil {
		return err
	}
	raw, err := json.Marshal(cm)
	if err != nil {
		return err
	}
	object := &unstructured.Unstructured{}
	if err = json.Unmarshal(raw, &object.Object); err != nil {
		return err
	}
	if !validEvictionTracker(object, e.l.Owner, string(cm.UID)) {
		return fmt.Errorf("INCONCLUSIVE: invalid eviction tracker identity")
	}
	var entries map[string]map[string]interface{}
	if err = json.Unmarshal([]byte(cm.Data["entries"]), &entries); err != nil {
		return err
	}
	found := false
	for _, entry := range entries {
		if entry["triggerPodUID"] == string(target.UID) && entry["triggerPodName"] == target.Name {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("INCONCLUSIVE: admission tracker did not record actual evicted UID")
	}
	e.evictionTrackerUID = string(cm.UID)
	deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for {
		current, getErr := e.r.kube.CoreV1().Pods(e.namespace).Get(deadline, target.Name, metav1.GetOptions{})
		if getErr != nil && !apierrors.IsNotFound(getErr) {
			return getErr
		}
		gone := apierrors.IsNotFound(getErr) || (current != nil && current.UID != target.UID)
		observed := false
		if err = e.locked(func() error { observed = e.o.objects["pods"][string(target.UID)] == nil; return nil }); err != nil {
			return err
		}
		if gone && observed {
			if err = writeJSON(filepath.Join(e.dir, prefix+"-eviction-old-absent.json"), map[string]interface{}{"received": time.Now().UTC(), "oldUID": target.UID, "notFound": apierrors.IsNotFound(getErr), "current": current, "directWatchAbsent": observed}); err != nil {
				return err
			}
			break
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("INCONCLUSIVE: evicted old UID deletion not fully observed")
		case <-time.After(50 * time.Millisecond):
		}
	}
	return e.finishStep(ctx, p, prefix)
}
