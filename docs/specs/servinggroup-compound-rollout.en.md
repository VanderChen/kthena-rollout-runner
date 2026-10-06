# ServingGroupRollingUpdate compound rollout expectations

Version 2.1 · 2026-10-06 · [简体中文](servinggroup-compound-rollout.zh-CN.md)

This is a product design contract, not a claim that production implements it or that Kind verification has passed. Its scope is ServingGroupRollingUpdate; nested Role replica changes are included, but RoleRollingUpdate is not.

The 35 design IDs and the state tables below match the Chinese edition. Tables show one permitted trajectory, not a required total ordering of asynchronous events. v1/v2/v3 are successive requested templates. Ready means the complete group is Ready. Deleting groups occupy physical capacity until they disappear. N/U/S/P denote desired groups, maxUnavailable, maxSurge and partition. Counts list active and Ready groups. A dash means no group exists at that ordinal.

New replacements select outdated eligible groups from highest to lowest ordinal. Scale-out fills the lowest missing ordinal. Scaling capacity is arranged before destructive rollout, but newly added groups need not all become Ready: each healthy deletion must independently preserve R >= max(0,N-U). Scale-out deficits do not grant deletion credit. U floors and S ceils percentages of the latest N.

At a stable checkpoint, outdated unhealthy cleanup is limited by Q=max(0,C-(N-U)-V), where C includes actually created surge and V includes all non-Ready groups of the current target, including surge. Only when C=N may this be shortened to max(0,U-V). A new non-Ready surge increments both C and V; configured but uncreated surge grants no credit. Do not claim the same allowance again before replacements become Ready. A non-Ready group already on the current target is not repeatedly rebuilt.

Only explicit scale-down may leave new stable ordinal holes. Preserve healthy retained groups that need no template update; holes alone do not trigger rolling replacement. Partition uses absolute ordinals and determines the template of a replacement's new slot. A previously created surge becomes retained only if its ordinal enters the new formal range; matching a total count is insufficient. Appendix A states the complete rules.

<a id="scenario-tables"></a>

## 1. Sparse scale-down, opportunistic hole repair and order

### SG-S01: Scale-down holes and incremental repair

At N=4, deletion costs select 1 and 2, preserving healthy 3. N=2 retains {0,3}; N=3 has only one new capacity slot and fills 1, leaving hole 2. Stable reconciliation must not delete 3 to fill 2. A later N=4 fills 2 while preserving the UID of 3. Scale-down removals are not charged again as rollout unavailability.

| Step | N | sg-0 | sg-1 | sg-2 | sg-3 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | active 4; Ready 4 |
| 2 | 2 | v1 Ready | v1 Deleting | v1 Deleting | v1 Ready | active 4→2; Ready 2–4 |
| 3 | 2 | v1 Ready | — | — | v1 Ready | active 2; Ready 2 |
| 4 | 3 | v1 Ready | v1 NotReady | — | v1 Ready | active 3; Ready 2 |
| 5 | 3 | v1 Ready | v1 Ready | — | v1 Ready | active 3; Ready 3 |
| 6 | 4 | v1 Ready | v1 Ready | v1 NotReady→v1 Ready | v1 Ready | active 4; Ready 3→4 |

### SG-S02: An outdated high group can repay a low hole

From N=2, {0:v1,3:v1}, P=0/U=1/S=0, submit v2. Group 3 is already due for replacement: delete it and put its replacement in lowest hole 1. Once Ready, replace 0 in place. The existing hole disappears as a side effect of necessary replacement, without moving a healthy target group.

| Step | sg-0 | sg-1 | sg-3 | Groups (active; Ready) |
| --- | --- | --- | --- | --- |
| 1 | v1 Ready | — | v1 Ready | active 2; Ready 2 |
| 2 | v1 Ready | — | v1 Deleting | active 2→1; Ready 1 |
| 3 | v1 Ready | v2 NotReady→v2 Ready | — | active 2; Ready 1→2 |
| 4 | v1 Deleting | v2 Ready | — | active 2→1; Ready 1 |
| 5 | v2 Ready | v2 Ready | — | active 2; Ready 2 |

### SG-S03: Preserve a healthy target high group

From N=2, {0:v1,3:v2}, P=0/U=1/S=0, only 0 needs replacement. Final {0:v2,3:v2} keeps hole 1 and the UID of 3. S=1 does not permit creating 1:v2 and deleting retained healthy 3:v2 merely to compact ordinals. Retained high identities and temporary surge must be distinguished.

| Step | sg-0 | sg-1 | sg-3 | Groups (active; Ready) |
| --- | --- | --- | --- | --- |
| 1 | v1 Ready | — | v2 Ready | active 2; Ready 2 |
| 2 | v1 Deleting | — | v2 Ready | active 2→1; Ready 1 |
| 3 | v2 Ready | — | v2 Ready | active 2; Ready 2 |

### SG-S04: A surge outside the expanded range stays temporary

Retained {0:v1,1:v1,3:v2} at N=3/U=0/S=1 has temporary 4:v2. Increasing N to 4 makes retained 3 in-range but does not absorb surge 4. Although the old total count equals the new N, create retained 2:v2 under ceiling 5. Use surge 4 to roll 1 then 0, and remove 4 at the end. Preserve retained 3 throughout.

