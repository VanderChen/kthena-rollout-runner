// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestContainerRestartRequiresSamePodAndActualNewProcess(t *testing.T) {
	sent := time.Now().UTC()
	before := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "entry", UID: "old-pod"}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "workload", ContainerID: "containerd://old", RestartCount: 2, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	after := before.DeepCopy()
	status := &after.Status.ContainerStatuses[0]
	status.ContainerID = "containerd://new"
	status.RestartCount = 3
	status.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{ExitCode: 42, FinishedAt: metav1.NewTime(sent)}
	if ok, err := containerRestartObserved(before, after, sent); !ok || err != nil {
		t.Fatal("actual same-Pod process restart rejected", err)
	}
	for _, test := range []struct {
		name   string
		change func(*corev1.Pod)
	}{
		{"replacement Pod", func(p *corev1.Pod) { p.UID = "replacement" }},
		{"terminating original", func(p *corev1.Pod) { now := metav1.Now(); p.DeletionTimestamp = &now }},
		{"old container", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].ContainerID = "containerd://old" }},
		{"count unchanged", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].RestartCount = 2 }},
		{"old crash", func(p *corev1.Pod) {
			p.Status.ContainerStatuses[0].LastTerminationState.Terminated.FinishedAt = metav1.NewTime(sent.Add(-time.Minute))
		}},
		{"unrelated exit", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].LastTerminationState.Terminated.ExitCode = 137 }},
		{"not running", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].State.Running = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := after.DeepCopy()
			test.change(p)
			if ok, _ := containerRestartObserved(before, p, sent); ok {
				t.Fatal("false restart evidence accepted")
			}
		})
	}
}

func TestContainerRestartCatalogueHasNoPodDeleteAndRollsOnlyAfterRestart(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "container-restart"))
	if err != nil || len(cases) != 12 {
		t.Fatalf("12 restart cases: %d %v", len(cases), err)
	}
	for i, c := range cases {
		if c.ID != fmt.Sprintf("RUN-%03d", 389+i) || len(c.Scenario.Steps) != 2 {
			t.Fatal("restart coverage gap")
		}
		first, second := c.Scenario.Steps[0], c.Scenario.Steps[1]
		if first.Action != "restart-container" || first.PodFault != nil || first.Release != "none" || first.StableSeconds < 30 || !first.Expect.NoReplacement || !first.Expect.NoNewRevision || second.Action != "update" {
			t.Fatal("restart must preserve Pods/history until Ready before update")
		}
		a := cloneMap(c.Scenario.InitialSpec)
		if a["recoveryPolicy"] != "None" {
			t.Fatal("restart recovery policy changed")
		}
		for _, raw := range listValue(mapValue(a, "template"), "roles") {
			role := raw.(map[string]interface{})
			if role["name"] != "frontend" {
				continue
			}
			entry := mapValue(mapValue(role, "entryTemplate"), "spec")
			if entry["restartPolicy"] != "Always" {
				t.Fatal("Always omitted")
			}
			command := listValue(listValue(entry, "containers")[0].(map[string]interface{}), "command")
			if !reflect.DeepEqual(command, []interface{}{"sh", "-c", "trap 'exit 42' USR1; while :; do sleep 1 & wait $!; done"}) {
				t.Fatal("PID1 process fixture does not handle explicit exit signal")
			}
			for _, field := range []string{"entryTemplate", "workerTemplate"} {
				container := listValue(mapValue(mapValue(role, field), "spec"), "containers")[0].(map[string]interface{})
				listValue(container, "env")[0].(map[string]interface{})["value"] = "B"
			}
		}
		if !reflect.DeepEqual(a, second.Spec) {
			t.Fatal("B changes more than frontend templates")
		}
		c.Scenario.Steps[0].Action = "recover-pod"
		if c.validateScenario() == nil {
			t.Fatal("Pod deletion accepted as in-place restart")
		}
	}
}
