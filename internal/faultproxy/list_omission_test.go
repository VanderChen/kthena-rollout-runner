// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func omissionRule() Rule {
	zero := int64(0)
	return Rule{ID: "stale", Namespace: "test", Resource: "controllerrevisions", Methods: []string{"GET"}, Mode: "omit-list-object", Count: -1, DurationSeconds: 30, CollectionOnly: true, LabelSelector: "modelserving.volcano.sh/name=model", ListLimit: &zero, OmitObject: &ListOmission{Name: "model-b", UID: "b", OwnerUID: "model-uid"}}
}

func TestListOmissionPreservesOtherObjectsAndStoredHistory(t *testing.T) {
	original := `{"apiVersion":"apps/v1","kind":"ControllerRevisionList","metadata":{"resourceVersion":"123","continue":""},"items":[{"metadata":{"namespace":"test","name":"model-a","uid":"a"},"data":{"keep":"A"}},{"metadata":{"namespace":"test","name":"model-b","uid":"b","ownerReferences":[{"uid":"model-uid","controller":true}]},"data":{"keep":"B"}}]}`
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, original)
	}))
	rule := omissionRule()
	control(t, s, "POST", "/v1/rules", rule, 201)
	for _, suffix := range []string{"/model-b", "?limit=500&labelSelector=modelserving.volcano.sh%2Fname%3Dmodel", "?labelSelector=another%3Dmodel"} {
		r, err := http.Get(api.URL + "/apis/apps/v1/namespaces/test/controllerrevisions" + suffix)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if string(body) != original || state(t, s).Rules[0].Hits != 0 {
			t.Fatal("unrelated read changed")
		}
	}
	url := api.URL + "/apis/apps/v1/namespaces/test/controllerrevisions?labelSelector=modelserving.volcano.sh%2Fname%3Dmodel"
	r, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	var actual, source map[string]interface{}
	json.Unmarshal(body, &actual)
	json.Unmarshal([]byte(original), &source)
	if len(actual["items"].([]interface{})) != 1 || !reflect.DeepEqual(actual["metadata"], source["metadata"]) || !reflect.DeepEqual(actual["items"].([]interface{})[0], source["items"].([]interface{})[0]) || state(t, s).Rules[0].Hits != 1 {
		t.Fatal("selected response omitted wrong data", string(body))
	}
	control(t, s, "DELETE", "/v1/rules/stale", nil, 204)
	r, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if string(body) != original {
		t.Fatal("stored history was changed")
	}
}

func TestListOmissionRequiresExactUIDAndControllerOwner(t *testing.T) {
	for _, metadata := range []string{
		`{"namespace":"test","name":"model-b","uid":"different","ownerReferences":[{"uid":"model-uid","controller":true}]}`,
		`{"namespace":"test","name":"model-b","uid":"b","ownerReferences":[{"uid":"other","controller":true}]}`,
		`{"namespace":"test","name":"model-b","uid":"b","ownerReferences":[{"uid":"model-uid","controller":false}]}`,
	} {
		s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"items":[{"metadata":`+metadata+`}]}`)
		}))
		control(t, s, "POST", "/v1/rules", omissionRule(), 201)
		r, err := http.Get(api.URL + "/apis/apps/v1/namespaces/test/controllerrevisions?labelSelector=modelserving.volcano.sh%2Fname%3Dmodel")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var list struct{ Items []json.RawMessage }
		json.Unmarshal(body, &list)
		if len(list.Items) != 1 || state(t, s).Rules[0].Hits != 0 {
			t.Fatal("neighboring identity consumed omission")
		}
	}
}

func TestListOmissionRejectsBroadOrUnboundedRules(t *testing.T) {
	for _, mutate := range []func(*Rule){
		func(r *Rule) { r.Namespace = "" }, func(r *Rule) { r.CollectionOnly = false }, func(r *Rule) { r.Resource = "pods" }, func(r *Rule) { r.OmitObject = nil }, func(r *Rule) { r.OmitObject.UID = "" }, func(r *Rule) { r.OmitObject.OwnerUID = "" }, func(r *Rule) { r.LabelSelector = "" }, func(r *Rule) { r.ListLimit = nil }, func(r *Rule) { *r.ListLimit = 500 }, func(r *Rule) { r.Methods = []string{"POST"} }, func(r *Rule) { r.Count = 1 }, func(r *Rule) { r.DurationSeconds = 0 }, func(r *Rule) { r.Mode = "error"; r.StatusCode = 503 },
	} {
		r := omissionRule()
		mutate(&r)
		if r.validate() == nil {
			t.Fatal("unsafe omission rule accepted", r)
		}
	}
}

func TestNewListRuleCannotRetroactivelyAlterInFlightNativeResponse(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s, api, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf")
		io.WriteString(w, "native-binary-response")
	}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		r, err := http.Get(api.URL + "/apis/apps/v1/namespaces/test/controllerrevisions?labelSelector=modelserving.volcano.sh%2Fname%3Dmodel")
		if err != nil {
			t.Error(err)
			return
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 || string(body) != "native-binary-response" {
			t.Error("preexisting request changed", r.StatusCode, string(body))
		}
	}()
	<-started
	control(t, s, "POST", "/v1/rules", omissionRule(), 201)
	close(release)
	<-done
	actual := state(t, s)
	if actual.Rules[0].Hits != 0 || len(actual.Errors) != 0 {
		t.Fatal("new rule consumed an earlier request", actual)
	}
}
