// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"kthena.local/rollout-runner/internal/faultproxy"
)

//go:embed fixtures/history-collision-target.json
var observedCollisionTarget []byte

type collisionTarget struct {
	SourceArtifact       string                 `json:"sourceArtifact"`
	SourceArtifactSHA256 string                 `json:"sourceArtifactSHA256"`
	ControllerCommit     string                 `json:"controllerCommit"`
	ObservedCRUID        string                 `json:"observedCRUID"`
	Name                 string                 `json:"name"`
	Data                 map[string]interface{} `json:"data"`
}

func verifyObservedCollisionTarget(spec map[string]interface{}) (collisionTarget, error) {
	var target collisionTarget
	if err := json.Unmarshal(observedCollisionTarget, &target); err != nil {
		return target, err
	}
	observed, _ := json.Marshal(target.Data["data"])
	actual, _ := json.Marshal(mapValue(spec, "template")["roles"])
	if target.ControllerCommit != ProductionCommit || target.SourceArtifactSHA256 == "" || target.ObservedCRUID == "" || !bytes.Equal(observed, actual) {
		return target, fmt.Errorf("INCONCLUSIVE: admitted target Role data differs from the actual observed collision fixture")
	}
	return target, nil
}

func (e *normalExecution) createHistoryCollision(ctx context.Context, request *appsv1.ControllerRevision, prefix string) (created *appsv1.ControllerRevision, err error) {
	if err = saveYAML(filepath.Join(e.dir, prefix+"-request.yaml"), request); err != nil {
		return nil, err
	}
	err = e.locked(func() error {
		var createErr error
		created, createErr = e.r.kube.AppsV1().ControllerRevisions(e.namespace).Create(ctx, request, metav1.CreateOptions{})
		if createErr != nil {
			return createErr
		}
		// A second insertion deliberately races a held native POST. Its one real
		// API-assigned UID may enter the no-new-history window; other UIDs may not.
		e.l.Revisions[string(created.UID)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, saveYAML(filepath.Join(e.dir, prefix+"-server.yaml"), created)
}

func nativeCollisionResponse(records []faultproxy.Record, id, namespace, name string) (faultproxy.Record, bool) {
	held := map[uint64]bool{}
	released := map[uint64]bool{}
	for _, record := range records {
		if record.RuleID == id && record.Action == "hold-request" && record.Namespace == namespace && record.Name == name && record.Resource == "controllerrevisions" && record.Method == "POST" {
			held[record.Request] = true
		}
		if record.RuleID == id && record.Action == "release-request" && held[record.Request] {
			released[record.Request] = true
		}
		if record.Action == "response" && record.Status == 409 && released[record.Request] {
			return record, true
		}
	}
	return faultproxy.Record{}, false
}

func (e *normalExecution) captureNativeCollision(ctx context.Context, id, name, prefix string) error {
	parsed, err := url.Parse(e.r.opt.FaultProxyAPI)
	if err != nil {
		return err
	}
	host := strings.Split(parsed.Hostname(), ".")
	if len(host) < 3 || host[2] != "svc" {
		return fmt.Errorf("INCONCLUSIVE: expected declared in-cluster proxy Service")
	}
	svc, err := e.r.kube.CoreV1().Services(host[1]).Get(ctx, host[0], metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(svc.Spec.Selector) == 0 {
		return fmt.Errorf("INCONCLUSIVE: proxy Service has no Pod selector")
	}
	pods, err := e.r.kube.CoreV1().Pods(svc.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String()})
	if err != nil {
		return err
	}
	if len(pods.Items) != 1 || pods.Items[0].DeletionTimestamp != nil {
		return fmt.Errorf("INCONCLUSIVE: proxy Pod identity ambiguous")
	}
	pod := pods.Items[0]
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Name != "proxy" {
		return fmt.Errorf("INCONCLUSIVE: unexpected proxy Pod layout")
	}
	req := e.r.kube.CoreV1().RESTClient().Post().Resource("pods").Namespace(pod.Namespace).Name(pod.Name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: "proxy", Command: []string{"tail", "-n", "20000", "/evidence/trace.jsonl"}, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(e.r.rest, "POST", req.URL())
	if err != nil {
		return err
	}
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err = executor.StreamWithContext(commandCtx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		return fmt.Errorf("INCONCLUSIVE: read proxy journal: %w", err)
	}
	if err = os.WriteFile(filepath.Join(e.dir, prefix+"-native-proxy-tail.jsonl"), stdout.Bytes(), 0644); err != nil {
		return err
	}
	var records []faultproxy.Record
	for _, line := range bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n")) {
		var record faultproxy.Record
		if err = json.Unmarshal(line, &record); err != nil {
			return fmt.Errorf("INCONCLUSIVE: incomplete proxy journal: %w", err)
		}
		records = append(records, record)
	}
	current, err := e.r.kube.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if current.UID != pod.UID || !reflect.DeepEqual(current.Status.ContainerStatuses, pod.Status.ContainerStatuses) {
		return fmt.Errorf("INCONCLUSIVE: proxy changed while reading native response")
	}
	response, ok := nativeCollisionResponse(records, id, e.namespace, name)
	if !ok {
		return fmt.Errorf("INCONCLUSIVE: held native ControllerRevision POST did not yield a proven upstream AlreadyExists")
	}
	return writeJSON(filepath.Join(e.dir, prefix+"-native-already-exists.json"), map[string]interface{}{"response": response, "proxyPodUID": pod.UID, "proxyServiceUID": svc.UID, "ruleID": id, "namespace": e.namespace, "name": name, "retainedTailRows": len(records)})
}

func (e *normalExecution) historyCollisionRecovery(ctx context.Context, p ScenarioStep, prefix string) (result error) {
	api := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace)
	before, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	next := objectForSpec(e.namespace, e.c.ID, e.materializeSpec(p.Spec))
	next.SetUID(before.GetUID())
	next.SetResourceVersion(before.GetResourceVersion())
	admitted, err := api.Update(ctx, next, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-B-dry-run-admitted.yaml"), admitted.Object); err != nil {
		return err
	}
	target, err := verifyObservedCollisionTarget(mapValue(admitted.Object, "spec"))
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-target-provenance.json"), target); err != nil {
		return err
	}
	current, err := api.Get(ctx, "model", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if current.GetGeneration() != before.GetGeneration() || !reflect.DeepEqual(current.Object["spec"], before.Object["spec"]) {
		return fmt.Errorf("INCONCLUSIVE: dry-run changed the actual A source")
	}
	oldHistory, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, "model-"+textValue(mapValue(before.Object, "status"), "currentRevision"), metav1.GetOptions{})
	if err != nil {
		return err
	}
	data := *oldHistory.Data.DeepCopy()
	request := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: target.Name, Namespace: e.namespace, Labels: map[string]string{"modelserving.volcano.sh/name": "model", "modelserving.volcano.sh/revision": strings.TrimPrefix(target.Name, "model-")}, OwnerReferences: oldHistory.OwnerReferences}, Revision: 1, Data: data}
	if e.c.ID == "RUN-534" {
		raw, err := json.Marshal(target.Data)
		if err != nil {
			return err
		}
		request.Data = runtime.RawExtension{Raw: raw}
		spec := cloneMap(e.c.Scenario.InitialSpec)
		spec["replicas"] = float64(0)
		other := objectForSpec(e.namespace, e.c.ID, spec)
		other.SetName("history-foreign-owner")
		foreign, err := api.Create(ctx, other, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		if err = saveYAML(filepath.Join(e.dir, prefix+"-foreign-owner.yaml"), foreign.Object); err != nil {
			return err
		}
		controlled, block := true, true
		request.OwnerReferences = []metav1.OwnerReference{{APIVersion: foreign.GetAPIVersion(), Kind: foreign.GetKind(), Name: foreign.GetName(), UID: foreign.GetUID(), Controller: &controlled, BlockOwnerDeletion: &block}}
	}
	first, err := e.createHistoryCollision(ctx, request, prefix+"-initial-collision")
	if err != nil {
		return err
	}
	if err = e.snapshot(prefix + "-source-initial-collision"); err != nil {
		return err
	}
	ready := true
	held := ScenarioStep{Name: "precreated-B-name-conflict-retains-source-A", Action: "update", Spec: p.Spec, Until: "conditions", Release: "none", HoldSeconds: 30, TimeoutSeconds: 90, Conditions: []ScenarioCondition{{Kind: "unit", Role: "frontend", Version: "A", Ready: &ready, Count: 3}}, Expect: ScenarioExpectation{NoReplacement: true, NoNewRevision: true}}
	if err = e.step(ctx, held); err != nil {
		return err
	}
	id := fmt.Sprintf("%s-%s-native-collision", e.r.opt.RunID, strings.ToLower(e.c.ID))
	cleared := false
	defer func() {
		if !cleared {
			result = errors.Join(result, e.resumeRecovery([]string{id}, prefix+"-cleanup"))
		}
	}()
	rule := faultproxy.Rule{ID: id, Namespace: e.namespace, Resource: "controllerrevisions", Name: target.Name, Methods: []string{"POST"}, Mode: "hold", Count: -1, DurationSeconds: 90}
	var installed faultproxy.RuleStatus
	if err = e.r.faultControl(ctx, "POST", "/v1/rules", rule, &installed); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(e.dir, prefix+"-post-hold-installed.json"), installed); err != nil {
		return err
	}
	if !reflect.DeepEqual(rule, installed.Rule) {
		return fmt.Errorf("INCONCLUSIVE: native collision POST rule changed")
	}
	if err = e.deleteHistoryFixture(ctx, first, prefix+"-first-collision"); err != nil {
		return err
	}
	if err = e.waitFaultState(ctx, prefix+"-actual-post-held", []string{id}, func(state faultproxy.State) bool {
		for _, r := range state.Rules {
			if r.ID == id {
				return r.Hits > 0
			}
		}
		return false
	}); err != nil {
		return err
	}
	second, err := e.createHistoryCollision(ctx, request, prefix+"-raced-collision")
	if err != nil {
		return err
	}
	if second.UID == first.UID {
		return fmt.Errorf("INCONCLUSIVE: collision insertion did not acquire a new UID")
	}
	if err = e.resumeRecovery([]string{id}, prefix+"-native-post"); err != nil {
		return err
	}
	cleared = true
	e.waitDeadline = time.Time{}
	held.Name = "native-AlreadyExists-conflict-retains-source-A"
	held.Action = "observe"
	if err = e.wait(ctx, held); err != nil {
		return err
	}
	if err = e.snapshot(prefix + "-native-conflict-boundary"); err != nil {
		return err
	}
	if err = e.captureNativeCollision(ctx, id, target.Name, prefix); err != nil {
		return err
	}
	controller, err := e.recoveryController(ctx)
	if err != nil {
		return err
	}
	logs, err := e.r.kube.CoreV1().Pods(controller.Namespace).GetLogs(controller.Name, &corev1.PodLogOptions{Timestamps: true}).DoRaw(ctx)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(e.dir, prefix+"-collision-controller.log"), logs, 0644); err != nil {
		return err
	}
	diagnostic := false
	for _, line := range strings.Split(string(logs), "\n") {
		if strings.Contains(line, e.namespace) && strings.Contains(line, target.Name) && (strings.Contains(line, "different template data") || strings.Contains(line, "not controlled")) {
			diagnostic = true
		}
	}
	if !diagnostic {
		return fmt.Errorf("INCONCLUSIVE: actual collision lacks scoped diagnostic")
	}
	live, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, second.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if live.UID != second.UID || !reflect.DeepEqual(live.Data, second.Data) || !reflect.DeepEqual(live.OwnerReferences, second.OwnerReferences) {
		return fmt.Errorf("HISTORY_COLLISION_OVERWRITTEN: %s", second.Name)
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-collision-immutable-before-clear.yaml"), live); err != nil {
		return err
	}
	if err = e.locked(func() error {
		return e.l.Transition(e.l.Model.Spec, "collision-cleared-automatic-B", p.Expect, e.o.objects)
	}); err != nil {
		return err
	}
	if err = e.deleteHistoryFixture(ctx, second, prefix+"-final-collision"); err != nil {
		return err
	}
	e.historyReferences = true
	e.waitDeadline = time.Time{}
	p.Name = "collision-cleared-automatic-B"
	return e.finishStep(ctx, p, prefix+"-final")
}
