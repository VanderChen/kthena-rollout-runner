// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"net/http"
	"testing"
)

func TestGCListFaultIgnoresPagedObservationAndDifferentSelectors(t *testing.T) {
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
	}))
	zero := int64(0)
	rule := Rule{ID: "live-reference-list", Namespace: "test", Resource: "pods", Methods: []string{"GET"}, CollectionOnly: true, Mode: "error", StatusCode: 503, Count: 1, DurationSeconds: 30, LabelSelector: "modelserving.volcano.sh/name=model", ListLimit: &zero}
	control(t, s, "POST", "/v1/rules", rule, 201)
	selector := "labelSelector=modelserving.volcano.sh%2Fname%3Dmodel"
	for _, suffix := range []string{
		"/test/pods?" + selector + "&limit=500",
		"/other/pods?" + selector,
		"/test/pods?labelSelector=modelserving.volcano.sh%2Frole%3Dfrontend",
		"/test/pods?" + selector + "&watch=true",
		"/test/pods/name?" + selector,
		"/test/pods?" + selector + "&limit=bad",
		"/test/pods?" + selector + "&limit=-1",
	} {
		response, err := http.Get(api.URL + "/api/v1/namespaces" + suffix)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 || state(t, s).Rules[0].Hits != 0 {
			t.Fatal("non-GC-shaped request consumed fault", suffix)
		}
	}
	for _, expected := range []int{503, 200} {
		response, err := http.Get(api.URL + "/api/v1/namespaces/test/pods?" + selector)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != expected {
			t.Fatal("actual unpaged reference List fault/retry", response.StatusCode)
		}
	}
	st := state(t, s)
	if st.Rules[0].Hits != 1 || st.Rules[0].Active || st.Rules[0].EndReason != "count-exhausted" || len(st.Errors) != 0 {
		t.Fatal("finite List fault not exhausted cleanly", st)
	}
}

func TestListFiltersRequireExplicitCollectionRead(t *testing.T) {
	zero := int64(0)
	base := Rule{ID: "gc", Namespace: "test", Resource: "pods", Methods: []string{"GET"}, CollectionOnly: true, Mode: "error", StatusCode: 503, Count: 1, DurationSeconds: 30, ListLimit: &zero}
	for _, alter := range []func(*Rule){
		func(r *Rule) { r.CollectionOnly = false },
		func(r *Rule) { r.Methods = []string{"DELETE"} },
		func(r *Rule) { negative := int64(-1); r.ListLimit = &negative },
		func(r *Rule) { r.Name = "named" },
		func(r *Rule) { r.Mode = "hold"; r.Count = -1; r.StatusCode = 0 },
	} {
		r := base
		alter(&r)
		if r.validate() == nil {
			t.Fatal("ambiguous List filter accepted", r)
		}
	}
	for _, query := range []string{"", "?limit=0"} {
		r, _ := http.NewRequest("GET", "https://test/api/v1/namespaces/test/pods"+query, nil)
		if !base.matchesRequest(metadata(r)) {
			t.Fatal("omitted and zero limit must both match native unpaged List")
		}
	}
}
