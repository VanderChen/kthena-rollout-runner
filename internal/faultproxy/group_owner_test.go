// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

const groupPath = "/apis/workload.serving.volcano.sh/v1alpha1/namespaces/test/servinggroups"

func groupJSON(name, uid, owner string) string {
	return fmt.Sprintf(`{"kind":"ServingGroup","metadata":{"name":%q,"namespace":"test","uid":%q,"ownerReferences":[{"kind":"ModelServing","uid":%q,"controller":true}]}}`, name, uid, owner)
}

func podBody(name, ownerKind, ownerUID string) string {
	return fmt.Sprintf(`{"metadata":{"name":%q,"ownerReferences":[{"kind":%q,"name":"model-0","uid":%q,"controller":true}]}}`, name, ownerKind, ownerUID)
}

func postPod(t *testing.T, api, body string) int {
	t.Helper()
	response, err := http.Post(api+"/api/v1/namespaces/test/pods", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return response.StatusCode
}

func TestOwnerRuleMatchesThroughServingGroup(t *testing.T) {
	groups := func(known map[string][]string) groupOwners {
		return func(uid string) ([]string, bool) { owners, ok := known[uid]; return owners, ok }
	}
	index := groups(map[string][]string{"group-old": {"model-uid"}, "group-foreign": {"other-model"}, "group-ownerless": {}})
	rule := Rule{Namespace: "test", OwnerUID: "model-uid"}
	for _, c := range []struct {
		name   string
		object objectMeta
		groups groupOwners
		want   bool
	}{
		{"direct owner", objectMeta{Namespace: "test", OwnerReferences: []ownerMeta{{UID: "model-uid", Kind: "ModelServing"}}}, index, true},
		{"the ModelServing itself", objectMeta{Namespace: "test", UID: "model-uid"}, index, true},
		{"through its ServingGroup", objectMeta{Namespace: "test", OwnerReferences: []ownerMeta{{UID: "group-old", Kind: servingGroupKind}}}, index, true},
		{"ServingGroup of another ModelServing", objectMeta{Namespace: "test", OwnerReferences: []ownerMeta{{UID: "group-foreign", Kind: servingGroupKind}}}, index, false},
		{"ServingGroup without owner", objectMeta{Namespace: "test", OwnerReferences: []ownerMeta{{UID: "group-ownerless", Kind: servingGroupKind}}}, index, false},
		{"same-name ServingGroup never seen", objectMeta{Namespace: "test", OwnerReferences: []ownerMeta{{UID: "group-new", Kind: servingGroupKind}}}, index, false},
		{"other owner kind is not looked through", objectMeta{Namespace: "test", OwnerReferences: []ownerMeta{{UID: "group-old", Kind: "ReplicaSet"}}}, index, false},
		{"no index", objectMeta{Namespace: "test", OwnerReferences: []ownerMeta{{UID: "group-old", Kind: servingGroupKind}}}, nil, false},
		{"other namespace", objectMeta{Namespace: "other", OwnerReferences: []ownerMeta{{UID: "group-old", Kind: servingGroupKind}}}, index, false},
	} {
		if got := rule.matchesObject(c.object, c.groups); got != c.want {
			t.Errorf("%s: matched=%t want %t", c.name, got, c.want)
		}
	}
}

func TestPodCreateFaultFollowsServingGroupLearnedFromCreateAndList(t *testing.T) {
	var upstreamPods atomic.Int32
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == groupPath && r.Method == "POST":
			if r.Header.Get("Accept-Encoding") != "identity" {
				t.Error("ServingGroup create response could arrive compressed")
			}
			w.WriteHeader(201)
			_, _ = io.WriteString(w, groupJSON("model-0", "group-created", "model-uid"))
		case r.URL.Path == groupPath && r.Method == "GET":
			_, _ = io.WriteString(w, `{"kind":"ServingGroupList","items":[`+groupJSON("model-1", "group-listed", "model-uid")+`,`+groupJSON("model-0", "group-foreign", "other-model")+`]}`)
		default:
			upstreamPods.Add(1)
			w.WriteHeader(201)
		}
	}))
	rule := Rule{ID: "create", Namespace: "test", Resource: "pods", Methods: []string{"POST"}, OwnerUID: "model-uid", Mode: "error", StatusCode: 503, Count: 2, DurationSeconds: 30}
	control(t, s, "POST", "/v1/rules", rule, 201)
	created, err := http.Post(api.URL+groupPath, "application/json", strings.NewReader(`{"metadata":{"name":"model-0"}}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(created.Body)
	created.Body.Close()
	if created.StatusCode != 201 || string(body) != groupJSON("model-0", "group-created", "model-uid") {
		t.Fatalf("ServingGroup create response changed: %d %s", created.StatusCode, body)
	}
	listed, err := http.Get(api.URL + groupPath)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(listed.Body)
	listed.Body.Close()
	if !strings.Contains(string(body), "group-listed") {
		t.Fatalf("ServingGroup list response changed: %s", body)
	}
	// A Pod of a foreign ModelServing's group must not spend the finite fault.
	if code := postPod(t, api.URL, podBody("foreign", servingGroupKind, "group-foreign")); code != 201 || state(t, s).Rules[0].Hits != 0 {
		t.Fatalf("Pod of another ModelServing consumed the owner-selected fault: %d", code)
	}
	for _, group := range []string{"group-created", "group-listed"} {
		if code := postPod(t, api.URL, podBody("member", servingGroupKind, group)); code != 503 {
			t.Fatalf("Pod of ServingGroup %s was not selected through its ModelServing: %d", group, code)
		}
	}
	if final := state(t, s); final.Rules[0].Hits != 2 || final.Rules[0].Active || len(final.Errors) != 0 || upstreamPods.Load() != 1 {
		t.Fatalf("owner-selected fault accounting: %+v upstream=%d", final, upstreamPods.Load())
	}
}

func TestUnseenServingGroupOwnerIsAnEvidenceErrorNotASilentMiss(t *testing.T) {
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }))
	rule := Rule{ID: "create", Namespace: "test", Resource: "pods", Methods: []string{"POST"}, OwnerUID: "model-uid", Mode: "error", StatusCode: 503, Count: 1, DurationSeconds: 30}
	control(t, s, "POST", "/v1/rules", rule, 201)
	for i := 0; i < 2; i++ {
		if code := postPod(t, api.URL, podBody("member", servingGroupKind, "group-never-seen")); code != 201 {
			t.Fatalf("unresolved owner was matched by guess: %d", code)
		}
	}
	final := state(t, s)
	if final.Rules[0].Hits != 0 || len(final.Errors) != 1 || !strings.Contains(final.Errors[0], "group-never-seen") {
		t.Fatalf("unseen ServingGroup must latch exactly one evidence error: %+v", final)
	}
	// Without an owner-selected rule nothing needs resolving and nothing is reported.
	control(t, s, "DELETE", "/v1/rules/create", nil, 204)
	postPod(t, api.URL, podBody("member", servingGroupKind, "group-also-unseen"))
	if again := state(t, s); len(again.Errors) != 1 {
		t.Fatalf("unrelated traffic reported an owner gap: %v", again.Errors)
	}
}

func TestServingGroupWatchFramesTeachOwnersBeforeTheyAreHeld(t *testing.T) {
	send, release := make(chan struct{}), make(chan struct{})
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") != "true" {
			w.WriteHeader(201)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-send
		_, _ = io.WriteString(w, `{"type":"ADDED","object":`+groupJSON("model-0", "group-watched", "model-uid")+"}\n")
		w.(http.Flusher).Flush()
		<-release
	}))
	defer close(release)
	// The controller's watch exists before the "controller paused" fixture
	// holds ServingGroup events. A group created during the pause is decoded
	// and held; its owner must still be known, because the Pod rule below is
	// evaluated while the event waits.
	watch, err := http.Get(api.URL + groupPath + "?watch=true")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Body.Close()
	control(t, s, "POST", "/v1/rules", Rule{ID: "pause", Namespace: "test", Resource: "servinggroups", Mode: "hold", Count: -1, DurationSeconds: 30}, 201)
	control(t, s, "POST", "/v1/rules", Rule{ID: "create", Namespace: "test", Resource: "pods", Methods: []string{"POST"}, OwnerUID: "model-uid", Mode: "error", StatusCode: 503, Count: 1, DurationSeconds: 30}, 201)
	close(send)
	eventually(t, func() bool {
		for _, rule := range state(t, s).Rules {
			if rule.ID == "pause" && rule.Hits == 1 {
				return true
			}
		}
		return false
	})
	if code := postPod(t, api.URL, podBody("member", servingGroupKind, "group-watched")); code != 503 {
		t.Fatalf("held ServingGroup event did not teach the owner: %d", code)
	}
	control(t, s, "DELETE", "/v1/rules/pause", nil, 204)
	line, err := bufio.NewReader(watch.Body).ReadString('\n')
	if err != nil || !strings.Contains(line, "group-watched") {
		t.Fatalf("released ServingGroup event was not delivered unchanged: %q %v", line, err)
	}
	if final := state(t, s); len(final.Errors) != 0 {
		t.Fatalf("facility errors: %v", final.Errors)
	}
}

func TestUndecodableServingGroupResponseIsAnEvidenceError(t *testing.T) {
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf")
		_, _ = w.Write([]byte{0x6b, 0x38, 0x73, 0x00})
	}))
	response, err := http.Get(api.URL + groupPath)
	if err == nil {
		response.Body.Close()
	}
	if final := state(t, s); len(final.Errors) == 0 || !strings.Contains(final.Errors[0], "ServingGroup response") {
		t.Fatalf("an unreadable ServingGroup list must not leave the owner index silently incomplete: %v", final.Errors)
	}
}

// A controller may probe the ServingGroup resource at start-up with a list of
// one object before it becomes Ready. Holding that probe would stop the
// process instead of leaving its ServingGroup informer unsynced.
func TestInitialSyncRuleSkipsTheOneObjectProbe(t *testing.T) {
	rule := Rule{ID: "initial", Resource: "servinggroups", Mode: "hold", Methods: []string{"GET"}, InitialSync: true, Count: -1, DurationSeconds: 30}
	collection := "/apis/workload.serving.volcano.sh/v1alpha1/servinggroups"
	for _, test := range []struct {
		name, query string
		held        bool
	}{
		{name: "start-up probe", query: "?limit=1"},
		{name: "paged informer list", query: "?limit=500&resourceVersion=0", held: true},
		{name: "unpaged informer list", query: "", held: true},
		{name: "streaming informer sync", query: "?watch=true&sendInitialEvents=true", held: true},
		{name: "streaming sync limited to one object", query: "?watch=true&sendInitialEvents=true&limit=1", held: true},
		{name: "ordinary watch", query: "?watch=true"},
		{name: "namespaced list", query: "", held: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := collection + test.query
			if test.name == "namespaced list" {
				path = "/apis/workload.serving.volcano.sh/v1alpha1/namespaces/test/servinggroups"
			}
			request, err := http.NewRequest(http.MethodGet, "https://proxy"+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := rule.matchesRequest(metadata(request)); got != test.held {
				t.Fatalf("held = %t, want %t", got, test.held)
			}
		})
	}
}