| Step | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3 | v1 Ready | v1 Ready | — | v2 Ready | v2 Ready (temporary surge) | active 4; Ready 4 |
| 2 | 4 | v1 Ready | v1 Ready | v2 NotReady | v2 Ready | v2 Ready (temporary surge) | active 5; Ready 4 |
| 3 | 4 | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5; Ready 5 |
| 4 | 4 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 4–5; Ready 4→5 |
| 5 | 4 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | — | active 4; Ready 4 |

### SG-S05: A low unhealthy group can block strict descending order

This state comes from scaling, not descending rollout: scale v1 from 3 to 2 by deleting low-cost 1; expand to 3 with bad v2 in hole 1, retaining 0/2:v1. Submit v3 with U=1/S=0. Ready=2 and Q=1, but the highest old group 2 is healthy and deleting it would leave Ready=1. Do not skip 2 to repair unhealthy 1. Wait for self-recovery, legal Ready surge, changed budget or external intervention.

| Step | N / target | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | active 3; Ready 3 |
| 2 | 2/v1 | v1 Ready | v1 Deleting | v1 Ready | active 3→2; Ready 2–3 |
| 3 | 2/v1 | v1 Ready | — | v1 Ready | active 2; Ready 2 |
| 4 | 3/v2 | v1 Ready | v2 NotReady | v1 Ready | active 3; Ready 2 |
| 5 | 3/v3 | v1 Ready | v2 NotReady | v1 Ready | active 3; Ready 2 |
| 6 | 3/v3 | v1 Ready | v2 NotReady | v1 Ready | active 3; Ready 2 |

### SG-S06: Repair two bad v2 groups with v3

N=5/U=2/S=0: replace highest 4 and 3 as a batch, but v2 stays non-Ready. At target v2, C=5/V=2/Q=0. Submitting v3 makes bad v2 old, V=0/Q=2. Clean 4 then 3 without further Ready loss and create v3. Wait for both to become Ready before batching 2 then 1, then 0. With no new target, do not rebuild same-target bad v2 repeatedly.

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

All five v1 groups are non-Ready before rollout, N=5/U=2/S=0. Submitting v2 gives Q=2 despite Ready=0. Replace 4/3, wait for v2 Ready, then 2/1, then 0. Never replace all five at once. If the first two v2 groups remain non-Ready, V=2/Q=0 and further cleanup stops until the target changes or another explicit recovery mechanism applies.

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

N changes 3 to 5 with U=1/S=0 while deletion of old 2 is already accepted. Fill new slots 3/4 with v2; keep completing the issued replacement. Ready=2 is below the new floor 4 because of scale-out and in-flight work, not permission to delete another v1. After real Ready credit, roll 1 then 0.

| Step | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 2 | 3 | v1 Ready | v1 Ready | v1 Deleting→v2 NotReady | — | — | active 2–3; Ready 2 |
| 3 | 5 | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | v2 NotReady | active 5; Ready 2 |
| 4 | 5 | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |
| 5 | 5 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 6 | 5 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 7 | 5 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |

### SG-C02: Scale down during rollout

N changes 3 to 2 with U=1 while 2:v2 is still non-Ready. Remove that unwanted non-Ready group first, wait for scale-down completion, then roll retained 1 and 0. Never recreate removed slot 2.

| Step | N | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 3 | v1 Ready | v1 Ready | v2 NotReady | active 3; Ready 2 |
| 2 | 2 | v1 Ready | v1 Ready | v2 Deleting | active 3→2; Ready 2 |
| 3 | 2 | v1 Ready | v1 Ready | — | active 2; Ready 2 |
| 4 | 2 | v1 Ready | v1 Deleting→v2 Ready | — | active 1–2; Ready 1→2 |
| 5 | 2 | v1 Deleting→v2 Ready | v2 Ready | — | active 1–2; Ready 1→2 |
| 6 | 2 | v2 Ready | v2 Ready | — | active 2; Ready 2 |

### SG-C03: Absorb old surge during scale-out

N changes 3 to 5, U=0/S=1/P=0. Old Ready surge 3:v2 becomes retained with its UID intact. New 4:v2 is formal capacity; 5:v2 is the new temporary surge. Once real availability permits, roll 2,1,0 and remove 5. UID-preserving absorption is a ModelServing identity rule; Deployment aggregate surge does not itself promise Pod UID reuse.

| Step | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3 | v1 Ready | v1 Ready | v1 Ready | v2 Ready (temporary surge) | — | — | active 4; Ready 4 |
| 2 | 5 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | v2 NotReady (temporary surge) | active 6; Ready 4 |
| 3 | 5 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 6; Ready 6 |
| 4 | 5 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 5 | 5 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 6 | 5 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 5–6; Ready 5→6 |
| 7 | 5 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | — | active 5; Ready 5 |

### SG-C12: Keep useful surge while shrinking the retained set

N changes 4 to 3, U=0/S=1, with retained 0..3:v1 and Ready surge 4:v2. Remove retained 3 for scale-down first. The name of surge 4 being outside [0,N+S) is not a reason to discard its service credit: after 3 disappears, four active groups satisfy the count ceiling. Use the same surge to roll 2,1,0 and remove it at the end, without rebuilding it or first rolling the departing 3.

