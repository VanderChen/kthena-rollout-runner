// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestHistoryCreationCatalogueLimitsAndCompoundActions(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "history-create"))
	if err != nil || len(cases) != 30 {
		t.Fatal("history coverage", len(cases), err)
	}
	limits := map[int]int{}
	for _, c := range cases {
		config := mapValue(c.Scenario.Source, "config")
		want, has := config["revisionHistoryLimit"]
		if want == "omitted" {
			has = false
		}
		for _, spec := range []map[string]interface{}{c.Scenario.InitialSpec, c.Scenario.Steps[0].Spec} {
			_, present := spec["revisionHistoryLimit"]
			if present != has || has && intValue(spec, "revisionHistoryLimit", -1) != intValue(config, "revisionHistoryLimit", -2) {
				t.Fatal("literal history limit lost", c.ID)
			}
		}
		limits[intValue(c.Scenario.InitialSpec, "revisionHistoryLimit", -1)]++
		model, _ := readModel(c.Scenario.Steps[0].Spec)
		cmodel, err := readModel(historyVersionSpec(c.Scenario.Steps[0].Spec, "C"))
		if err != nil || cmodel.Roles["frontend"].Entry != "C" || !reflect.DeepEqual(model.Roles["backend"], cmodel.Roles["backend"]) {
			t.Fatal("C must change only frontend", err)
		}
		if !strings.Contains(textValue(c.Scenario.Source, "initial"), "O={0,1,2}") {
			t.Fatal("sparse fixture silently replaced")
		}
		c.Scenario.Steps[0].Action = "update"
		if c.validateScenario() == nil {
			t.Fatal("plain rollout substituted for fault sequence")
		}
	}
	if !reflect.DeepEqual(limits, map[int]int{0: 18, 1: 6, -1: 6}) {
		t.Fatal("history limit matrix", limits)
	}
}

func TestHistoryReferenceMissingDuringTerminationIsFailureButGonePodIsNot(t *testing.T) {
	for _, gone := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminating-live", true: "gone-between-reads"}[gone], func(t *testing.T) {
			l, objects := normalFixture(t, "RUN-143")
			p := normalTestPod("Role", "frontend", 0, "A", true, "entry")
			p.SetNamespace("history-test")
			now := metav1.Now()
			p.SetDeletionTimestamp(&now)
			var pod corev1.Pod
			if err := convertPod(p, &pod); err != nil {
				t.Fatal(err)
			}
			client := fake.NewSimpleClientset()
			client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
				return true, &corev1.PodList{Items: []corev1.Pod{pod}}, nil
			})
			if !gone {
				client.PrependReactor("get", "pods", func(clienttesting.Action) (bool, runtime.Object, error) { return true, &pod, nil })
			}
			e := &normalExecution{r: &Runner{kube: client}, o: &Observer{objects: objects}, l: l, namespace: "history-test", dir: t.TempDir()}
			err := e.verifyHistoryReferences(context.Background())
			if gone && err != nil || !gone && (err == nil || !strings.Contains(err.Error(), "LIVE_HISTORY_MISSING")) {
				t.Fatal("history reference classification", err)
			}
		})
	}
}

func TestHistoryReferenceRejectsVersionDisguisedByOldLabel(t *testing.T) {
	l, objects := normalFixture(t, "RUN-143")
	p := normalTestPod("Role", "frontend", 0, "B", true, "entry")
	p.SetNamespace("history-test")
	labels := p.GetLabels()
	labels["modelserving.volcano.sh/revision"] = "fixture-A"
	p.SetLabels(labels)
	var pod corev1.Pod
	if err := convertPod(p, &pod); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]interface{}{"data": mapValue(l.Model.Spec, "template")["roles"]})
	cr := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: "model-fixture-A", Namespace: pod.Namespace, UID: "history", OwnerReferences: pod.OwnerReferences}, Data: runtime.RawExtension{Raw: data}}
	client := fake.NewSimpleClientset(&pod, cr)
	e := &normalExecution{r: &Runner{kube: client}, o: &Observer{objects: objects}, l: l, namespace: pod.Namespace, dir: t.TempDir()}
	err := e.verifyHistoryReferences(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HISTORY_TEMPLATE_MISMATCH") {
		t.Fatal("latest B disguised as old A history accepted", err)
	}
}
