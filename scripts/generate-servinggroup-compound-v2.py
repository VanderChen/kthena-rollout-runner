#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0

"""Generate the independent SG compound-v2 cases from compact phase recipes.

Run after changing a recipe, then run TestCompoundCatalogueIsExecutable.
The pinned commit is the candidate under test, not proof that it passes v2.
"""

import copy
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEST = ROOT / "cases" / "servinggroup-compound-v2"
COMMIT = "bd0d650f6ab3fd3d2b473d765d26a1a9d1be4a2b"
TEMPLATE = json.loads((ROOT / "cases" / "rollout-blocking" / "RUN-612.yaml").read_text())["scenario"]["initialSpec"]


def spec(n, version="A", u=1, s=0, p=0, members=1, workers=0, worker_version=None):
    value = copy.deepcopy(TEMPLATE)
    value["replicas"] = n
    budget = value["rolloutStrategy"]["rollingUpdateConfiguration"]
    budget.update(maxUnavailable=u, maxSurge=s, partition=p)
    role = value["template"]["roles"][0]
    role["replicas"] = members
    role["workerReplicas"] = workers
    container = role["entryTemplate"]["spec"]["containers"][0]
    container["env"][0]["value"] = version
    if workers:
        role["workerTemplate"] = copy.deepcopy(role["entryTemplate"])
        role["workerTemplate"]["spec"]["containers"][0]["env"][0]["value"] = worker_version or version
    return value


def predicate(state, *, blocked=False, same=None, members=None, running=()):
    groups = []
    for field in state.split(","):
        ordinal, version = field.strip().split(":")
        ready = not version.endswith("!")
        version = version.rstrip("!")
        group = {"ordinal": int(ordinal), "version": version, "ready": ready}
        if same and int(ordinal) in same:
            group["uidSameAs"] = same[int(ordinal)]
        if members and int(ordinal) in members:
            group["members"] = members[int(ordinal)]
        if int(ordinal) in running:
            group["runningNotReady"] = True
        groups.append(group)
    return {"active": len(groups), "ready": sum(g["ready"] for g in groups), "groups": groups, "blocked": blocked}


def condition(group, kind="unit", version="", ready=None):
    value = {"kind": kind, "group": group, "count": 1}
    if version:
        value["version"] = version
    if ready is not None:
        value["ready"] = ready
    return value


class Case:
    def __init__(self, design, initial, initial_state=None, *, bad=()):
        self.design = design
        self.number = (617 + int(design[1:]) if design[0] == "S" else
                       624 + int(design[1:]) if design[0] == "C" else
                       640 + int(design[1:]) if design[0] == "P" else 652)
        self.id = f"RUN-{self.number:03d}"
        self.current = initial
        self.state = initial_state or ",".join(f"{i}:A" for i in range(initial["replicas"]))
        release = "except" if bad else "all"
        baseline = {"name": "baseline", "action": "observe", "until": "compound", "release": release,
                    "stableSeconds": 1, "holdSeconds": 0, "expect": {"compound": predicate(self.state, running=bad)}}
        if bad:
            baseline["exclude"] = [condition(i) for i in bad]
        self.data = {"format": "rollout-runner/compound-v2", "id": self.id, "baseline": COMMIT,
                     "scenario": {"designID": "SG-" + design, "source": {"id": self.id, "designID": "SG-" + design,
                     "design": "servinggroup-compound-rollout.zh-CN.md"},
                     "profile": "controlled", "initialSpec": initial, "baseline": baseline, "steps": []}}

    def step(self, name, state, *, action="observe", next_spec=None, release="none", stable=0,
             blocked=False, same=None, members=None, running=(), conditions=None, costs=None, live=False,
             source=False, timeout=None):
        step = {"name": name, "action": action, "until": "conditions" if conditions and action != "pin" else "compound",
                "release": release, "stableSeconds": stable, "holdSeconds": 0,
                "expect": {"compound": predicate(state, blocked=blocked, same=same, members=members, running=running)}}
        if next_spec is not None:
            step["spec"] = next_spec
            self.current = next_spec
        if conditions:
            step["conditions"] = conditions
        if costs:
            step["deletionCosts"] = {str(k): v for k, v in costs.items()}
        if live:
            step["requireLiveTerminating"] = True
        if source:
            step["sourceState"] = True
        if timeout:
            step["timeoutSeconds"] = timeout
        self.data["scenario"]["steps"].append(step)
        self.state = state
        return self

    def update(self, name, state, **changes):
        options = {k: changes.pop(k) for k in list(changes) if k in
                  {"release", "stable", "blocked", "same", "group_members", "running", "conditions", "live", "source", "timeout"}}
        if "group_members" in options:
            options["members"] = options.pop("group_members")
        values = {"n": self.current["replicas"],
                  "version": self.current["template"]["roles"][0]["entryTemplate"]["spec"]["containers"][0]["env"][0]["value"],
                  "u": self.current["rolloutStrategy"]["rollingUpdateConfiguration"]["maxUnavailable"],
                  "s": self.current["rolloutStrategy"]["rollingUpdateConfiguration"]["maxSurge"],
                  "p": self.current["rolloutStrategy"]["rollingUpdateConfiguration"]["partition"],
                  "members": self.current["template"]["roles"][0]["replicas"],
                  "workers": self.current["template"]["roles"][0]["workerReplicas"]}
        role = self.current["template"]["roles"][0]
        if role["workerReplicas"]:
            values["worker_version"] = role["workerTemplate"]["spec"]["containers"][0]["env"][0]["value"]
        values.update(changes)
        return self.step(name, state, action="update", next_spec=spec(**values), **options)

    def hold(self, name, state=None, **options):
        return self.step(name, state or self.state, **options)

    def cost(self, values):
        return self.step("set-explicit-deletion-costs", self.state, action="set-deletion-cost", costs=values)

    def pin(self, group):
        return self.step(f"pin-old-group-{group}", self.state, action="pin", conditions=[condition(group)])

    def unpin(self, state, *, release="none"):
        return self.step("release-pinned-deletion", state, action="unpin", release=release)