| Step | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready (temporary surge) | active 5; Ready 5 |
| 2 | 3 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | v2 Ready (temporary surge) | active 5→4; Ready 4–5 |
| 3 | 3 | v1 Ready | v1 Ready | v1 Ready | — | v2 Ready (temporary surge) | active 4; Ready 4 |
| 4 | 3 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | — | v2 Ready (temporary surge) | active 3–4; Ready 3→4 |
| 5 | 3 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | — | v2 Ready (temporary surge) | active 3–4; Ready 3→4 |
| 6 | 3 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | — | v2 Ready (temporary surge) | active 3–4; Ready 3→4 |
| 7 | 3 | v2 Ready | v2 Ready | v2 Ready | — | — | active 3; Ready 3 |

### SG-C15: Recompute percentage U during scale-out

U=25% gives U=1/floor=4 at N=5 and U=2/floor=7 at N=9, with S=0. New slots 5..8 use v2. Ready=7 still gives no healthy deletion credit; Ready=8 permits deleting one old group. A non-Ready replacement 3 and non-Ready expansion 8 jointly consume U=2. The last table row is a separate bad-v2 run where initial 4:v2 was also never Ready: Ready=4 cannot justify another healthy deletion.

| Step | N / U / Ready floor | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | sg-6 | sg-7 | sg-8 | Groups (active; Ready) |
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

### SG-C16: Count actual surge in both C and V

N=3/U=1/S=1/P=0, old 2 is already unhealthy. Creating non-Ready surge 3:v2 changes C/V from 3/0 to 4/1, leaving Q=1. Clean old 2 without Ready loss; rebuilding it makes V=2/Q=0. Wait for new Ready credit before descending through 1 and 0. Never churn same-target bad surge; remove it when formal groups are complete. Using U-V alone would falsely block initial cleanup; ignoring V would overgrant credit.

| Step | sg-0 | sg-1 | sg-2 | sg-3 (temporary surge) | C / R / V / Q |
| --- | --- | --- | --- | --- | --- |
| 1 | v1 Ready | v1 Ready | v1 NotReady | — | 3 / 2 / 0 / 1 |
| 2 | v1 Ready | v1 Ready | v1 NotReady | v2 NotReady | 4 / 2 / 1 / 1 |
| 3 | v1 Ready | v1 Ready | — | v2 NotReady | 3 / 2 / 1 / 0 |
| 4 | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | 4 / 2 / 2 / 0 |
| 5 | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | 4 / 3 / 1 / 1 |
| 6 | v1 Ready | — | v2 Ready | v2 NotReady | 3 / 2 / 1 / 0 |
| 7 | v1 Ready | v2 NotReady | v2 Ready | v2 NotReady | 4 / 2 / 2 / 0 |
| 8 | v1 Ready | v2 Ready | v2 Ready | v2 NotReady | 4 / 3 / 1 / 1 |
| 9 | — | v2 Ready | v2 Ready | v2 NotReady | 3 / 2 / 1 / 0 |
| 10 | v2 NotReady | v2 Ready | v2 Ready | v2 NotReady | 4 / 2 / 2 / 0 |
| 11 | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | 4 / 3 / 1 / 1 |
| 12 | v2 Ready | v2 Ready | v2 Ready | — | 3 / 3 / 0 / 1 |

## 3. Rollout during scaling and atomic requests

### SG-C04: A new template arrives during scale-out

N changes 3 to 5; creation of 3:v1 was already accepted while 4 is absent. Submit v2: leave in-flight 3 as v1 and create 4:v2. With 4 non-Ready, V=1/Q=0. Once 4 is Ready, Q=1 permits cleaning highest old 3 even if it never becomes Ready. Wait for replacement 3:v2 Ready, then roll healthy 2,1,0.

| Step | N / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
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

| Step | N / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
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

Submit N=5/v2 from N=3/v1 in one request. Arrange formal slots 3/4 with v2 first. Their non-Ready deficit cannot be borrowed as rollout credit. Once Ready capacity permits, replace highest retained old 2,1,0.

| Step | N / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 2 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | active 5; Ready 3 |
| 3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |
| 4 | 5/v2 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 5 | 5/v2 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 6 | 5/v2 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 4–5; Ready 4→5 |
| 7 | 5/v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |

### SG-C07: Atomic scale-down and template change

Submit N=3/v2 from N=5/v1. Select scale victims first; their deletions may run together. Wait for them to disappear, then roll retained 2,1,0 under the new budget. If health or deletion costs instead preserve a sparse high identity, apply S01–S03: only necessary old replacements may repay holes.

| Step | N / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | active 5; Ready 5 |
| 2 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Deleting | v1 Deleting | active 5→3; Ready 3–5 |
| 3 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 4 | 3/v2 | v1 Ready | v1 Ready | v1 Deleting→v2 Ready | — | — | active 2–3; Ready 2→3 |
| 5 | 3/v2 | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | — | — | active 2–3; Ready 2→3 |
| 6 | 3/v2 | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | — | — | active 2–3; Ready 2→3 |
| 7 | 3/v2 | v2 Ready | v2 Ready | v2 Ready | — | — | active 3; Ready 3 |

### SG-C13: Allocate surge against the expanded N

