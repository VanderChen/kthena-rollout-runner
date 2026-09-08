// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func priorIntentFixture(t *testing.T) (*NormalLedger, Objects, requestInterval, string) {
	t.Helper()
	l, objects := normalFixture(t, "RUN-183")
	c := normalCase(t, "RUN-183")
	for _, step := range c.Scenario.Steps[:2] {
		if err := l.Transition(step.Spec, step.Name, step.Expect, objects); err != nil {
			t.Fatal(err)
		}
	}
	deleteNormal(l, objects, "frontend", 0, 2)
	deleteNormal(l, objects, "frontend", 0, 1)
	step := c.Scenario.Steps[2]
	if err := l.Transition(step.Spec, step.Name, step.Expect, objects); err != nil {
		t.Fatal(err)
	}
	deleteNormal(l, objects, "frontend", 0, 0)
	start := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	for i := range l.Starts {
		l.Starts[i].At = start.Add(time.Duration(i) * time.Second)
	}
	l.Starts[len(l.Starts)-1].At = start.Add(6 * time.Second)
	request := requestInterval{Sent: start.Add(4 * time.Second), Received: start.Add(5 * time.Second)}
	log := "2026-09-08T00:00:03Z object=\"test/model\" reason=\"RoleDeleting\" message=\"Role frontend/frontend-0 in ServingGroup model-0 is now Deleting\"\n"
	return l, objects, request, log
}

func TestPriorRoleIntentIsFiniteAndBeforeRequest(t *testing.T) {
	l, objects, request, logs := priorIntentFixture(t)
	before, _ := json.Marshal(l)
	review, err := inspectPriorRoleIntent(l, objects, "test", logs, request)
	if err != nil || review.PreviousMinimum != 0 || review.UID != l.Starts[len(l.Starts)-1].UIDs[0] {
		t.Fatalf("review=%+v error=%v", review, err)
	}
	after, _ := json.Marshal(l)
	if string(before) != string(after) {
		t.Fatal("inspection mutated the raw ledger")
	}
}

func TestPriorRoleIntentRejectsUnsupportedEvidence(t *testing.T) {
	for _, name := range []string{"request-overlap", "after-response", "wrong-namespace", "duplicate-intent", "missing-intent", "wrong-owner", "extra-violation", "changed-template", "new-uid", "nonzero-old-minimum", "old-log", "other-active-member"} {
		t.Run(name, func(t *testing.T) {
			l, objects, request, logs := priorIntentFixture(t)
			uid := l.Starts[len(l.Starts)-1].UIDs[0]
			switch name {
			case "request-overlap":
				logs = strings.ReplaceAll(logs, "00:00:03Z", "00:00:04.5Z")
			case "after-response":
				logs = strings.ReplaceAll(logs, "00:00:03Z", "00:00:05.5Z")
			case "wrong-namespace":
				logs = strings.ReplaceAll(logs, "test/model", "other/model")
			case "duplicate-intent":
				logs += logs
			case "missing-intent":
				logs = ""
			case "wrong-owner":
				objects["pods"][uid].SetOwnerReferences(nil)
			case "extra-violation":
				l.Violations = append(l.Violations, "another failure")
			case "changed-template":
				r := l.Model.Roles["frontend"]
				r.Entry = "C"
				l.Model.Roles["frontend"] = r
			case "new-uid":
				delete(objects["pods"], uid)
			case "nonzero-old-minimum":
				i := len(l.History) - 2
				r := l.History[i].Roles["frontend"]
				r.U = 0
				l.History[i].Roles["frontend"] = r
			case "old-log":
				logs = strings.ReplaceAll(logs, "00:00:03Z", "00:00:00Z")
			case "other-active-member":
				for key, p := range objects["pods"] {
					if key != uid && p.GetLabels()[LabelRole] == "frontend" {
						p.SetDeletionTimestamp(nil)
						break
					}
				}
			}
			if _, err := inspectPriorRoleIntent(l, objects, "test", logs, request); err == nil {
				t.Fatal("unsupported intent accepted")
			}
		})
	}
}

// Optional read-only regression against independently captured Kind traces.
// No cluster mutation, replay or replacement of the original result is involved.
func TestPriorRoleIntentKindArtifacts(t *testing.T) {
	root := os.Getenv("RUNNER_INTENT_ARTIFACTS")
	if root == "" {
		t.Skip("set RUNNER_INTENT_ARTIFACTS to exported artifacts directory")
	}
	for _, row := range []struct {
		run, id  string
		accepted bool
	}{{"normal-r10-183-addendum", "RUN-183", true}, {"normal-r10-303", "RUN-183", false}, {"normal-r10-303", "RUN-193", false}} {
		t.Run(row.run+"/"+row.id, func(t *testing.T) {
			dir := filepath.Join(root, row.run, row.id, "attempt-1")
			var l NormalLedger
			if err := readIntentJSON(filepath.Join(dir, "ledger.json"), &l); err != nil {
				t.Fatal(err)
			}
			for _, phase := range []int{2, 3} {
				data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("step-%02d-server.yaml", phase)))
				if err != nil {
					t.Fatal(err)
				}
				var o unstructured.Unstructured
				if err := yaml.Unmarshal(data, &o.Object); err != nil {
					t.Fatal(err)
				}
				m, err := readModel(mapValue(o.Object, "spec"))
				if err != nil {
					t.Fatal(err)
				}
				l.History = append(l.History, m)
				l.Model = m
			}
			var interval requestInterval
			if err := readIntentJSON(filepath.Join(dir, "step-03-request-time.json"), &interval); err != nil {
				t.Fatal(err)
			}
			objects := Objects{"pods": {}}
			data, err := os.ReadFile(filepath.Join(dir, "observations.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			last := l.Starts[len(l.Starts)-1]
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var e Observation
				if err := json.Unmarshal([]byte(line), &e); err != nil {
					t.Fatal(err)
				}
				if e.Received.After(last.At) {
					break
				}
				if e.Kind != "pods" {
					continue
				}
				uid := string(e.Object.GetUID())
				if e.Event == "DELETED" {
					delete(objects["pods"], uid)
				} else {
					objects["pods"][uid] = e.Object
				}
			}
			logs, err := os.ReadFile(filepath.Join(root, row.run, row.id, "controller.log"))
			if err != nil {
				t.Fatal(err)
			}
			var result Result
			if err := readIntentJSON(filepath.Join(dir, "result.json"), &result); err != nil {
				t.Fatal(err)
			}
			review, err := inspectPriorRoleIntent(&l, objects, result.Namespace, string(logs), interval)
			if (err == nil) != row.accepted {
				t.Fatalf("accepted=%v review=%+v error=%v", row.accepted, review, err)
			}
		})
	}
}