cases = []

def add(case):
    cases.append(case.data)


# Sparse identities: each hole below comes from an actual scale-down request.
c = Case("S01", spec(4, u=1)); c.cost({0: 100, 1: -100, 2: -100, 3: 100})
c.update("shrink-to-sparse-0-and-3", "0:A,3:A", n=2, same={0: "baseline", 3: "baseline"})
c.update("fill-only-lowest-hole-1", "0:A,1:A!,3:A", n=3, same={0: "baseline", 3: "baseline"})
c.hold("first-expansion-stays-sparse", "0:A,1:A,3:A", release="all", stable=30, same={3: "baseline"})
c.update("fill-second-hole-2", "0:A,1:A,2:A!,3:A", n=4, same={3: "baseline"})
c.hold("second-expansion-ready", "0:A,1:A,2:A,3:A", release="all", same={3: "baseline"}); add(c)

c = Case("S02", spec(4, u=1)); c.cost({0: 100, 1: -100, 2: -100, 3: 100})
c.update("establish-sparse-source", "0:A,3:A", n=2)
c.update("replace-high-old-through-low-hole", "0:A,1:B!", version="B")
c.hold("low-hole-B-ready", "0:A,1:B", release="all")
c.hold("complete-B", "0:B,1:B", release="all"); add(c)

c = Case("S03", spec(4, u=1, p=3)); c.update("establish-high-B", "0:A,1:A,2:A,3:B", version="B", release="all")
c.cost({0: 100, 1: -100, 2: -100, 3: 100})
c.update("shrink-with-high-B-retained", "0:A,3:B", n=2, p=2, same={3: "establish-high-B"})
c.update("update-only-old-0", "0:B,3:B", p=0, release="all", stable=30, same={3: "establish-high-B"})
c.update("S1-still-keeps-healthy-high-B", "0:B,3:B", s=1, stable=30, same={3: "establish-high-B"}); add(c)

c = Case("S04", spec(4, u=0, s=1, p=3)); c.update("high-B-source", "0:A,1:A,2:A,3:B", version="B", release="all")
c.cost({0: 100, 1: 100, 2: -100, 3: 100})
c.update("retain-high-B-and-low-old", "0:A,1:A,3:B", n=3)
c.update("old-surge-outside-new-range", "0:A,1:A,3:B,4:B", p=0, release="all")
c.update("new-formal-hole-before-surge-reclassification", "0:A,1:A,2:B!,3:B,4:B", n=4, same={3: "old-surge-outside-new-range", 4: "old-surge-outside-new-range"})
c.hold("all-formal-B-and-surge-retired", "0:B,1:B,2:B,3:B", release="all", same={3: "old-surge-outside-new-range"}); add(c)