At desired N=5, scale-out has already created Ready 3:v1 but not 4. v2 arrives with U=0/S=1. Fill formal 4 with v2, then use 5 as temporary surge. After Ready credit, roll 3,2,1,0 and remove 5. A formal expansion slot and a surge slot cannot both claim the same capacity.

| Step | N / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | Groups (active; Ready) |
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

### SG-C08: v3 supersedes an incomplete v2 rollout at fixed N

N=3/U=1/S=0/P=0. Group 2:v2 is Ready and deletion of 1:v1 is accepted, but its replacement create is not. Submit v3: finish that issued action by creating 1:v3, wait for Ready, then newly select highest old 2:v2 and finally 0:v1. If creation of 1:v2 was already accepted, do not edit it into v3. A non-Ready 1:v2 can block behind healthy 2:v2 under strict descending order despite Q=1; if it becomes Ready, roll 2 before 1.

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

| Step | N / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
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

| Step | N / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
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

From N=3, expand to 5: 3:v1 is Ready while 4 has not been created. Bad v2 arrives and 4:v2 never becomes Ready. With N=5/U=1/S=0, leave healthy old groups intact. Returning the target to v1 makes unhealthy highest 4 outdated, so replace it with v1 without further Ready loss. All other v1 UIDs remain. This intentional automatic recovery differs from StatefulSet OrderedReady rollback cases that can require manual Pod deletion.

| Step | N / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | — | — | active 3; Ready 3 |
| 2 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | — | active 4; Ready 4 |
| 3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | active 5; Ready 4 |
| 4 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Deleting | active 5→4; Ready 4 |
| 5 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | active 5; Ready 4 |
| 6 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | active 5; Ready 5 |

### SG-C14: Old-version surge still provides indispensable service

N=3/U=0/S=1/P=0. Ready surge 3:v2 enabled accepted deletion of 2:v1. v3 arrives: recreate 2 with v3 but retain healthy 3:v2 until 2 becomes Ready, otherwise Ready would fall below 3. Only then replace surge 3 with v3, serially reusing its slot after deletion. Use it to roll 1 then 0 and finally remove it. If 2:v3 also stays bad, preserve 3:v2 and stop further healthy deletion.

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

### SG-P01: Expand after a P=1 canary stop

Start N=3/P=1 with 0:v1 and 1/2:v2 Ready. Expand to 5: new 3/4 use v2, and old 0..2 retain their UIDs.

| Step | N / P | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/1 | v1 Ready (protected) | v2 Ready | v2 Ready | — | — | active 3; Ready 3 |
| 2 | 5/1 | v1 Ready (protected) | v2 Ready | v2 Ready | v2 NotReady | v2 NotReady | active 5; Ready 3 |
| 3 | 5/1 | v1 Ready (protected) | v2 Ready | v2 Ready | v2 Ready | v2 Ready | active 5; Ready 5 |

### SG-P02: Atomically raise integer P while expanding

Start N=3/P=3, fully protected. Submit N=6/P=5 together: new 3/4 use historical v1 and 5 uses v2. Final five v1 and one v2. The old initial N=3/P=5 example is now rejected by admission; the valid replacement changes P with N rather than bypassing the active integer bound.

| Step | N / P / target | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/3/v2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | — | — | — | active 3; Ready 3 |
| 2 | 6/5/v2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v1 NotReady (protected) | v1 NotReady (protected) | v2 NotReady | active 6; Ready 3 |
| 3 | 6/5/v2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | active 6; Ready 6 |

### SG-P03: Scale-down can cross the old protected range and leave holes

From N=5/P=3, atomically shrink to N=2/P=2. With equal health/cost, remove 4:v2, 3:v2 and 2:v1; retain 0/1:v1. P is not a replica minimum, but the resulting P must not exceed N. The second table starts {0:v1 Ready,1:v1 non-Ready,2:v2 Ready}, N=3/P=2: shrinking to 2 removes unhealthy protected 1, preserving healthy high 2. Re-expansion fills protected hole 1 with historical v1 and retains UID 2.

| Step | N / P | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | v2 Ready | active 5; Ready 5 |
| 2 | 2/2 | v1 Ready (protected) | v1 Ready (protected) | v1 Deleting | v2 Deleting | v2 Deleting | active 5→2; Ready 2–5 |
| 3 | 2/2 | v1 Ready (protected) | v1 Ready (protected) | — | — | — | active 2; Ready 2 |

| Step | N / P | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 3/2 | v1 Ready (protected) | v1 NotReady (protected) | v2 Ready | active 3; Ready 2 |
| 2 | 2/2 | v1 Ready (protected) | — | v2 Ready | active 2; Ready 2 |
| 3 | 3/2 | v1 Ready (protected) | v1 NotReady (protected)→v1 Ready (protected) | v2 Ready | active 3; Ready 2→3 |

### SG-P04: Scale-out raises a percentage partition

P=50% resolves 2 at N=3 and 3 at N=5. Existing 2:v2 becomes protected but is not rolled back; create 3/4:v2. The result need not contain exactly P old-version groups. If 2 later disappears, restore that protected slot with historical v1.

