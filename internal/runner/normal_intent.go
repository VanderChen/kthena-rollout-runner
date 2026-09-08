// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type requestInterval struct {
	Sent, Received time.Time
}

type priorRoleIntent struct {
	UID               string          `json:"uid"`
	Owner             string          `json:"owner"`
	OriginalViolation string          `json:"originalViolation"`
	OriginalStart     NormalStart     `json:"originalStart"`
	IntentTime        time.Time       `json:"intentTime"`
	Request           requestInterval `json:"request"`
	PreviousDesired   int             `json:"previousDesired"`
	PreviousMinimum   int             `json:"previousMinimum"`
}

// This is a deliberately finite correction for an already-issued Role action.
// The API request interval itself remains unresolved; an intent after the
// successful response remains a new action under the new budget.
func inspectPriorRoleIntent(l *NormalLedger, objects Objects, namespace, logs string, request requestInterval) (priorRoleIntent, error) {
	bad := func() (priorRoleIntent, error) { return priorRoleIntent{}, fmt.Errorf("prior Role intent not proved") }
	if l.Model.Mode != "Role" || len(l.History) < 2 || len(l.Violations) != 1 || len(l.Starts) == 0 || !request.Sent.Before(request.Received) {
		return bad()
	}
	start := l.Starts[len(l.Starts)-1]
	old := l.History[len(l.History)-2]
	r, ok := old.Roles[start.Role]
	next := l.Model.Roles[start.Role]
	// Only a replicas restore from an all-unavailable-permitted, single-member
	// Role is supported. Other templates, budgets, roles and top-level layout
	// must be identical. This cannot exempt an arbitrary rollout deletion.
	expect := old
	expect.Roles = map[string]RoleLayout{}
	for name, value := range old.Roles {
		expect.Roles[name] = value
	}
	rRestored := r
	rRestored.R = next.R
	expect.Roles[start.Role] = rRestored
	expectSpec := cloneMap(old.Spec)
	for _, value := range listValue(mapValue(expectSpec, "template"), "roles") {
		role := value.(map[string]interface{})
		if textValue(role, "name") == start.Role {
			role["replicas"] = next.R
		}
	}
	beforeSpec, beforeErr := json.Marshal(expectSpec)
	afterSpec, afterErr := json.Marshal(l.Model.Spec)
	violation := fmt.Sprintf("%s: BUDGET_VIOLATION: %s ready=%d delete=%s minimum=%d", l.Phase, start.Scope, start.ReadyBefore, start.Key, start.Minimum)
	if !ok || beforeErr != nil || afterErr != nil || string(beforeSpec) != string(afterSpec) || old.Mode != "Role" || r.R != 1 || r.U != 1 || r.S != 0 || r.P != 0 || r.W != 0 || next.R <= r.R || !sameEffectiveModel(expect, l.Model) ||
		start.Reason != "rollout" || start.Phase != l.Phase || start.Ordinal != 0 || start.ReadyBefore != 1 || start.Minimum != max(next.R-next.U, 0) ||
		start.Version == r.Entry || len(start.UIDs) != 1 || l.Violations[0] != violation {
		return bad()
	}
	uid := start.UIDs[0]
	pod := objects["pods"][uid]
	var typed corev1.Pod
	if pod == nil || convertPod(pod, &typed) != nil || !owned(&typed, l.Owner) || roleUnitKey(&typed) != start.Key || podVersion(&typed) != start.Version || !l.Committed[uid] {
		return bad()
	}
	var intents []time.Time
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, fmt.Sprintf("object=%q", namespace+"/model")) || !strings.Contains(line, `reason="RoleDeleting"`) ||
			!strings.Contains(line, fmt.Sprintf("message=%q", fmt.Sprintf("Role %s/%s-%d in ServingGroup model-%d is now Deleting", start.Role, start.Role, start.Ordinal, start.Group))) {
			continue
		}
		fields := strings.Fields(line)
		at, err := time.Parse(time.RFC3339Nano, fields[0])
		if err != nil {
			return bad()
		}
		intents = append(intents, at)
	}
	if len(intents) != 1 || !intents[0].Before(request.Sent) || !intents[0].Before(start.At) {
		return bad()
	}
	// Every earlier member of this Role scope must have an observed finite
	// deletion before the intent, leaving only the exact old ordinal-zero UID.
	for _, prior := range l.Starts[:len(l.Starts)-1] {
		if prior.Scope == start.Scope && !prior.At.Before(intents[0]) {
			return bad()
		}
	}
	for otherUID, object := range objects["pods"] {
		var other corev1.Pod
		if convertPod(object, &other) != nil || !owned(&other, l.Owner) || other.Labels[LabelGroup] != typed.Labels[LabelGroup] || other.Labels[LabelRole] != start.Role {
			continue
		}
		if otherUID != uid && object.GetDeletionTimestamp() == nil {
			return bad()
		}
	}
	return priorRoleIntent{UID: uid, Owner: l.Owner, OriginalViolation: violation, OriginalStart: start, IntentTime: intents[0], Request: request, PreviousDesired: r.R, PreviousMinimum: 0}, nil
}