c = Case("S05", spec(3, u=1)); c.cost({0: 100, 1: -100, 2: 100})
c.update("sparse-source", "0:A,2:A", n=2)
c.update("bad-low-new-capacity", "0:A,1:B!,2:A", n=3, version="B", running=[1])
c.update("strict-high-old-blocks-low-repair", "0:A,1:B!,2:A", version="C", blocked=True, stable=30,
         same={0: "bad-low-new-capacity", 1: "bad-low-new-capacity", 2: "bad-low-new-capacity"}, running=[1]); add(c)

c = Case("S06", spec(5, u=2)); c.update("two-bad-B-high-groups", "0:A,1:A,2:A,3:B!,4:B!", version="B", running=[3,4])
c.hold("same-target-does-not-rebuild-bad-B", stable=30, blocked=True, same={3: "two-bad-B-high-groups", 4: "two-bad-B-high-groups"}, running=[3,4])
c.update("C-repairs-high-bad-batch", "0:A,1:A,2:A,3:C!,4:C!", version="C")
c.hold("C-converges-after-batches", "0:C,1:C,2:C,3:C,4:C", release="all"); add(c)

c = Case("S07", spec(5, u=2), "0:A!,1:A!,2:A!,3:A!,4:A!", bad=range(5))
c.update("first-Q-batch-only", "0:A!,1:A!,2:A!,3:B!,4:B!", version="B", blocked=True, stable=30, running=[0,1,2,3,4])
c.hold("release-first-batch-and-finish", "0:B,1:B,2:B,3:B,4:B", release="all"); add(c)

# Scale/update interleavings. Intermediate checkpoints carry exact identities;
# the ledger audits all Watch events between them, including transient deletes.
c = Case("C01", spec(3, u=1)); c.pin(2)
c.update("B-high-delete-in-flight", "0:A,1:A,2:A!", version="B",
         conditions=[condition(2, kind="terminating")])
c.update("expand-during-old-deletion", "0:A,1:A,2:A!,3:B!,4:B!", n=5, live=True,
         conditions=[condition(2, kind="terminating")])
c.unpin("0:A,1:A,2:B!,3:B!,4:B!")
c.hold("new-scale-does-not-fund-old-deletion", stable=30, blocked=True, same={0: "baseline", 1: "baseline"})
c.hold("complete-expanded-rollout", "0:B,1:B,2:B,3:B,4:B", release="all"); add(c)

c = Case("C02", spec(3, u=1)); c.update("B-high-not-ready", "0:A,1:A,2:B!", version="B")
c.update("shrink-discards-unneeded-target", "0:A,1:A", n=2)
c.hold("finish-retained-set", "0:B,1:B", release="all"); add(c)

c = Case("C03", spec(3, u=0, s=1)); c.update("ready-B-surge-3", "0:A,1:A,2:A,3:B", version="B", release="all")
c.update("reuse-3-add-formal-4-and-surge-5", "0:A,1:A,2:A,3:B,4:B!,5:B!", n=5, same={3: "ready-B-surge-3"})
c.hold("converge-without-rebuilding-3", "0:B,1:B,2:B,3:B,4:B", release="all", same={3: "ready-B-surge-3"}); add(c)

c = Case("C04", spec(3, u=1)); c.update("expansion-first-new-group-unready", "0:A,1:A,2:A,3:A!", n=5, source=True, timeout=15)
c.update("new-version-fills-remaining-capacity", "0:A,1:A,2:A,3:A!,4:B!", version="B")
c.hold("wait-for-credit-and-replace-3", "0:B,1:B,2:B,3:B,4:B", release="all"); add(c)

c = Case("C05", spec(5, u=1)); c.pin(3); c.pin(4)
c.update("shrink-deletion-in-flight", "0:A,1:A,2:A,3:A!,4:A!", n=3,
         conditions=[condition(4, kind="terminating")])
c.update("B-arrives-during-shrink", "0:A,1:A,2:A,3:A!,4:A!", version="B", live=True,
         conditions=[condition(4, kind="terminating")])
c.unpin("0:A,1:A,2:A"); c.hold("only-retained-groups-updated", "0:B,1:B,2:B", release="all"); add(c)

c = Case("C06", spec(3, u=1)); c.update("atomic-grow-and-B", "0:A,1:A,2:A,3:B!,4:B!", n=5, version="B")
c.hold("finish-after-scale-readiness", "0:B,1:B,2:B,3:B,4:B", release="all"); add(c)

