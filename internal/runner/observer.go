// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
)

var (
	MSGVR  = schema.GroupVersionResource{Group: "workload.serving.volcano.sh", Version: "v1alpha1", Resource: "modelservings"}
	PGGVR  = schema.GroupVersionResource{Group: "scheduling.volcano.sh", Version: "v1beta1", Resource: "podgroups"}
	PodGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	CRGVR  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "controllerrevisions"}
)

type Observation struct {
	Sequence int64                      `json:"sequence"`
	Received time.Time                  `json:"received"`
	Kind     string                     `json:"kind"`
	Event    string                     `json:"event"`
	Object   *unstructured.Unstructured `json:"object,omitempty"`
	Error    string                     `json:"error,omitempty"`
}
type Observer struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	objects map[string]map[string]*unstructured.Unstructured
	journal *os.File
	encoder *json.Encoder
	seq     int64
	err     error
	ledger  *Ledger
	normal  *NormalLedger
}

func NewObserver(ctx context.Context, client dynamic.Interface, ns, dir string) (*Observer, error) {
	return newObserver(ctx, client, ns, dir, false)
}

func newObserver(ctx context.Context, client dynamic.Interface, ns, dir string, plugins bool) (*Observer, error) {
	f, err := os.Create(filepath.Join(dir, "observations.jsonl"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	o := &Observer{cancel: cancel, objects: map[string]map[string]*unstructured.Unstructured{}, journal: f, encoder: json.NewEncoder(f)}
	resources := []schema.GroupVersionResource{PodGVR, MSGVR, PGGVR, CRGVR}
	if plugins {
		resources = append(resources, schema.GroupVersionResource{Version: "v1", Resource: "services"}, schema.GroupVersionResource{Version: "v1", Resource: "configmaps"})
	}
	for _, gvr := range resources {
		api := client.Resource(gvr).Namespace(ns)
		list, err := api.List(ctx, metav1.ListOptions{})
		if err != nil {
			o.Close()
			return nil, err
		}
		o.mu.Lock()
		o.objects[gvr.Resource] = map[string]*unstructured.Unstructured{}
		o.mu.Unlock()
		for i := range list.Items {
			o.accept(gvr.Resource, "LIST", &list.Items[i])
		}
		// Establish the watch from the exact List cursor before mutating workloads.
		w, err := api.Watch(ctx, metav1.ListOptions{ResourceVersion: list.GetResourceVersion(), AllowWatchBookmarks: true})
		if err != nil {
			o.Close()
			return nil, err
		}
		o.wg.Add(1)
		go o.stream(ctx, api, gvr.Resource, list.GetResourceVersion(), w)
	}
	return o, nil
}
func (o *Observer) stream(ctx context.Context, api dynamic.ResourceInterface, kind, rv string, w watch.Interface) {
	defer o.wg.Done()
	for {
	streamLoop:
		for {
			var ev watch.Event
			var ok bool
			select {
			case <-ctx.Done():
				w.Stop()
				return
			case ev, ok = <-w.ResultChan():
				if !ok {
					break streamLoop
				}
			}
			if ev.Type == watch.Error {
				o.broken(kind, fmt.Errorf("watch error: %v", apierrors.FromObject(ev.Object)))
				w.Stop()
				return
			}
			u, ok := ev.Object.(*unstructured.Unstructured)
			if !ok {
				o.broken(kind, fmt.Errorf("unexpected watch object %T", ev.Object))
				w.Stop()
				return
			}
			if u.GetResourceVersion() != "" {
				rv = u.GetResourceVersion()
			}
			if ev.Type != watch.Bookmark {
				o.accept(kind, string(ev.Type), u)
			}
		}
		w.Stop()
		if ctx.Err() != nil {
			return
		}
		// Reconnect only from the last cursor. Never relist over an unobserved gap.
		var err error
		w, err = api.Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
		if err != nil {
			o.broken(kind, fmt.Errorf("resume from cursor %s: %w", rv, err))
			return
		}
	}
}
func (o *Observer) broken(kind string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err == nil {
		o.err = fmt.Errorf("INCONCLUSIVE observation stream %s: %w", kind, err)
	}
	o.seq++
	if e := o.encoder.Encode(Observation{Sequence: o.seq, Received: time.Now().UTC(), Kind: kind, Event: "GAP", Error: err.Error()}); e != nil {
		o.err = e
	}
}
func (o *Observer) accept(kind, event string, u *unstructured.Unstructured) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	if err := o.encoder.Encode(Observation{Sequence: o.seq, Received: time.Now().UTC(), Kind: kind, Event: event, Object: u}); err != nil {
		o.err = err
	}
	if o.normal != nil {
		o.normal.Before(kind, event, u, o.objects)
	}
	if event == "DELETED" {
		delete(o.objects[kind], string(u.GetUID()))
	} else {
		o.objects[kind][string(u.GetUID())] = u.DeepCopy()
	}
	if o.ledger != nil {
		o.ledger.Observe(kind, event, u, o.objects["pods"])
	}
	if o.normal != nil {
		o.normal.After(kind, event, u, o.objects)
	}
}
func (o *Observer) Arm(c Case, uid string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	l, err := NewLedger(c, uid, o.objects["pods"], o.objects["podgroups"])
	if err != nil {
		return err
	}
	o.ledger = l
	return nil
}
func (o *Observer) Inspect(fn func(*Ledger, map[string]map[string]*unstructured.Unstructured) (bool, error)) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return false, o.err
	}
	if o.ledger != nil && len(o.ledger.Violations) > 0 {
		return false, fmt.Errorf("FAIL: %s", o.ledger.Violations[0])
	}
	return fn(o.ledger, o.objects)
}
func (o *Observer) Snapshot(path string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return saveYAML(path, o.objects)
}
func (o *Observer) Finalize() ([]Metrics, []int, []string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ledger == nil {
		return nil, nil, nil, o.err
	}
	l := o.ledger
	o.ledger = nil
	return l.History, l.StartSequence, l.Violations, o.err
}
func (o *Observer) Close() error {
	o.cancel()
	o.wg.Wait()
	return o.journal.Close()
}