func readIntentJSON(path string, value interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func (e *normalExecution) resolvePriorRoleIntent(ctx context.Context, p ScenarioStep) bool {
	prefix := filepath.Join(e.dir, fmt.Sprintf("step-%02d", e.phase))
	var interval, previous requestInterval
	if e.phase < 2 || readIntentJSON(prefix+"-request-time.json", &interval) != nil ||
		readIntentJSON(filepath.Join(e.dir, fmt.Sprintf("step-%02d-request-time.json", e.phase-1)), &previous) != nil {
		return false
	}
	// The source's live-terminating trigger must have succeeded both before
	// and after the accepted restore. Never resume an unsubmitted request.
	var before, after struct {
		At              time.Time
		TerminatingUIDs map[string]bool
	}
	if readIntentJSON(prefix+"-terminating-before-proof.json", &before) != nil || readIntentJSON(prefix+"-terminating-after-proof.json", &after) != nil ||
		!before.At.Before(interval.Sent) || !interval.Received.Before(after.At) {
		return false
	}
	common := false
	for uid, yes := range before.TerminatingUIDs {
		common = common || yes && after.TerminatingUIDs[uid]
	}
	if !common {
		return false
	}
	// Capture the exact ledger/UID cohort at the failure before the log read.
	e.o.mu.Lock()
	if e.o.err != nil || len(e.l.Violations) != 1 {
		e.o.mu.Unlock()
		return false
	}
	ledgerData, err := json.Marshal(e.l)
	objects := Objects{"pods": {}}
	for uid, o := range e.o.objects["pods"] {
		objects["pods"][uid] = o.DeepCopy()
	}
	var saved NormalLedger
	if err == nil {
		err = json.Unmarshal(ledgerData, &saved)
	}
	saved.Model = e.l.Model
	saved.History = append([]NormalModel{}, e.l.History...)
	e.o.mu.Unlock()
	if err != nil {
		return false
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dep, err := e.r.kube.AppsV1().Deployments("kthena-system").Get(bounded, "kthena-controller-manager", metav1.GetOptions{})
	if err != nil {
		return false
	}
	pods, err := e.r.kube.CoreV1().Pods(dep.Namespace).List(bounded, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(dep.Spec.Selector.MatchLabels).String()})
	if err != nil || len(pods.Items) != 1 {
		return false
	}
	controller := pods.Items[0]
	if controller.DeletionTimestamp != nil || len(controller.Status.ContainerStatuses) != 1 || !controller.Status.ContainerStatuses[0].Ready || controller.Status.ContainerStatuses[0].RestartCount != 0 ||
		len(controller.Spec.Containers) != 1 || controller.Spec.Containers[0].Image != e.r.opt.ControllerImage {
		return false
	}
	since := metav1.NewTime(e.res.Started)
	logs, err := e.r.kube.CoreV1().Pods(dep.Namespace).GetLogs(controller.Name, &corev1.PodLogOptions{Container: controller.Spec.Containers[0].Name, Timestamps: true, SinceTime: &since}).DoRaw(bounded)
	if err != nil {
		return false
	}
	review, err := inspectPriorRoleIntent(&saved, objects, e.namespace, string(logs), interval)
	if err != nil || !review.IntentTime.After(previous.Received) {
		return false
	}
	// Preserve the original provisional observation before correcting only this
	// start. All future UIDs, violations, and the original deadline stay active.
	if os.WriteFile(prefix+"-prior-intent-before-ledger.json", ledgerData, 0644) != nil ||
		saveYAML(prefix+"-prior-intent-pods.yaml", objects) != nil ||
		os.WriteFile(prefix+"-prior-intent-controller.log", logs, 0644) != nil ||
		saveYAML(prefix+"-prior-intent-controller.yaml", controller) != nil || writeJSON(prefix+"-prior-intent-review.json", review) != nil {
		return false
	}
	e.o.mu.Lock()
	defer e.o.mu.Unlock()
	if e.o.err != nil || len(e.l.Violations) != 1 || e.l.Violations[0] != review.OriginalViolation || len(e.l.Starts) != len(saved.Starts) {
		return false
	}
	e.l.Violations = nil
	e.l.Starts[len(e.l.Starts)-1].Reason = "rollout-in-flight"
	e.l.Starts[len(e.l.Starts)-1].Minimum = review.PreviousMinimum
	fmt.Printf("PRIOR_ROLE_INTENT %s uid=%s intent=%s restoreSent=%s; continuing original phase\n", e.c.ID, review.UID, review.IntentTime.Format(time.RFC3339Nano), interval.Sent.Format(time.RFC3339Nano))
	return true
}
