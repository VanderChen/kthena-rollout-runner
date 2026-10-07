// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func replayRule() Rule {
	return Rule{ID: "replay", Namespace: "test", Resource: "pods", OwnerUID: "owner", UIDs: []string{"old-entry", "old-worker"}, Mode: "replay-deletion", Count: -1, DurationSeconds: 30}
}
func replayOrder() ReplayRequest {
	return ReplayRequest{RuleID: "replay", Order: []string{"old-worker", "old-entry", "old-worker", "old-entry"}}
}
func awaitOriginalReplayFrames(t *testing.T, s *Server) {
	t.Helper()
	eventually(t, func() bool {
		for _, stream := range state(t, s).ReplayStreams {
			if stream.RuleID == "replay" && len(stream.Forwarded) == 2 {
				return true
			}
		}
		return false
	})
}

func TestReplayCapturesActualDeletedFramesAndReversesBothOldUIDsOnce(t *testing.T) {
	s, origin, streams := watchFixture(t)
	response, upstream := connect(t, origin, streams)
	defer response.Body.Close()
	frames := readFrames(response)
	control(t, s, "POST", "/v1/rules", replayRule(), 201)
	control(t, s, "POST", "/v1/replay", replayOrder(), 409)
	for _, uid := range []string{"old-entry", "old-worker"} {
		upstream.events <- event("DELETED", "test", uid, true)
		if nextUID(t, frames) != uid {
			t.Fatal("original deletion was not forwarded")
		}
	}
	for _, uid := range []string{"new-entry", "new-worker"} {
		upstream.events <- event("ADDED", "test", uid, false)
		if nextUID(t, frames) != uid {
			t.Fatal("new UID did not flow normally")
		}
	}
	eventually(t, func() bool { return state(t, s).Rules[0].Hits == 2 })
	awaitOriginalReplayFrames(t, s)
	var receipt ReplayReceipt
	if err := json.Unmarshal(control(t, s, "POST", "/v1/replay", replayOrder(), 200), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Request == 0 || !reflect.DeepEqual(receipt.Order, replayOrder().Order) {
		t.Fatal("wrong actual stream replay acknowledgement", receipt)
	}
	for i, uid := range replayOrder().Order {
		if nextUID(t, frames) != uid || receipt.PayloadSHA256[i] != state(t, s).Rules[0].Captured[uid] {
			t.Fatal("replay changed old frame identity or payload")
		}
	}
	if r := state(t, s).Rules[0]; r.Replayed != 4 || len(r.Captured) != 2 || !r.ReplayRequested {
		t.Fatal("replay evidence incomplete", r)
	}
	control(t, s, "POST", "/v1/replay", replayOrder(), 409)
	upstream.events <- event("MODIFIED", "other", "unrelated", false)
	if nextUID(t, frames) != "unrelated" {
		t.Fatal("unrelated namespace did not flow")
	}
	control(t, s, "DELETE", "/v1/rules/replay", nil, 204)
	s.mu.Lock()
	cached, forwarded := len(s.replayFrames), 0
	for _, stream := range s.replayStreams {
		forwarded += len(stream.forwarded)
	}
	s.mu.Unlock()
	if cached != 0 || forwarded != 0 {
		t.Fatal("cleared raw replay payload or stream identities retained")
	}
	if len(state(t, s).Errors) != 0 {
		t.Fatal("unexpected replay facility error")
	}
}

func TestReplayCannotCaptureForeignUIDOrTreatModifiedAsDeleted(t *testing.T) {
	s, origin, streams := watchFixture(t)
	response, upstream := connect(t, origin, streams)
	defer response.Body.Close()
	frames := readFrames(response)
	control(t, s, "POST", "/v1/rules", replayRule(), 201)
	foreign := event("DELETED", "test", "old-entry", true)
	foreign["object"].(map[string]interface{})["metadata"].(map[string]interface{})["ownerReferences"] = []interface{}{map[string]interface{}{"uid": "foreign"}}
	for _, frame := range []map[string]interface{}{foreign, event("DELETED", "other", "old-entry", true), event("DELETED", "test", "new-entry", true), event("MODIFIED", "test", "old-entry", true)} {
		upstream.events <- frame
		nextUID(t, frames)
	}
	if r := state(t, s).Rules[0]; r.Hits != 0 || len(r.Captured) != 0 {
		t.Fatal("capture escaped explicit old deletion scope")
	}
	control(t, s, "POST", "/v1/replay", replayOrder(), 409)
	control(t, s, "DELETE", "/v1/rules/replay", nil, 204)
}

func TestReplayWaitsForOriginalHeldFramesToBeForwarded(t *testing.T) {
	s, origin, streams := watchFixture(t)
	response, upstream := connect(t, origin, streams)
	defer response.Body.Close()
	frames := readFrames(response)
	control(t, s, "POST", "/v1/rules", replayRule(), 201)
	control(t, s, "POST", "/v1/rules", Rule{ID: "pause", Namespace: "test", Resource: "pods", Mode: "hold", Count: -1, DurationSeconds: 30}, 201)
	for _, uid := range []string{"old-entry", "old-worker"} {
		upstream.events <- event("DELETED", "test", uid, true)
	}
	eventually(t, func() bool { return state(t, s).Rules[0].Hits == 2 })
	control(t, s, "POST", "/v1/replay", replayOrder(), 409)
	control(t, s, "DELETE", "/v1/rules/pause", nil, 204)
	for _, uid := range []string{"old-entry", "old-worker"} {
		if nextUID(t, frames) != uid {
			t.Fatal("original order changed")
		}
	}
	control(t, s, "POST", "/v1/replay", ReplayRequest{RuleID: "replay", Order: []string{"old-entry", "old-worker", "old-entry", "old-worker"}}, 409)
	awaitOriginalReplayFrames(t, s)
	control(t, s, "POST", "/v1/replay", replayOrder(), 200)
	for _, uid := range replayOrder().Order {
		if nextUID(t, frames) != uid {
			t.Fatal("wrong replay order")
		}
	}
	control(t, s, "DELETE", "/v1/rules/replay", nil, 204)
}

func TestReplayRequiresLiveUnambiguousOriginalStreamAndBoundedRule(t *testing.T) {
	for _, mutate := range []func(*Rule){
		func(r *Rule) { r.UIDs = []string{"old-entry"} },
		func(r *Rule) { r.UIDs = []string{"old-entry", "old-entry"} },
		func(r *Rule) { r.OwnerUID = "" },
		func(r *Rule) { r.Resource = "configmaps" },
		func(r *Rule) { r.Methods = []string{"DELETE"} },
		func(r *Rule) { r.Count = 4 },
		func(r *Rule) { r.DurationSeconds = 0 },
	} {
		r := replayRule()
		mutate(&r)
		if r.validate() == nil {
			t.Fatal("unbounded or incorrect replay scope accepted")
		}
	}
	s, origin, streams := watchFixture(t)
	response, upstream := connect(t, origin, streams)
	frames := readFrames(response)
	control(t, s, "POST", "/v1/rules", replayRule(), 201)
	for _, uid := range []string{"old-entry", "old-worker"} {
		upstream.events <- event("DELETED", "test", uid, true)
		nextUID(t, frames)
	}
	response.Body.Close()
	eventually(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.replayStreams) == 0 })
	control(t, s, "POST", "/v1/replay", replayOrder(), 409)
	s.mu.Lock()
	s.rules[0].Expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	control(t, s, "POST", "/v1/replay", replayOrder(), 409)
	if state(t, s).Rules[0].EndReason != "expired" {
		t.Fatal("expired rule remained live")
	}
}

