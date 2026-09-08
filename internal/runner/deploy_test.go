// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/yaml"
)

// Exercise the real startup path with checked-in Job arguments. A missing
// kubeconfig stops before any API call, after case and run-ID validation.
func TestAddendumJobsReachClientConfiguration(t *testing.T) {
	for _, name := range []string{"normal-301-addendum.yaml", "normal-183-boundary-addendum.yaml", "normal-write-conflict-addendum.yaml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../deploy", name))
			if err != nil {
				t.Fatal(err)
			}
			var job batchv1.Job
			if err := yaml.UnmarshalStrict(data, &job); err != nil {
				t.Fatal(err)
			}
			if len(job.Spec.Template.Spec.Containers) != 1 {
				t.Fatal("addendum must have one runner container")
			}
			var runID string
			for _, arg := range job.Spec.Template.Spec.Containers[0].Args {
				if strings.HasPrefix(arg, "--run-id=") {
					if runID != "" {
						t.Fatal("duplicate run ID argument")
					}
					runID = strings.TrimPrefix(arg, "--run-id=")
				}
			}
			if runID == "" || job.Name != "rollout-"+runID {
				t.Fatal("Job and explicit run ID differ")
			}
			missing := filepath.Join(t.TempDir(), "missing-kubeconfig")
			err = Run(context.Background(), Options{CaseDir: "../../cases/normal", RunID: runID, Kubeconfig: missing})
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("manifest did not pass startup validation: %v", err)
			}
		})
	}
}

func TestPreviousAddendumIDFailsBeforeClientConfiguration(t *testing.T) {
	err := Run(context.Background(), Options{CaseDir: "../../cases/normal", RunID: "normal-r10-183-boundary-addendum", Kubeconfig: filepath.Join(t.TempDir(), "missing-kubeconfig")})
	if err == nil || err.Error() != "invalid run ID" {
		t.Fatalf("expected original manifest startup failure, got %v", err)
	}
}
