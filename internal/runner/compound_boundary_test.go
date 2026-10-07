// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0
package runner

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestPartialScaleBoundariesPreserveDeclaredIntermediateSources(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "cases", "servinggroup-compound-v2"))
	if err != nil {
		t.Fatal(err)
	}
	var selected []string
	for _, c := range cases {
		if !needsPartialScaleBoundary(c) {
			continue
		}
		selected = append(selected, c.ID)
		ordinal, ok := partialScaleBoundary(c.Scenario.Steps[0])
		if !ok || ordinal != 4 {
			t.Fatalf("unexpected barrier %s: %d %t", c.ID, ordinal, ok)
		}
	}
	if !reflect.DeepEqual(selected, []string{"RUN-628", "RUN-633", "RUN-635", "RUN-637"}) {
		t.Fatalf("unexpected source changes: %v", selected)
	}
}