c = Case("C07", spec(5, u=1)); c.update("atomic-shrink-and-B", "0:A,1:A,2:A", n=3, version="B")
c.hold("finish-only-final-retained-set", "0:B,1:B,2:B", release="all"); add(c)

c = Case("C08", spec(3, u=1)); c.pin(1)
c.update("B-high-ready-and-old-1-committed", "0:A,1:A!,2:B", version="B", release="all",
         conditions=[condition(1, kind="terminating")])
c.update("C-inherits-committed-deletion", "0:A,1:A!,2:B", version="C", live=True,
         conditions=[condition(1, kind="terminating")])
c.unpin("0:A,1:C!,2:B"); c.hold("direct-C-convergence", "0:C,1:C,2:C", release="all")
c.update("reset-for-created-B-branch", "0:A,1:A,2:A", version="A", release="all")
c.update("B-1-create-already-issued", "0:A,1:B!,2:B", version="B", release="one")
c.update("C-blocked-by-higher-ready-B2", "0:A,1:B!,2:B", version="C", blocked=True,
         stable=30, same={1: "B-1-create-already-issued", 2: "B-1-create-already-issued"}); add(c)

c = Case("C09", spec(3, u=1)); c.update("scale-A-first-group", "0:A,1:A,2:A,3:A", n=5, release="all", source=True, timeout=15)
c.update("B-fills-last-scale-slot", "0:A,1:A,2:A,3:A,4:B", version="B", release="all")
c.update("C-supersedes-B", "0:A,1:A,2:A,3:A,4:B", version="C")
c.hold("high-to-low-direct-C", "0:C,1:C,2:C,3:C,4:C", release="all"); add(c)

c = Case("C10", spec(5, u=1)); c.pin(3); c.pin(4)
c.update("shrink-A-in-flight", "0:A,1:A,2:A,3:A!,4:A!", n=3,
         conditions=[condition(4, kind="terminating")])
c.update("B-during-shrink", "0:A,1:A,2:A,3:A!,4:A!", version="B", live=True,
         conditions=[condition(4, kind="terminating")])
c.unpin("0:A,1:A,2:A"); c.hold("B-high-retained", "0:A,1:A,2:B", release="all")
c.update("C-target", "0:A,1:A,2:B", version="C")
c.hold("C-only-final-set", "0:C,1:C,2:C", release="all"); add(c)

c = Case("C11", spec(3, u=1)); c.update("scale-A-first-group", "0:A,1:A,2:A,3:A", n=5, release="all", source=True, timeout=15)
c.update("bad-B-last-slot", "0:A,1:A,2:A,3:A,4:B!", version="B", blocked=True, stable=30, running=[4])
c.update("rollback-repairs-stale-B", "0:A,1:A,2:A,3:A,4:A", version="A", release="all",
         same={0: "baseline", 1: "baseline", 2: "baseline", 3: "scale-A-first-group"}); add(c)

c = Case("C12", spec(3, u=0, s=1)); c.update("serving-B-surge", "0:A,1:A,2:A,3:B", version="B", release="all")
c.update("shrink-retains-serving-high-surge", "0:A,1:A,3:B", n=2, same={3: "serving-B-surge"})
c.hold("finish-two-B-with-high-surge-retired", "0:B,1:B", release="all"); add(c)

c = Case("C13", spec(3, u=0, s=1)); c.update("scale-A-partial", "0:A,1:A,2:A,3:A", n=5, release="all", source=True, timeout=15)
c.update("start-B-at-new-N", "0:A,1:A,2:A,3:A,4:B!,5:B!", version="B")
c.hold("new-surge-index-5-then-finish", "0:B,1:B,2:B,3:B,4:B", release="all"); add(c)

c = Case("C14", spec(3, u=0, s=1)); c.pin(2)
c.update("B-surge-serving-and-A2-committed", "0:A,1:A,2:A!,3:B", version="B", release="all",
         conditions=[condition(2, kind="terminating")])
c.update("C-while-old-delete-committed", "0:A,1:A,2:A!,3:B", version="C", live=True,
         conditions=[condition(2, kind="terminating")])
c.unpin("0:A,1:A,2:C!,3:B"); c.hold("C-replacement-ready-before-surge-recycle", "0:A,1:A,2:C,3:B", release="all")
c.hold("converged-C", "0:C,1:C,2:C", release="all"); add(c)

