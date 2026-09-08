// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type stream struct {
	events chan map[string]interface{}
	closed chan struct{}
}

func watchFixture(t *testing.T) (*Server, string, chan stream) {
	t.Helper()
	streams := make(chan stream, 4)
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("JSON watch negotiation missing")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		current := stream{events: make(chan map[string]interface{}, 16), closed: make(chan struct{})}
		streams <- current
		defer close(current.closed)
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-current.events:
				if err := json.NewEncoder(w).Encode(event); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
		}
	}))
	return s, api.URL, streams
}
func event(kind, namespace, uid string, deleting bool) map[string]interface{} {
	meta := map[string]interface{}{"name": "same-name", "namespace": namespace, "uid": uid, "ownerReferences": []interface{}{map[string]interface{}{"uid": "owner"}}}
	if deleting {
		meta["deletionTimestamp"] = "2026-09-08T00:00:00Z"
	}
	return map[string]interface{}{"type": kind, "object": map[string]interface{}{"apiVersion": "v1", "kind": "Pod", "metadata": meta}}
}
func readFrames(response *http.Response) <-chan map[string]interface{} {
	frames := make(chan map[string]interface{}, 16)
	go func() {
		defer close(frames)
		decoder := json.NewDecoder(response.Body)
		for {
			var frame map[string]interface{}
			if decoder.Decode(&frame) != nil {
				return
			}
			frames <- frame
		}
	}()
	return frames
}
func nextUID(t *testing.T, frames <-chan map[string]interface{}) string {
	t.Helper()
	select {
	case frame, ok := <-frames:
		if !ok {
			t.Fatal("watch closed before expected event")
		}
		return frame["object"].(map[string]interface{})["metadata"].(map[string]interface{})["uid"].(string)
	case <-time.After(3 * time.Second):
		t.Fatal("expected watch event absent")
	}
	return ""
}
func connect(t *testing.T, origin string, streams chan stream) (*http.Response, stream) {
	t.Helper()
	req, _ := http.NewRequest("GET", origin+"/api/v1/pods?watch=true", nil)
	req.Header.Set("Accept", "application/vnd.kubernetes.protobuf,application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		t.Fatalf("watch returned %d", response.StatusCode)
	}
	select {
	case current := <-streams:
		return response, current
	case <-time.After(3 * time.Second):
		t.Fatal("upstream stream missing")
	}
	return nil, stream{}
}

func TestPausedWatchRetainsOrderAndForwardsOtherNamespaces(t *testing.T) {
	s, origin, streams := watchFixture(t)
	response, current := connect(t, origin, streams)
	defer response.Body.Close()
	frames := readFrames(response)
	control(t, s, "POST", "/v1/rules", Rule{ID: "pause", Namespace: "test", Resource: "pods", Mode: "hold", Count: -1, DurationSeconds: 30}, 201)
	current.events <- event("ADDED", "test", "old", false)
	current.events <- event("MODIFIED", "test", "old", true)
	current.events <- event("ADDED", "other", "bystander", false)
	if nextUID(t, frames) != "bystander" {
		t.Fatal("paused namespace escaped or bystander was blocked")
	}
	control(t, s, "DELETE", "/v1/rules/pause", nil, 204)
	if nextUID(t, frames) != "old" || nextUID(t, frames) != "old" {
		t.Fatal("held events lost or reordered")
	}
	eventually(t, func() bool { return state(t, s).Rules[0].Released == 2 })
	if len(state(t, s).Errors) != 0 {
		t.Fatal("watch facility failed")
	}
}

func TestDropExactOldDeletionAcrossWatchReconnectsButNotSameNameNewUID(t *testing.T) {
	s, origin, streams := watchFixture(t)
	control(t, s, "POST", "/v1/rules", Rule{ID: "lost-delete", Namespace: "test", Resource: "pods", UIDs: []string{"old"}, OwnerUID: "owner", Mode: "drop-deletion", Count: -1, DurationSeconds: 30}, 201)
	for attempt := 0; attempt < 2; attempt++ {
		response, current := connect(t, origin, streams)
		frames := readFrames(response)
		current.events <- event("MODIFIED", "test", "old", true)
		current.events <- event("DELETED", "test", "old", true)
		current.events <- event("ADDED", "test", fmt.Sprint("new-", attempt), false)
		if nextUID(t, frames) != fmt.Sprint("new-", attempt) {
			t.Fatal("old deletion leaked or new UID was dropped")
		}
		response.Body.Close()
		select {
		case <-current.closed:
		case <-time.After(3 * time.Second):
			t.Fatal("upstream watch leaked after cancellation")
		}
	}
	rule := state(t, s).Rules[0]
	if rule.Hits != 4 || !rule.Active {
		t.Fatalf("reconnect lost fault rule: %+v", rule)
	}
	control(t, s, "DELETE", "/v1/rules/lost-delete", nil, 204)
	response, current := connect(t, origin, streams)
	defer response.Body.Close()
	frames := readFrames(response)
	current.events <- event("DELETED", "test", "old", true)
	if nextUID(t, frames) != "old" {
		t.Fatal("cleared rule still drops events")
	}
}

func TestCanceledHeldWatchIsExplicitlyIncomplete(t *testing.T) {
	s, origin, streams := watchFixture(t)
	response, current := connect(t, origin, streams)
	control(t, s, "POST", "/v1/rules", Rule{ID: "pause", Namespace: "test", Resource: "pods", Mode: "hold", Count: -1, DurationSeconds: 30}, 201)
	current.events <- event("ADDED", "test", "old", false)
	eventually(t, func() bool { return state(t, s).Rules[0].Hits == 1 })
	response.Body.Close()
	eventually(t, func() bool { return len(state(t, s).Errors) > 0 })
	control(t, s, "DELETE", "/v1/rules/pause", nil, 204)
}

func TestPausedQueueOverflowCannotPassSilently(t *testing.T) {
	s, origin, streams := watchFixture(t)
	s.maxQueue = 1
	response, current := connect(t, origin, streams)
	defer response.Body.Close()
	control(t, s, "POST", "/v1/rules", Rule{ID: "pause", Namespace: "test", Resource: "pods", Mode: "hold", Count: -1, DurationSeconds: 30}, 201)
	current.events <- event("ADDED", "test", "old", false)
	current.events <- event("DELETED", "test", "old", true)
	eventually(t, func() bool { return len(state(t, s).Errors) > 0 })
	control(t, s, "DELETE", "/v1/rules/pause", nil, 204)
}

func TestInitialSyncRuleDoesNotCatchOrdinaryExistingWatch(t *testing.T) {
	s, origin, streams := watchFixture(t)
	control(t, s, "POST", "/v1/rules", Rule{ID: "initial", Resource: "pods", Mode: "error", Methods: []string{"GET"}, InitialSync: true, Count: 3, DurationSeconds: 30, StatusCode: 503}, 201)
	for _, path := range []string{"/api/v1/pods", "/api/v1/pods?watch=true&sendInitialEvents=true"} {
		response, err := http.Get(origin + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 503 {
			t.Fatal("initial sync was not blocked")
		}
	}
	response, current := connect(t, origin, streams)
	defer response.Body.Close()
	frames := readFrames(response)
	current.events <- event("ADDED", "test", "ready", false)
	if nextUID(t, frames) != "ready" {
		t.Fatal("restored watch failed")
	}
	if rule := state(t, s).Rules[0]; rule.Hits != 2 || !rule.Active {
		t.Fatal("ordinary watch was not tested against an active initial-sync rule")
	}
	control(t, s, "DELETE", "/v1/rules/initial", nil, 204)
}
