// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"strings"
	"testing"
)

func TestRecoveryCompletionDistinguishesTemporaryAndFormalHighInstances(t *testing.T) {
	for _, id := range []string{"RUN-195", "RUN-081"} {
		for _, variant := range []string{"temporary", "unmarked-formal", "adopted-in-range"} {
			t.Run(id+"/"+variant, func(t *testing.T) {
				e := settledOrdinalFixture(t, id)
				e.c.ID = "RUN-360" // Recovery endpoints do not inherit normal-only canonical ordinals.
				e.l.Recoveries = []*RecoveryRecord{{Scope: PodFaultScope{OwnerUID: "owner", TargetUID: "old-fault"}, Deleted: map[string]bool{"old-fault": true}, OrderComplete: true}}
				sg := e.l.Model.Mode == "SG"
				for _, p := range e.o.objects["pods"] {
					labels, annotations := p.GetLabels(), p.GetAnnotations()
					if sg {
						if labels[LabelGroup] != "model-0" {
							continue
						}
						if variant != "adopted-in-range" {
							labels[LabelGroup] = fmt.Sprintf("model-%d", e.l.Model.N)
							annotations["scheduling.k8s.io/group-name"] = labels[LabelGroup]
						}
						annotations["modelserving.volcano.sh/surge"] = "serving-group"
					} else {
						if labels[LabelRole] != "frontend" || ordinal(labels[LabelRoleID]) != 0 {
							continue
						}
						if variant != "adopted-in-range" {
							labels[LabelRoleID] = fmt.Sprintf("frontend-%d", e.l.Model.Roles["frontend"].R)
						}
						annotations["modelserving.volcano.sh/surge"] = "role"
					}
					if variant == "unmarked-formal" {
						delete(annotations, "modelserving.volcano.sh/surge")
					}
					p.SetLabels(labels)
					p.SetAnnotations(annotations)
				}
				if sg && variant != "adopted-in-range" {
					e.o.objects["podgroups"]["model-0"].SetName(fmt.Sprintf("model-%d", e.l.Model.N))
				}
				ok, reason := e.settled(ScenarioExpectation{})
				if variant == "temporary" {
					if ok || !strings.Contains(reason, "temporary recovery surge remains") {
						t.Fatal("surge accepted as settled or unrelated failure", ok, reason)
					}
				} else if !ok {
					t.Fatal("formal identity rejected solely by ordinal or an adopted marker", reason)
				}
			})
		}
	}
}
