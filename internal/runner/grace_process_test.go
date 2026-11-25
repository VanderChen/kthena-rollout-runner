// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"strings"
	"testing"
	"time"
)

func TestGraceRestartsInTheNewControllerProcess(t *testing.T) {
	fault := time.Date(2026, 11, 25, 5, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return fault.Add(time.Duration(seconds) * time.Second) }
	const grace = 60 * time.Second
	for _, c := range []struct {
		name                          string
		synced, recovered, observedTo int
		want                          string
	}{
		{"recreated one full grace after the new process synced", 55, 116, 116, ""},
		{"within tolerance of the restarted deadline", 55, 113, 113, ""},
		{"recreated at the old deadline: the fault start was carried over", 55, 61, 61, "GRACE_CARRIED_ACROSS_PROCESS"},
		{"recreated between the old and the restarted deadline", 55, 90, 90, "GRACE_CARRIED_ACROSS_PROCESS"},
		{"recreated as soon as the new process saw the fault", 45, 46, 46, "GRACE_NOT_RESTARTED"},
		{"recreated before the process change and before any grace", 55, 20, 20, "GRACE_IGNORED"},
		{"never recreated", 55, 0, 150, "GRACE_NEVER_RECOVERED"},
		{"recreated far too late", 55, 150, 150, "GRACE_NEVER_RECOVERED"},
		{"not watched long enough to say never", 55, 0, 120, "INCONCLUSIVE"},
		{"process change after the first grace window proves nothing", 59, 120, 120, "INCONCLUSIVE"},
		{"new process synced before the fault", -5, 55, 55, "INCONCLUSIVE"},
	} {
		recovered := time.Time{}
		if c.recovered != 0 {
			recovered = at(c.recovered)
		}
		err := checkGraceAcrossProcess(fault, at(c.synced), recovered, at(c.observedTo), grace)
		if (c.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v want %q", c.name, err, c.want)
		}
	}
	if err := checkGraceAcrossProcess(fault, at(5), at(20), at(20), 10*time.Second); err == nil || !strings.Contains(err.Error(), "INCONCLUSIVE") {
		t.Fatalf("a grace too short to separate the deadlines must be inconclusive: %v", err)
	}
}

func TestSyncMarkerTimeIgnoresTheOldProcessAndUnsyncedLines(t *testing.T) {
	changed := time.Date(2026, 11, 25, 5, 0, 40, 0, time.UTC)
	logs := strings.Join([]string{
		`2026-11-25T05:00:01.000000000Z I1125 run.go:124] "Starting the ModelServing controller" workers=5`,
		`2026-11-25T05:00:45.000000000Z I1125 leaderelection.go:258] "Attempting to acquire leader lease..."`,
		`2026-11-25T05:01:02.500000000Z I1125 run.go:168] "Starting the ServingGroup controller" workers=5`,
		`2026-11-25T05:01:02.600000000Z I1125 run.go:124] "Starting the ModelServing controller" workers=5`,
		`not a timestamp initial sync has been done`,
	}, "\n")
	at, ok := syncMarkerTime(logs, changed)
	if !ok || !at.Equal(time.Date(2026, 11, 25, 5, 1, 2, 600000000, time.UTC)) {
		t.Fatalf("sync time: %v %t", at, ok)
	}
	if _, ok := syncMarkerTime(logs, changed.Add(time.Hour)); ok {
		t.Fatal("a marker of an earlier process was accepted")
	}
	if at, ok := syncMarkerTime("2026-11-25T05:00:50Z I1125 initial sync has been done", changed); !ok || at.Second() != 50 {
		t.Fatal("marker of a build without ServingGroup objects not recognized")
	}
}