func TestReplayRejectsAmbiguousOriginalStreams(t *testing.T) {
	s, origin, streams := watchFixture(t)
	first, one := connect(t, origin, streams)
	defer first.Body.Close()
	second, two := connect(t, origin, streams)
	defer second.Body.Close()
	framesOne, framesTwo := readFrames(first), readFrames(second)
	control(t, s, "POST", "/v1/rules", replayRule(), 201)
	for _, uid := range []string{"old-entry", "old-worker"} {
		one.events <- event("DELETED", "test", uid, true)
		two.events <- event("DELETED", "test", uid, true)
		nextUID(t, framesOne)
		nextUID(t, framesTwo)
	}
	eventually(t, func() bool {
		candidates := 0
		for _, stream := range state(t, s).ReplayStreams {
			if len(stream.Forwarded) == 2 {
				candidates++
			}
		}
		return candidates == 2
	})
	control(t, s, "POST", "/v1/replay", replayOrder(), 409)
	if r := state(t, s).Rules[0]; r.ReplayRequested || r.Hits != 2 {
		t.Fatal("ambiguous request consumed replay or duplicated captures")
	}
	second.Body.Close()
	eventually(t, func() bool { return len(state(t, s).ReplayStreams) == 1 })
	control(t, s, "POST", "/v1/replay", replayOrder(), 200)
	for _, uid := range replayOrder().Order {
		if nextUID(t, framesOne) != uid {
			t.Fatal("wrong surviving stream replay")
		}
	}
	control(t, s, "DELETE", "/v1/rules/replay", nil, 204)
}

func TestReplaySelectsExactControllerWatch(t *testing.T) {
	s, origin, streams := watchFixture(t)
	unrelated, other := connect(t, origin, streams)
	defer unrelated.Body.Close()
	const selector = "modelserving.volcano.sh/group-name"
	selected, err := http.Get(origin + "/api/v1/pods?watch=true&labelSelector=" + url.QueryEscape(selector))
	if err != nil {
		t.Fatal(err)
	}
	defer selected.Body.Close()
	current := <-streams
	ordinaryFrames, selectedFrames := readFrames(unrelated), readFrames(selected)
	rule := replayRule()
	rule.LabelSelector = selector
	control(t, s, "POST", "/v1/rules", rule, 201)
	for _, uid := range []string{"old-entry", "old-worker"} {
		other.events <- event("DELETED", "test", uid, true)
		nextUID(t, ordinaryFrames)
	}
	if state(t, s).Rules[0].Hits != 0 {
		t.Fatal("unrelated controller stream was captured")
	}
	for _, uid := range []string{"old-entry", "old-worker"} {
		current.events <- event("DELETED", "test", uid, true)
		nextUID(t, selectedFrames)
	}
	awaitOriginalReplayFrames(t, s)
	control(t, s, "POST", "/v1/replay", replayOrder(), 200)
	for _, uid := range replayOrder().Order {
		if nextUID(t, selectedFrames) != uid {
			t.Fatal("wrong replay identity")
		}
	}
	other.events <- event("ADDED", "test", "ordinary", false)
	if nextUID(t, ordinaryFrames) != "ordinary" {
		t.Fatal("replay leaked into unrelated controller")
	}
	control(t, s, "DELETE", "/v1/rules/replay", nil, 204)
}