| Step | N / P | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | — | — | active 3; Ready 3 |
| 2 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready (protected) | v2 NotReady | v2 NotReady | active 5; Ready 3 |
| 3 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready (protected) | v2 Ready | v2 Ready | active 5; Ready 5 |

### SG-P05: Scale-down lowers a percentage partition

P=50% resolves 3 at N=5 and 2 at N=3. Remove high 4/3 first, then update newly eligible 2:v1 to v2. Final 0/1 remain v1.

| Step | N / P | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | v2 Ready | active 5; Ready 5 |
| 2 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready | v2 Deleting | v2 Deleting | active 5→3; Ready 3–5 |
| 3 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v1 Deleting→v2 Ready | — | — | active 2–3; Ready 2→3 |
| 4 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | — | — | active 3; Ready 3 |

### SG-P06: Raise P while an old deletion is in flight

N=3, 2:v2 Ready; deletion of 1:v1 was issued at P=0. Raising P to 3 cannot cancel deletion. Recreate slot 1 from historical v1, leave 0:v1 unchanged and preserve existing 2:v2 even though it is now protected.

| Step | N / P / target | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 3/0/v2 | v1 Ready | v1 Deleting | v2 Ready | active 3→2; Ready 2 |
| 2 | 3/3/v2 | v1 Ready (protected) | v1 Deleting (protected) | v2 Ready (protected) | active 3→2; Ready 2 |
| 3 | 3/3/v2 | v1 Ready (protected) | v1 NotReady (protected)→v1 Ready (protected) | v2 Ready (protected) | active 3; Ready 2→3 |

### SG-P07: Lower P and submit v3 together

From N=3/P=2 with 0/1:v1 and 2:v2, submit P=1/v3. Roll highest old 2 to v3 first, then newly eligible 1 directly from v1 to v3. Keep 0:v1.

| Step | N / P / target | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 3/2/v2 | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | active 3; Ready 3 |
| 2 | 3/1/v3 | v1 Ready (protected) | v1 Ready | v2 Deleting→v3 Ready | active 2–3; Ready 2→3 |
| 3 | 3/1/v3 | v1 Ready (protected) | v1 Deleting→v3 Ready | v3 Ready | active 2–3; Ready 2→3 |
| 4 | 3/1/v3 | v1 Ready (protected) | v3 Ready | v3 Ready | active 3; Ready 3 |

### SG-P08: Percentage partition absorbs surge on expansion

N=3/P=50%=2/U=0/S=1 has old 0..2 and non-Ready surge 3:v2. Expand to N=5/P=3: retain UID 3 as formal, fill 4:v2, and protect old 2. No old eligible group remains, so do not allocate extra surge 5. Final three v1 and two v2.

| Step | N / P | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/2 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready | v2 NotReady (temporary surge) | — | — | active 4; Ready 3 |
| 2 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 NotReady | v2 NotReady | — | active 5; Ready 3 |
| 3 | 5/3 | v1 Ready (protected) | v1 Ready (protected) | v1 Ready (protected) | v2 Ready | v2 Ready | — | active 5; Ready 5 |

### SG-P09: Protected faults consume the same availability budget

N=3/P=1/U=1/S=0, protected 0:v1 is non-Ready and 1/2:v1 are Ready. Ready is already at floor 2: do not delete healthy 2. Once 0 recovers using its historical template, roll 2 then 1, retaining protected 0.

| Step | sg-0 | sg-1 | sg-2 | Groups (active; Ready) |
| --- | --- | --- | --- | --- |
| 1 | v1 NotReady (protected) | v1 Ready | v1 Ready | active 3; Ready 2 |
| 2 | v1 Ready (protected) | v1 Ready | v1 Ready | active 3; Ready 3 |
| 3 | v1 Ready (protected) | v1 Ready | v1 Deleting→v2 Ready | active 2–3; Ready 2→3 |
| 4 | v1 Ready (protected) | v2 Ready | v2 Ready | active 3; Ready 3 |

### SG-P10: Continue partition rollout after absorbing surge

N changes 3 to 4, P=1/U=0/S=1. Ready surge 3:v2 becomes formal with its UID intact. Old 1/2 still need updates, so obtain Ready credit from new temporary 4:v2, roll 2 then 1, and remove 4. Keep 0:v1.

| Step | N / P | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 3/1 | v1 Ready (protected) | v1 Ready | v1 Ready | v2 Ready (temporary surge) | — | active 4; Ready 4 |
| 2 | 4/1 | v1 Ready (protected) | v1 Ready | v1 Ready | v2 Ready | v2 NotReady (temporary surge)→v2 Ready (temporary surge) | active 5; Ready 4→5 |
| 3 | 4/1 | v1 Ready (protected) | v1 Ready | v1 Deleting→v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 4–5; Ready 4→5 |
| 4 | 4/1 | v1 Ready (protected) | v1 Deleting→v2 Ready | v2 Ready | v2 Ready | v2 Ready (temporary surge) | active 4–5; Ready 4→5 |
| 5 | 4/1 | v1 Ready (protected) | v2 Ready | v2 Ready | v2 Ready | — | active 4; Ready 4 |

### SG-P11: The replacement absolute ordinal selects its version

