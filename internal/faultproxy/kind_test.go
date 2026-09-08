// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// This opt-in integration test operates only in its two new, UID-owned fixture
// namespaces. The controller Deployment and webhook configuration are untouched.
func TestKindProxyProtocolAndRecovery(t *testing.T) {
	proxyURL := os.Getenv("RUNNER_PROXY_URL")
	if proxyURL == "" {
		t.Skip("requires an explicitly prepared Kind proxy")
	}
	id, output := os.Getenv("RUNNER_PROXY_PROBE_ID"), os.Getenv("RUNNER_PROXY_PROBE_OUTPUT")
	if id == "" || len(id) > 30 || strings.ContainsAny(id, "/ .") || output == "" {
		t.Fatal("explicit unique probe ID/output required")
	}
	if err := os.Mkdir(output, 0755); err != nil {
		t.Fatal(err)
	}
	proof := map[string]interface{}{"probeID": id, "started": time.Now().UTC(), "scope": "two isolated ConfigMap/Pod fixture namespaces; no Kthena changes"}
	save := func(name string, value interface{}) {
		t.Helper()
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(output, name), append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { proof["finished"] = time.Now().UTC(); proof["failed"] = t.Failed(); save("result.json", proof) }()
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("RUNNER_PROXY_KUBECONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	direct, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	seconds := int64(3600)
	token, err := direct.CoreV1().ServiceAccounts("rollout-runner").CreateToken(ctx, "rollout-runner", &authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: &seconds}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pc := rest.AnonymousClientConfig(config)
	pc.Host = proxyURL
	pc.TLSClientConfig = rest.TLSClientConfig{}
	pc.BearerToken = token.Status.Token
	if strings.HasPrefix(proxyURL, "https://") {
		// Exercise the same kubeconfig/tokenFile path used by production. HTTP
		// kubeconfig endpoints intentionally omit credentials in client-go.
		privateDir := t.TempDir()
		tokenPath := filepath.Join(privateDir, "token")
		if err := os.WriteFile(tokenPath, []byte(token.Status.Token), 0600); err != nil {
			t.Fatal(err)
		}
		kubeconfigPath := filepath.Join(privateDir, "config")
		kc := clientcmdapi.Config{
			Clusters:  map[string]*clientcmdapi.Cluster{"proxy": {Server: proxyURL, CertificateAuthority: os.Getenv("RUNNER_PROXY_CA_FILE")}},
			AuthInfos: map[string]*clientcmdapi.AuthInfo{"caller": {TokenFile: tokenPath}},
			Contexts:  map[string]*clientcmdapi.Context{"probe": {Cluster: "proxy", AuthInfo: "caller"}}, CurrentContext: "probe",
		}
		if err := clientcmd.WriteToFile(kc, kubeconfigPath); err != nil {
			t.Fatal(err)
		}
		pc, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil || pc.BearerTokenFile != tokenPath {
			t.Fatalf("production kubeconfig authentication path: %v", err)
		}
		proof["authentication"] = "HTTPS kubeconfig with CA verification and caller tokenFile"
	}
	pc.ContentType = "application/vnd.kubernetes.protobuf"
	pc.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	proxied, err := kubernetes.NewForConfig(pc)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := os.ReadFile(os.Getenv("RUNNER_PROXY_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	admin := func(method, path string, value interface{}, want int) []byte {
		t.Helper()
		body, _ := json.Marshal(value)
		req, err := http.NewRequestWithContext(ctx, method, os.Getenv("RUNNER_PROXY_CONTROL_URL")+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(secret)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("control %s: %d %s", path, resp.StatusCode, data)
		}
		return data
	}
	getState := func() State {
		var state State
		if err := json.Unmarshal(admin("GET", "/v1/state", nil, 200), &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	if len(getState().Errors) > 0 {
		t.Fatal("proxy has pre-existing facility errors")
	}
	var namespaces []*corev1.Namespace
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cleanupOK := true
		for _, suffix := range []string{"-error", "-drop", "-hold", "-replay", "-gc-list"} {
			req, err := http.NewRequestWithContext(cleanup, "DELETE", os.Getenv("RUNNER_PROXY_CONTROL_URL")+"/v1/rules/"+id+suffix, nil)
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(secret)))
				var response *http.Response
				response, err = http.DefaultClient.Do(req)
				if err == nil {
					response.Body.Close()
					if response.StatusCode != 204 && response.StatusCode != 404 {
						err = fmt.Errorf("status %d", response.StatusCode)
					}
				}
			}
			if err != nil {
				t.Errorf("rule cleanup %s: %v", suffix, err)
			}
		}
		for _, ns := range namespaces {
			cms, err := direct.CoreV1().ConfigMaps(ns.Name).List(cleanup, metav1.ListOptions{})
			if err == nil {
				for _, cm := range cms.Items {
					if len(cm.Finalizers) > 0 {
						cm.Finalizers = nil
						_, _ = direct.CoreV1().ConfigMaps(ns.Name).Update(cleanup, &cm, metav1.UpdateOptions{})
					}
				}
			}
			uid := ns.UID
			if err := direct.CoreV1().Namespaces().Delete(cleanup, ns.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
				cleanupOK = false
				t.Errorf("fixture cleanup: %v", err)
			}
			for {
				_, err := direct.CoreV1().Namespaces().Get(cleanup, ns.Name, metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					break
				}
				if err != nil {
					cleanupOK = false
					t.Errorf("cleanup observation: %v", err)
					break
				}
				select {
				case <-cleanup.Done():
					cleanupOK = false
					t.Error("fixture cleanup timeout")
				case <-time.After(100 * time.Millisecond):
					continue
				}
				break
			}
		}
		proof["namespacesDeleted"] = cleanupOK
	}()
	for _, suffix := range []string{"", "-other"} {
		ns, err := direct.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "rr-" + id + suffix, Labels: map[string]string{"rollout-runner/proxy-probe": id}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		namespaces = append(namespaces, ns)
	}
	save("namespaces.json", namespaces)
	ns, other := namespaces[0].Name, namespaces[1].Name
	makeCM := func(namespace, name string) *corev1.ConfigMap {
		return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"rollout-runner/proxy-probe": id}}, Data: map[string]string{"value": "original"}}
	}
	created, err := proxied.CoreV1().ConfigMaps(ns).Create(ctx, makeCM(ns, "probe"), metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := direct.CoreV1().ConfigMaps(ns).Get(ctx, "probe", metav1.GetOptions{})
	if err != nil || actual.UID != created.UID || actual.Data["value"] != "original" {
		t.Fatalf("transparent create mismatch: %v", err)
	}
	proof["transparentUID"] = created.UID
	fault := Rule{ID: id + "-error", Namespace: ns, Resource: "configmaps", Name: "fail-twice", Methods: []string{"POST"}, Mode: "error", StatusCode: 503, Count: 2, DurationSeconds: 30}
	// POST has no object name in the request URL; ownership/name matching is
	// checked from the native protobuf request body.
	admin("POST", "/v1/rules", fault, 201)
	for i := 0; i < 2; i++ {
		_, err = proxied.CoreV1().ConfigMaps(ns).Create(ctx, makeCM(ns, "fail-twice"), metav1.CreateOptions{})
		if !apierrors.IsServiceUnavailable(err) {
			t.Fatalf("expected actual503, got %v", err)
		}
	}
	if _, err = direct.CoreV1().ConfigMaps(ns).Get(ctx, "fail-twice", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("injected failed create reached API server")
	}
	recovered, err := proxied.CoreV1().ConfigMaps(ns).Create(ctx, makeCM(ns, "fail-twice"), metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	proof["recoveredCreateUID"] = recovered.UID
	listFault := Rule{ID: id + "-list", Namespace: ns, Resource: "configmaps", Methods: []string{"GET"}, CollectionOnly: true, Mode: "error", StatusCode: 503, Count: 1, DurationSeconds: 30}
	admin("POST", "/v1/rules", listFault, 201)
	if _, err = proxied.CoreV1().ConfigMaps(ns).Get(ctx, "probe", metav1.GetOptions{}); err != nil {
		t.Fatal("named Get consumed List error", err)
	}
	if _, err = proxied.CoreV1().ConfigMaps(other).List(ctx, metav1.ListOptions{}); err != nil {
		t.Fatal("other namespace consumed List error", err)
	}
	listWatch, err := proxied.CoreV1().ConfigMaps(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: recovered.ResourceVersion})
	if err != nil {
		t.Fatal("Watch consumed List error", err)
	}
	listWatch.Stop()
	if _, err = proxied.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{}); !apierrors.IsServiceUnavailable(err) {
		t.Fatal("actual List must fail once", err)
	}
	listed, err := proxied.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal("List must recover automatically", err)
	}
	foundOriginal := false
	for _, item := range listed.Items {
		if item.UID == created.UID {
			foundOriginal = true
		}
	}
	if !foundOriginal {
		t.Fatal("recovered List lost existing object")
	}
	proof["collectionOnlyFailureAndRecovery"] = map[string]interface{}{"resource": "configmaps", "namespace": ns, "originalUIDPreserved": created.UID, "getAndWatchAndOtherNamespaceUnaffected": true}
	if os.Getenv("RUNNER_PROXY_LIST_FILTER") == "true" {
		zero := int64(0)
		selector := "rollout-runner/proxy-probe=" + id
		rule := Rule{ID: id + "-gc-list", Namespace: ns, Resource: "configmaps", Methods: []string{"GET"}, CollectionOnly: true, Mode: "error", StatusCode: 503, Count: 1, DurationSeconds: 30, LabelSelector: selector, ListLimit: &zero}
		admin("POST", "/v1/rules", rule, 201)
		paged, err := proxied.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 500})
		if err != nil || len(paged.Items) == 0 {
			t.Fatal("paged observation consumed reference-list fault", err)
		}
		if _, err = proxied.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{LabelSelector: "rollout-runner/proxy-probe=other"}); err != nil {
			t.Fatal("different selector consumed reference-list fault", err)
		}
		if _, err = proxied.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{LabelSelector: selector}); !apierrors.IsServiceUnavailable(err) {
			t.Fatal("exact actual unpaged List must fail once", err)
		}
		recovered, err := proxied.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil || len(recovered.Items) != len(paged.Items) {
			t.Fatal("filtered List did not recover automatically", err)
		}
		before, after := map[types.UID]bool{}, map[types.UID]bool{}
		for _, item := range paged.Items {
			before[item.UID] = true
		}
		for _, item := range recovered.Items {
			after[item.UID] = true
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("filtered List lost original live identities")
		}
		save("gc-list-paged.json", paged)
		save("gc-list-recovered.json", recovered)
		proof["exactListFilter"] = map[string]interface{}{"selector": selector, "pagedLimit": 500, "failedLimit": 0, "originalUIDs": before, "recoveredUIDs": after}
	}
	finalized := makeCM(ns, "old")
	finalized.Finalizers = []string{"rollout-runner/proxy-probe"}
	old, err := direct.CoreV1().ConfigMaps(ns).Create(ctx, finalized, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	selector := "rollout-runner/proxy-probe=" + id
	list, err := direct.CoreV1().ConfigMaps(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatal(err)
	}
	w, err := proxied.CoreV1().ConfigMaps(metav1.NamespaceAll).Watch(ctx, metav1.ListOptions{LabelSelector: selector, ResourceVersion: list.ResourceVersion, AllowWatchBookmarks: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	truth, err := direct.CoreV1().ConfigMaps(metav1.NamespaceAll).Watch(ctx, metav1.ListOptions{LabelSelector: selector, ResourceVersion: list.ResourceVersion, AllowWatchBookmarks: true})
	if err != nil {
		t.Fatal(err)
	}
	defer truth.Stop()
	next := func(stream watch.Interface, match func(watch.Event, *corev1.ConfigMap) bool) []map[string]interface{} {
		t.Helper()
		var events []map[string]interface{}
		for {
			select {
			case <-ctx.Done():
				t.Fatal("actual Watch proof timed out")
			case e, ok := <-stream.ResultChan():
				if !ok {
					t.Fatal("actual Watch closed")
				}
				if e.Type == watch.Error {
					t.Fatal("actual Watch error")
				}
				cm, ok := e.Object.(*corev1.ConfigMap)
				if !ok {
					continue
				}
				events = append(events, map[string]interface{}{"type": e.Type, "object": cm})
				if match(e, cm) {
					return events
				}
			}
		}
	}
	drop := Rule{ID: id + "-drop", Namespace: ns, Resource: "configmaps", UIDs: []string{string(old.UID)}, Mode: "drop-deletion", Count: -1, DurationSeconds: 30}
	admin("POST", "/v1/rules", drop, 201)
	if err = direct.CoreV1().ConfigMaps(ns).Delete(ctx, "old", metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &old.UID}}); err != nil {
		t.Fatal(err)
	}
	terminating, err := direct.CoreV1().ConfigMaps(ns).Get(ctx, "old", metav1.GetOptions{})
	if err != nil || terminating.DeletionTimestamp == nil {
		t.Fatalf("actual deletion intent absent: %v", err)
	}
	terminating.Finalizers = nil
	if _, err = direct.CoreV1().ConfigMaps(ns).Update(ctx, terminating, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	replacement, err := direct.CoreV1().ConfigMaps(ns).Create(ctx, makeCM(ns, "old"), metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.UID == old.UID {
		t.Fatal("fixture did not replace UID")
	}
	seenTruth := next(truth, func(e watch.Event, cm *corev1.ConfigMap) bool {
		return e.Type == watch.Added && cm.UID == replacement.UID
	})
	save("actual-delete-watch.json", seenTruth)
	var sawDT, sawDeleted bool
	for _, row := range seenTruth {
		cm := row["object"].(*corev1.ConfigMap)
		if cm.UID == old.UID {
			sawDT = sawDT || cm.DeletionTimestamp != nil
			sawDeleted = sawDeleted || row["type"] == watch.Deleted
		}
	}
	if !sawDT || !sawDeleted {
		t.Fatal("direct observer did not prove both deletion notifications")
	}
	seenProxy := next(w, func(e watch.Event, cm *corev1.ConfigMap) bool {
		if cm.UID == old.UID && (cm.DeletionTimestamp != nil || e.Type == watch.Deleted) {
			t.Fatal("old deletion leaked through active rule")
		}
		return e.Type == watch.Added && cm.UID == replacement.UID
	})
	save("proxied-delete-watch.json", seenProxy)
	admin("DELETE", "/v1/rules/"+drop.ID, nil, 204)
	proof["oldUID"], proof["replacementUID"] = old.UID, replacement.UID
	hold := Rule{ID: id + "-hold", Namespace: ns, Resource: "configmaps", Mode: "hold", Count: -1, DurationSeconds: 30}
	admin("POST", "/v1/rules", hold, 201)
	replacement.Data["value"] = "paused-change"
	changed, err := direct.CoreV1().ConfigMaps(ns).Update(ctx, replacement, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bystander, err := direct.CoreV1().ConfigMaps(other).Create(ctx, makeCM(other, "bystander"), metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seenOther := next(w, func(e watch.Event, cm *corev1.ConfigMap) bool {
		if cm.UID == changed.UID && cm.ResourceVersion == changed.ResourceVersion {
			t.Fatal("paused change leaked")
		}
		return e.Type == watch.Added && cm.UID == bystander.UID
	})
	save("bystander-watch.json", seenOther)
	getDone := make(chan *corev1.ConfigMap, 1)
	getError := make(chan error, 1)
	go func() {
		cm, err := proxied.CoreV1().ConfigMaps(ns).Get(ctx, "old", metav1.GetOptions{})
		if err != nil {
			getError <- err
		} else {
			getDone <- cm
		}
	}()
	for {
		st := getState()
		hits := 0
		for _, r := range st.Rules {
			if r.ID == hold.ID {
				hits = r.Hits
			}
		}
		if hits >= 2 {
			break
		}
		select {
		case err := <-getError:
			t.Fatal(err)
		case <-getDone:
			t.Fatal("held request reached API server")
		case <-ctx.Done():
			t.Fatal("hold hit proof absent")
		case <-time.After(10 * time.Millisecond):
		}
	}
	admin("DELETE", "/v1/rules/"+hold.ID, nil, 204)
	seenReleased := next(w, func(e watch.Event, cm *corev1.ConfigMap) bool {
		return cm.UID == changed.UID && cm.ResourceVersion == changed.ResourceVersion
	})
	save("released-watch.json", seenReleased)
	select {
	case cm := <-getDone:
		if cm.UID != changed.UID || cm.Data["value"] != "paused-change" {
			t.Fatal("held request response differs")
		}
	case err := <-getError:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("held request did not resume")
	}
	if os.Getenv("RUNNER_PROXY_REPLAY") == "true" {
		proof["deletionReplay"] = verifyKindDeletionReplay(t, ctx, direct, proxied, id, ns, other, admin, getState, save)
	}
	st := getState()
	save("proxy-state.json", st)
	if len(st.Errors) > 0 {
		t.Fatal("proxy reports incomplete evidence")
	}
	for _, r := range st.Rules {
		if !strings.HasPrefix(r.ID, id+"-") {
			continue
		}
		if r.Active || r.Hits < 1 {
			t.Fatalf("fault never hit or still active: %s", r.ID)
		}
	}
	proof["assertions"] = []string{"native protobuf create reaches real API with caller authentication", "two actual503 responses suppress creates, third creates real UID", "direct Watch proves deletionTimestamp and DELETED", "proxied Watch drops exact old UID while forwarding same-name new UID", "held namespace event does not block bystander namespace", "explicit resume releases original event and HTTP GET"}
	proof["sourceWatchRV"] = list.ResourceVersion
	fmt.Printf("PROXY_KIND_PROBE %s PASS oldUID=%s newUID=%s\n", id, old.UID, replacement.UID)
}
