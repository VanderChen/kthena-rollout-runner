// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// graceSlack absorbs clock and polling granularity between the runner, the
// kubelet and the controller log.
const graceSlack = 3 * time.Second

// syncMarkerTime returns when a controller process, started after the given
// instant, logged that its informer caches were filled. Log lines carry the
// RFC3339 timestamp added by the kubelet.
func syncMarkerTime(logs string, after time.Time) (time.Time, bool) {
	for _, line := range strings.Split(logs, "\n") {
		stamp, _, _ := strings.Cut(line, " ")
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if err == nil && at.After(after) && controllerSynced(line) {
			return at, true
		}
	}
	return time.Time{}, false
}

// checkGraceAcrossProcess judges when the recovery scope of a continuous
// restart fault was recreated, relative to a controller process change that
// happened inside the first grace window (API section 6.2: the fault start is
// not carried across processes; the new process observes again and waits one
// full grace period).
//
//	faultAt     the fault began (the old process cannot have seen it earlier)
//	syncedAt    the new process finished its initial sync (it cannot have seen
//	            the fault earlier)
//	recoveredAt the first member of the scope was being deleted; zero if never
//	observedTo  how long the scope was watched when it was never recreated
func checkGraceAcrossProcess(faultAt, syncedAt, recoveredAt, observedTo time.Time, grace time.Duration) error {
	if grace < 30*time.Second || !syncedAt.After(faultAt) || !syncedAt.Before(faultAt.Add(grace-graceSlack)) {
		return fmt.Errorf("INCONCLUSIVE: the process change must fall inside the first grace window (fault %s, synced %s, grace %s)", faultAt.Format(time.RFC3339), syncedAt.Format(time.RFC3339), grace)
	}
	restarted := syncedAt.Add(grace)
	if recoveredAt.IsZero() {
		if observedTo.Before(restarted.Add(grace / 2)) {
			return fmt.Errorf("INCONCLUSIVE: scope watched only %.0fs after the new process started", observedTo.Sub(syncedAt).Seconds())
		}
		return fmt.Errorf("GRACE_NEVER_RECOVERED: scope not recreated %.0fs after the new process started (grace %s)", observedTo.Sub(syncedAt).Seconds(), grace)
	}
	if recoveredAt.Before(syncedAt) {
		return fmt.Errorf("GRACE_IGNORED: scope recreated %.0fs after the fault, before the grace of %s and before the new process", recoveredAt.Sub(faultAt).Seconds(), grace)
	}
	if recoveredAt.Before(restarted.Add(-graceSlack)) {
		if !recoveredAt.Before(faultAt.Add(grace - graceSlack)) {
			return fmt.Errorf("GRACE_CARRIED_ACROSS_PROCESS: scope recreated %.0fs after the fault and only %.0fs after the new process started; the grace of %s must restart in the new process", recoveredAt.Sub(faultAt).Seconds(), recoveredAt.Sub(syncedAt).Seconds(), grace)
		}
		return fmt.Errorf("GRACE_NOT_RESTARTED: the new process recreated the scope %.0fs after it started, without waiting the grace of %s", recoveredAt.Sub(syncedAt).Seconds(), grace)
	}
	if recoveredAt.After(restarted.Add(grace / 2)) {
		return fmt.Errorf("GRACE_NEVER_RECOVERED: scope recreated only %.0fs after the new process started (grace %s)", recoveredAt.Sub(syncedAt).Seconds(), grace)
	}
	return nil
}

func (e *normalExecution) leaderElectedCase() bool {
	return e.c.ID == "RUN-441" || e.c.ID == "RUN-442" || e.processChange == "terminate-leader"
}

