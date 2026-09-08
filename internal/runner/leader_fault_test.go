// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"path/filepath"
	"testing"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func leaderTestState() (*corev1.PodList, *coordinationv1.Lease) {
	pods := &corev1.PodList{}
	for _, name := range []string{"manager-a", "manager-b"} {
		pods.Items = append(pods.Items, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name + "-uid")}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "controller", Image: "production", Args: []string{"--leader-elect=true"}}}}, Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	}
	holder := "manager-b_random-identity"
	duration := int32(15)
	renewed := metav1.NowMicro()
	return pods, &coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: controllerLeaseName, UID: "lease-uid"}, Spec: coordinationv1.LeaseSpec{HolderIdentity: &holder, LeaseDurationSeconds: &duration, RenewTime: &renewed}}
}

func TestLeaderSelectionUsesLeaseIdentityAndRequiresTwoActualReadyProcesses(t *testing.T) {
	pods, lease := leaderTestState()
	leader, standby, err := selectElectedController(pods, lease, "production")
	if err != nil || leader.Name != "manager-b" || standby.Name != "manager-a" {
		t.Fatal("list ordering cannot choose leader", leader, standby, err)
	}
	for _, mutate := range []func(*corev1.PodList, *coordinationv1.Lease){
		func(p *corev1.PodList, l *coordinationv1.Lease) { p.Items = p.Items[:1] },
		func(p *corev1.PodList, l *coordinationv1.Lease) { p.Items[0].Status.Conditions = nil },
		func(p *corev1.PodList, l *coordinationv1.Lease) {
			p.Items[1].Spec.Containers[0].Args = []string{"--leader-elect=false"}
		},
		func(p *corev1.PodList, l *coordinationv1.Lease) { p.Items[0].Spec.Containers[0].Image = "other" },
		func(p *corev1.PodList, l *coordinationv1.Lease) { v := "gone_random"; l.Spec.HolderIdentity = &v },
		func(p *corev1.PodList, l *coordinationv1.Lease) { v := "manager-a"; l.Spec.HolderIdentity = &v },
		func(p *corev1.PodList, l *coordinationv1.Lease) {
			now := metav1.Now()
			p.Items[1].DeletionTimestamp = &now
		},
		func(p *corev1.PodList, l *coordinationv1.Lease) { p.Items[0].UID = p.Items[1].UID },
		func(p *corev1.PodList, l *coordinationv1.Lease) { l.Spec.RenewTime = nil },
	} {
		p, l := pods.DeepCopy(), lease.DeepCopy()
		mutate(p, l)
		if _, _, err := selectElectedController(p, l, "production"); err == nil {
			t.Fatal("invalid actual leader fixture accepted")
		}
	}
}

func TestLeaderSwitchCasesRequireMixedReadyVersions(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "leader-switch"))
	if err != nil || len(cases) != 2 || cases[0].ID != "RUN-441" || cases[1].ID != "RUN-442" {
		t.Fatal("leader source coverage", cases, err)
	}
	for _, c := range cases {
		conditions := c.Scenario.Steps[0].Conditions
		if len(conditions) != 2 || conditions[0].Version != "A" || conditions[1].Version != "B" || conditions[0].Ready == nil || !*conditions[0].Ready || conditions[1].Ready == nil || !*conditions[1].Ready {
			t.Fatal("leader deletion must require actual A/B Ready mixture")
		}
		c.Scenario.Steps[1].Action = "terminate-controller"
		if c.validateScenario() == nil {
			t.Fatal("ordinary single-controller restart cannot replace elected failover")
		}
	}
}