N=2 retains {0:v1,3:v1}; submit v2 at P=2/U=1/S=0. Outdated eligible 3 may be removed and its replacement placed in lowest hole 1, which uses historical v1 because 1<P. The two-v1 result is a canary stop, not full v2 adoption: currentRevision=v1, updateRevision=v2, updatedReplicas=0. Lowering P to 1 updates slot 1 using the same target revision. This preserves the separately approved 2026-10-06 clarification; a healthy target 3:v2 would instead remain under S03. The same absolute-ordinal interpretation applies to Role partition.

| Step | N / P / target | sg-0 | sg-1 | sg-3 | Groups (active; Ready) |
| --- | --- | --- | --- | --- | --- |
| 1 | 2/2/v2 | v1 Ready (protected) | — | v1 Ready | active 2; Ready 2 |
| 2 | 2/2/v2 | v1 Ready (protected) | — | v1 Deleting | active 2→1; Ready 1 |
| 3 | 2/2/v2 | v1 Ready (protected) | v1 NotReady→v1 Ready (protected) | — | active 2; Ready 1→2 |
| 4 | 2/2/v2 | v1 Ready (protected) | v1 Ready (protected) | — | active 2; Ready 2 |
| 5 | 2/1/v2 | v1 Ready (protected) | v1 Deleting→v2 Ready | — | active 1–2; Ready 1–2 |

## 6. Nested Role replica changes

### SG-R01: Increase Role members during a partition canary

N=3/P=1: protected 0 uses historical worker W1; 1/2 use target W2. Raising Role replicas from 1 to 2 creates no SG template revision and does not trigger hole compaction. Under U=1/S=0, apply new membership to 2, wait Ready, then 1, then 0 using W1. Groups not yet selected retain Ready credit against their own applied old membership; the global replica edit cannot make all groups unavailable at once.

| Step | Role members per group | sg-0 | sg-1 | sg-2 | Ready groups |
| --- | --- | --- | --- | --- | --- |
| 1 | 1 | v1/W1×1 Ready (protected) | v2/W2×1 Ready | v2/W2×1 Ready | 3 |
| 2 | 2 | v1/W1×1 Ready (protected) | v2/W2×1 Ready | v2/W2×1 Ready+v2/W2×1 NotReady | 2→3 |
| 3 | 2 | v1/W1×1 Ready (protected) | v2/W2×1 Ready+v2/W2×1 NotReady | v2/W2×2 Ready | 2→3 |
| 4 | 2 | v1/W1×1 Ready (protected)+v1/W1×1 NotReady | v2/W2×2 Ready | v2/W2×2 Ready | 2→3 |
| 5 | 2 | v1/W1×2 Ready (protected) | v2/W2×2 Ready | v2/W2×2 Ready | 3 |

## 7. Acceptance and counterexamples

Record the current N/P/U/S and each group's ordinal, version, UID, Ready and deletion state. Distinguish retained identities from temporary surge, and keep accepted actions and historical templates. An interleaving test must prove that its preceding operation was still in flight when the next request arrived.

Final count alone is insufficient. Check that non-scaling work creates no new stable holes, healthy target retained identities are not deleted for compaction, creates obey the surge ceiling, and healthy rollout deletion preserves the availability floor. When a pre-existing fault already violates the floor, outdated unhealthy cleanup may proceed only without reducing Ready, within Q, and without skipping a higher old candidate. Transient excess after scale-down can only decrease.

Three counterexamples are mandatory: S01's {0,3} to {0,1,3} leaves hole 2; S05's low unhealthy group remains blocked behind a healthy higher old group; C14 retains outdated surge while it is still needed for service. S06/S07 permit bounded recovery of outdated unhealthy highest groups even below the Ready floor. C16 requires accounting for both C and V when actual non-Ready surge exists. No state-table row is itself a Kind result.

<a id="behavior-rules"></a>

## Appendix A. SG behavioral rules

Original design dated 2026-09-20, with separately recorded 2026-10-06 contract amendments. These product requirements do not derive their authority from the currently observed controller implementation. The 35 scenarios instantiate these rules.

### A.1. Decisions and community references

Replacing a whole inference group can reload models, occupy expensive GPUs and incur lengthy warm-up. Priority is: preserve existing availability and capacity ceilings, then avoid rebuilding healthy groups that need no update, then converge scale and opportunistically repay existing identity holes. Every newly selected template replacement still follows descending old-group ordinals; a lower priority never excuses skipping that order.

| Topic | Community reference | ModelServing decision |
| --- | --- | --- |
| Identity and partition | StatefulSet stable ordinals, historical template below P, descending replacement | Absolute threshold, but health/cost-aware scale-down may retain sparse identities |
| Budgets | Deployment floors U and ceils S; LWS budgets complete groups | Recompute against latest N; only complete groups count Ready; scale-out deficits grant no deletion credit |
| Latest intent | Deployment rollover; StatefulSet bad-template recovery may require intervention | A to B to C may skip full B completion; an outdated unhealthy highest candidate can be replaced by C |
| Inference cost | LWS group rollout; RBG Role/coordination distinction | Preserve healthy target retained identities and keep SG and Role replica axes separate |