// graceAcrossProcess makes one member faulty (container restart, not Ready),
// replaces the active controller process inside the grace window and measures
// when the recovery scope is recreated.
func (e *normalExecution) graceAcrossProcess(ctx context.Context, p ScenarioStep, prefix string) error {
	e.processChange = p.ProcessChange
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	e.faultController = controller
	spec := mapValue(e.current.Object, "spec")
	grace := time.Duration(intValue(mapValue(spec, "template"), "restartGracePeriodSeconds", 0)) * time.Second
	policy := textValue(spec, "recoveryPolicy")
	if grace < 60*time.Second || policy != "RoleRecreate" && policy != "ServingGroupRecreate" {
		return fmt.Errorf("INCONCLUSIVE: admitted spec needs a finite grace of at least 60 seconds and a recreating recoveryPolicy")
	}
	if err = e.locked(func() error { return e.l.Transition(e.l.Model.Spec, p.Name, p.Expect, e.o.objects) }); err != nil {
		return err
	}
	pods, err := e.r.kube.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	f := p.ContainerRestart
	var target *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if owned(pod, e.l.Owner) && ordinal(pod.Labels[LabelGroup]) == f.Group && pod.Labels[LabelRole] == f.Role && ordinal(pod.Labels[LabelRoleID]) == f.Ordinal && podIsEntry(pod) == (f.Member == "entry") {
			target = pod
		}
	}
	if target == nil || !podReady(target) || target.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		return fmt.Errorf("INCONCLUSIVE: faulty member must be Ready with restartPolicy Always")
	}
	before, _ := workloadStatus(target)
	scope, err := capturePodFaultScope(e.l.Owner, string(target.UID), policy, pods.Items)
	if err != nil {
		return fmt.Errorf("INCONCLUSIVE: capture fault scope: %w", err)
	}
	if err = e.locked(func() error { return e.l.armRecovery(scope, e.o.objects) }); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-fault-scope.json"), scope); err != nil {
		return err
	}
	req := e.r.kube.CoreV1().RESTClient().Post().Resource("pods").Namespace(e.namespace).Name(target.Name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: "workload", Command: []string{"sh", "-c", "kill -USR1 1"}, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(e.r.rest, "POST", req.URL())
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	faultAt := time.Now().UTC()
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	_ = executor.StreamWithContext(commandCtx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}) // the exit closes the stream
	cancel()
	// scopeDeleting reports the first destructive observation of the scope.
	scopeDeleting := func() (bool, error) {
		deleting := false
		err := e.locked(func() error {
			deleting = len(e.l.Recoveries[len(e.l.Recoveries)-1].Deleted) > 0
			return nil
		})
		return deleting, err
	}
	restarted := false
	// The successor needs up to a lease duration to take over; change the
	// process early enough that it observes the fault inside the first window.
	hold := faultAt.Add(grace / 3)
	for time.Now().Before(hold) {
		current, getErr := e.r.kube.CoreV1().Pods(e.namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if getErr != nil && !apierrors.IsNotFound(getErr) {
			return getErr
		}
		if getErr == nil && current.UID == target.UID {
			if status, found := workloadStatus(current); found && status.RestartCount > before.RestartCount && !podReady(current) {
				restarted = true
			}
		}
		deleting, err := scopeDeleting()
		if err != nil {
			return err
		}
		if deleting {
			return fmt.Errorf("GRACE_IGNORED: scope was being deleted %.0fs after the fault (grace %s)", time.Since(faultAt).Seconds(), grace)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if !restarted {
		return fmt.Errorf("INCONCLUSIVE: container restart of the faulty member not established")
	}
	changed := time.Now().UTC()
	logName := prefix + "-new-controller.log"
	if p.ProcessChange == "terminate-leader" {
		logName = prefix + "-new-leader.log"
		err = e.terminateLeader(ctx, prefix)
	} else {
		err = e.terminateController(ctx, prefix)
	}
	if err != nil {
		return err
	}
	logs, err := os.ReadFile(filepath.Join(e.dir, logName))
	if err != nil {
		return err
	}
	syncedAt, ok := syncMarkerTime(string(logs), changed)
	if !ok {
		return fmt.Errorf("INCONCLUSIVE: initial sync time of the new process not found")
	}
	var recoveredAt time.Time
	limit := syncedAt.Add(grace + grace/2 + 5*time.Second)
	for time.Now().Before(limit) {
		deleting, err := scopeDeleting()
		if err != nil {
			return err
		}
		if deleting {
			recoveredAt = time.Now().UTC()
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	verdict := checkGraceAcrossProcess(faultAt, syncedAt, recoveredAt, time.Now().UTC(), grace)
	evidence := map[string]interface{}{"graceSeconds": grace.Seconds(), "faultAt": faultAt, "processChangeRequested": changed, "newProcessSyncedAt": syncedAt, "scopeFirstDeletingAt": recoveredAt, "processChange": p.ProcessChange, "target": target.Name, "targetUID": target.UID}
	if verdict != nil {
		evidence["verdict"] = verdict.Error()
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-grace-across-process.json"), evidence); err != nil {
		return err
	}
	if verdict != nil {
		return verdict
	}
	return e.finishStep(ctx, p, prefix)
}
