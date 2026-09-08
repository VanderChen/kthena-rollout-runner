// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type scenarioWrite struct {
	Object, Request *unstructured.Unstructured
	Prefix          string
	Sent, Received  time.Time
}

// updateScenario retries optimistic concurrency conflicts without repeating the
// scenario transition. Every retry refreshes only resourceVersion, verifies the
// original object/spec, and rechecks the real trigger immediately before writing.
func updateScenario(ctx context.Context, api dynamic.ResourceInterface, current, desired *unstructured.Unstructured, dir, prefix string, before func(string) error) (*scenarioWrite, error) {
	latest := current.DeepCopy()
	for attempt := 1; attempt <= 5; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt > 1 {
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			var err error
			latest, err = api.Get(ctx, current.GetName(), metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			if latest.GetUID() != current.GetUID() || !reflect.DeepEqual(latest.Object["spec"], current.Object["spec"]) {
				return nil, fmt.Errorf("RUNNER_WRITE_PRECONDITION: ModelServing UID or spec changed during conflict retry")
			}
		}
		attemptPrefix := fmt.Sprintf("%s-write-%02d", prefix, attempt)
		if err := saveYAML(filepath.Join(dir, attemptPrefix+"-current.yaml"), latest.Object); err != nil {
			return nil, err
		}
		if before != nil {
			if err := before(attemptPrefix); err != nil {
				return nil, err
			}
		}
		request := desired.DeepCopy()
		request.SetResourceVersion(latest.GetResourceVersion())
		if err := saveYAML(filepath.Join(dir, attemptPrefix+"-request.yaml"), request.Object); err != nil {
			return nil, err
		}
		sent := time.Now().UTC()
		response, err := api.Update(ctx, request, metav1.UpdateOptions{})
		received := time.Now().UTC()
		record := map[string]interface{}{"attempt": attempt, "sent": sent, "received": received, "requestResourceVersion": request.GetResourceVersion(), "uid": request.GetUID()}
		if err != nil {
			record["error"] = err.Error()
			if status, ok := err.(apierrors.APIStatus); ok {
				record["status"] = status.Status()
			}
		} else {
			record["resourceVersion"] = response.GetResourceVersion()
			record["generation"] = response.GetGeneration()
		}
		if saveErr := writeJSON(filepath.Join(dir, attemptPrefix+"-receipt.json"), record); saveErr != nil {
			return nil, saveErr
		}
		if err == nil {
			if err = saveYAML(filepath.Join(dir, attemptPrefix+"-server.yaml"), response.Object); err != nil {
				return nil, err
			}
			return &scenarioWrite{Object: response, Request: request, Prefix: attemptPrefix, Sent: sent, Received: received}, nil
		}
		if !apierrors.IsConflict(err) || attempt == 5 {
			return nil, err
		}
	}
	panic("unreachable")
}
