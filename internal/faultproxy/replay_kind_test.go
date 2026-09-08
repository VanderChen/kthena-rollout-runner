// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

func verifyKindDeletionReplay(t *testing.T, ctx context.Context, direct, proxied kubernetes.Interface, id, ns, other string, admin func(string, string, interface{}, int) []byte, getState func() State, save func(string, interface{})) map[string]interface{} {
	t.Helper()
	parent, err := direct.CoreV1().ConfigMaps(ns).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "replay-owner"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	createPod := func(namespace, name string) *corev1.Pod {
		t.Helper()
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"rollout-runner/replay-probe": id}}, Spec: corev1.PodSpec{SchedulerName: "runner-proxy-probe-no-scheduler", Containers: []corev1.Container{{Name: "probe", Image: "busybox:1.36", Command: []string{"sleep", "3600"}}}}}
		if namespace == ns {
			p.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: parent.Name, UID: parent.UID}}
		}
		created, err := direct.CoreV1().Pods(namespace).Create(ctx, p, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	old := []*corev1.Pod{createPod(ns, "replay-entry"), createPod(ns, "replay-worker")}
	save("replay-original-pods.json", old)
	selector := "rollout-runner/replay-probe=" + id
	list, err := direct.CoreV1().Pods("").List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatal(err)
	}
	w, err := proxied.CoreV1().Pods("").Watch(ctx, metav1.ListOptions{LabelSelector: selector, ResourceVersion: list.ResourceVersion, AllowWatchBookmarks: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	events := make(chan watch.Event, 64)
	go func() {
		defer close(events)
		for ev := range w.ResultChan() {
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	var observed []map[string]interface{}
	defer func() { save("replay-native-watch.json", observed) }()
	next := func(predicate func(watch.Event, *corev1.Pod) bool) *corev1.Pod {
		t.Helper()
		for {
			select {
			case ev, ok := <-events:
				if !ok || ev.Type == watch.Error {
					t.Fatal("native Pod watch ended or errored")
				}
				if ev.Type == watch.Bookmark {
					continue
				}
				p, ok := ev.Object.(*corev1.Pod)
				if !ok {
					t.Fatal("native Watch did not decode a Pod")
				}
				observed = append(observed, map[string]interface{}{"received": time.Now().UTC(), "event": ev.Type, "pod": p})
				if predicate(ev, p) {
					return p
				}
			case <-ctx.Done():
				t.Fatal("native Pod watch witness timed out")
			}
		}
	}
	rule := Rule{ID: id + "-replay", Namespace: ns, Resource: "pods", OwnerUID: string(parent.UID), UIDs: []string{string(old[0].UID), string(old[1].UID)}, Mode: "replay-deletion", Count: -1, DurationSeconds: 60}
	admin("POST", "/v1/rules", rule, 201)
	var deleteProof []map[string]interface{}
	for _, p := range old {
		zero := int64(0)
		options := metav1.DeleteOptions{GracePeriodSeconds: &zero, Preconditions: &metav1.Preconditions{UID: &p.UID}}
		sent := time.Now().UTC()
		if err = direct.CoreV1().Pods(ns).Delete(ctx, p.Name, options); err != nil {
			t.Fatal(err)
		}
		deleteProof = append(deleteProof, map[string]interface{}{"name": p.Name, "uid": p.UID, "options": options, "sent": sent, "accepted": true})
		next(func(ev watch.Event, obj *corev1.Pod) bool { return ev.Type == watch.Deleted && obj.UID == p.UID })
	}
	save("replay-actual-deletes.json", deleteProof)
	newPods := []*corev1.Pod{createPod(ns, old[0].Name), createPod(ns, old[1].Name)}
	for i, p := range newPods {
		if p.UID == old[i].UID {
			t.Fatal("replacement unexpectedly retained old UID")
		}
		next(func(ev watch.Event, obj *corev1.Pod) bool { return ev.Type == watch.Added && obj.UID == p.UID })
	}
	save("replay-new-pods-before.json", newPods)
	for {
		st := getState()
		candidates := 0
		for _, stream := range st.ReplayStreams {
			if stream.RuleID == rule.ID && len(stream.Forwarded) == 2 {
				candidates++
			}
		}
		if candidates == 1 {
			save("replay-before.json", st)
			break
		}
		if candidates > 1 {
			t.Fatal("ambiguous actual Watch streams")
		}
		select {
		case <-ctx.Done():
			t.Fatal("original deletion forwarding not established")
		case <-time.After(10 * time.Millisecond):
		}
	}
	request := ReplayRequest{RuleID: rule.ID, Order: []string{string(old[1].UID), string(old[0].UID), string(old[1].UID), string(old[0].UID)}}
	save("replay-request.json", request)
	var receipt ReplayReceipt
	if err = json.Unmarshal(admin("POST", "/v1/replay", request, 200), &receipt); err != nil {
		t.Fatal(err)
	}
	save("replay-receipt.json", receipt)
	if !reflect.DeepEqual(receipt.Order, request.Order) {
		t.Fatal("replay acknowledgement identity/order mismatch")
	}
	for _, uid := range request.Order {
		next(func(ev watch.Event, p *corev1.Pod) bool {
			if ev.Type != watch.Deleted {
				return false
			}
			if string(p.UID) != uid {
				t.Fatal("native client received wrong old UID order or deleted a replacement")
			}
			return true
		})
	}
	for _, p := range newPods {
		current, err := direct.CoreV1().Pods(ns).Get(ctx, p.Name, metav1.GetOptions{})
		if err != nil || current.UID != p.UID || current.DeletionTimestamp != nil {
			t.Fatal("replay altered the actual replacement API object", err)
		}
	}
	admin("POST", "/v1/replay", request, 409)
	bystander := createPod(other, "replay-bystander")
	next(func(ev watch.Event, p *corev1.Pod) bool { return ev.Type == watch.Added && p.UID == bystander.UID })
	save("replay-bystander.json", bystander)
	st := getState()
	for _, r := range st.Rules {
		if r.ID == rule.ID && (r.Hits != 2 || r.Replayed != 4 || len(r.Captured) != 2) {
			t.Fatal("actual replay counters mismatch")
		}
	}
	save("replay-after.json", st)
	admin("DELETE", "/v1/rules/"+rule.ID, nil, 204)
	return map[string]interface{}{"scope": "two real old Pod deletions; same-name new Pods and unrelated namespace remain unchanged in direct API", "sourceWatchRV": list.ResourceVersion, "oldUIDs": rule.UIDs, "newUIDs": []string{string(newPods[0].UID), string(newPods[1].UID)}, "replayOrder": receipt.Order, "watchRequest": receipt.Request, "nativeReplayEvents": 4, "secondReplayRejected": true}
}
