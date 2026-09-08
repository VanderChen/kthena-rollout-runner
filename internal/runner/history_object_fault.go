// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type historyObjectFault struct {
	Original *appsv1.ControllerRevision `json:"original"`
	Injected *appsv1.ControllerRevision `json:"injected,omitempty"`
	Kind     string                     `json:"kind"`
}

func historyFixtureData(cr *appsv1.ControllerRevision) (string, error) {
	var data interface{}
	if err := json.Unmarshal(cr.Data.Raw, &data); err != nil {
		return "", err
	}
	raw, err := json.Marshal(data)
	return string(raw), err
}

func invalidHistoryData(original *appsv1.ControllerRevision, kind string) (runtime.RawExtension, error) {
	switch kind {
	case "corrupt-data":
		return runtime.RawExtension{Raw: []byte(`{"data":"invalid-history-role-list"}`)}, nil
	case "missing-role":
		var data struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(original.Data.Raw, &data); err != nil {
			return runtime.RawExtension{}, err
		}
		roles := []map[string]interface{}{}
		for _, r := range data.Data {
			if textValue(r, "name") != "frontend" {
				roles = append(roles, r)
			}
		}
		raw, err := json.Marshal(map[string]interface{}{"data": roles})
		return runtime.RawExtension{Raw: raw}, err
	case "foreign-owner":
		return *original.Data.DeepCopy(), nil
	default:
		return runtime.RawExtension{}, fmt.Errorf("unknown historical object fault %q", kind)
	}
}

func (e *normalExecution) deleteHistoryFixture(ctx context.Context, cr *appsv1.ControllerRevision, prefix string) error {
	uid := cr.UID
	options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}
	sent := time.Now().UTC()
	err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Delete(ctx, cr.Name, options)
	if saveErr := writeJSON(filepath.Join(e.dir, prefix+"-delete.json"), map[string]interface{}{"name": cr.Name, "uid": uid, "options": options, "sent": sent, "received": time.Now().UTC(), "accepted": err == nil}); saveErr != nil {
		return saveErr
	}
	return err
}

func (e *normalExecution) installHistoryObjectFault(ctx context.Context, original *appsv1.ControllerRevision, kind, prefix string) (*historyObjectFault, error) {
	fixture := &historyObjectFault{Original: original, Kind: kind}
	if err := e.deleteHistoryFixture(ctx, original, prefix+"-original-history"); err != nil {
		return nil, err
	}
	if kind != "missing" {
		data, err := invalidHistoryData(original, kind)
		if err != nil {
			return nil, err
		}
		request := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: original.Name, Namespace: original.Namespace, Labels: original.Labels, Annotations: original.Annotations, OwnerReferences: original.OwnerReferences}, Revision: original.Revision, Data: data}
		if kind == "foreign-owner" {
			spec := cloneMap(e.c.Scenario.InitialSpec)
			spec["replicas"] = float64(0)
			other := objectForSpec(e.namespace, e.c.ID, spec)
			other.SetName("history-foreign-owner")
			created, err := e.r.dynamic.Resource(MSGVR).Namespace(e.namespace).Create(ctx, other, metav1.CreateOptions{})
			if err != nil {
				return nil, err
			}
			if err = saveYAML(filepath.Join(e.dir, prefix+"-foreign-owner.yaml"), created.Object); err != nil {
				return nil, err
			}
			controller, block := true, true
			request.OwnerReferences = []metav1.OwnerReference{{APIVersion: created.GetAPIVersion(), Kind: created.GetKind(), Name: created.GetName(), UID: created.GetUID(), Controller: &controller, BlockOwnerDeletion: &block}}
		}
		// Register only the API-assigned exact UID and injected immutable data while
		// holding observation delivery. No unrelated or later history is exempted.
		err = e.locked(func() error {
			created, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Create(ctx, request, metav1.CreateOptions{})
			if err != nil {
				return err
			}
			fixture.Injected = created
			value, err := historyFixtureData(created)
			if err != nil {
				return err
			}
			if e.l.HistoryFixtureData == nil {
				e.l.HistoryFixtureData = map[string]string{}
			}
			e.l.HistoryFixtureData[string(created.UID)] = value
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if err := writeJSON(filepath.Join(e.dir, prefix+"-history-object-fixture.json"), fixture); err != nil {
		return nil, err
	}
	current, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, original.Name, metav1.GetOptions{})
	if kind == "missing" {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("INCONCLUSIVE: historical absence not established: %v", err)
		}
	} else if err != nil || current.UID != fixture.Injected.UID || !reflect.DeepEqual(current.Data, fixture.Injected.Data) {
		return nil, fmt.Errorf("INCONCLUSIVE: exact invalid historical fixture not established: %v", err)
	}
	return fixture, nil
}

func (e *normalExecution) restoreHistoryObject(ctx context.Context, fixture *historyObjectFault, prefix string) (*appsv1.ControllerRevision, error) {
	original := fixture.Original
	current, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Get(ctx, original.Name, metav1.GetOptions{})
	if err == nil {
		if fixture.Injected != nil && current.UID == fixture.Injected.UID {
			if !reflect.DeepEqual(current.Data, fixture.Injected.Data) || !reflect.DeepEqual(current.OwnerReferences, fixture.Injected.OwnerReferences) {
				return nil, fmt.Errorf("HISTORY_FIXTURE_MUTATED: %s", current.Name)
			}
			if err = e.deleteHistoryFixture(ctx, current, prefix+"-invalid-history"); err != nil {
				return nil, err
			}
		} else {
			// Correct autonomous reconstruction is safe. Never delete a different UID
			// merely because it occupies the original fixture name.
			want, _ := historyFixtureData(original)
			got, _ := historyFixtureData(current)
			if got != want || !reflect.DeepEqual(current.OwnerReferences, original.OwnerReferences) {
				return nil, fmt.Errorf("HISTORY_UNEXPECTED_REPLACEMENT: %s", current.Name)
			}
			if err = saveYAML(filepath.Join(e.dir, prefix+"-autonomously-restored-history.yaml"), current); err != nil {
				return nil, err
			}
			return current, nil
		}
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}
	request := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: original.Name, Namespace: original.Namespace, Labels: original.Labels, Annotations: original.Annotations, OwnerReferences: original.OwnerReferences}, Revision: original.Revision, Data: *original.Data.DeepCopy()}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-restore-history-request.yaml"), request); err != nil {
		return nil, err
	}
	restored, err := e.r.kube.AppsV1().ControllerRevisions(e.namespace).Create(ctx, request, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	if err = saveYAML(filepath.Join(e.dir, prefix+"-restore-history-server.yaml"), restored); err != nil {
		return nil, err
	}
	return restored, nil
}