c = Case("C15", spec(5, u="25%")); c.update("high-B-ready", "0:A,1:A,2:A,3:A,4:B", version="B", release="all")
c.update("new-N-recomputes-U-as-two", "0:A,1:A,2:A,3:A,4:B,5:B!,6:B!,7:B!,8:B!", n=9)
c.hold("partial-new-readiness-at-floor", "0:A,1:A,2:A,3:A,4:B,5:B,6:B,7:B!,8:B!",
       release="one", same={3: "baseline"})
c.hold("at-floor-stable-block", stable=30, blocked=True, same={3: "baseline"})
c.hold("finish-after-real-credit", "0:B,1:B,2:B,3:B,4:B,5:B,6:B,7:B,8:B", release="all")
c.update("reset-for-bad-B-branch", "0:A,1:A,2:A,3:A,4:A,5:A,6:A,7:A,8:A", version="A", release="all")
c.update("shrink-back-to-five-A", "0:A,1:A,2:A,3:A,4:A", n=5)
c.update("bad-B-high-at-N5", "0:A,1:A,2:A,3:A,4:B!", version="B", running=[4])
c.update("bad-B-scale-N9-must-stall", "0:A,1:A,2:A,3:A,4:B!,5:B!,6:B!,7:B!,8:B!", n=9,
         blocked=True, stable=30, same={0: "shrink-back-to-five-A", 1: "shrink-back-to-five-A",
         2: "shrink-back-to-five-A", 3: "shrink-back-to-five-A", 4: "bad-B-high-at-N5"}, running=[4,5,6,7,8]); add(c)

c = Case("C16", spec(3, u=1, s=1), "0:A,1:A,2:A!", bad=[2])
c.pin(2)
c.update("new-unready-surge-counts-C-and-V", "0:A,1:A,2:A!,3:B!", version="B", running=[3],
         conditions=[condition(2, kind="terminating")])
c.unpin("0:A,1:A,2:B!,3:B!")
c.hold("Q-cleans-old-bad-high", "0:A,1:A,2:B!,3:B!", blocked=True, stable=30, running=[2,3])
c.hold("release-new-credit-and-complete", "0:B,1:B,2:B", release="all"); add(c)

# Partition, percentage and historical-template cases.
c = Case("P01", spec(3, u=1, p=1)); c.update("P1-B-gray", "0:A,1:B,2:B", version="B", release="all")
c.update("expand-new-slots-B", "0:A,1:B,2:B,3:B!,4:B!", n=5, same={0: "baseline", 1: "P1-B-gray", 2: "P1-B-gray"})
c.hold("P1-expanded-ready", "0:A,1:B,2:B,3:B,4:B", release="all", same={0: "baseline"}); add(c)

c = Case("P02", spec(3, u=1, p=3)); c.update("target-B-blocked-by-P3", "0:A,1:A,2:A", version="B", stable=30)
c.update("expand-across-P5", "0:A,1:A,2:A,3:A!,4:A!,5:B!", n=6, p=5)
c.hold("historical-protected-slots-ready", "0:A,1:A,2:A,3:A,4:A,5:B", release="all"); add(c)

c = Case("P03", spec(5, u=1, p=3)); c.update("P3-gray", "0:A,1:A,2:A,3:B,4:B", version="B", release="all")
c.update("shrink-through-partition", "0:A,1:A", n=2, p=2)
c.hold("P-does-not-impose-minimum", "0:A,1:A", stable=30)
c.update("restore-three-with-B2", "0:A,1:A,2:B", n=3, p=2, release="all")
c.step("fault-protected-old-1", "0:A,1:A!,2:B", action="drop-ready", running=[1])
c.data["scenario"]["steps"][-1]["readinessTarget"] = {"kind": "unit", "group": 1, "role": "frontend",
    "ordinal": 0, "version": "A", "ready": True, "count": 1}
c.update("shrink-prefers-unready-protected-1", "0:A,2:B", n=2, same={2: "restore-three-with-B2"})
c.update("reexpand-protected-hole-with-history-A", "0:A,1:A!,2:B", n=3, same={2: "restore-three-with-B2"})
c.hold("sparse-reexpanded-ready", "0:A,1:A,2:B", release="all", same={2: "restore-three-with-B2"}); add(c)

c = Case("P04", spec(3, u=1, p="50%")); c.update("P2-gray", "0:A,1:A,2:B", version="B", release="all")
c.update("P-rises-to-three", "0:A,1:A,2:B,3:B!,4:B!", n=5, same={2: "P2-gray"})
c.hold("existing-B-not-reverted", "0:A,1:A,2:B,3:B,4:B", release="all", same={2: "P2-gray"}); add(c)

