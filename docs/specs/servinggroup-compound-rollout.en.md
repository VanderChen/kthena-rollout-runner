# ServingGroupRollingUpdate compound rollout expectations

Version 2.7 · 2026-10-08 · [简体中文](servinggroup-compound-rollout.zh-CN.md)

This document defines **design expectations**; implementation compliance and actual Kind results are recorded separately. The main tables retain all 35 `ServingGroupRollingUpdate` scenarios. Shared principles also state their scope for `RoleRollingUpdate`. [Appendix B](#budget-lookup) compares budgets and behavior in both modes; it does not add an equivalent set of executable Role cases.

<a id="basic-principles"></a>

## 0. Reading guide and basic principles

**Core principles:** Changes to `spec.replicas` or a Role’s `replicas` are scaling; changes to `workerReplicas` or Pod templates are rolling updates. Replace complete ServingGroups or complete Role instances according to the configured strategy, counting availability only when every required member is Ready. Check `maxUnavailable`, `maxSurge` and `partition` against the latest configuration and actual state before each action. By default, select eligible old NotReady instances first and descend by ordinal within each health class; coordinated Roles cannot skip stable candidates. Preserve healthy identities that need no update.

The sections below cover changes, units, budgets, ordering, versions and identity. See the [scenario tables](#scenario-tables) for trajectories and [Appendix A](#behavior-rules) for complete rules.

### 0.1 Change classification: scaling and template rollout

ModelServing has three replica counts: the number of ServingGroups, the number of Role instances within each group, and the number of workers within each Role instance. **Increasing or decreasing `workerReplicas` is a template rollout**, replacing the unit selected by the configured rollout strategy.

| Changed field | Meaning of the change | Basic behavior |
| --- | --- | --- |
| `spec.replicas` | ServingGroup count, for example 2 → 3 | Scale SGs; changing only this count does not create a template revision |
| `spec.template.roles[].replicas` | Instances of this Role within each SG, for example 1 → 2 | Scale Role instances; changing only this count does not create a template revision, and existing instances keep their applicable templates |
| `spec.template.roles[].workerReplicas` | Workers within one Role instance, for example 0 → 1 or 2 → 1 | Change the entry + workers layout; this is a template update subject to the active rollout budgets and partition |
| `spec.template.roles[].entryTemplate` / `workerTemplate` | Entry / worker Pod templates, for example an image update | Update through the configured SG or Role rollout strategy |
| Active `maxUnavailable` / `maxSurge` / `partition` | Rollout budgets or protection boundary | Reevaluate subsequent actions; changing only these settings does not create a template revision |

When the worker count changes, old instances not yet selected for rollout continue to use their own historical worker layout for completeness and Ready checks. Applying the new worker count immediately to all old instances must not misclassify them as missing Pods and rebuild them through recovery outside the rollout budget. Replacements use the layout applicable to their slot: the new layout for unprotected slots, or the historical layout for protected slots under §0.5. A unit regains availability only when all required members are Ready.

For example, an SG has 3 instances of a Role, and each instance changes from 1 worker to 2 workers. There are still 3 Role instances; each instance's layout changes. Role mode rolls complete Role instances, while SG mode rolls complete SGs. Field ranges and validation are in [API §1.4](modelserving-api-reference.en.md#14-replica-counts-and-role-templates).

### 0.2 Rollout units, budget scope and Ready

| Rollout strategy | Update unit | Active budget and scope | Identity of an unchanged Role |
| --- | --- | --- | --- |
| `ServingGroupRollingUpdate` | A complete SG, including all Role instances and their Pods | Top-level `rollingUpdateConfiguration`, counted in SGs | Replaced with the SG even when this Role's template is unchanged |
| `RoleRollingUpdate` | One complete Role instance: 1 entry + all workers required by that instance | That Role's `maxUnavailable` / `maxSurge` / `partition`, calculated independently within each SG | A Role with an unchanged template retains its Pod UIDs |

A unit counts as one Ready only when **all required members are Ready**. In Role mode, a Ready entry alone does not make the instance Ready, and worker Pod counts are not available Role counts. Roles and SGs do not borrow each other's Role budgets; multiple SGs may advance their own Role rollouts concurrently.

Inactive-layer budgets are allowed but ignored. `roleCoordination` applies only in Role mode; dependency and progress constraints supplement the local budgets. Mode restrictions are in [API §2](modelserving-api-reference.en.md#2-rolloutstrategytype).

### 0.3 Budgets: account for actual capacity before allowing actions

| Constraint | Decision rule |
| --- | --- |
| Budget base | Use the latest desired replica count; floor percentage `maxUnavailable`, and ceil percentage `maxSurge` and `partition` |
| Active capacity | Actual active units must not exceed desired replicas + maxSurge; Deleting units occupy capacity until fully gone, and create reservations must also be counted |
| Healthy deletion | Recheck actual Ready before every deletion; the result must remain at least `max(0, desired replicas − maxUnavailable)` |
| Existing failures | Protected units, old versions and usable surge all contribute to the actual availability ledger, excluding committed deletions from Ready; when failures already put Ready below the floor, eligible outdated NotReady units may still be replaced within the total cleanup allowance, without further reducing Ready |
| New capacity and in-flight actions | Count only actual created surge; latest-target NotReady surge increments both active and target-unavailable counts. Never claim the same deletion/replacement allowance twice; reservations and already-observed state changes are counted once |

Unready formal scale-out slots do not grant extra rollout credit. Configured `maxSurge` alone provides no usable capacity. If scale-down temporarily leaves existing active units above the new ceiling, only cleanup or waiting is allowed; creating more must not worsen the excess.

See [API §2.3](modelserving-api-reference.en.md#23-shared-budgets-and-default-candidate-selection) for the full formulas and the distinction between total cleanup allowance and the healthy-deletion bound, and [Appendix B](#budget-lookup) for numeric examples. Budgets bound actions; each complete Ready unit triggers fresh accounting and progress within the remaining legal allowance, without an extra batch barrier.

### 0.4 Candidate selection: eligibility before ordering

Candidates must first satisfy version, partition, identity, dependency and in-flight constraints. Ordering never overrides budgets or physical capacity. The ordering below applies to template rollout; explicit scale-down selection is described separately in §0.6.

| Mode | Old-instance selection order | When the highest healthy old instance is blocked |
| --- | --- | --- |
| SG mode | Eligible old NotReady first, descending by ordinal within each health class, then old Ready | May first handle an eligible lower old NotReady instance |
| Role mode without `roleCoordination` | Same as SG mode | May first handle an eligible lower old NotReady instance |
| Role mode with `roleCoordination` | Descending stable old instances, with dependency and progress constraints | Wait; do not skip the highest stable candidate |

These are defaults with no new switch. `maxSkew` constrains proportional progress and does not promise index-paired updates. Legal reclamation of obsolete old NotReady **temporary surge** is not a stable-candidate skip, but still requires budget, dependency and in-flight checks and does not return stable-start allowance.

A NotReady unit already on the latest target is not repeatedly rebuilt by template rollout; explicit `recoveryPolicy` behavior applies separately. With successive requests v1 → v2 → v3, reclassify old versions and candidates against the latest target without requiring v2 to finish first. Already-issued deletions cannot be canceled.

### 0.5 Partition and versions: protection uses absolute ordinals

| State or action | Version and identity rule |
| --- | --- |
| An existing instance has ordinal below `partition` | Do not proactively update it because the target template changed; protection applies to fixed ordinals, not the first few currently sorted instances |
| Create or recreate a protected slot | Use the fixed, traceable historical template from before this canary rollout, based on partition at action time; wait safely if history cannot be determined |
| Create or recreate an unprotected slot | Use the latest target; when replacing an outdated high group in a low hole, the **new slot's** absolute ordinal selects the version |
| An existing newer instance becomes protected when partition rises | Retain it rather than proactively rebuild it to an older version; recreation after an actual subsequent deletion still follows the applicable historical rules |
| v1 → v2 → v3 has not fully completed | An intermediate version does not automatically become the protected baseline; advance that baseline only after full completion |

Partition limits template updates but does not prevent explicit scale-down from deleting protected instances. Protected failures still consume availability budget. When scaling Role instance counts, protected SGs use historical Pod/worker templates. See SG-P01–P11 and SG-R01 for trajectories.

A local fault repair retains the enclosing rollout unit’s applied template and worker layout; it must not apply the newest target merely because a member failed. The rollout unit is a complete SG in ServingGroupRollingUpdate and one Role instance in RoleRollingUpdate. Rebuilding a whole rollout unit selects the protected baseline or latest target using partition at action time. If partition rises around an existing v2 unit, a local Pod/Role repair still retains that unit’s applied v2; only a whole-unit rebuild reselects the protected baseline and may return to v1. If the relevant history cannot be established, wait and report rather than guess the latest version. Slot creation/recreation above means a complete rollout unit; local member repair follows this paragraph and API section 6.1.

### 0.6 Scaling, sparse ordinals and surge identity

When scaling and updating together, arrange formal capacity for the latest desired size before destructive rollout of healthy old units. Formal scale-out slots must be placed first, but new instances need not all be Ready; every healthy deletion still requires a fresh budget check. In SG mode, increasing Role instance counts must be applied in batches if it temporarily makes whole groups NotReady. Groups not yet selected use their applied member targets for Ready checks, preserving the same availability floor.

| Scenario | Identity and ordering rule |
| --- | --- |
| Explicit scale-down | Among retained groups, select NotReady first, then lower deletion cost, then higher ordinal; sparse ordinals are allowed, and scale-down removals are not charged again as rollout unavailability |
| Later scale-out | Use actual added capacity slots to fill the lowest holes first; one action need not fill every old hole |
| An old-template group already requiring replacement | Prefer the original ordinal within the formal range; an outdated group outside it may be replaced in the lowest old hole, using the historical/target version appropriate to the new slot |
| A healthy retained group needing no update | Do not delete and rebuild it merely to compact ordinals, even outside the desired ordinal range |
| Scale-out absorbs temporary surge | Retain its UID and adopt it only when its ordinal enters the new formal range and it can serve formal capacity; a matching total count is insufficient |
| Temporary surge still supplies required Ready capacity | Wait for replacement capacity to become Ready before cleanup; a retained high-ordinal group is not thereby temporary surge |

Only explicit scale-down can add stable holes. In-flight gaps from rollout or failures must still be repaired, not treated as a new stable sparse result. See SG-S01–S04, SG-C14 and SG-P11 for detailed counterexamples.

### 0.7 Completion, blocking and how to read the tables

Distinguish scale completion, a partition canary stopping point and full adoption of the target version. Rollout completion requires all eligible retained units to reach the latest target and become Ready, issued replacements to finish, temporary surge to be removed and the desired scale to be met. Existing sparse ordinals that cannot be repaired without disruption may remain. A canary stopping point may still contain historical versions and must not be reported as full target adoption.

A bad target, missing historical revision, exhausted budget, capacity or coordination constraint can block progress. Wait and report the cause instead of silently relaxing budgets or rebuilding healthy instances for contiguous numbering. After each complete unit becomes Ready, recompute maxScaleDown, maxHealthyScaleDown and inFlightReservations from current state. Continue when an eligible candidate and all budget, partition, identity, physical-capacity and coordination constraints allow; do not wait for every other unit from the earlier batch. Readiness loss or another fault consumes allowance again; a Ready event is not permanent or repeatable credit. Coordinated Roles still obey stable-candidate order, dependencies and maxSkew. See Appendix B.5 for partial readiness.

Scenario versions v1 / v2 / v3 are successive requested templates. Ready means the complete unit is ready; NotReady means it is not fully ready; Deleting still occupies active capacity; a dash means no instance exists at that ordinal. Configuration uses full field names; counts and budgets use the full names from [API §2.3](modelserving-api-reference.en.md#23-shared-budgets-and-default-candidate-selection), defined in [Appendix B](#budget-lookup).

Each table is **one permitted trajectory**, not a required total ordering of asynchronous events. “Active 4→2; Ready 2–4” means active count gradually decreases during deletion while Ready may vary within that range. Check counts, versions, UIDs, readiness and temporary/retained identities together, rather than only the final count.

<a id="scenario-tables"></a>

## 1. Sparse scale-down, opportunistic hole repair and order

### SG-S01: Scale-down holes and incremental repair

At desiredReplicas=4, deletion costs select 1 and 2, preserving healthy 3. desiredReplicas=2 retains {0,3}; desiredReplicas=3 has only one new capacity slot and fills 1, leaving hole 2. Stable reconciliation must not delete 3 to fill 2. A later desiredReplicas=4 fills 2 while preserving the UID of 3. Scale-down removals are not charged again as rollout unavailability.

| Step | desiredReplicas | sg-0 | sg-1 | sg-2 | sg-3 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | active 4; Ready 4 |
| 2 | 2 | v1 Ready | v1 Deleting | v1 Deleting | v1 Ready | active 4→2; Ready 2–4 |
| 3 | 2 | v1 Ready | — | — | v1 Ready | active 2; Ready 2 |
| 4 | 3 | v1 Ready | v1 NotReady | — | v1 Ready | active 3; Ready 2 |
| 5 | 3 | v1 Ready | v1 Ready | — | v1 Ready | active 3; Ready 3 |
| 6 | 4 | v1 Ready | v1 Ready | v1 NotReady→v1 Ready | v1 Ready | active 4; Ready 3→4 |

### SG-S02: An outdated high group can repay a low hole

From desiredReplicas=2, {0:v1,3:v1}, partition=0/maxUnavailable=1/maxSurge=0, submit v2. Group 3 is already due for replacement: delete it and put its replacement in lowest hole 1. Once Ready, replace 0 in place. The existing hole disappears as a side effect of necessary replacement, without moving a healthy target group.

| Step | sg-0 | sg-1 | sg-3 | Groups (active; Ready) |
| --- | --- | --- | --- | --- |
| 1 | v1 Ready | — | v1 Ready | active 2; Ready 2 |
| 2 | v1 Ready | — | v1 Deleting | active 2→1; Ready 1 |
| 3 | v1 Ready | v2 NotReady→v2 Ready | — | active 2; Ready 1→2 |
| 4 | v1 Deleting | v2 Ready | — | active 2→1; Ready 1 |
| 5 | v2 Ready | v2 Ready | — | active 2; Ready 2 |

### SG-S03: Preserve a healthy target high group

From desiredReplicas=2, {0:v1,3:v2}, partition=0/maxUnavailable=1/maxSurge=0, only 0 needs replacement. Final {0:v2,3:v2} keeps hole 1 and the UID of 3. maxSurge=1 does not permit creating 1:v2 and deleting retained healthy 3:v2 merely to compact ordinals. Retained high identities and temporary surge must be distinguished.

| Step | sg-0 | sg-1 | sg-3 | Groups (active; Ready) |
| --- | --- | --- | --- | --- |
| 1 | v1 Ready | — | v2 Ready | active 2; Ready 2 |
| 2 | v1 Deleting | — | v2 Ready | active 2→1; Ready 1 |
| 3 | v2 Ready | — | v2 Ready | active 2; Ready 2 |

### SG-S04: A surge outside the expanded range stays temporary

Retained {0:v1,1:v1,3:v2} at desiredReplicas=3/maxUnavailable=0/maxSurge=1 has temporary 4:v2. Increasing desiredReplicas to 4 makes retained 3 in-range but does not absorb surge 4. Although the old total count equals the new desiredReplicas, create retained 2:v2 under ceiling 5. Use surge 4 to roll 1 then 0, and remove 4 at the end. Preserve retained 3 throughout.

| Step | desiredReplicas | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3 | v1 Ready | v1 Ready | — | v2 Ready | v2 Ready (temporary surge) | active 4; Ready 4 |
| 2 | 4 | v1 Ready | v1 Ready | v2 NotReady | v2 Ready | v2 Ready (temporary surge) | active 5; Ready 4 |
| 3 | 4 | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5; Ready 5 |
| 4 | 4 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 4–5; Ready 4→5 |
| 5 | 4 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | — | active 4; Ready 4 |

### SG-S05: Repair a low unhealthy instance first by default

Scale from 3 to 2, deleting low-cost 1 and retaining 0/2:v1. Expand to 3 with bad v2 in hole 1, then submit v3 at desiredReplicas=3/maxUnavailable=1/maxSurge=0/partition=0. SG and independent Role rollout default to eligible old NotReady first: skip healthy old 2 to repair bad old 1, without an extra switch.

| Step | desiredReplicas / target | sg-0 | sg-1 | sg-2 | Groups (active; Ready) | Reason |
| --- | --- | --- | --- | --- | --- | --- |
| Initial | 3/v1 | v1 Ready | v1 Ready | v1 Ready | active 3; Ready 3 | Healthy baseline |
| Scale down | 2/v1 | v1 Ready | v1 Deleting | v1 Ready | active 3→2; Ready 2–3 | Cost selects 1 |
| Hole | 2/v1 | v1 Ready | — | v1 Ready | active 2; Ready 2 | High 2 remains formal |
| Expand and submit v2 | 3/v2 | v1 Ready | v2 NotReady | v1 Ready | active 3; Ready 2 | New formal slot fills hole |
| Submit v3 | 3/v3 | v1 Ready | v2 NotReady | v1 Ready | active 3; Ready 2 | minAvailable=2, maxScaleDown=1, maxHealthyScaleDown=0; cannot delete healthy 2 |
| Repair 1 first | 3/v3 | v1 Ready | v2 Deleting→v3 NotReady | v1 Ready | active 2–3; Ready 2 | Delete only bad old 1, replace in place |
| Repair ready | 3/v3 | v1 Ready | v3 Ready | v1 Ready | active 3; Ready 3 | Healthy credit returns |
| Roll high 2 | 3/v3 | v1 Ready | v3 Ready | v1 Deleting→v3 Ready | active 2–3; Ready 2→3 | Descending healthy candidates |
| Roll 0 | 3/v3 | v1 Deleting→v3 Ready | v3 Ready | v3 Ready | active 2–3; Ready 2→3 | Last old group |
| Final | 3/v3 | v3 Ready | v3 Ready | v3 Ready | active 3; Ready 3 | No extra healthy loss |

In the equivalent **Role + coordination comparison**, highest old 2 is healthy with maxHealthyScaleDown=0, so it must wait rather than skip to 1. maxScaleDown=1 cannot override order, maxSkew or dependencies. If replacement v3-1 also stays NotReady, unavailableTargetReplicas=1/maxScaleDown=0: wait instead of churning that target. This supersedes the old SG-S05 strict-order blocking expectation; Appendix B records executable migration coverage.

### SG-S06: Repair two bad v2 groups with v3

Whole-batch Ready rows show a trajectory where those completions are all observed before the next reconciliation, not a required batch barrier. Appendix B.5 distinguishes partial readiness.

desiredReplicas=5/maxUnavailable=2/maxSurge=0: replace highest 4 and 3 as a batch, but v2 stays non-Ready. At target v2, activeReplicas=5/unavailableTargetReplicas=2/maxScaleDown=0. Submitting v3 makes bad v2 old, unavailableTargetReplicas=0/maxScaleDown=2. Clean 4 then 3 without further Ready loss and create v3. As each complete v3 group becomes Ready, recompute allowance and continue through 2,1,0 without waiting for both. With no new target, do not rebuild same-target bad v2 repeatedly.

| Step | Target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | active 5; Ready 5 |
| 2 | v2 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | v1 Deleting | active 3–5; Ready 3 |
| 3 | v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | active 5; Ready 3 |
| 4 | v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | active 5; Ready 3 |
| 5 | v3 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | active 5; Ready 3 |
| 6 | v3 | v1 Ready | v1 Ready | v1 Ready | v2 Deleting | v2 Deleting | active 3–5; Ready 3 |
| 7 | v3 | v1 Ready | v1 Ready | v1 Ready | v3 NotReady | v3 NotReady | active 5; Ready 3 |
| 8 | v3 | v1 Ready | v1 Ready | v1 Ready | v3 Ready | v3 Ready | active 5; Ready 5 |
| 9 | v3 | v1 Ready | v1 Deleting | v1 Deleting | v3 Ready | v3 Ready | active 3–5; Ready 3 |
| 10 | v3 | v1 Ready | v3 NotReady | v3 NotReady | v3 Ready | v3 Ready | active 5; Ready 3 |
| 11 | v3 | v1 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | active 5; Ready 5 |
| 12 | v3 | v1 Deleting→v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | active 4–5; Ready 4→5 |
| 13 | v3 | v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | active 5; Ready 5 |

### SG-S07: Repair an entirely unhealthy initial version in bounded batches

Whole-batch Ready rows show a trajectory where those completions are all observed before the next reconciliation, not a required batch barrier. Appendix B.5 distinguishes partial readiness.

All five v1 groups are non-Ready before rollout, desiredReplicas=5/maxUnavailable=2/maxSurge=0. Submitting v2 gives maxScaleDown=2 despite Ready=0. Replace 4/3 initially; each complete v2 group becoming Ready restores the allowance permitted by fresh accounting, so progress through 2,1,0 need not wait for the whole earlier batch. Never replace all five at once. If the first two v2 groups remain non-Ready, unavailableTargetReplicas=2/maxScaleDown=0 and further cleanup stops until the target changes or another explicit recovery mechanism applies.

| Step | Target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | v1 | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | active 5; Ready 0 |
| 2 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | active 5; Ready 0 |
| 3 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v1 Deleting | v1 Deleting | active 3–5; Ready 0 |
| 4 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v2 NotReady | v2 NotReady | active 5; Ready 0 |
| 5 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v2 Ready | v2 Ready | active 5; Ready 2 |
| 6 | v2 | v1 NotReady | v1 Deleting | v1 Deleting | v2 Ready | v2 Ready | active 3–5; Ready 2 |
| 7 | v2 | v1 NotReady | v2 NotReady | v2 NotReady | v2 Ready | v2 Ready | active 5; Ready 2 |
| 8 | v2 | v1 NotReady | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 4 |
| 9 | v2 | v1 Deleting→v2 NotReady→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 10 | v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |

## 2. Scaling during rollout

### SG-C01: Scale out during rollout without surge

desiredReplicas changes 3 to 5 with maxUnavailable=1/maxSurge=0 while deletion of old 2 is already accepted. Fill new slots 3/4 with v2; keep completing the issued replacement. Ready=2 is below the new floor 4 because of scale-out and in-flight work, not permission to delete another v1. After real Ready credit, roll 1 then 0.

| Step | desiredReplicas | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 2 | 3 | v1 Ready | v1 Ready | v1 Deleting→v2 NotReady | — | — | active 2–3; Ready 2 |
| 3 | 5 | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | v2 NotReady | active 5; Ready 2 |
| 4 | 5 | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |
| 5 | 5 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 6 | 5 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 7 | 5 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |

### SG-C02: Scale down during rollout

desiredReplicas changes 3 to 2 with maxUnavailable=1 while 2:v2 is still non-Ready. Remove that unwanted non-Ready group first, wait for scale-down completion, then roll retained 1 and 0. Never recreate removed slot 2.

| Step | desiredReplicas | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 3 | v1 Ready | v1 Ready | v2 NotReady | active 3; Ready 2 |
| 2 | 2 | v1 Ready | v1 Ready | v2 Deleting | active 3→2; Ready 2 |
| 3 | 2 | v1 Ready | v1 Ready | — | active 2; Ready 2 |
| 4 | 2 | v1 Ready | v1 Deleting→v2 Ready | — | active 1–2; Ready 1→2 |
| 5 | 2 | v1 Deleting→v2 Ready | v2 Ready | — | active 1–2; Ready 1→2 |
| 6 | 2 | v2 Ready | v2 Ready | — | active 2; Ready 2 |

### SG-C03: Absorb old surge during scale-out

desiredReplicas changes 3 to 5, maxUnavailable=0/maxSurge=1/partition=0. Old Ready surge 3:v2 becomes retained with its UID intact. New 4:v2 is formal capacity; 5:v2 is the new temporary surge. Once real availability permits, roll 2,1,0 and remove 5. UID-preserving absorption is a ModelServing identity rule; Deployment aggregate surge does not itself promise Pod UID reuse.

| Step | desiredReplicas | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3 | v1 Ready | v1 Ready | v1 Ready | v2 Ready (temporary surge) | — | — | active 4; Ready 4 |
| 2 | 5 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | v2 NotReady (temporary surge) | active 6; Ready 4 |
| 3 | 5 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 6; Ready 6 |
| 4 | 5 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 5 | 5 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 6 | 5 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 7 | 5 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | — | active 5; Ready 5 |

### SG-C12: Keep useful surge while shrinking the retained set

desiredReplicas changes 4 to 3, maxUnavailable=0/maxSurge=1, with retained 0..3:v1 and Ready surge 4:v2. Remove retained 3 for scale-down first. The name of surge 4 being outside [0,desiredReplicas+maxSurge) is not a reason to discard its service credit: after 3 disappears, four active groups satisfy the count ceiling. Use the same surge to roll 2,1,0 and remove it at the end, without rebuilding it or first rolling the departing 3.

| Step | desiredReplicas | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready (temporary surge) | active 5; Ready 5 |
| 2 | 3 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | v2 Ready (temporary surge) | active 5→4; Ready 4–5 |
| 3 | 3 | v1 Ready | v1 Ready | v1 Ready | — | v2 Ready (temporary surge) | active 4; Ready 4 |
| 4 | 3 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | — | v2 Ready (temporary surge) | active 3–4; Ready 3→4 |
| 5 | 3 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | — | v2 Ready (temporary surge) | active 3–4; Ready 3→4 |
| 6 | 3 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | — | v2 Ready (temporary surge) | active 3–4; Ready 3→4 |
| 7 | 3 | v2 Ready | v2 Ready | v2 Ready | — | — | active 3; Ready 3 |

<a id="sg-c15-recompute-percentage-u-during-scale-out"></a>

### SG-C15: Recompute percentage maxUnavailable during scale-out

maxUnavailable=25% gives maxUnavailable=1/floor=4 at desiredReplicas=5 and maxUnavailable=2/floor=7 at desiredReplicas=9, with maxSurge=0. New slots 5..8 use v2. Ready=7 still gives no healthy deletion credit; Ready=8 permits deleting one old group. A non-Ready replacement 3 and non-Ready expansion 8 jointly consume maxUnavailable=2. The last table row is a separate bad-v2 run where initial 4:v2 was also never Ready: Ready=4 cannot justify another healthy deletion.

| Step | desiredReplicas / maxUnavailable / Ready floor | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | sg-6 | sg-7 | sg-8 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/1/4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | — | — | — | — | active 5; Ready 5 |
| 2 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | v2 NotReady | v2 NotReady | v2 NotReady | active 9; Ready 5 |
| 3 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | v2 NotReady | active 9; Ready 7 |
| 4 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | active 9; Ready 8 |
| 5 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting→v2 NotReady | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | active 8–9; Ready 7 |
| 6 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 9; Ready 9 |
| 7 | 9/2/7 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 8–9; Ready 8→9 |
| 8 | 9/2/7 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 8–9; Ready 8→9 |
| 9 | 9/2/7 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 8–9; Ready 8→9 |
| 10 | 9/2/7 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 9; Ready 9 |
| Separate bad-v2 run | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | v2 NotReady | v2 NotReady | v2 NotReady | active 9; Ready 4 |

<a id="sg-c16-count-actual-surge-in-both-c-and-v"></a>

### SG-C16: Count actual surge in both activeReplicas and unavailableTargetReplicas

desiredReplicas=3/maxUnavailable=1/maxSurge=1/partition=0, old 2 is already unhealthy. Creating non-Ready surge 3:v2 changes activeReplicas/unavailableTargetReplicas from 3/0 to 4/1, leaving maxScaleDown=1. Clean old 2 without Ready loss; rebuilding it makes unavailableTargetReplicas=2/maxScaleDown=0. Wait for new Ready credit before descending through 1 and 0. Never churn same-target bad surge; remove it when formal groups are complete. Using maxUnavailable-unavailableTargetReplicas alone would falsely block initial cleanup; ignoring unavailableTargetReplicas would overgrant credit.

| Step | sg-0 | sg-1 | sg-2 | sg-3 (temporary surge) | Counts before the action |
| --- | --- | --- | --- | --- | --- |
| 1 | v1 Ready | v1 Ready | v1 NotReady | — | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>maxScaleDown=1 |
| 2 | v1 Ready | v1 Ready | v1 NotReady | v2 NotReady | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>maxScaleDown=1 |
| 3 | v1 Ready | v1 Ready | — | v2 NotReady | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>maxScaleDown=0 |
| 4 | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=2<br>maxScaleDown=0 |
| 5 | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>maxScaleDown=1 |
| 6 | v1 Ready | — | v2 Ready | v2 NotReady | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>maxScaleDown=0 |
| 7 | v1 Ready | v2 NotReady | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=2<br>maxScaleDown=0 |
| 8 | v1 Ready | v2 Ready | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>maxScaleDown=1 |
| 9 | — | v2 Ready | v2 Ready | v2 NotReady | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>maxScaleDown=0 |
| 10 | v2 NotReady | v2 Ready | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=2<br>maxScaleDown=0 |
| 11 | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>maxScaleDown=1 |
| 12 | v2 Ready | v2 Ready | v2 Ready | — | activeReplicas=3<br>readyReplicas=3<br>unavailableTargetReplicas=0<br>maxScaleDown=1 |

## 3. Rollout during scaling and atomic requests

### SG-C04: A new template arrives during scale-out

desiredReplicas changes 3 to 5; creation of 3:v1 was already accepted while 4 is absent. Submit v2: leave in-flight 3 as v1 and create 4:v2. With 4 non-Ready, unavailableTargetReplicas=1/maxScaleDown=0. Once 4 is Ready, maxScaleDown=1 permits cleaning highest old 3 even if it never becomes Ready. Wait for replacement 3:v2 Ready, then roll healthy 2,1,0.

| Step | desiredReplicas / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 2 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | — | active 4; Ready 3 |
| 3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | v2 NotReady | active 5; Ready 3 |
| 4 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | v2 Ready | active 5; Ready 4 |
| 5 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | v2 Ready | active 5→4; Ready 4 |
| 6 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 Ready | active 5; Ready 4 |
| 7 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |
| 8 | 5/v2 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 9 | 5/v2 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 10 | 5/v2 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 11 | 5/v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |

### SG-C05: A new template arrives during scale-down

Scale 5 to 3. Deletion of 4 is already issued when v2 arrives. Continue removing 3, finish scale-down, then roll only retained 2,1,0. Do not first roll a group that is already selected for removal or resurrect 3/4.

| Step | desiredReplicas / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | active 5; Ready 5 |
| 2 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | active 5→4; Ready 4–5 |
| 3 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | — | active 4→3; Ready 3–4 |
| 4 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 5 | 3/v2 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | — | — | active 2–3; Ready 2→3 |
| 6 | 3/v2 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | — | — | active 2–3; Ready 2→3 |
| 7 | 3/v2 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | — | — | active 2–3; Ready 2→3 |
| 8 | 3/v2 | v2 Ready | v2 Ready | v2 Ready | — | — | active 3; Ready 3 |

### SG-C06: Atomic scale-out and template change

Submit desiredReplicas=5/v2 from desiredReplicas=3/v1 in one request. Arrange formal slots 3/4 with v2 first. Their non-Ready deficit cannot be borrowed as rollout credit. Once Ready capacity permits, replace highest retained old 2,1,0.

| Step | desiredReplicas / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 2 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | active 5; Ready 3 |
| 3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |
| 4 | 5/v2 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 5 | 5/v2 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 6 | 5/v2 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 7 | 5/v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |

### SG-C07: Atomic scale-down and template change

Submit desiredReplicas=3/v2 from desiredReplicas=5/v1. Select scale victims first; their deletions may run together. Wait for them to disappear, then roll retained 2,1,0 under the new budget. If health or deletion costs instead preserve a sparse high identity, apply S01–S03: only necessary old replacements may repay holes.

| Step | desiredReplicas / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | active 5; Ready 5 |
| 2 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | v1 Deleting | active 5→3; Ready 3–5 |
| 3 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 4 | 3/v2 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | — | — | active 2–3; Ready 2→3 |
| 5 | 3/v2 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | — | — | active 2–3; Ready 2→3 |
| 6 | 3/v2 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | — | — | active 2–3; Ready 2→3 |
| 7 | 3/v2 | v2 Ready | v2 Ready | v2 Ready | — | — | active 3; Ready 3 |

<a id="sg-c13-allocate-surge-against-the-expanded-n"></a>

### SG-C13: Allocate surge against the expanded desiredReplicas

At desired desiredReplicas=5, scale-out has already created Ready 3:v1 but not 4. v2 arrives with maxUnavailable=0/maxSurge=1. Fill formal 4 with v2, then use 5 as temporary surge. After Ready credit, roll 3,2,1,0 and remove 5. A formal expansion slot and a surge slot cannot both claim the same capacity.

| Step | desiredReplicas / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | — | — | active 4; Ready 4 |
| 2 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | — | active 5; Ready 4 |
| 3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady (temporary surge)→v2 Ready (temporary surge) | active 6; Ready 5→6 |
| 4 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 5 | 5/v2 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 6 | 5/v2 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 7 | 5/v2 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 8 | 5/v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | — | active 5; Ready 5 |

## 4. v1 to v2 to v3, bad targets and rollover

<a id="sg-c08-v3-supersedes-an-incomplete-v2-rollout-at-fixed-n"></a>

### SG-C08: v3 supersedes an incomplete v2 rollout at fixed desiredReplicas

desiredReplicas=3/maxUnavailable=1/maxSurge=0/partition=0. Group 2:v2 is Ready and deletion of 1:v1 is accepted, but its replacement create is not. Submit v3: finish that issued action by creating 1:v3, wait for Ready, then newly select highest old 2:v2 and finally 0:v1. If creation of 1:v2 was already accepted, do not edit it into v3. If 1:v2 is NotReady and now outdated, default SG/independent Role selection repairs it first: desiredReplicas=3/maxUnavailable=1/activeReplicas=3/readyReplicas=2/unavailableTargetReplicas=0/inFlightReservations=0/maxScaleDown=1/maxHealthyScaleDown=0. Replace only bad 1 in place with v3, preserving Ready=2; once Ready, roll healthy 2, then 0. If 1:v2 is Ready, roll healthy 2 before healthy 1. The coordinated Role comparison still waits behind healthy high 2 at maxHealthyScaleDown=0; percentage maxSkew is unchanged.

| Step | Target | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | v1 | v1 Ready | v1 Ready | v1 Ready | active 3; Ready 3 |
| 2 | v2 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | active 2–3; Ready 2→3 |
| 3 | v2 | v1 Ready | v1 Deleting | v2 Ready | active 3→2; Ready 2 |
| 4 | v3 | v1 Ready | v3 NotReady→v3 Ready | v2 Ready | active 3; Ready 2→3 |
| 5 | v3 | v1 Ready | v3 Ready | v2 Deleting→v3 Ready | active 2–3; Ready 2→3 |
| 6 | v3 | v1 Deleting→v3 Ready | v3 Ready | v3 Ready | active 2–3; Ready 2→3 |
| 7 | v3 | v3 Ready | v3 Ready | v3 Ready | active 3; Ready 3 |

### SG-C09: v1 to v2 to v3 during scale-out

Scale 3 to 5; 3:v1 already exists and 4 does not. Create last formal slot 4 with v2. When v3 arrives after 4:v2 is Ready, roll 4 to v3 first, then 3,2,1,0 directly from v1 to v3. There is no requirement to complete all-v2 first.

| Step | desiredReplicas / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 2 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | — | active 4; Ready 4 |
| 3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady→v2 Ready | active 5; Ready 4→5 |
| 4 | 5/v3 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | active 5; Ready 5 |
| 5 | 5/v3 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Deleting→v3 Ready | active 4–5; Ready 4→5 |
| 6 | 5/v3 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting→v3 Ready | v3 Ready | active 4–5; Ready 4→5 |
| 7 | 5/v3 | v1 Ready | v1 Ready | v1 Deleting→v3 Ready | v3 Ready | v3 Ready | active 4–5; Ready 4→5 |
| 8 | 5/v3 | v1 Ready | v1 Deleting→v3 Ready | v3 Ready | v3 Ready | v3 Ready | active 4–5; Ready 4→5 |
| 9 | 5/v3 | v1 Deleting→v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | active 4–5; Ready 4→5 |
| 10 | 5/v3 | v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | active 5; Ready 5 |

### SG-C10: v1 to v2 to v3 during scale-down

Scale 5 to 3, preserving accepted deletions. v2 first finishes scale-down and updates retained 2. v3 then updates 2 again, followed by 1 and 0 directly from v1. Only final retained identities are rolled. If scaling preserves sparse high groups, do not move healthy v3 groups merely because another target arrived.

| Step | desiredReplicas / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | active 5; Ready 5 |
| 2 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | active 5→4; Ready 4–5 |
| 3 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | — | active 4→3; Ready 3–4 |
| 4 | 3/v2 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | — | — | active 2–3; Ready 2→3 |
| 5 | 3/v3 | v1 Ready | v1 Ready | v2 Ready | — | — | active 3; Ready 3 |
| 6 | 3/v3 | v1 Ready | v1 Ready | v2 Deleting→v3 Ready | — | — | active 2–3; Ready 2→3 |
| 7 | 3/v3 | v1 Ready | v1 Deleting→v3 Ready | v3 Ready | — | — | active 2–3; Ready 2→3 |
| 8 | 3/v3 | v1 Deleting→v3 Ready | v3 Ready | v3 Ready | — | — | active 2–3; Ready 2→3 |
| 9 | 3/v3 | v3 Ready | v3 Ready | v3 Ready | — | — | active 3; Ready 3 |

### SG-C11: Rollback a bad expansion target

From desiredReplicas=3, expand to 5: 3:v1 is Ready while 4 has not been created. Bad v2 arrives and 4:v2 never becomes Ready. With desiredReplicas=5/maxUnavailable=1/maxSurge=0, leave healthy old groups intact. Returning the target to v1 makes unhealthy highest 4 outdated, so replace it with v1 without further Ready loss. All other v1 UIDs remain. This intentional automatic recovery differs from StatefulSet OrderedReady rollback cases that can require manual Pod deletion.

| Step | desiredReplicas / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 2 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | — | active 4; Ready 4 |
| 3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | active 5; Ready 4 |
| 4 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Deleting | active 5→4; Ready 4 |
| 5 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | active 5; Ready 4 |
| 6 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | active 5; Ready 5 |

### SG-C14: Old-version surge still provides indispensable service

desiredReplicas=3/maxUnavailable=0/maxSurge=1/partition=0. Ready surge 3:v2 enabled accepted deletion of 2:v1. v3 arrives: recreate 2 with v3 but retain healthy 3:v2 until 2 becomes Ready, otherwise Ready would fall below 3. Only then replace surge 3 with v3, serially reusing its slot after deletion. Use it to roll 1 then 0 and finally remove it. If 2:v3 also stays bad, preserve 3:v2 and stop further healthy deletion.

| Step | Target | sg-0 | sg-1 | sg-2 | sg-3 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | v1 | v1 Ready | v1 Ready | v1 Ready | — | active 3; Ready 3 |
| 2 | v2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready (temporary surge) | active 4; Ready 4 |
| 3 | v2 | v1 Ready | v1 Ready | v1 Deleting | v2 Ready (temporary surge) | active 4→3; Ready 3 |
| 4 | v3 | v1 Ready | v1 Ready | v3 NotReady | v2 Ready (temporary surge) | active 4; Ready 3 |
| 5 | v3 | v1 Ready | v1 Ready | v3 Ready | v2 Ready (temporary surge) | active 4; Ready 4 |
| 6 | v3 | v1 Ready | v1 Ready | v3 Ready | v2 Deleting (temporary surge)→v3 NotReady (temporary surge)→v3 Ready (temporary surge) | active 3–4; Ready 3→4 |
| 7 | v3 | v1 Ready | v1 Deleting→v3 Ready | v3 Ready | v3 Ready (temporary surge) | active 3–4; Ready 3→4 |
| 8 | v3 | v1 Deleting→v3 Ready | v3 Ready | v3 Ready | v3 Ready (temporary surge) | active 3–4; Ready 3→4 |
| 9 | v3 | v3 Ready | v3 Ready | v3 Ready | — | active 3; Ready 3 |

## 5. Partition, rounding and surge classification

<a id="sg-p01-expand-after-a-p1-canary-stop"></a>

### SG-P01: Expand after a partition=1 canary stop

Start desiredReplicas=3/partition=1 with 0:v1 and 1/2:v2 Ready. Expand to 5: new 3/4 use v2, and old 0..2 retain their UIDs.

| Step | desiredReplicas / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/1 | v1 Ready (protected) | v2 Ready | v2 Ready | — | — | active 3; Ready 3 |
| 2 | 5/1 | v1 Ready (protected) | v2 Ready | v2 Ready | v2 NotReady | v2 NotReady | active 5; Ready 3 |
| 3 | 5/1 | v1 Ready (protected) | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |

<a id="sg-p02-atomically-raise-integer-p-while-expanding"></a>

### SG-P02: Atomically raise integer partition while expanding

Start desiredReplicas=3/partition=3, fully protected. Submit desiredReplicas=6/partition=5 together: new 3/4 use historical v1 and 5 uses v2. Final five v1 and one v2. The old initial desiredReplicas=3/partition=5 example is now rejected by admission; the valid replacement changes partition with desiredReplicas rather than bypassing the active integer bound.

| Step | desiredReplicas / partition / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/3/v2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | — | — | — | active 3; Ready 3 |
| 2 | 6/5/v2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v1 NotReady (protected) | v1 NotReady (protected) | v2 NotReady | active 6; Ready 3 |
| 3 | 6/5/v2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | active 6; Ready 6 |

### SG-P03: Scale-down can cross the old protected range and leave holes

From desiredReplicas=5/partition=3, atomically shrink to desiredReplicas=2/partition=2. With equal health/cost, remove 4:v2, 3:v2 and 2:v1; retain 0/1:v1. partition is not a replica minimum, but the resulting partition must not exceed desiredReplicas. The second table starts {0:v1 Ready,1:v1 non-Ready,2:v2 Ready}, desiredReplicas=3/partition=2: shrinking to 2 removes unhealthy protected 1, preserving healthy high 2. Re-expansion fills protected hole 1 with historical v1 and retains UID 2.

| Step | desiredReplicas / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | v2 Ready | active 5; Ready 5 |
| 2 | 2/2 | v1 Ready (protected) | v1 Ready (protected) | v1 Deleting | v2 Deleting | v2 Deleting | active 5→2; Ready 2–5 |
| 3 | 2/2 | v1 Ready (protected) | v1 Ready (protected) | — | — | — | active 2; Ready 2 |

| Step | desiredReplicas / partition | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 3/2 | v1 Ready (protected) | v1 NotReady (protected) | v2 Ready | active 3; Ready 2 |
| 2 | 2/2 | v1 Ready (protected) | — | v2 Ready | active 2; Ready 2 |
| 3 | 3/2 | v1 Ready (protected) | v1 NotReady (protected)→v1 Ready (protected) | v2 Ready | active 3; Ready 2→3 |

### SG-P04: Scale-out raises a percentage partition

partition=50% resolves 2 at desiredReplicas=3 and 3 at desiredReplicas=5. Existing 2:v2 becomes protected but is not rolled back; create 3/4:v2. The result need not contain exactly partition old-version groups. If 2 later disappears, restore that protected slot with historical v1.

| Step | desiredReplicas / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | — | — | active 3; Ready 3 |
| 2 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready (protected) | v2 NotReady | v2 NotReady | active 5; Ready 3 |
| 3 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready (protected) | v2 Ready | v2 Ready | active 5; Ready 5 |

### SG-P05: Scale-down lowers a percentage partition

partition=50% resolves 3 at desiredReplicas=5 and 2 at desiredReplicas=3. Remove high 4/3 first, then update newly eligible 2:v1 to v2. Final 0/1 remain v1.

| Step | desiredReplicas / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | v2 Ready | active 5; Ready 5 |
| 2 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready | v2 Deleting | v2 Deleting | active 5→3; Ready 3–5 |
| 3 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v1 Deleting→v2 Ready | — | — | active 2–3; Ready 2→3 |
| 4 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | — | — | active 3; Ready 3 |

<a id="sg-p06-raise-p-while-an-old-deletion-is-in-flight"></a>

### SG-P06: Raise partition while an old deletion is in flight

desiredReplicas=3, 2:v2 Ready; deletion of 1:v1 was issued at partition=0. Raising partition to 3 cannot cancel deletion. Recreate slot 1 from historical v1, leave 0:v1 unchanged and preserve existing 2:v2 even though it is now protected.

| Step | desiredReplicas / partition / target | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 3/0/v2 | v1 Ready | v1 Deleting | v2 Ready | active 3→2; Ready 2 |
| 2 | 3/3/v2 | v1 Ready (protected) | v1 Deleting (protected) | v2 Ready (protected) | active 3→2; Ready 2 |
| 3 | 3/3/v2 | v1 Ready (protected) | v1 NotReady (protected)→v1 Ready (protected) | v2 Ready (protected) | active 3; Ready 2→3 |

<a id="sg-p07-lower-p-and-submit-v3-together"></a>

### SG-P07: Lower partition and submit v3 together

From desiredReplicas=3/partition=2 with 0/1:v1 and 2:v2, submit partition=1/v3. Roll highest old 2 to v3 first, then newly eligible 1 directly from v1 to v3. Keep 0:v1.

| Step | desiredReplicas / partition / target | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 3/2/v2 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | active 3; Ready 3 |
| 2 | 3/1/v3 | v1 Ready (protected) | v1 Ready | v2 Deleting→v3 Ready | active 2–3; Ready 2→3 |
| 3 | 3/1/v3 | v1 Ready (protected) | v1 Deleting→v3 Ready | v3 Ready | active 2–3; Ready 2→3 |
| 4 | 3/1/v3 | v1 Ready (protected) | v3 Ready | v3 Ready | active 3; Ready 3 |

### SG-P08: Percentage partition absorbs surge on expansion

desiredReplicas=3/partition=50%=2/maxUnavailable=0/maxSurge=1 has old 0..2 and non-Ready surge 3:v2. Expand to desiredReplicas=5/partition=3: retain UID 3 as formal, fill 4:v2, and protect old 2. No old eligible group remains, so do not allocate extra surge 5. Final three v1 and two v2.

| Step | desiredReplicas / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready | v2 NotReady (temporary surge) | — | — | active 4; Ready 3 |
| 2 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 NotReady | v2 NotReady | — | active 5; Ready 3 |
| 3 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | v2 Ready | — | active 5; Ready 5 |

### SG-P09: Protected faults consume the same availability budget

desiredReplicas=3/partition=1/maxUnavailable=1/maxSurge=0, protected 0:v1 is non-Ready and 1/2:v1 are Ready. Ready is already at floor 2: do not delete healthy 2. Once 0 recovers using its historical template, roll 2 then 1, retaining protected 0.

| Step | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- |
| 1 | v1 NotReady (protected) | v1 Ready | v1 Ready | active 3; Ready 2 |
| 2 | v1 Ready (protected) | v1 Ready | v1 Ready | active 3; Ready 3 |
| 3 | v1 Ready (protected) | v1 Ready | v1 Deleting→v2 Ready | active 2–3; Ready 2→3 |
| 4 | v1 Ready (protected) | v2 Ready | v2 Ready | active 3; Ready 3 |

### SG-P10: Continue partition rollout after absorbing surge

desiredReplicas changes 3 to 4, partition=1/maxUnavailable=0/maxSurge=1. Ready surge 3:v2 becomes formal with its UID intact. Old 1/2 still need updates, so obtain Ready credit from new temporary 4:v2, roll 2 then 1, and remove 4. Keep 0:v1.

| Step | desiredReplicas / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/1 | v1 Ready (protected) | v1 Ready | v1 Ready | v2 Ready (temporary surge) | — | active 4; Ready 4 |
| 2 | 4/1 | v1 Ready (protected) | v1 Ready | v1 Ready | v2 Ready | v2 NotReady (temporary surge)→v2 Ready (temporary surge) | active 5; Ready 4→5 |
| 3 | 4/1 | v1 Ready (protected) | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 4–5; Ready 4→5 |
| 4 | 4/1 | v1 Ready (protected) | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 4–5; Ready 4→5 |
| 5 | 4/1 | v1 Ready (protected) | v2 Ready | v2 Ready | v2 Ready | — | active 4; Ready 4 |

### SG-P11: The replacement absolute ordinal selects its version

desiredReplicas=2 retains {0:v1,3:v1}; submit v2 at partition=2/maxUnavailable=1/maxSurge=0. Outdated eligible 3 may be removed and its replacement placed in lowest hole 1, which uses historical v1 because 1<partition. The two-v1 result is a canary stop, not full v2 adoption: currentRevision=v1, updateRevision=v2, updatedReplicas=0. Lowering partition to 1 updates slot 1 using the same target revision. This preserves the separately approved 2026-10-06 clarification; a healthy target 3:v2 would instead remain under S03. The same absolute-ordinal interpretation applies to Role partition.

| Step | desiredReplicas / partition / target | sg-0 | sg-1 | sg-3 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 2/2/v2 | v1 Ready (protected) | — | v1 Ready | active 2; Ready 2 |
| 2 | 2/2/v2 | v1 Ready (protected) | — | v1 Deleting | active 2→1; Ready 1 |
| 3 | 2/2/v2 | v1 Ready (protected) | v1 NotReady→v1 Ready (protected) | — | active 2; Ready 1→2 |
| 4 | 2/2/v2 | v1 Ready (protected) | v1 Ready (protected) | — | active 2; Ready 2 |
| 5 | 2/1/v2 | v1 Ready (protected) | v1 Deleting→v2 Ready | — | active 1–2; Ready 1–2 |

## 6. Nested Role replica changes

### SG-R01: Increase Role members during a partition canary

desiredReplicas=3/partition=1: protected 0 uses historical worker W1; 1/2 use target W2. Raising Role replicas from 1 to 2 creates no SG template revision and does not trigger hole compaction. Under maxUnavailable=1/maxSurge=0, apply new membership to 2, wait Ready, then 1, then 0 using W1. Groups not yet selected retain Ready credit against their own applied old membership; the global replica edit cannot make all groups unavailable at once.

| Step | Role members per group | sg-0 | sg-1 | sg-2 | Ready groups |
| --- | --- | --- | --- | --- | --- |
| 1 | 1 | v1/W1×1 Ready (protected) | v2/W2×1 Ready | v2/W2×1 Ready | 3 |
| 2 | 2 | v1/W1×1 Ready (protected) | v2/W2×1 Ready | v2/W2×1 Ready+v2/W2×1 NotReady | 2→3 |
| 3 | 2 | v1/W1×1 Ready (protected) | v2/W2×1 Ready+v2/W2×1 NotReady | v2/W2×2 Ready | 2→3 |
| 4 | 2 | v1/W1×1 Ready (protected)+v1/W1×1 NotReady | v2/W2×2 Ready | v2/W2×2 Ready | 2→3 |
| 5 | 2 | v1/W1×2 Ready (protected) | v2/W2×2 Ready | v2/W2×2 Ready | 3 |

## 7. Acceptance and counterexamples

Record the current desiredReplicas/partition/maxUnavailable/maxSurge and each group's ordinal, version, UID, Ready and deletion state. Distinguish retained identities from temporary surge, and keep accepted actions and historical templates. An interleaving test must prove that its preceding operation was still in flight when the next request arrived.

Final count alone is insufficient. Check that non-scaling work creates no new stable holes, healthy target retained identities are not deleted for compaction, creates obey the surge ceiling, and healthy rollout deletion preserves the availability floor. When a pre-existing fault already violates the floor, outdated unhealthy cleanup may proceed only without reducing Ready, within maxScaleDown, descending within the selected health class; only coordinated Role comparisons prohibit skipping a healthy higher old candidate. Transient excess after scale-down can only decrease.

Three counterexamples are mandatory: S01's {0,3} to {0,1,3} leaves hole 2; S05 repairs the bad lower old instance first without spending maxScaleDown on a healthy higher one, while its coordinated Role comparison waits; C14 retains outdated surge while it is still needed for service. S06/S07 permit bounded recovery of outdated unhealthy highest groups even below the Ready floor. C16 requires accounting for both activeReplicas and unavailableTargetReplicas when actual non-Ready surge exists. No state-table row is itself a Kind result.

<a id="behavior-rules"></a>

## Appendix A. SG behavioral rules

Original design dated 2026-09-20, with recorded amendments through 2026-10-07. Shared budget, identity/history and selection principles apply to complete Role instances; API section 5 and Appendix B add coordination limits. SG-R01 nested membership expansion is not mechanically a Role template rollout. These product requirements do not derive their authority from the currently observed controller implementation. The 35 scenarios instantiate these rules.

### A.1. Decisions and community references

Replacing a whole inference group can reload models, occupy expensive GPUs and incur lengthy warm-up. Priority is: preserve existing availability and capacity ceilings, then avoid rebuilding healthy groups that need no update, then converge scale and opportunistically repay existing identity holes. SG/independent Role selection prioritizes eligible old NotReady, then old Ready, descending within each class. Coordinated Role stable candidates cannot skip. Selection cannot override maxScaleDown, healthy bound maxHealthyScaleDown, partition, dependencies or physical capacity.

| Topic | Community reference | ModelServing decision |
| --- | --- | --- |
| Identity and partition | StatefulSet stable ordinals, historical template below partition, descending replacement | Absolute threshold, but health/cost-aware scale-down may retain sparse identities |
| Budgets | Deployment floors maxUnavailable and ceils maxSurge; LWS budgets complete groups | Recompute against latest desiredReplicas; only complete groups count Ready; scale-out deficits grant no deletion credit |
| Latest intent | Deployment rollover; StatefulSet bad-template recovery may require intervention | A to B to C may skip full B completion; eligible outdated NotReady B can take priority for replacement by C, descending within its class; coordinated Role stable order remains |
| Inference cost | LWS group rollout; RBG Role/coordination distinction | Preserve healthy target retained identities and keep SG and Role replica axes separate |

The Deployment calculation `maxScaledDown = allPodsCount - minAvailable - newReplicaSetPodsUnavailable` illustrates the accounting of actual capacity and unavailable new replicas. Cleanup does not borrow configured but uncreated surge. With desiredReplicas=10/maxUnavailable=2/maxSurge=3 and 13 actual replicas, five unavailable new replicas give 13-8-5=0 cleanup allowance; returning to an old good target with one unavailable target replica gives 4, while a brand-new empty target gives 5. Bad replicas that become old can then be cleaned without further availability loss. This does not grant extra maxUnavailable.

For SG at a stable checkpoint with no in-flight creates/deletes:

```text
minAvailable = max(0, desiredReplicas-maxUnavailable)
maxScaleDown = max(0, activeReplicas-minAvailable-unavailableTargetReplicas-inFlightReservations)
maxHealthyScaleDown = max(0, readyReplicas-minAvailable)
```

activeReplicas includes actual surge; unavailableTargetReplicas includes latest-target NotReady surge. inFlightReservations is committed allowance not yet reflected in activeReplicas/unavailableTargetReplicas, counted once; readyReplicas includes protected/old Ready capacity and excludes committed deletions. At inFlightReservations=0, maxScaleDown=max(0,activeReplicas-minAvailable-unavailableTargetReplicas); only with maxUnavailable<=desiredReplicas and activeReplicas=desiredReplicas does this become max(0,maxUnavailable-unavailableTargetReplicas). Use API section 2.3 definitions. Default selection takes oldNotReadyToReplace=min(|eligibleOldNotReady|,maxScaleDown), then oldReadyToReplace=min(|eligibleOldReady|,maxScaleDown-oldNotReadyToReplace,maxHealthyScaleDown), subject to partition, identity, dependencies, reservations and physical capacity. Recheck actual readyReplicas before healthy deletion. A new NotReady surge increments activeReplicas and unavailableTargetReplicas together, leaving maxScaleDown unchanged.

References: [Deployment rolling.go](https://github.com/kubernetes/kubernetes/blob/master/pkg/controller/deployment/rolling.go#L803-L918), [StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/), [Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/), [LWS rollout](https://lws.sigs.k8s.io/docs/concepts/leaderworkerset/rollout-strategy/), [LWS API](https://lws.sigs.k8s.io/docs/reference/leaderworkerset.v1/), [RBG API](https://github.com/sgl-project/rbg/blob/main/doc/reference/api.md).

These are references, not a claim that one upstream controller specifies the combined behavior. Deployment's choice among old ReplicaSets does not impose SG ordinal order; this design borrows accounting, not arbitrary unhealthy-first selection. LWS's update-start percentage base is not copied: use latest desiredReplicas. Nor is its partition burst retention copied: at a canary stop, remove unneeded temporary surge once eligible retained groups and availability satisfy the contract, without waiting for partition to reach zero.

### A.2. State, budgets and identity

desiredReplicas is latest desired replicas; desiredOrdinalRange=[0,desiredReplicas) is the preferred ordinal range, not mandatory compaction on every reconciliation. activeGroups includes groups that have not completely disappeared, including Deleting physical capacity. readyReplicas counts complete Ready groups still serving. Distinguish retained groups (`retainedGroups`) from temporary surge groups (`temporarySurgeGroups`): both count in activeGroups and in real activity/availability. A high retained group does not become temporary surge merely because ordinal>=desiredReplicas. A previous temporary surge group becomes a retained group only when its ordinal enters the new desiredOrdinalRange and can supply formal capacity, not because |activeGroups|=desiredReplicas. missingOrdinals=desiredOrdinalRange minus ord(retainedGroups) describes holes; after shrinking, |retainedGroups|=desiredReplicas with missingOrdinals nonempty is legal. maxSurge limits actual excess above desiredReplicas, not group names.

partition is an absolute ordinal boundary. Existing ordinal<partition groups are protected from template-driven replacement; ordinal>=partition may converge to the latest target. Percentage partition ceils against latest desiredReplicas. partition does not impose a minimum replica count or forbid explicit removal of protected groups during shrinking; the admission contract still requires active integer partition<=desiredReplicas.

maxUnavailable is the configured integer or floor(percent*desiredReplicas); maxSurge is the integer or ceil(percent*desiredReplicas), defaulting to 1/0. Do not raise a positive percentage maxUnavailable to one. Reject resolved active maxUnavailable=maxSurge=0 even at zero replicas or full partition: desiredReplicas=3/maxUnavailable=25%/maxSurge=0 must be rejected, not silently corrected. Active integer maxUnavailable/partition cannot exceed desiredReplicas; zero replicas allow default/explicit integer maxUnavailable=1. maxSurge may exceed 100%, but desiredReplicas+maxSurge must fit int32. Recompute after scaling rather than freezing at rollout start.

Require |activeGroups|<=desiredReplicas+maxSurge. If a reduction of desiredReplicas temporarily leaves excess existing groups, only cleanup/wait is permitted until creates fit again. Healthy rollout deletion preserves readyReplicas>=max(0,desiredReplicas-maxUnavailable). An already unhealthy outdated group can be replaced below that floor without further Ready loss only within maxScaleDown, descending order, partition and in-flight reservations. Do not repeatedly rebuild a same-target unhealthy group. Protected faults and new non-Ready groups charge the same availability accounting.

Explicit scale-down is authorized toward latest desiredReplicas; do not double-charge its removals as additional rollout disruptions. Subsequent template replacements obey the new availability floor. Surge is a temporary ceiling, not a required replica count, and scaling and rollout cannot spend it twice.

### A.3. Order and minimal disruption

Only scale-down can create new stable holes. Rolling replacement and fault recovery may have transient missing slots, but cannot treat those as new stable sparse outcomes. At settled checkpoints, scale-out and rollout do not increase identity debt: |missingOrdinalsAfter|<=|missingOrdinalsBefore|, accounting for expansion of desiredOrdinalRange and its new target slots. Do not compare transient in-flight counts as stable layouts. Existing holes can be repaid over multiple operations; missingOrdinals need not become empty in one operation.

1. Add at most the genuinely new formal capacity, filling the lowest missing ordinal within desiredOrdinalRange first. Reuse UIDs when expansion absorbs old surge or high retained groups. An old surge still outside desiredOrdinalRange remains temporary even if total count equals new desiredReplicas; it may yield capacity under maxUnavailable/maxSurge. Without a free formal slot, do not delete healthy target retained groups to compact identities.
2. Preserve healthy retained groups requiring no update: both existing protected groups (even a later-protected B) and eligible current-target groups. A position outside desiredOrdinalRange does not authorize compaction or forced rollback.
3. Replace an outdated in-range group at its own ordinal; deleting 0 and rebuilding at 4 would create a new hole. An outdated out-of-range retained group already due for replacement may instead fill the lowest hole within desiredOrdinalRange. Its new slot's absolute ordinal selects history or target. A protected destination uses history, and may result in an all-old canary stop as in P11; never claim full target adoption.
4. A zero-downtime temporary group paired with an outdated out-of-range retained group due for deletion may be created directly in the lowest formal hole, selecting history/target by that slot's partition. Otherwise use the lowest free out-of-range ordinal and clean it afterward. Do not create an unretainable group in a hole and later delete a healthy target just to restore layout.
5. Determine scale-down count from retainedGroups first. Within retainedGroups prefer non-Ready, then lower deletion cost, then higher ordinal; partition does not change this ordering. Evaluate temporarySurgeGroups separately against new desiredReplicas/maxSurge and the Ready floor. A numerically highest useful surge is not automatically the first scale victim. Protected old groups may be removed while higher new groups remain. Accepted deletion is irreversible; new spec governs future actions.

Thus {0,3} at desiredReplicas=2 expanded to desiredReplicas=3 yields {0,1,3}, still missing 2. A healthy 3 needing no update is preserved. Later desiredReplicas=4 can fill 2 without recreating 3. Order means avoiding new stable holes, preferring the lowest existing hole at a legal opportunity, and retaining useful identities, not forcing [0,desiredReplicas) every time.

### A.4. Operation ordering and version selection

Recompute desiredReplicas/partition/maxUnavailable/maxSurge/target from latest spec. Previously planned future actions are not commitments; accepted API creates/deletes remain facts. Recreating an already deleted slot completes an issued action and is not newly skipping a higher candidate. Subsequent SG/independent Role selection prioritizes old NotReady, descending within each health class; coordinated Role stable selection remains descending without skipping.

1. Recognize deleting/creating groups and surge, reclassify using new desiredReplicas/partition. A deleting group retains its name and physical reservation until fully gone.
2. On shrink, select victims and wait for planned scale deletions. On expansion, arrange new formal slots in lowest holes; creates already accepted keep their template, unissued creates use the latest applicable version. Distinct formal slots may be created together. Arrange formal capacity before destructive rollout, but no blanket wait for all new groups Ready is imposed: each healthy deletion independently checks real readyReplicas-1>=max(0,desiredReplicas-maxUnavailable). Increased maxUnavailable after expansion does not make its missing capacity free credit. Do not delete old groups to make room for formal slots that have not yet been arranged.
3. SG/independent Role selection first takes eligible old NotReady instances, descending within that class, then healthy old instances. A low bad old instance may precede a healthy higher one, but cleanup stays within remaining maxScaleDown and partition/identity constraints. Wait for deletion before reusing names or physical capacity. Accepted actions reserve credit; do not reclaim it repeatedly while replacements are unready. A bad current target waits for correction or explicit recovery rather than same-version churn. Coordinated Role comparisons instead take a descending stable old prefix, also constrained by remainingStart and dependencies; stop at a blocker.
4. Healthy candidates remain descending. Obtain legal surge if needed, preserve readyReplicas>=max(0,desiredReplicas-maxUnavailable) after each deletion and stay within maxHealthyScaleDown. Otherwise wait for healthy deletion credit. maxUnavailable>1 permits multiple eligible starts together. After each complete unit becomes Ready, recompute maxScaleDown, maxHealthyScaleDown and inFlightReservations from current state. Continue when an eligible candidate and all budget, partition, identity, physical-capacity and coordination constraints allow; do not wait for every other unit from the earlier batch. Readiness loss or another fault consumes allowance again; a Ready event is not permanent or repeatable credit. Coordinated Roles still obey stable-candidate order, dependencies and maxSkew. Protected missing/faulted slots use history and cannot bypass partition.
5. Retain healthy temporary surge while it supports the desiredReplicas-maxUnavailable floor; remove it after replacement capacity is Ready. Preserve its UID when desiredReplicas absorbs it. Superseded old NotReady surge may be legally reclaimed first within maxScaleDown, without transferring its cleanup credit to healthy stable instances. Check actual temporary identity, dependencies and in-flight actions; coordinated Role cleanup neither skips a stable ordinal nor refunds stable starts.

The desiredReplicas=5/maxUnavailable=2 examples show why both rejecting all recovery below the floor and replacing all bad groups at once are wrong. With bad target B at 4/3, maxScaleDown=0; a new target C makes maxScaleDown=2 and permits replacing those two first. If only one of the two C groups becomes Ready, readyReplicas=4, unavailableTargetReplicas=1 and both maxScaleDown and maxHealthyScaleDown equal 1: healthy old 2 may start without waiting for the other. All-unhealthy A at Ready=0 similarly permits initial starts 4/3 followed by fresh accounting after each complete new Ready unit, rather than a fixed batch barrier. In S05, high 2:v1 is Ready, low 1:v2 is NotReady and the target is v3: with maxScaleDown=1/maxHealthyScaleDown=0, default selection repairs 1 in place without Ready loss, then rolls healthy 2 after capacity returns. The equivalent coordinated Role state still waits behind its highest old candidate. Exhausted maxScaleDown, bad targets, protected faults, dependencies and scheduling capacity can still block progress; automatic recovery is not guaranteed for every state.

When creating/recreating a complete rollout unit, ordinal<partition uses the fixed, traceable historical template for this canary, and ordinal>=partition uses latest target. Increasing partition protects an existing B without rolling it back; if it later disappears, historical A may be recreated in its protected slot. The baseline is the most recently fully completed template before this canary; intermediate A→B→C submissions do not promote B. Only full adoption after releasing protection establishes the target as a future baseline. If history cannot be established, wait safely rather than guess the newest template.

Role replica changes affect internal membership, not SG template revision. Protected groups keep historical Pod/worker templates. If adding required members makes groups temporarily non-Ready, apply membership in batches that respect real desiredReplicas-maxUnavailable. Groups not yet selected remain evaluated against their own applied old membership; global desired-count edits cannot instantly revoke Ready credit everywhere. A selected group becomes Ready again only when all new members are Ready. Existing faults that consume maxUnavailable require recovery or legal surge, not relaxed budgets.

A local fault repair retains the enclosing rollout unit’s applied template and worker layout; it must not apply the newest target merely because a member failed. The rollout unit is a complete SG in ServingGroupRollingUpdate and one Role instance in RoleRollingUpdate. Rebuilding a whole rollout unit selects the protected baseline or latest target using partition at action time. If partition rises around an existing v2 unit, a local Pod/Role repair still retains that unit’s applied v2; only a whole-unit rebuild reselects the protected baseline and may return to v1. If the relevant history cannot be established, wait and report rather than guess the latest version. Local old-version repair followed by a valid template rollout is permitted; the earlier recovery action cannot delete replacement UIDs.

<a id="legacy-deletion-records"></a>

**Deletion convergence after controller restart (2.6)**

The deletion-marker and transaction version has not been deployed, so there is no legacy-record compatibility burden. The controller does not persist the Pod set, operation phase or cross-restart authority for a Role/SG deletion batch, and no Pod annotation carries that authority. While the controller process remains active, RoleRecreate and ServingGroupRecreate still complete their configured scope, with original UID and owner checks preventing retries from absorbing same-name replacements.

After controller restart, abandon exact continuation of the old batch and converge from current facts:

| Fact at restart | Expected handling | Example |
| --- | --- | --- |
| No DELETE from the old batch was issued | Re-evaluate latest configuration, partition, candidates and budgets; the discarded plan grants no authority | Raising partition immediately protects the original Pods that still exist |
| A whole-group or Role deletion completed only partly and healthy members survive | Refill missing members from the enclosing rollout unit's applied historical template; once Ready, retain healthy survivors rather than deleting them to finish the old batch | Prefill was deleted while decode survived; refill same-version prefill and retain the decode UID |
| An old Pod is still Terminating or the current unit is unavailable | Count actual occupancy/unavailability in current capacity and budgets; losing process-local state grants no duplicate deletion credit | Do not reuse the name or slot while the old UID exists, and do not delete another healthy unit with the same allowance |
| The refilled workload still satisfies current fault conditions | Start a new recovery episode under current recoveryPolicy, grace and complete health checks | None/grace=-1 performs no proactive deletion; finite grace may wait another detection cycle |

Local refill uses traceable historical templates, applied membership and current partition rules. Units that still need an update continue through normal current budgets, order and roleCoordination. UID/owner preconditions, stale-event isolation, complete Ready classification, Terminating occupancy and same-name replacement protection remain mandatory. A genuinely lost complete Role with reliable completion facts still follows the current recoveryPolicy scope; only an incomplete controller-initiated batch is not replayed after restart.

<a id="restart-convergence-cases"></a>

**Interrupted recovery and restart: trace and case lookup (2.6)**

All four cases use `desiredReplicas=2`, `maxUnavailable=1`, `maxSurge=0`. A unit is a complete group in SG mode, or a complete prefill entry + worker instance in Role mode; decode stays at its original version in Role mode. Establish fully Ready A, submit B with `partition=2`, then trigger recovery through a real Failed Pod. Restart only after the failed UID is gone, the healthy survivor DELETE has been rejected and the recovery scope's completion facts have been cleared. This distinguishes an already-started partial recovery from an externally deleted complete Role while the controller is offline.

| Case | Interrupted recovery boundary | Healthy members that must survive restart | Refill and later update |
| --- | --- | --- | --- |
| [RC-01](../../cases/restart-convergence/suite.json): SG + ServingGroupRecreate | Group 0 decode is gone; prefill survives | Original prefill UID; all group 1 UIDs while missing decode consumes the budget | Refill historical A decode; group 1 may roll to B once group 0 is completely Ready |
| RC-02: SG + RoleRecreate | Group 0 prefill worker is gone; entry survives | Original entry and same-group decode UIDs; group 1 remains budget constrained | Refill with the enclosing SG's applied A template and worker layout, without replacing its healthy entry |
| RC-03: independent Role + RoleRecreate | Prefill instance 0 worker is gone; entry survives | Instance 0 entry and all other Role UIDs; no deletion of healthy prefill instance 1 while incomplete | Refill historical A worker; instance 1 may roll to B after the whole instance becomes Ready |
| RC-04: coordinated Role + RoleRecreate | Same as RC-03, with roleCoordination and maxSkew=50% | Same as RC-03; restart grants no skipping, extra allowance or coordination exemption | Converge under current partition, budgets and coordination; this is not exhaustive maxSkew/dependency/order coverage |

The common observable trace is:

| Phase | Current facts and action | Allowed result | Failure condition |
| --- | --- | --- | --- |
| Stop after partial recovery; partition 2→1 | Low unit is missing a member; healthy high A becomes eligible | A new controller UID starts with the same image and completes initialSync | PASS without establishing a genuine partial recovery boundary |
| Temporarily reject missing-member POST after restart | One complete Ready unit; zero healthy deletion allowance | Retain survivors and all high-unit UIDs while awaiting refill | Replay survivor deletion or roll the high unit, dropping Ready 1→0 |
| Release the POST fault | Low unit remains protected by partition=1 | First replacement UID uses historical A; complete entry/worker Ready; retain healthy survivors | Refill protected members with B, release unit credit for entry-only Ready, or delete the fresh replacement again |
| Resume the current rollout after refill | Low A is completely Ready and allowance returns | Legally roll high to B; Ready remains at least 1 | Delete healthy survivors to finish an obsolete batch, or exceed the budget |
| partition 1→0 | Low A now requires an update | Ordinary rollout may replace the previously retained A UIDs; eligible targets finish B Ready | Treat survivor retention as a permanent exemption from future lawful rollout |
| Inject another real fault without restarting | Prior rollout is stable; active recovery is still configured | Complete the new configured Role/SG scope, replacing only those UIDs | Apply restart simplification to uninterrupted recovery by repairing only one Pod |

If refill remains unhealthy, reassess only under **current** health, recoveryPolicy and grace: pure NotReady alone does not trigger proactive recovery; None and grace=-1 do not proactively delete. Existing fault-start persistence across restart still applies. Those boundaries retain API §6 and `recovery-contract` / `grace-restart` coverage; RC-01–04 do not add grace-timing coverage. Rolling restarts with a still-Terminating old Pod remain separate in `controller-restart/RUN-436` and `RUN-439`; they do not replace these partial fault-recovery cases.

See [restart convergence execution](../RESTART_CONVERGENCE.md) for the command and evidence requirements. Definitions, validator tests and actual Kind outcomes are recorded separately; final B Ready alone does not establish the intermediate guarantees above.

### A.5. Completion, blocking and implementability

- Scale is complete when planned shrink deletions are gone, |retainedGroups|=desiredReplicas, and newly retained groups are fully Ready on their applicable versions. Stable identity debt may remain if it cannot be repaid without needless disruption.
- Rollout is complete when all existing eligible retained groups are Ready on the latest target, issued replacements are complete, temporary surge is removed and |activeGroups|=desiredReplicas. missingOrdinals need not be empty. Even at partition=desiredReplicas, a sparse retained high ordinal can still be eligible. A canary pause exists only after no eligible retained group remains; never rebuild healthy groups merely to manufacture it.
- Track holes and out-of-range retained identities separately. They are not inherently incomplete versions and do not authorize deletion on an ordinary reconcile. Fault-created missing capacity still requires recovery and must not be confused with stable scale-down debt.
- Bad targets, missing history, exhausted maxScaleDown, insufficient capacity/budget or coordination blockers stop the relevant new actions and report the cause. Do not silently relax maxUnavailable/maxSurge, reverse accepted deletion or add rebuilds for superficial continuity.

A controller must preserve enough state across restart to distinguish retained groups, temporary surge and scale-down identity debt; count/ordinal alone is insufficient. Save/recover live historical templates and account for real Ready/Deleting/latest desiredReplicas/actual activeReplicas/target unavailableTargetReplicas with in-flight reservations. This needs finite controller state and selection logic, not impossible Pod renaming. The contract does not claim current production compliance or substitute static reasoning for Kind verification.

<a id="budget-lookup"></a>

## Appendix B. Shared SG / Role budget and behavior lookup

Use API section 2.3: `minAvailable=max(0,desiredReplicas-maxUnavailable)`, `maxScaleDown=max(0,activeReplicas-minAvailable-unavailableTargetReplicas-inFlightReservations)`, `maxHealthyScaleDown=max(0,readyReplicas-minAvailable)`. One unit is a complete SG, or a complete instance of one Role within one SG. Default means SG or Role without coordination; coordinated means Role rollout with coordination. Unless stated otherwise, history is known, no conflicting action is in flight, and coordination has remaining starts and satisfied dependencies. Versions use v1/v2/v3; the hole set is named missingOrdinals.

| Full name | Meaning |
| --- | --- |
| `desiredReplicas` | Latest desired count: `spec.replicas` for ServingGroup mode, or that Role’s `replicas` within each group for Role mode |
| `maxUnavailable` / `maxSurge` / `partition` | Resolved integers at the active level; percentages use the latest desired replica count |
| `activeReplicas` | Actual active instances, including actual surge; Deleting instances occupy capacity until fully gone |
| `readyReplicas` | Instances whose required members are all Ready, including protected, old and usable surge capacity, excluding committed deletions |
| `unavailableTargetReplicas` | Unavailable latest-target instances, including target surge |
| `inFlightReservations` | Committed allowance not yet reflected by lower active count or higher target-unavailable count; deduct once |
| `minAvailable` | Minimum Ready count |
| `maxScaleDown` | Total old-instance cleanup allowance |
| `maxHealthyScaleDown` | Healthy old-instance deletion bound |

### B.1 Numeric and candidate-selection comparison

`desiredReplicas/maxUnavailable/maxSurge/partition` is the latest effective configuration; `activeReplicas/readyReplicas/unavailableTargetReplicas/inFlightReservations` is the actual ledger before the action. RU-Bxx are lookup IDs, not new executable case IDs or passing-test counts.

| Lookup ID / state | Effective configuration | Counts before the action | Deletion budget | Default: SG / independent Role | Role + coordination |
| --- | --- | --- | --- | --- | --- |
| RU-B01 All old healthy; submit v2 | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=3<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | Replace highest old 2 first; Ready stays at least 2 | Same, also bounded by remainingStart/dependencies |
| RU-B02 Low old 1 bad, high old 2 healthy; target v3 | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | Repair 1 in place, preserving Ready=2; roll 2 after readiness returns | Highest old 2 is healthy with maxHealthyScaleDown=0; wait without skipping |
| RU-B03 All old v1 bad; submit v2 | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=0<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | Repair only highest old 2 first | Repair highest old 2 if progress/dependencies permit; readyReplicas<minAvailable alone does not reject all recovery |
| RU-B04 RU-B03 has one unready v2 replacement | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=0<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Wait; no more old cleanup or repeated same-target v2 replacement | Same |
| RU-B05 Two old v2 bad; submit v3 | desiredReplicas=5<br>maxUnavailable=2<br>maxSurge=0<br>partition=0 | activeReplicas=5<br>readyReplicas=3<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=2<br>maxHealthyScaleDown=0 | Repair at most two old bad instances, descending within class; retain healthy old ones | Only a legal descending prefix; do not bypass a healthy blocker |
| RU-B06 025: only protected 0 is bad | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=1 | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | No eligible old bad candidate and no healthy deletion; Ready stays 2 | Same; protected failure remains in the readyReplicas deficit |
| RU-B07 Old healthy; latest-target surge unready | desiredReplicas=3<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Wait for surge Ready; activeReplicas/unavailableTargetReplicas rise together without healthy credit | Same |
| RU-B08 049: healthy v1 plus old bad v2 surge; target v3 | desiredReplicas=1<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | Reclaim only legal old bad surge; retain healthy v1 | Same independent surge cleanup; no stable ordinal is skipped |
| RU-B09 RU-B08 replacement v3 surge is Ready | desiredReplicas=1<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=2<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | May start replacing stable old v1 | Start only when stable order, remainingStart and dependencies allow |
| RU-B10 Healthy old surge supports floor; new stable v3 unready | desiredReplicas=3<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Retain healthy old surge despite superseded version | Same, also retain necessary old dependencies |
| RU-B11 Issued old deletion still occupies physical slot, not yet unavailableTargetReplicas | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=1 | maxScaleDown=0<br>maxHealthyScaleDown=0 | inFlightReservations reserves the action; do not reclaim maxUnavailable | Same; already-started work also consumes coordination allowance |
| RU-B12 N=4, maxSkew=25%, slowest Ready=0, already started 1 | desiredReplicas=4<br>maxUnavailable=2<br>maxSurge=0<br>partition=0 | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | Without coordination, local budget allows one more healthy old replacement | allowedStarted=1, remainingStart=0; wait for stable replacement |
| RU-B13 Target dependency not ready, or last required old dependency retained | desiredReplicas=2<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=2<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | Without coordination, use default selection | Numeric credit cannot override the corresponding dependency constraint; see [B.6](#terminal-maxskew-example) for a terminal cycle with skew gating |
| RU-B14 All old bad, maxUnavailable=0, surge Pending for lack of resources | desiredReplicas=3<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=4<br>readyReplicas=0<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Still blocked; default skipping does not guarantee recovery from every fault | Same; no budget/dependency bypass |
| RU-B15 Old 2 bad and latest-target surge unready | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=1<br>partition=0 | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | Repair one old bad 2; its unready replacement raises unavailableTargetReplicas=2/maxScaleDown=0 | Same if highest candidate and coordination constraints allow |
| RU-B16 Zero desired and zero actual update work | desiredReplicas=0<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=0<br>readyReplicas=0<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | No template replacement or permanent UpdateInProgress | N=0 is neither a progress denominator nor a permanent blocker; a nonzero partition stop still participates |

**Coordination boundary clarification (2.7; RU-B11/12/13/16):** Use full formal target-version capacity V/N, including partition-protected instances; partition completion does not remove a changed nonzero Role from the baseline. Validate a common nominal partition fraction at configuration time without propagating individual partitions. RU-B12 has partition=0, so its numbers are unchanged. RU-B13 still protects necessary old dependencies. API section 5.1 permits a single final caller step only for an all-Ready full-rollout tail with at most one old formal instance per changing Role and no in-flight or extra old capacity; this explicit terminal quantization can exceed maxSkew temporarily and never applies to a canary stop. Shared budgets and selection rules remain unchanged. Task 059 records concrete Go/Kind verification separately; these lookup notes add no runner executable coverage.

Explicit scale-down follows latest desiredReplicas before template replacement; these formulas do not grant arbitrary scale deletion. Recompute percentages against latest desiredReplicas: floor maxUnavailable, ceil maxSurge/partition; classify old versions against the latest target. When pre-existing faults cause readyReplicas<minAvailable, the rule prevents further rollout-induced Ready loss rather than promising immediate restoration to minAvailable.

<a id="b2-all-old-versions-unavailable-repair-one-at-a-time-with-u1"></a>

### B.2 All old versions unavailable: repair one at a time with maxUnavailable=1

desiredReplicas=3/maxUnavailable=1/maxSurge=0/partition=0, all v1 bad, target v2 healthy. The same trajectory applies to SG and independent Role; coordinated Role additionally needs legal descending starts, dependencies and proportional allowance.

| Checkpoint | Instance 0 | Instance 1 | Instance 2 | Counts before the action | Deletion budget | Next action |
| --- | --- | --- | --- | --- | --- | --- |
| Submit v2 | v1 NotReady | v1 NotReady | v1 NotReady | readyReplicas=0<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | Repair 2 |
| Deletion of 2 committed, object still present | v1 NotReady | v1 NotReady | v1 Deleting | readyReplicas=0<br>unavailableTargetReplicas=0<br>inFlightReservations=1 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Wait for deletion/replacement; no reused credit |
| Replacement 2 exists | v1 NotReady | v1 NotReady | v2 NotReady | readyReplicas=0<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Wait for v2 Ready |
| 2 Ready | v1 NotReady | v1 NotReady | v2 Ready | readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | Repair 1 |
| Replacement 1 exists | v1 NotReady | v2 NotReady | v2 Ready | readyReplicas=1<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Wait |
| 1 Ready | v1 NotReady | v2 Ready | v2 Ready | readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | Repair 0 |
| Replacement 0 exists | v2 NotReady | v2 Ready | v2 Ready | readyReplicas=2<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Wait |
| All Ready | v2 Ready | v2 Ready | v2 Ready | readyReplicas=3<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | No old candidates; stop deleting |

Rows have activeReplicas=3. Between disappearance and replacement, activeReplicas=2 and the corresponding inFlightReservations is no longer deducted; maxScaleDown remains 0. Positive maxScaleDown still requires an eligible old candidate; do not delete target instances merely to spend allowance.

### B.3 Superseded surge during rollover: 049 comparison

Each Role has desiredReplicas=1/maxUnavailable=0/maxSurge=1, stable 0:v1 Ready and temporary 1:v2 NotReady; target changes to v3. The two-Role coordinated example must preserve at least 2 Ready Pods throughout (workerReplicas=0); the analogous desiredReplicas=3 per-Role example must preserve at least 6.

| Stage | Stable 0 | Temporary surge 1 | Counts before the action | Deletion budget | Expected action |
| --- | --- | --- | --- | --- | --- |
| Submit v3 | v1 Ready | v2 NotReady | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | Reclaim only old bad surge, retain healthy 0 |
| Old surge deletion issued | v1 Ready | v2 Deleting | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=1 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Reserve inFlightReservations; do not also delete 0 |
| Create v3 surge | v1 Ready | v3 NotReady | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Wait for new Ready capacity |
| Surge Ready | v1 Ready | v3 Ready | activeReplicas=2<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | Start stable replacement only when coordination/dependencies allow |
| Stable replacement exists | v3 NotReady | v3 Ready | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | Retain still-serving surge |
| Stable Ready | v3 Ready | v3 Ready | activeReplicas=2<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | Reclaim healthy temporary capacity no longer needed |
| Final | v3 Ready | — | activeReplicas=1<br>readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | One complete Ready instance per Role |

After old surge disappears, the brief checkpoint is activeReplicas=1/inFlightReservations=0/maxScaleDown=0; subsequent creation still obeys desiredReplicas+maxSurge and dependencies. Ordinal>=desiredReplicas alone does not establish surge identity: retained high instances and absorbed former surge follow their real identity. If healthy old surge supports readyReplicas=minAvailable, preserve it as in RU-B10 instead of mechanically applying bad-surge cleanup.

### B.4 Coverage and remaining execution distinctions

- The **35** existing SG design IDs remain SG-S01–S07, SG-C01–C16, SG-P01–P11 and SG-R01. This appendix adds **16 lookup scenarios**, not executable cases or Kind passes.
- `servinggroup-compound-v2/RUN-622` (SG-S05) and `RUN-632` (the old lower NotReady branch of SG-C08) now implement 2.2 in cases, generator and order verdicts. RUN-624 releases only the new version, leaving the old version faulty. Actual results and failures are recorded in [issue 051](../../../issues/features/051-production-baseline-realignment-DONE/runner-production-20261007/README.md); historical results do not establish conformance of the new candidate.
- Role needs equivalent fault/in-flight cases plus coordinated comparisons; the 35 SG cases are not Role coverage. Existing 025/049 reproductions are defect evidence, not passing results for the new contract.
- Version 2.3 applies the 2026-10-07 approval for per-Ready progress, replacing the A.4 batch barrier and aligning with ordinary `CORE_EXPECTATIONS.md` item 6. Existing release-all/convergence cases do not prove partial-readiness timing; add explicit executable coverage and Kind verification using B.5. This revision updates documentation only.
- Admission, recovery policy, worker completeness, live Ready updates, reservation deduplication and restart identity recovery still require their own verification. Skipping does not relax them.

### B.5 Continue after partial batch readiness

desiredReplicas=5, maxUnavailable=2, maxSurge=0, partition=0; old 2/1/0 are healthy, with no other fault or coordination blocker. activeReplicas stays 5 and minAvailable=3 below; a deleting old object still physically present remains active.

| Checkpoint | readyReplicas | unavailableTargetReplicas | inFlightReservations | maxScaleDown | maxHealthyScaleDown | Action |
| --- | --- | --- | --- | --- | --- | --- |
| New 4/3 both NotReady | 3 | 2 | 0 | 0 | 0 | Wait |
| Release only 4 to complete Ready; 3 stays NotReady | 4 | 1 | 0 | 1 | 1 | Start old 2 without waiting for 3 |
| Deletion of 2 committed; old object still exists | 3 | 1 | 1 | 0 | 0 | Do not also delete old 1 |
| Replacement 2 is target NotReady; 3 still NotReady | 3 | 2 | 0 | 0 | 0 | Wait for further actual Ready capacity |

The timing regression must release only the complete members of 4, keep 3 NotReady, and observe 2 start within the deadline. Waiting for all units Ready cannot distinguish the old rule. SG counts complete groups; Role counts complete instances of the same Role within one SG. Coordinated Roles retain ordered candidates, dependencies and maxSkew. Readiness loss, reservations, protected faults or other constraints may exhaust allowance again; that is not a batch barrier. This is an approved design expectation, not a new executable case or Kind pass.

Version 2.4 local-recovery version selection is defined in API section 6.1. Existing recovery first-version and duplicate-creation assertions require dedicated migration. The 35 SG scenarios, B.5 timing example and prior Kind results do not establish execution coverage of this intersection.

<a id="terminal-maxskew-example"></a>

### B.6 Why the final step can exceed maxSkew: 8/4/10 replicas

This example expands the approved 2.7 full-rollout terminal rule in API section 5.1, showing how RU-B12 progress allowance and RU-B13 old dependency retention can block each other. Configure prefill→decode→fff (arrows mean dependsOn), with 8/4/10 formal replicas. All three Roles change to v2, with partition=0, maxUnavailable=1, maxSurge=0 and maxSkew=10%. At the first checkpoint each retains one v1 instance; every formal instance is completely Ready with known history, with no missing instances, no work in flight and no extra old capacity.

Progress is `p=target-version Ready/formal replicas`. The slowest Role is decode at `3/4=75%`; the ordinary cumulative start limit is `min(N,ceil((75%+10%)*N))`. Already-started counts equal target Ready counts at this checkpoint.

| Role | Target Ready / formal replicas | Actual target proportion | Ordinary cumulative start limit | Remaining starts | Why another replacement is blocked |
| --- | --- | --- | --- | --- | --- |
| prefill | 7/8 | 87.5% | ceil(0.85×8)=ceil(6.8)=7 | 7−7=0 | Ordinary skew gating blocks the final v1 instance |
| decode | 3/4 | 75% | ceil(0.85×4)=ceil(3.4)=4 | 4−3=1 | Old prefill still needs the last old decode instance |
| fff | 9/10 | 90% | ceil(0.85×10)=ceil(8.5)=9 | 9−9=0 | Ordinary skew gating blocks progress, and old decode still needs old fff |

Even one healthy deletion credit per Role cannot break this cycle. Under the approved terminal conditions, prefill has no old caller requiring its old capacity and depends on retained old decode capacity. It may start its own final instance: cumulative starts rise from 7 to 8, exceeding the ordinary limit of 7 by **exactly one**. Old dependency protection for decode/fff remains in force.

| One legal sequence of Ready checkpoints | prefill target Ready | decode target Ready | fff target Ready | Largest proportion gap Δ=max(p)−min(p) | Permission and next action |
| --- | --- | --- | --- | --- | --- |
| One old instance remains in each Role | 7/8=87.5% | 3/4=75% | 9/10=90% | 15 percentage points | prefill needs the terminal permit; other Roles cannot first remove required old dependencies |
| prefill's last replacement is Ready; decode has not finished | 8/8=100% | 3/4=75% | 9/10=90% | **25 percentage points** | One prefill instance adds 12.5 percentage points; old prefill is gone, so decode can finish under its ordinary limit of 4 |
| decode's last replacement is Ready | 8/8=100% | 4/4=100% | 9/10=90% | 10 percentage points | fff's ordinary limit is ceil((90%+10%)×10)=10; old decode is gone, so its final step is permitted |
| All complete | 8/8=100% | 4/4=100% | 10/10=100% | 0 percentage points | No old instances; rollout ends |

Two distinct discretization effects apply. **Ordinary ceiling rounding** already allows fff to reach 90%, a 15-percentage-point lead over the slowest 75%. The **additional terminal permit** lets the otherwise blocked prefill reach 100%, temporarily leading 75% by 25 percentage points. The ordinary formula did not admit prefill's eighth start, and this is not floating-point error. The 25-point gap is possible in this example, not a new global threshold; configured maxSkew remains 10%.

The table illustrates one Ready-checkpoint order, not a requirement to wait for the caller's replacement to become Ready before progressing its dependencies. Once the old caller has fully disappeared, its dependency can reevaluate ordinary allowance, budgets and other constraints; execution may interleave. An unready replacement consumes a start without increasing p. The terminal permit requires every condition in API section 5.1 and never applies to a partition>0 canary stop. Exhausted budgets, missing capacity, unavailable targets, unknown history, terminating/in-flight work or extra old capacity cannot use this table as permission. Minimum Ready counts remain 7/3/9.

This section illustrates existing rules without adding lookup IDs, runner executable cases or Kind pass counts. Implementation and existing cluster evidence remain in the [task 059 record](../../../issues/bugs/059-role-coordination-skew-boundaries-DONE/PROPOSAL_COMMIT.md).
