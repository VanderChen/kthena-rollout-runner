// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

const controlToken = "isolated-unit-test-control-token-32-bytes"

func fixture(t *testing.T, handler http.Handler) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	upstream := httptest.NewServer(handler)
	origin, _ := url.Parse(upstream.URL)
	journal := &bytes.Buffer{}
	proxy, err := New(origin, http.DefaultTransport, controlToken, journal)
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(proxy)
	t.Cleanup(func() { proxy.Close(); api.CloseClientConnections(); api.Close(); upstream.Close() })
	return proxy, api, journal
}

func control(t *testing.T, s *Server, method, path string, value interface{}, code int) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+controlToken)
	response := httptest.NewRecorder()
	s.ControlHandler().ServeHTTP(response, req)
	if response.Code != code {
		t.Fatalf("control %s %s: %d %s", method, path, response.Code, response.Body.String())
	}
	return response.Body.Bytes()
}
func state(t *testing.T, s *Server) State {
	t.Helper()
	var value State
	if err := json.Unmarshal(control(t, s, "GET", "/v1/state", nil, 200), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("bounded condition did not occur")
}

func TestTransparentRequestsPreserveAuthenticationAndBodyWithoutLoggingSecrets(t *testing.T) {
	const secret = "not-to-be-recorded-client-token"
	body := []byte(`{"metadata":{"name":"p"},"data":{"private":"not-to-be-recorded-body"}}`)
	s, api, journal := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer "+secret || r.Method != "POST" || r.URL.RawQuery != "fieldManager=test" || !bytes.Equal(data, body) {
			t.Error("transparent request changed")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write(body)
	}))
	req, _ := http.NewRequest("POST", api.URL+"/api/v1/namespaces/test/configmaps?fieldManager=test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	got, _ := io.ReadAll(response.Body)
	if response.StatusCode != 201 || !bytes.Equal(got, body) {
		t.Fatal("transparent response changed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.Contains(journal.String(), secret) || strings.Contains(journal.String(), "not-to-be-recorded-body") || strings.Contains(journal.String(), controlToken) {
		t.Fatal("journal exposed authentication or object body")
	}
}

func TestFiniteErrorOnlyMatchesExactNamespaceAndCreateOwnerIncludingProtobuf(t *testing.T) {
	var upstreamCalls atomic.Int32
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { upstreamCalls.Add(1); w.WriteHeader(201) }))
	rule := Rule{ID: "create", Namespace: "test", Resource: "pods", Name: "p", Methods: []string{"POST"}, OwnerUID: "model-uid", Mode: "error", StatusCode: 503, Count: 2, DurationSeconds: 30}
	control(t, s, "POST", "/v1/rules", rule, 201)
	// Collection POST URLs have no name: both predicates must be evaluated from
	// the body, without spending the finite fault on neighboring objects.
	for _, body := range []string{
		`{"metadata":{"name":"other","ownerReferences":[{"uid":"model-uid"}]}}`,
		`{"metadata":{"name":"p","ownerReferences":[{"uid":"other-owner"}]}}`,
	} {
		response, err := http.Post(api.URL+"/api/v1/namespaces/test/pods", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 201 || state(t, s).Rules[0].Hits != 0 {
			t.Fatal("unrelated object consumed selected create fault")
		}
	}
	pod := &corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{Name: "p", OwnerReferences: []metav1.OwnerReference{{UID: "model-uid"}}}}
	var encoded []byte
	for _, info := range scheme.Codecs.SupportedMediaTypes() {
		if info.MediaType == "application/vnd.kubernetes.protobuf" {
			var err error
			encoded, err = runtime.Encode(scheme.Codecs.EncoderForVersion(info.Serializer, corev1.SchemeGroupVersion), pod)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(encoded) == 0 {
		t.Fatal("protobuf codec absent")
	}
	for i, ns := range []string{"other", "test", "test", "test"} {
		req, _ := http.NewRequest("POST", api.URL+"/api/v1/namespaces/"+ns+"/pods", bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/vnd.kubernetes.protobuf")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		want := 503
		if i == 0 || i == 3 {
			want = 201
		}
		if response.StatusCode != want {
			t.Fatalf("request %d: %d %s", i, response.StatusCode, data)
		}
		if want == 503 {
			var status metav1.Status
			if json.Unmarshal(data, &status) != nil || status.Code != 503 || status.Kind != "Status" || status.Reason != metav1.StatusReasonServiceUnavailable {
				t.Fatal("injected response is not API Status")
			}
		}
	}
	st := state(t, s)
	if upstreamCalls.Load() != 4 || st.Rules[0].Hits != 2 || st.Rules[0].Active || st.Rules[0].EndReason != "count-exhausted" || len(st.Errors) != 0 {
		t.Fatalf("wrong finite fault state: %+v", st)
	}
	control(t, s, "POST", "/v1/rules", rule, 409)
}

func TestHoldPreventsRequestUntilExplicitReleaseAndPreservesGlobalReconnectGuard(t *testing.T) {
	var calls atomic.Int32
	s, api, journal := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	rule := Rule{ID: "pause", Namespace: "test", Resource: "pods", Mode: "hold", Count: -1, DurationSeconds: 30}
	control(t, s, "POST", "/v1/rules", rule, 201)
	done := make(chan error, 1)
	go func() {
		response, err := http.Get(api.URL + "/api/v1/pods")
		if err == nil {
			response.Body.Close()
		}
		done <- err
	}()
	eventually(t, func() bool { return state(t, s).Rules[0].Hits == 1 })
	if calls.Load() != 0 {
		t.Fatal("held global reconnect reached upstream")
	}
	response, err := http.Get(api.URL + "/api/v1/namespaces/other/pods")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if calls.Load() != 1 {
		t.Fatal("unrelated namespace was blocked")
	}
	control(t, s, "DELETE", "/v1/rules/pause", nil, 204)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("held request not released")
	}
	if calls.Load() != 2 || state(t, s).Rules[0].Released != 1 {
		t.Fatal("wrong release evidence")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.Contains(journal.String(), "global-list-watch-reconnect-guard") {
		t.Fatal("global reconnect effect was not recorded")
	}
}

func TestRuleExpiryReleasesHoldWithoutSilentlyRenewingIt(t *testing.T) {
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	control(t, s, "POST", "/v1/rules", Rule{ID: "expiry", Namespace: "test", Resource: "pods", Mode: "hold", Count: -1, DurationSeconds: 1}, 201)
	response, err := http.Get(api.URL + "/api/v1/namespaces/test/pods")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	rule := state(t, s).Rules[0]
	if rule.Active || rule.Hits != 1 || rule.Released != 1 || rule.EndReason != "expired" {
		t.Fatalf("wrong expiry state: %+v", rule)
	}
}

func TestControlRejectsUnauthenticatedAndUnscopedRules(t *testing.T) {
	s, _, _ := fixture(t, http.NotFoundHandler())
	w := httptest.NewRecorder()
	s.ControlHandler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/state", nil))
	if w.Code != 401 {
		t.Fatal("control lacks authentication")
	}
	for _, rule := range []Rule{
		{ID: "global", Resource: "pods", Mode: "error", Methods: []string{"GET"}, Count: 1, DurationSeconds: 30, StatusCode: 503},
		{ID: "unbounded", Namespace: "test", Resource: "pods", Mode: "hold", Count: -1, DurationSeconds: 0},
		{ID: "drop-new", Namespace: "test", Resource: "pods", Mode: "drop-deletion", Count: -1, DurationSeconds: 30},
		{ID: "fake-initial", Namespace: "test", Resource: "pods", Mode: "error", Methods: []string{"GET"}, InitialSync: true, Count: 1, DurationSeconds: 30, StatusCode: 503},
	} {
		control(t, s, "POST", "/v1/rules", rule, 400)
	}
}
