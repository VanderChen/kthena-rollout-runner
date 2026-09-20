// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

type cancelWatch struct{ events chan watch.Event }

func (w *cancelWatch) Stop()                          {}
func (w *cancelWatch) ResultChan() <-chan watch.Event { return w.events }

func TestObserverShutdownDoesNotTreatCanceledWatchAsGap(t *testing.T) {
	for i := 0; i < 40; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		journal, err := os.CreateTemp(t.TempDir(), "watch-")
		if err != nil {
			t.Fatal(err)
		}
		o := &Observer{journal: journal, encoder: json.NewEncoder(journal)}
		w := &cancelWatch{events: make(chan watch.Event, 1)}
		w.events <- watch.Event{Type: watch.Error, Object: &metav1.Status{Status: metav1.StatusFailure}}
		cancel()
		o.wg.Add(1)
		go o.stream(ctx, nil, "pods", "0", w)
		o.wg.Wait()
		if o.err != nil {
			t.Fatalf("intentional shutdown produced observation gap: %v", o.err)
		}
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
