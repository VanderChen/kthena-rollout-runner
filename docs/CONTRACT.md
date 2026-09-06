# Independent case contract

Each cases/core/RUN-NNN.yaml is a complete input and expectation for one original
catalogue ID. The files use JSON-compatible YAML 1.2 to preserve explicit scalar
types and absent keys; there is no matrix expansion at runtime.

## Fields

- format: rollout-runner/v1.
- baseline: pinned production source commit.
- input.spec: raw ModelServing configuration. The fixture adds scheduler/plugins/
  Pod templates but never inserts rolling U/S/P into the request.
- update: frontend A to B; backend remains A.
- expect: mode, desired budget units, effective U/S/P, explicit old start order,
  protected ordinals, unchanged Role names, final A/B counts, initial start count.
- process: positive holdSeconds and one-unit-at-a-time Ready release.

The first implementation validates the supported core shape explicitly and fails
unknown/malformed configurations instead of silently treating them as covered.
Defaulted API responses are saved and checked separately from raw input.

## Observation boundary

Observers start before baseline creation. Assertions arm only after the initial
healthy A identities have been captured. All four resource streams are journaled
before any state reduction: Pods, ModelServings, Volcano PodGroups, ControllerRevisions.

The Pod stream drives exact destructive-start and current Ready accounting.
SGs/Role instances are logical records, not CRs. Old identities remain in the ledger
after deletion. Count one Role across all its required entry/worker Pods. Role
budget scope in this suite is G0/frontend; backend is an explicitly protected scope.

PG deletion is an earlier SG signal. Since PG and Pod watches do not share a
portable total order, PG starts use a separate provable ceiling: U plus the number
of distinct B units that the runner has explicitly allowed to become Ready.
Actual Ready credit is counted only from Pods. This ceiling is not a substitute
for the stricter Pod-stream check and cannot grant Ready credit by itself.

An API-invisible internal reservation is outside this black-box observer. A missed
stream interval is inconclusive, not safe. Cases currently assume no external
actors delete/recreate their private namespace's objects during a run.

## Safety and progress

In the healthy fixed-size A to B transition:

- K = distinct baseline units that started destructive replacement;
- T = current complete, non-terminating Ready B units, including surge;
- committed capacity = D - K + T;
- require committed capacity and actual available capacity >= max(D-U, 0);
- require non-terminating active logical units <= D+S;
- enforce explicit eligible/protected sets and old-start order.

Every target Pod must remain NotReady until its exact UID has been authorized by
the runner's release action. An unauthorized Ready Pod latches CONTROL_VIOLATION,
including a single Ready entry in an otherwise incomplete Role. This assertion
checks the experiment's controllability; it does not grant readiness credit on
release. Credit still requires an actual complete Ready unit from the Pod stream.

All invariants run throughout waits and holds. Each released unit must become
Ready, and when eligible old units remain, the next old start must occur within
the deadline. Final partition stops can contain A. Complete P=0 rollout must
also converge status and owned revision references.

Raw PodGroup count is not used as an instantaneous surge budget; PG ownership,
gang minMember/minResources and association are verified at convergence.
No HyperNode/topology tree or placement correctness is configured or tested.
The controlled fixture uses a one-second termination grace period for both A and B;
long termination/finalizer behavior is outside this core-suite execution profile.

## Extension policy

Add new case files and reviewed process checks for new scenario families.
Do not reinterpret Pending as NotReady, combine Role budgets across groups,
reset in-flight starts on configuration change, or import the controller's
budget/candidate algorithms as the expectation oracle.

Manual/observe-only execution from the V2 design is not implemented in this
first 60-case milestone; remaining catalogue coverage must be stated explicitly.