c = Case("P05", spec(5, u=1, p="50%")); c.update("P3-gray", "0:A,1:A,2:A,3:B,4:B", version="B", release="all")
c.update("shrink-recalculates-P2-and-unlocks-2", "0:A,1:A,2:B", n=3, release="all")
c.hold("newly-unprotected-2-updates", "0:A,1:A,2:B", stable=30); add(c)

c = Case("P06", spec(3, u=1)); c.pin(1)
c.update("B-high-ready-and-old-1-committed", "0:A,1:A!,2:B", version="B", release="all",
         conditions=[condition(1, kind="terminating")])
c.update("raise-P-while-deleting", "0:A,1:A!,2:B", p=3, live=True,
         conditions=[condition(1, kind="terminating")])
c.unpin("0:A,1:A!,2:B"); c.hold("protected-hole-uses-A", "0:A,1:A,2:B", release="all", same={2: "B-high-ready-and-old-1-committed"}); add(c)

c = Case("P07", spec(3, u=1, p=2)); c.update("P2-gray", "0:A,1:A,2:B", version="B", release="all")
c.update("lower-P-and-submit-C", "0:A,1:A,2:C!", p=1, version="C")
c.hold("direct-A-to-C-in-1", "0:A,1:C,2:C", release="all"); add(c)

c = Case("P08", spec(3, u=0, s=1, p="50%")); c.update("B-surge-not-ready", "0:A,1:A,2:A,3:B!", version="B")
c.update("expand-reclassifies-3", "0:A,1:A,2:A,3:B!,4:B!", n=5, same={3: "B-surge-not-ready"})
c.hold("no-extra-surge-5", "0:A,1:A,2:A,3:B,4:B", release="all", stable=30); add(c)

c = Case("P09", spec(3, u=1, p=1), "0:A!,1:A,2:A", bad=[0]); c.update("protected-bad-charges-U", "0:A!,1:A,2:A", version="B", blocked=True, stable=30, running=[0])
c.hold("recover-protected-old", "0:A,1:A,2:A", release="all")
c.hold("then-gray-roll", "0:A,1:B,2:B", release="all", same={0: "baseline"}); add(c)

c = Case("P10", spec(3, u=0, s=1, p=1)); c.update("P1-B-surge-ready", "0:A,1:A,2:A,3:B", version="B", release="all")
c.update("reuse-3-but-require-new-surge-4", "0:A,1:A,2:A,3:B,4:B!", n=4, same={3: "P1-B-surge-ready"})
c.hold("P1-expanded-rollout", "0:A,1:B,2:B,3:B", release="all", same={3: "P1-B-surge-ready"}); add(c)

c = Case("P11", spec(4, u=1, p=2)); c.cost({0: 100, 1: -100, 2: -100, 3: 100})
c.update("sparse-protected-source", "0:A,3:A", n=2)
c.update("high-old-fills-protected-hole", "0:A,1:A", version="B", release="all", same={0: "baseline"}, stable=30)
c.update("lower-P-releases-new-ordinal", "0:A,1:B", p=1, release="all", same={0: "baseline"})
c.hold("protected-low-history-retained", "0:A,1:B", same={0: "baseline"}, stable=30); add(c)

c = Case("R01", spec(3, u=1, p=1, workers=1, worker_version="W1"))
c.update("historical-and-new-worker-templates", "0:A,1:B,2:B", version="B", worker_version="W2", release="all")
c.update("first-group-applies-second-member", "0:A,1:B,2:B!", members=2, group_members={2: 2},
         conditions=[condition(2, kind="unit", version="B", ready=False)])
# The Role member increase is the request under test; the next checkpoint
# verifies three exact group member counts and retains the protected W1 group.
c.hold("all-members-ready", "0:A,1:B,2:B", release="all", members={0: 2, 1: 2, 2: 2}, same=None); add(c)

assert len(cases) == 35, len(cases)
assert [case["id"] for case in cases] == [f"RUN-{i:03d}" for i in range(618, 653)]
DEST.mkdir(parents=True, exist_ok=True)
for case in cases:
    (DEST / f"{case['id']}.yaml").write_text(json.dumps(case, ensure_ascii=False, indent=2) + "\n")
(DEST / "suite.json").write_text(json.dumps({"format": "rollout-runner/compound-v2", "controllerCommit": COMMIT,
    "design": "servinggroup-compound-rollout.zh-CN.md", "ids": [case["id"] for case in cases]}, indent=2) + "\n")