The Deployment calculation `maxScaledDown = allPodsCount - minAvailable - newReplicaSetPodsUnavailable` illustrates the accounting of actual capacity and unavailable new replicas. Cleanup does not borrow configured but uncreated surge. With N=10/U=2/S=3 and 13 actual replicas, five unavailable new replicas give 13-8-5=0 cleanup allowance; returning to an old good target with one unavailable target replica gives 4, while a brand-new empty target gives 5. Bad replicas that become old can then be cleaned without further availability loss. This does not grant extra maxUnavailable.

For SG at a stable checkpoint with no in-flight creates/deletes:

```text
Q = max(0, C - (N-U) - V) = max(0, U + (C-N) - V)
```

C counts actual groups including created surge; V counts non-Ready groups of the latest target including surge. C-N is actual excess, not configured S. Only at C=N is Q=max(0,U-V). Q is not an independent budget: candidate order, partition, physical capacity and outstanding reservations still constrain it. Deleting groups cannot repeatedly grant cleanup credit. For healthy deletion, separately require R-1 >= max(0,N-U). Creating a non-Ready surge increments C and V together and leaves Q unchanged.

References: [Deployment rolling.go](https://github.com/kubernetes/kubernetes/blob/master/pkg/controller/deployment/rolling.go#L803-L918), [StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/), [Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/), [LWS rollout](https://lws.sigs.k8s.io/docs/concepts/leaderworkerset/rollout-strategy/), [LWS API](https://lws.sigs.k8s.io/docs/reference/leaderworkerset.v1/), [RBG API](https://github.com/sgl-project/rbg/blob/main/doc/reference/api.md).

These are references, not a claim that one upstream controller specifies the combined behavior. Deployment's choice among old ReplicaSets does not impose SG ordinal order; this design borrows accounting, not arbitrary unhealthy-first selection. LWS's update-start percentage base is not copied: use latest N. Nor is its partition burst retention copied: at a canary stop, remove unneeded temporary surge once eligible retained groups and availability satisfy the contract, without waiting for P to reach zero.

### A.2. State, budgets and identity

N is latest desired replicas; D=[0,N) is the preferred ordinal range, not mandatory compaction on every reconciliation. L includes groups that have not completely disappeared, including Deleting physical capacity. R counts complete Ready groups still serving. Distinguish retained K from temporary surge T: both count in L and in real activity/availability. A high retained ordinal does not become T merely because ordinal>=N. A previous T becomes K only when its ordinal enters the new D and can supply formal capacity, not because |L|=N. H=D minus ord(K) describes holes; after shrinking, |K|=N with H nonempty is legal. S limits actual excess above N, not group names.

P is an absolute ordinal boundary. Existing ordinal<P groups are protected from template-driven replacement; ordinal>=P may converge to the latest target. Percentage P ceils against latest N. P does not impose a minimum replica count or forbid explicit removal of protected groups during shrinking; the admission contract still requires active integer P<=N.

U is the configured integer or floor(percent*N); S is the integer or ceil(percent*N), defaulting to 1/0. Do not raise a positive percentage U to one. Reject resolved active U=S=0 even at zero replicas or full partition: N=3/U=25%/S=0 must be rejected, not silently corrected. Active integer U/P cannot exceed N; zero replicas allow default/explicit integer U=1. S may exceed 100%, but N+S must fit int32. Recompute after scaling rather than freezing at rollout start.

Require |L|<=N+S. If a reduction of N temporarily leaves excess existing groups, only cleanup/wait is permitted until creates fit again. Healthy rollout deletion preserves R>=max(0,N-U). An already unhealthy outdated group can be replaced below that floor without further Ready loss only within Q, descending order, partition and in-flight reservations. Do not repeatedly rebuild a same-target unhealthy group. Protected faults and new non-Ready groups charge the same availability accounting.

Explicit scale-down is authorized toward latest N; do not double-charge its removals as additional rollout disruptions. Subsequent template replacements obey the new availability floor. Surge is a temporary ceiling, not a required replica count, and scaling and rollout cannot spend it twice.

### A.3. Order and minimal disruption

Only scale-down can create new stable holes. Rolling replacement and fault recovery may have transient missing slots, but cannot treat those as new stable sparse outcomes. At settled checkpoints, scale-out and rollout do not increase identity debt: |H_after|<=|H_before|, accounting for expansion of D and its new target slots. Do not compare transient in-flight counts as stable layouts. Existing holes can be repaid over multiple operations; H need not become empty in one operation.

1. Add at most the genuinely new formal capacity, filling the lowest missing ordinal within D first. Reuse UIDs when expansion absorbs old surge or high retained groups. An old surge still outside D remains temporary even if total count equals new N; it may yield capacity under U/S. Without a free formal slot, do not delete healthy target retained groups to compact identities.
2. Preserve healthy retained groups requiring no update: both existing protected groups (even a later-protected B) and eligible current-target groups. Outside-D position does not authorize compaction or forced rollback.
3. Replace an outdated in-range group at its own ordinal; deleting 0 and rebuilding at 4 would create a new hole. An outdated out-of-range retained group already due for replacement may instead fill the lowest hole within D. Its new slot's absolute ordinal selects history or target. A protected destination uses history, and may result in an all-old canary stop as in P11; never claim full target adoption.
4. A zero-downtime temporary group paired with an outdated out-of-range retained group due for deletion may be created directly in the lowest formal hole, selecting history/target by that slot's partition. Otherwise use the lowest free out-of-range ordinal and clean it afterward. Do not create an unretainable group in a hole and later delete a healthy target just to restore layout.
5. Determine scale-down count from K first. Within K prefer non-Ready, then lower deletion cost, then higher ordinal; P does not change this ordering. Evaluate T separately against new N/S and the Ready floor. A numerically highest useful surge is not automatically the first scale victim. Protected old groups may be removed while higher new groups remain. Accepted deletion is irreversible; new spec governs future actions.

Thus {0,3} at N=2 expanded to N=3 yields {0,1,3}, still missing 2. A healthy 3 needing no update is preserved. Later N=4 can fill 2 without recreating 3. Order means avoiding new stable holes, preferring the lowest existing hole at a legal opportunity, and retaining useful identities, not forcing [0,N) every time.

### A.4. Operation ordering and version selection

Recompute N/P/U/S/target from latest spec. Previously planned future actions are not commitments; accepted API creates/deletes remain facts. Recreating an already deleted slot completes an issued action and is not newly skipping a higher candidate. Subsequent new selection resumes from the highest old eligible ordinal.

1. Recognize deleting/creating groups and surge, reclassify using new N/P. A deleting group retains its name and physical reservation until fully gone.
2. On shrink, select victims and wait for planned scale deletions. On expansion, arrange new formal slots in lowest holes; creates already accepted keep their template, unissued creates use the latest applicable version. Distinct formal slots may be created together. Arrange formal capacity before destructive rollout, but no blanket wait for all new groups Ready is imposed: each healthy deletion independently checks real R-1>=max(0,N-U). Increased U after expansion does not make its missing capacity free credit. Do not delete old groups to make room for formal slots that have not yet been arranged.
3. Select the highest eligible outdated retained group. Never skip it for a lower unhealthy group. If it is already unhealthy, cleanup may proceed without reducing R, even below the floor, while Q remains. A batch consists of consecutive highest old candidates and cannot exceed remaining Q. Wait for deletion before reusing names/capacity, and do not spend the same credit repeatedly while replacements are non-Ready. If the new target is also bad, wait for a changed target or report blocking rather than churn it.
4. For a healthy candidate, obtain legal surge if needed and preserve the floor after deletion. Otherwise wait without jumping lower. U>1 allows a consecutive highest-candidate batch; wait for that batch Ready before the next one. Protected missing/faulted slots use history and cannot bypass P.
5. Remove temporary surge last, after its service credit is no longer needed. Preserve its UID when N absorbs it. Old-target surge may be reclaimed after a target change only when budget permits.

The N=5/U=2 examples show why both rejecting all recovery below the floor and replacing all bad groups at once are wrong. With bad target B at 4/3, Q=0; a new target C makes Q=2 and permits replacing those two first. All-unhealthy A at Ready=0 similarly permits batches 4/3,2/1,0 as each new batch becomes Ready. Conversely S05's healthy high A and unhealthy lower B can remain blocked at the floor under target C because selection order is strict. These are deliberate tradeoffs, not automatic recovery guarantees for every state.

At creation/recreation time, ordinal<P uses the fixed, traceable historical template for this canary, and ordinal>=P uses latest target. Increasing P protects an existing B without rolling it back; if it later disappears, historical A may be recreated in its protected slot. The baseline is the most recently fully completed template before this canary; intermediate A→B→C submissions do not promote B. Only full adoption after releasing protection establishes the target as a future baseline. If history cannot be established, wait safely rather than guess the newest template.

Role replica changes affect internal membership, not SG template revision. Protected groups keep historical Pod/worker templates. If adding required members makes groups temporarily non-Ready, apply membership in batches that respect real N-U. Groups not yet selected remain evaluated against their own applied old membership; global desired-count edits cannot instantly revoke Ready credit everywhere. A selected group becomes Ready again only when all new members are Ready. Existing faults that consume U require recovery or legal surge, not relaxed budgets.

### A.5. Completion, blocking and implementability

- Scale is complete when planned shrink deletions are gone, |K|=N, and newly retained groups are fully Ready on their applicable versions. Stable identity debt may remain if it cannot be repaid without needless disruption.
- Rollout is complete when all existing eligible retained groups are Ready on the latest target, issued replacements are complete, temporary surge is removed and |L|=N. H need not be empty. Even at P=N, a sparse retained high ordinal can still be eligible. A canary pause exists only after no eligible retained group remains; never rebuild healthy groups merely to manufacture it.
- Track holes and out-of-range retained identities separately. They are not inherently incomplete versions and do not authorize deletion on an ordinary reconcile. Fault-created missing capacity still requires recovery and must not be confused with stable scale-down debt.
- Bad targets, missing history, exhausted Q, insufficient capacity/budget or strict-order blocking stop the relevant new actions and report the cause. Do not silently relax U/S, reverse accepted deletion or add rebuilds for superficial continuity.

A controller must preserve enough state across restart to distinguish retained groups, temporary surge and scale-down identity debt; count/ordinal alone is insufficient. Save/recover live historical templates and account for real Ready/Deleting/latest N/actual C/target V with in-flight reservations. This needs finite controller state and selection logic, not impossible Pod renaming. The contract does not claim current production compliance or substitute static reasoning for Kind verification.
