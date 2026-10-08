# ModelServing API Reference

Version 2.7 · 2026-10-08 · [简体中文](modelserving-api-reference.zh-CN.md)

This reference lists field paths, semantics, defaults, accepted values, percentage rounding, validation constraints, and minimal reference YAML.

**Contract status.** Mutability and validation statements describe the revised API contract, including the agreed immutable fields. **Mutable** means an in-place update is allowed if the resulting object passes all validation. **Immutable** means the value and, for an optional object, its presence cannot change after creation. Immutability still applies after rollout completion or scaling to zero. Some rules strengthen the inspected production baseline, `production/release-1.0@a011cd5a`; section 11 lists those differences so target requirements are not mistaken for implemented checks.

The YAML snippets are partial configurations that illustrate individual fields. Supply the remaining required fields when creating a ModelServing. Behavior tables follow section 2.3 candidate selection; asynchronous events need not occur in a total row-by-row order.

## 1. Core concepts

A ModelServing has three levels of replication:

- `spec.replicas`: the number of ServingGroups. Each ServingGroup contains the set of Roles needed to serve inference requests.
- `spec.template.roles[].replicas`: the number of instances of a particular Role in each ServingGroup.
- `spec.template.roles[].workerReplicas`: the number of worker Pods accompanying the single entry Pod of each Role instance.

### 1.1 Field paths and defaults

| Feature | Field path | Behavior when omitted |
| --- | --- | --- |
| Rollout granularity | `spec.rolloutStrategy.type` | `ServingGroupRollingUpdate` |
| ServingGroup rollout budgets | `spec.rolloutStrategy.rollingUpdateConfiguration.*` | `maxUnavailable=1`, `maxSurge=0`, `partition=0` |
| Role rollout budgets | `spec.template.roles[].maxUnavailable/maxSurge/partition` | `maxUnavailable=1`, `maxSurge=0`, `partition=0` for each Role under `RoleRollingUpdate` |
| Role coordination | `spec.rolloutStrategy.roleCoordination` | Roles roll independently |
| Failure recovery scope | `spec.recoveryPolicy` | `RoleRecreate` |
| Recovery grace period | `spec.template.restartGracePeriodSeconds` | `0` seconds; immediate handling |
| Eviction protection | `spec.rolloutStrategy.evictionStrategy` | No eviction budget is enabled |
| Gang minimum replicas | `spec.template.gangPolicy.minRoleReplicas` | All instances of each Role count toward the gang minimum |
| Topology placement boundary | `spec.template.networkTopology.groupPolicy/rolePolicy` | No corresponding boundary is configured |
| Topology relationships **\[NOTE\] Planned Feature [issues#645](https://github.com/volcano-sh/kthena/issues/645)** | `spec.template.networkTopology.servingGroupAntiAffinity/roleAffinity/roleAntiAffinity` | No corresponding relationship is configured |
| Plugin chain | `spec.plugins` | No plugin chain; substantive contents immutable after creation (section 12) |

### 1.2 Integers, percentages, and rounding

`maxUnavailable`, `maxSurge`, `partition`, and eviction protection thresholds use Kubernetes `IntOrString`: a non-negative integer or a percentage string. Quote percentage values in YAML, for example `"25%"`.

| Field | Percentage base | Rounding | Example: base `3`, value `"25%"` |
| --- | --- | --- | --- |
| ServingGroup `maxUnavailable` | `spec.replicas` | Round down; no minimum-of-one adjustment | `floor(3 × 25%) = 0` |
| Role `maxUnavailable` | The corresponding Role's `replicas` | Round down; no minimum-of-one adjustment | `floor(3 × 25%) = 0` |
| `maxSurge` | Desired replicas at the corresponding level | Round up | `ceil(3 × 25%) = 1` |
| `partition` | Desired replicas at the corresponding level | Round up | `ceil(3 × 25%) = 1` |
| Eviction `minAvailable` / `roleMinAvailable` | Desired replicas at the corresponding level | Round up | `ceil(3 × 25%) = 1` |

`maxUnavailable`, `partition`, and eviction thresholds accept non-negative integers or whole-number percentages from `"0%"` through `"100%"`, subject to their replica-count limits. This revised reference retains the broader `maxSurge` range: non-negative integers or whole-number percentages with no API-contract upper bound of `100%`. The production baseline still rejects percentages above `100%`; see section 11. Use an integer such as `1`, not a numeric string such as `"1"` or a fractional percentage such as `"2.5%"`.

Apply defaults (`maxUnavailable=1`, `maxSurge=0`) and percentage rounding at the **active** rollout level. Reject resolved `maxUnavailable=0` and `maxSurge=0`, even at zero replicas or with every ordinal protected by partition. At zero replicas, default or explicitly configured integer `maxUnavailable=1` is allowed; `maxUnavailable=0` with omitted maxSurge is rejected. For either mode, `desiredReplicas=3/maxUnavailable=25%/maxSurge=0` is rejected, `desiredReplicas=3/maxUnavailable=25%/maxSurge=1` is allowed, and `desiredReplicas=4/maxUnavailable=25%/maxSurge=0` is allowed. Scaling revalidates the same rules against the new replica count.

Inactive-level budgets may remain configured but are ignored. Their basic format, percentage range and non-negativity are still validated; replica upper bounds and the nonzero-pair check apply only to the active level. Active `replicas + resolved maxSurge` must fit in signed int32 (`<= 2147483647`), including when the surge percentage exceeds 100%.

### 1.3 Role names

Path: `spec.template.roles[].name`

**Accepted values**

Define 1–4 Roles with unique DNS-1035 names of at most 12 characters: start with a lowercase letter, end with a lowercase letter or digit, and otherwise use lowercase letters, digits and hyphens.

**Mutability**

The name set is immutable, including zero-replica Roles. Array order and valid mutable contents may change; match entries by name.

**Meaning**

A Role name identifies the Role across updates; contents are associated by name rather than array position.

**Reference snippet**

```yaml
spec:
  template:
    roles:
      - name: prefill
      - name: decode
```

**Behavior examples**

| Operation | Result |
| --- | --- |
| prefill → prefill-v2 | Rejected; create a new ModelServing to change a name |
| [prefill, decode] → [decode, prefill] | Pure reorder allowed; semantic revision unchanged |
| Exchange mutable templates of existing Roles | Roll under the selected strategy if all other validation passes |

**Notes**

Generated Pod names, including ModelServing name and ordinals, must remain valid DNS-1035 labels of at most 63 characters. Mutable edits do not permit changing the name set.

### 1.4 Replica counts and Role templates

Scaling must remain compatible with immutable gangPolicy, networkTopology and roleCoordination. A fixed gang minimum of two prevents scaling that Role to one. YAML snippets are partial; supply other required fields when creating an object.

#### 1.4.1 `spec.replicas`

Path: `spec.replicas`

**Accepted values**

Non-negative int32; default 1.

**Mutability**

Mutable, subject to validation of the complete updated object.

**Meaning**

Number of ServingGroups.

**Reference snippet**

```yaml
spec:
  replicas: 3
```

**Behavior examples**

| Operation | Effect |
| --- | --- |
| 1 → 3 | Add two ServingGroups without a new template revision |

**Notes**

Revalidate ServingGroup budgets, partition and eviction limits. Scale-down may retain sparse ordinals.

#### 1.4.2 `spec.template.roles[].replicas`

Path: `spec.template.roles[].replicas`

**Accepted values**

Non-negative int32; default 1.

**Mutability**

Mutable, subject to validation of the complete updated object.

**Meaning**

Instances of this Role in each ServingGroup.

**Reference snippet**

```yaml
spec:
  template:
    roles:
      - name: prefill
        replicas: 3
```

**Behavior examples**

| Operation | Effect |
| --- | --- |
| 1 → 3 | Add two prefill instances per group independently of template revision |

**Notes**

Existing named Roles only; revalidate Role budgets, gang minima, eviction thresholds, topology conflicts and dependency capacity.

#### 1.4.3 `spec.template.roles[].entryTemplate`

Path: `spec.template.roles[].entryTemplate`

**Accepted values**

Required Pod template; the rendered Pod must pass Kubernetes validation.

**Mutability**

Mutable, subject to validation of the complete updated object.

**Meaning**

Template of the entry Pod in each Role instance.

**Reference snippet**

```yaml
spec:
  template:
    roles:
      - name: prefill
        entryTemplate:
          spec:
            containers:
              - name: inference
                image: example/inference:v2
```

**Behavior examples**

| Operation | Effect |
| --- | --- |
| 镜像 v1 → v2 / image v1 → v2 | Participates in revision comparison and rolls at the selected granularity |

**Notes**

ServingGroup mode replaces the whole group; Role mode preserves unchanged Roles. The image is a placeholder.

#### 1.4.4 `spec.template.roles[].workerReplicas`

Path: `spec.template.roles[].workerReplicas`

**Accepted values**

Non-negative int32; explicitly set 0 for no workers.

**Mutability**

Mutable, subject to validation of the complete updated object.

**Meaning**

Worker Pods accompanying each entry Pod.

**Reference snippet**

```yaml
spec:
  template:
    roles:
      - name: prefill
        workerReplicas: 2
```

**Behavior examples**

| Operation | Effect |
| --- | --- |
| 0 → 2 | Each instance becomes 1 entry + 2 workers; layout changes trigger rollout |

**Notes**

Positive values require workerTemplate. This is a layout change, distinct from changing Role replicas.

#### 1.4.5 `spec.template.roles[].workerTemplate`

Path: `spec.template.roles[].workerTemplate`

**Accepted values**

Required when workerReplicas>0; rendered Pods must pass Kubernetes validation.

**Mutability**

Mutable, subject to validation of the complete updated object.

**Meaning**

Template of the worker Pods in each Role instance.

**Reference snippet**

```yaml
spec:
  template:
    roles:
      - name: prefill
        workerReplicas: 1
        workerTemplate:
          spec:
            containers:
              - name: inference
                image: example/inference:v2
```

**Behavior examples**

| Operation | Effect |
| --- | --- |
| 镜像 v1 → v2 / image v1 → v2 | Template changes participate in revision and follow the selected strategy |

**Notes**

Cannot remove this template while workerReplicas>0. The image is a placeholder.

## 2. `rolloutStrategy.type`

Path: `spec.rolloutStrategy.type`

**Accepted values**

`ServingGroupRollingUpdate` or `RoleRollingUpdate`; default ServingGroupRollingUpdate, including when rolloutStrategy is omitted.

**Mutability**

Mutable; changing the mode alone creates no template revision. Revalidate newly active budgets.

**Meaning**

Select whether template rollout replaces complete ServingGroups or individual Role instances.

**Reference snippet**

```yaml
spec:
  rolloutStrategy:
    type: ServingGroupRollingUpdate
```

**Behavior examples**

| Value | Effect when only prefill changes | Active budgets |
| --- | --- | --- |
| ServingGroupRollingUpdate | Replace the complete ServingGroup, including decode Pod UIDs | rollingUpdateConfiguration |
| RoleRollingUpdate | Replace prefill instances; retain decode Pod UIDs | roles[].maxUnavailable / maxSurge / partition |

**Notes**

ServingGroup mode allows but ignores Role budgets and forbids roleCoordination. Role mode allows but ignores top-level budgets (including an empty object) and forbids ServingGroupRecreate. Existing immutable coordination prevents switching back to ServingGroup mode. Role budgets are independent per Role per ServingGroup: three groups with Role maxUnavailable=1 may each have one unavailable instance. The original reference recommends spec.replicas=1 when one group suffices. See sections 2.1–2.2 for detailed stages.

### 2.1 `ServingGroupRollingUpdate`

Path: `spec.rolloutStrategy.type`

**Accepted values**

Only the top-level rollout budget is effective; Role budgets may be present but are ignored. Omit `roleCoordination`. Any of the three recovery policies is compatible with this mode. The top-level budgets must satisfy the range, rounding, and nonzero-budget rules in section 3. Default rollout mode.

**Mutability**

The strategy value may be selected or changed in place only when the resulting configuration satisfies section 2 and preserves all immutable fields.

**Meaning**

Replace a complete ServingGroup when a Role template changes.

**Reference snippet**

```yaml
spec:
  rolloutStrategy:
    type: ServingGroupRollingUpdate
```

**Behavior examples**

Only the prefill template changes. Both prefill and decode Pods are recreated because the rollout unit is the whole ServingGroup.

| Stage | prefill | decode | ServingGroup state | Effect |
| --- | --- | --- | --- | --- |
| Initial | v1 Ready (Pod UID P1) | v1 Ready (Pod UID D1) | Ready | The old group is running |
| 1 | Deleting / recreating | Deleting / recreating | Unavailable | Changing one Role replaces the whole group |
| Final | v2 Ready (new Pod UID P2) | v1 Ready (new Pod UID D2) | Ready | The decode template is unchanged, but its Pods are recreated with the group |

**Notes**

The complete object must satisfy the cross-field rules in the parent section. Snippets are partial configurations.

### 2.2 `RoleRollingUpdate`

Path: `spec.rolloutStrategy.type`

**Accepted values**

Top-level `rollingUpdateConfiguration` is allowed but ignored, including `{}`. Place active budgets directly under each Role. `recoveryPolicy` must be `RoleRecreate` or `None`; `ServingGroupRecreate` is rejected. Coordination is optional, but must have been declared at creation if it is needed. Optional alternative to the default ServingGroupRollingUpdate.

**Mutability**

The strategy value may be selected or changed in place only when the resulting configuration satisfies section 2. Changing the mode cannot add an immutable `roleCoordination` object that was absent at creation.

**Meaning**

Replace instances of changed Roles within each ServingGroup, preserving unchanged Roles.

**Reference snippet**

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
```

**Behavior examples**

Only the prefill template changes. The unchanged decode Pods retain their UIDs.

| Stage | prefill | decode | ServingGroup state | Effect |
| --- | --- | --- | --- | --- |
| Initial | v1 Ready (Pod UID P1) | v1 Ready (Pod UID D1) | Ready | Both Roles use their original templates |
| 1 | Deleting / recreating | v1 Ready (same Pod UID D1) | Partially available | Only the changed Role is replaced |
| Final | v2 Ready (new Pod UID P2) | v1 Ready (same Pod UID D1) | Ready | The unchanged decode Pods are preserved |

**Notes**

The complete object must satisfy the cross-field rules in the parent section. Snippets are partial configurations.

### 2.3 Shared budgets and default candidate selection

This section applies to both ServingGroup and Role rollout, with **no new ordering switch**. Resolve desiredReplicas/maxUnavailable/maxSurge/partition from the latest spec at the active level. One unit is a complete ServingGroup, or a complete instance of one Role within one ServingGroup. A Role contributes one Ready unit only when its entry and every required worker are Ready. Budgets are not shared across Roles or ServingGroups; unchanged Roles retain their UIDs.

| Rollout mode | Default old-instance selection | May skip a healthy higher old instance? |
| --- | --- | --- |
| ServingGroup | Eligible old NotReady first, descending ordinal within each health class, then old Ready | Yes, automatically |
| Role without roleCoordination | Same as ServingGroup | Yes, automatically |
| Role with roleCoordination | Descending stable old ordinals; stop when the highest candidate is blocked | No; no switch. The participant list separately determines maxSkew/dependency scope |

**Calculate the complete ledger before filtering candidates.** Protected, nonparticipating or temporarily undeletable instances remain in the availability accounting for their own budget scope.

```text
minAvailable         = max(0, desiredReplicas-maxUnavailable)
maxScaleDown         = max(0, activeReplicas-minAvailable-unavailableTargetReplicas-inFlightReservations)
maxHealthyScaleDown  = max(0, readyReplicas-minAvailable)
```

| Full name | Definition |
| --- | --- |
| desiredReplicas | Latest desired replicas at the active level: spec.replicas for ServingGroups, or the corresponding Role.replicas |
| maxUnavailable / maxSurge / partition | Resolved integer values after defaults and percentage rounding at the latest active level |
| minAvailable | Derived Ready floor for rollout; this is not the eviction configuration field |
| maxScaleDown | Total old-instance cleanup allowance; healthy deletion also obeys maxHealthyScaleDown |
| activeReplicas | Actual active instances, including created surge. Deleting instances occupy physical capacity until gone; uncreated maxSurge grants no credit |
| readyReplicas | All complete Ready capacity, including protected, old-version and usable surge instances; exclude capacity already committed to deletion |
| unavailableTargetReplicas | Unavailable instances of the latest target, including target surge; do not count every old NotReady instance as unavailableTargetReplicas |
| inFlightReservations | Committed allowance not yet reflected by a decrease in activeReplicas or increase in unavailableTargetReplicas. Account for each reservation once; zero at a stable checkpoint |
| maxHealthyScaleDown | Healthy-deletion limit; distinct from the hole-set concept in the ServingGroup appendix |

For example, an old instance with an issued deletion that remains in activeReplicas but not unavailableTargetReplicas consumes inFlightReservations. Once it disappears, activeReplicas decreases and the same inFlightReservations is no longer deducted; an unready latest-target replacement consumes unavailableTargetReplicas instead. If partition requires a historical-template replacement, its absence from unavailableTargetReplicas must not prematurely release the still-unrestored reservation. Reclassify when the target changes rather than permanently locking superseded bad versions in an old in-flight count. All actions share one projected ledger; creates must also respect desiredReplicas+maxSurge including outstanding creation reservations.

For default skipping, let eligibleOldNotReady/eligibleOldReady be old NotReady/old Ready candidates allowed by partition, version, identity and in-flight constraints:

```text
oldNotReadyToReplace     = min(|eligibleOldNotReady|, maxScaleDown)
oldReadyToReplace = min(|eligibleOldReady|, maxScaleDown-oldNotReadyToReplace, maxHealthyScaleDown)
```

maxScaleDown bounds total cleanup; **it does not authorize arbitrary healthy deletion**. Recheck readyReplicas for each healthy deletion. When external faults already leave readyReplicas<minAvailable, eligible old bad versions may be repaired within maxScaleDown without further rollout-induced Ready loss. Do not repeatedly template-roll an unready instance already on the current target; explicit recoveryPolicy behavior remains separate. Zero update work must not hold rollout progress indefinitely, and scale-to-zero follows the explicit scale-down intent.

With coordination, keep maxScaleDown/maxHealthyScaleDown and additionally apply section 5's remaining starts, dependencies and a descending stable-candidate prefix. Legal cleanup of a superseded old NotReady **temporary surge** consumes the same maxScaleDown, but neither skips a stable instance nor increases or refunds stable-start allowance. Confirm it has not become formal capacity, is not needed by dependencies and has no conflicting in-flight action. Healthy old surge also consumes maxHealthyScaleDown and cannot retire while needed for the availability floor. Ordinal>=desiredReplicas alone does not identify temporary surge.

See the [shared ServingGroup/Role lookup tables](servinggroup-compound-rollout.en.md#budget-lookup) for trajectories and mode comparisons. After each complete unit becomes Ready, recompute maxScaleDown, maxHealthyScaleDown and inFlightReservations from current state. Continue when an eligible candidate and all budget, partition, identity, physical-capacity and coordination constraints allow; do not wait for every other unit from the earlier batch. Readiness loss or another fault consumes allowance again; a Ready event is not permanent or repeatable credit. Coordinated Roles still obey stable-candidate order, dependencies and maxSkew.

## 3. ServingGroup rollout configuration

Configure these fields under `spec.rolloutStrategy.rollingUpdateConfiguration`.

**Mutability.** The object and its budget fields are mutable. They control rollout progress and are not inputs to template revision identity.

**Validation.** This object is effective with `ServingGroupRollingUpdate`, including the default mode, and allowed but ignored under Role rollout. Here `desiredReplicas = spec.replicas`. Apply section 1.2 defaults, rounding and active-level limits; reject resolved maxUnavailable/maxSurge=0/0 even at zero replicas or full partition. This restates the existing validation contract.

### 3.1 `maxUnavailable`

Path: `spec.rolloutStrategy.rollingUpdateConfiguration.maxUnavailable`

**Accepted values**

Effective only with `ServingGroupRollingUpdate`; under Role rollout this budget is allowed but ignored (section 1.2). An explicitly configured integer must be in `[0, spec.replicas]`; a percentage must be in `"0%"`–`"100%"`. If this active budget resolves to `0`, `maxSurge` must resolve above `0`. Resolved double-zero is rejected, including zero replicas, omitted fields, percentage rounding and full partition protection. At zero replicas, integer maxUnavailable=1 is allowed, whether defaulted or explicit. See section 11 for historical differences.

| Property | Description |
| --- | --- |
| Type | Non-negative integer [0, replica] or percentage [0%, 100%] |
| Default | `1` |
| Percentage rounding | Round down, with no minimum-of-one adjustment |

**Mutability**

Mutable; changes the availability budget of an existing rollout without creating a new revision.

**Meaning**

Maximum permitted unavailability relative to the desired ServingGroup count. Use the total cleanup and healthy-deletion limits in section 2.3.

**Reference snippet**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxUnavailable: 2
```

**Behavior examples**

With three groups, `maxUnavailable=2`, and the default `maxSurge=0`, two groups may be replaced together.

| Stage | servinggroup-0 | servinggroup-1 | servinggroup-2 | Available groups | Effect of `maxUnavailable=2` |
| --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | 3 | Update has not started |
| 1 | v1 Ready | Deleting / recreating | Deleting / recreating | 1 | Up to two groups may be unavailable |
| 2 | v1 Ready | v2 Ready | v2 Ready | 3 | The two Ready groups release budget |
| 3 | Deleting / recreating | v2 Ready | v2 Ready | 2 | Update the remaining old group |
| Final | v2 Ready | v2 Ready | v2 Ready | 3 | Rollout complete |

**Notes**

Use section 2.3's shared formula: `maxScaleDown=max(0,activeReplicas-max(0,desiredReplicas-maxUnavailable)-unavailableTargetReplicas-inFlightReservations)` and the separate healthy-deletion bound `maxHealthyScaleDown=max(0,readyReplicas-max(0,desiredReplicas-maxUnavailable))`. Partition filters candidates, not the complete Ready ledger; protected faults must still reduce availability.

For all-healthy candidates the descending sequence shown follows section 2.3. Eligible old NotReady instances take priority when present; the numeric budget alone does not select candidates.

### 3.2 `maxSurge`

Path: `spec.rolloutStrategy.rollingUpdateConfiguration.maxSurge`

**Accepted values**

Effective only with `ServingGroupRollingUpdate`; under Role rollout this budget is allowed but ignored (section 1.2). Accept a non-negative integer or whole-number percentage; the revised range is not capped by `spec.replicas` or `100%`. If this active budget resolves to `0`, `maxUnavailable` must resolve above `0`. Resolved double-zero is rejected, including zero replicas, omitted fields, percentage rounding and full partition protection. Require `spec.replicas + resolved maxSurge <= 2147483647`; percentages above 100% are allowed within this bound.

| Property | Description |
| --- | --- |
| Type | Non-negative integer or whole percentage; resolved sum bounded by int32 |
| Default | `0` |
| Percentage rounding | Round up |

**Mutability**

Mutable; adjusts temporary rollout capacity without creating a new revision.

**Meaning**

Temporary additional ServingGroups above the desired count. The active capacity ceiling is `replicas + resolved maxSurge`.

**Reference snippet**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxSurge: 1
      maxUnavailable: 0
```

**Behavior examples**

With three desired groups, `maxSurge=1`, and the fixed prerequisite `maxUnavailable=0`, create Ready replacement capacity before deleting a healthy old group.

| Stage | servinggroup-0 | servinggroup-1 | servinggroup-2 | servinggroup-3 (surge) | Live / available groups | Effect of `maxSurge=1` |
| --- | --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | Absent | 3 / 3 | Update has not started |
| 1 | v1 Ready | v1 Ready | v1 Ready | v2 creating | 4 / 3 | Create a fourth group before deleting a healthy old group |
| 2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | 4 / 4 | The Ready new group provides capacity for an old group to be removed |
| 3 | v1 Ready | v1 Ready | Deleting / recreating | v2 Ready | 3–4 / 3 | Delete one old group |
| 4 | v1 Ready | Deleting / recreating | v2 Ready | v2 Ready | 3–4 / 3 | Delete one old group |
| 5 | Deleting / recreating | v2 Ready | v2 Ready | v2 Ready | 3–4 / 3 | Delete one old group |
| Final | v2 Ready | v2 Ready | v2 Ready | removed | 3 / 3 | Retain three v2 groups |

**Notes**

For the resolved `maxSurge`, the controller's live group count is bounded by `desiredReplicas + maxSurge`. This is a capacity limit, not a guarantee that the cluster can schedule the extra groups.

Repeat the create-and-replace cycle until all old groups have been replaced, then return to three groups. If the new group never becomes Ready, this example stops at stage 1. A rollout starting with contiguous ordinals returns to `servinggroup-0..servinggroup-2` after temporary surge is removed. Previously retained healthy high ordinals can remain in a sparse layout (SG-S03); an outdated high ordinal can instead be replaced in a low hole, whose absolute ordinal determines its partition version (section 3.3).

### 3.3 `partition`

Path: `spec.rolloutStrategy.rollingUpdateConfiguration.partition`

**Accepted values**

Effective only with `ServingGroupRollingUpdate`; under Role rollout this budget is allowed but ignored (section 1.2). An integer must be in `[0, spec.replicas]`; a percentage must be in `"0%"`–`"100%"`. Revalidate this limit when `spec.replicas` changes. Lowering partition to expose rollout candidates requires nonzero resolved rollout capacity. The integer upper bound strengthens the baseline; see section 11.

| Property | Description |
| --- | --- |
| Type | Non-negative integer [0, replica] or percentage [0%, 100%] |
| Default | `0` |
| Percentage rounding | Round up |

**Mutability**

Mutable; lowering it can release protected groups for update, while raising it protects eligible old groups. It does not roll back already updated groups or create a new revision.

**Meaning**

An absolute ordinal boundary: protect old ServingGroups whose ordinal is below the resolved partition.

**Reference snippet**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      partition: 1
```

**Behavior examples**

With `partition=1` and the default budgets, ordinal 0 remains on v1 while the other two groups update.

| Stage | servinggroup-0 | servinggroup-1 | servinggroup-2 | Effect of `partition=1` |
| --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | Update has not started |
| 1 | v1 Ready (protected) | v1 Ready | Deleting / recreating | Ordinal 0 is excluded from rollout candidates |
| 2 | v1 Ready (protected) | v1 Ready | v2 Ready | servinggroup-2 has completed |
| 3 | v1 Ready (protected) | Deleting / recreating | v2 Ready | Update servinggroup-1 |
| Final | v1 Ready (protected) | v2 Ready | v2 Ready | Ordinals in `[0, 1)` retain the old version |

**Notes**

Partition is an absolute ordinal boundary, not a count of arbitrary groups to retain.

**Sparse ordinals during a template rollout (clarified 2026-10-06).** A hole left by replica-only scale-down does not itself start a rollout. During an actual template rollout, replacing an outdated high-ordinal group can restore a missing low ordinal. Partition applies to the **replacement's absolute ordinal**, not the deleted group's ordinal or its position in a sorted list. An empty protected slot is recreated from the historical template.

For example, start with `desiredReplicas=2`, `{servinggroup-0:v1, servinggroup-3:v1}`, `partition=2`, `maxUnavailable=1`, `maxSurge=0`, then submit v2. The controller can delete outdated servinggroup-3 and create servinggroup-1 using historical v1. The converged layout is `{servinggroup-0:v1, servinggroup-1:v1}`: **both groups remain v1** because both absolute ordinals are below partition. This is a partition pause with the desired layout, not full adoption of v2: `currentRevision` remains v1, `updateRevision` is v2, and `updatedReplicas` is zero. Lowering partition releases those slots for v2. All creation and deletion still obey the configured rollout budgets; there is no exception that makes a protected replacement adopt v2 merely because its predecessor had an unprotected high ordinal. See SG-P11 in the compound rollout expectations for this boundary.

## 4. Role rollout configuration

Role fields are placed directly under `spec.template.roles[]` and are evaluated separately for each Role in each ServingGroup.

**Mutability.** Each existing Role's rollout fields are mutable and excluded from template revision identity.

**Validation.** These fields are effective with `RoleRollingUpdate`, and allowed but ignored under ServingGroup rollout. Do not wrap them in a per-Role `rollingUpdateConfiguration`. Here desiredReplicas is that Role's replicas. Apply section 1.2 defaults, rounding and active-level limits; reject resolved maxUnavailable/maxSurge=0/0 even at zero replicas or full partition. Use the same maxScaleDown/maxHealthyScaleDown/inFlightReservations and selection rules as ServingGroup in section 2.3; coordination further constrains candidates without expanding budgets.

### 4.1 Role `maxUnavailable`

Path: `spec.template.roles[].maxUnavailable`

**Accepted values**

Effective only with `RoleRollingUpdate`; under ServingGroup rollout this budget is allowed but ignored (section 1.2). An explicitly configured integer must be in `[0, role.replicas]`; a percentage must be in `"0%"`–`"100%"`. Revalidate after replica changes. Percentage rounding can produce `0` even for a positive percentage: with `replicas: 3`, `maxUnavailable: "25%"`, and `partition: 0`, a positive resolved `maxSurge` is required. Resolved double-zero is rejected, including zero replicas and full partition protection. At zero Role replicas, default or explicit integer maxUnavailable=1 is allowed. This field is ignored under ServingGroup rollout.

| Property | Description |
| --- | --- |
| Type | Non-negative integer [0, replica] or percentage [0%, 100%] |
| Default | `1` |
| Percentage rounding | Round down, as at ServingGroup level |
| Additional limit | The active value must not exceed Role replicas, except default/explicit integer maxUnavailable=1 at zero replicas |

**Mutability**

Mutable; affects only this Role's rollout budget in each ServingGroup.

**Meaning**

Maximum permitted unavailability for this Role in each ServingGroup, counted in complete Role instances.

**Reference snippet**

```yaml
spec:
  replicas: 1
  rolloutStrategy:
    type: RoleRollingUpdate
  template:
    roles:
      - name: prefill
        replicas: 3
        maxUnavailable: 2
      - name: decode
        replicas: 1
```

**Behavior examples**

Within one ServingGroup, update three prefill instances with `maxUnavailable=2`. The unchanged decode Pods retain their UIDs.

| Stage | prefill-0 | prefill-1 | prefill-2 | decode | Effect of `maxUnavailable=2` |
| --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | v1 Ready (original UIDs) | Update has not started |
| 1 | v1 Ready | Deleting / recreating | Deleting / recreating | v1 Ready (original UIDs) | Up to two prefill instances in this group may be unavailable |
| 2 | v1 Ready | v2 Ready | v2 Ready | v1 Ready (original UIDs) | Two Ready instances release budget |
| 3 | Deleting / recreating | v2 Ready | v2 Ready | v1 Ready (original UIDs) | Update the last old instance |
| Final | v2 Ready | v2 Ready | v2 Ready | v1 Ready (original UIDs) | decode Pods have not been recreated |

**Notes**

The complete object must satisfy the cross-field rules in the parent section. Snippets are partial configurations.

### 4.2 Role `maxSurge`

Path: `spec.template.roles[].maxSurge`

**Accepted values**

Effective only with `RoleRollingUpdate`; under ServingGroup rollout this budget is allowed but ignored (section 1.2). Accept a non-negative integer or whole-number percentage, without the revised contract imposing a `role.replicas` or `100%` upper bound. If this active budget resolves to `0`, Role `maxUnavailable` must resolve above `0`. Resolved double-zero is rejected, including zero replicas and full partition protection. Surge does not replace the stable replica capacity required by dependency validation. Require `role.replicas + resolved maxSurge <= 2147483647`; percentages above 100% are allowed within this bound.

| Property | Description |
| --- | --- |
| Type | Non-negative integer or whole percentage; resolved sum bounded by int32 |
| Default | `0` |
| Percentage rounding | Round up |

**Mutability**

Mutable; adjusts this Role's temporary rollout capacity in each ServingGroup.

**Meaning**

Temporary additional instances of this Role in each ServingGroup; unchanged Roles do not scale out for it.

**Reference snippet**

```yaml
spec:
  replicas: 1
  rolloutStrategy:
    type: RoleRollingUpdate
  template:
    roles:
      - name: prefill
        replicas: 3
        maxSurge: 1
        maxUnavailable: 0
```

**Behavior examples**

Create a fourth prefill instance using `maxSurge=1` and the fixed prerequisite `maxUnavailable=0`. decode does not scale out or restart.

| Stage | prefill-0 | prefill-1 | prefill-2 | prefill-3 (surge) | decode | Effect of `maxSurge=1` |
| --- | --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | Absent | v1 Ready (original pod UIDs) | Update has not started |
| 1 | v1 Ready | v1 Ready | v1 Ready | v2 creating | v1 Ready (original pod UIDs) | Add a fourth prefill instance |
| 2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v1 Ready (original pod UIDs) | Wait for new prefill capacity before deleting an old instance |
| 3 | v1 Ready | v1 Ready | Deleting / recreating | v2 Ready | v1 Ready (original pod UIDs) | Replace prefill-2; decode does not scale out |
| 4 | v1 Ready | Deleting / recreating | v2 Ready | v2 Ready | v1 Ready (original pod UIDs) | Replace prefill-1; decode does not scale out |
| 5 | Deleting / recreating | v2 Ready | v2 Ready | v2 Ready | v1 Ready (original pod UIDs) | Replace prefill-0; decode does not scale out |
| Final | v2 Ready | v2 Ready | v2 Ready | removed | v1 Ready (original UIDs) | Retain three v2 prefill instances |

**Notes**

Repeat the replacement cycle and scale back to three v2 prefill instances. Each ServingGroup applies its Role budget independently.

### 4.3 Role `partition`

Path: `spec.template.roles[].partition`

**Accepted values**

Effective only with `RoleRollingUpdate`; under ServingGroup rollout this budget is allowed but ignored (section 1.2). An integer must be in `[0, role.replicas]`; a percentage must be in `"0%"`–`"100%"`. Revalidate after replica changes. When coordination needs a changed dependency's target-version capacity, partition must leave a usable target slot; increasing partition must not remove required capacity during an active coordinated rollout. Lowering partition must also leave nonzero resolved rollout capacity. The integer upper bound strengthens the baseline; see sections 5.2 and 11.

| Property | Description |
| --- | --- |
| Type | Non-negative integer [0, replica] or percentage [0%, 100%] |
| Default | `0` |
| Percentage rounding | Round up |

**Mutability**

Mutable; changes the protected ordinal range for this Role without creating a new revision or rolling back already updated instances.

**Meaning**

Protect this Role’s old instances whose absolute ordinal is below the resolved partition, separately in each ServingGroup.

**Reference snippet**

```yaml
spec:
  replicas: 1
  rolloutStrategy:
    type: RoleRollingUpdate
  template:
    roles:
      - name: prefill
        replicas: 3
        partition: 1
```

**Behavior examples**

With three prefill instances and `partition=1`, prefill-0 retains v1. The other prefill instances update; decode is unchanged.

| Stage | prefill-0 | prefill-1 | prefill-2 | decode | Effect of `partition=1` |
| --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | v1 Ready (original UIDs) | Update has not started |
| 1 | v1 Ready (protected) | v1 Ready | Deleting / recreating | v1 Ready (original UIDs) | prefill ordinal 0 is not updated |
| 2 | v1 Ready (protected) | v1 Ready | v2 Ready | v1 Ready (original UIDs) | prefill-2 has completed |
| 3 | v1 Ready (protected) | Deleting / recreating | v2 Ready | v1 Ready (original UIDs) | Update prefill-1 |
| Final | v1 Ready (protected) | v2 Ready | v2 Ready | v1 Ready (original UIDs) | Only prefill-0 retains the old prefill version |

**Notes**

For the resolved `partition`, Role instances with ordinals in `[0, partition)` are protected independently in each ServingGroup.

The same absolute-ordinal and historical-template rule from section 3.3 applies to Role replica partition: restoring a protected low Role ordinal during an actual rollout can leave every desired Role replica on the historical version.

## 5. `roleCoordination`

Path: `spec.rolloutStrategy.roleCoordination`

**Accepted values**

Optional and permitted only with RoleRollingUpdate. When present, maxSkew is required; omitted or [] roles selects all Roles and must resolve to at least two distinct existing Roles. Dependencies must satisfy section 5.2.

**Mutability**

The whole object and its presence are immutable, including roles, maxSkew and dependencies, even after completion. Semantic set/map reordering is allowed. Role replicas and budgets remain mutable subject to other constraints.

**Meaning**

Apply each Role’s independent budgets first, then tighten starts through proportional progress and dependencies. Omission means independent rollout; this is not a mutable Boolean switch.

**Reference snippet**

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
    roleCoordination:
      roles: [prefill, decode]
      maxSkew: "25%"
```

**Behavior examples**

| Configuration | Effect |
| --- | --- |
| 省略 / omitted | Independent Roles; eligible old NotReady instances may take priority |
| 已配置 / configured | Descending stable old-instance prefix; wait at a blocked highest candidate while obeying progress and dependencies |

**Notes**

Progress measures actual target-version capacity: `N` is the latest desired formal Role population, including partition-protected instances and formal expansion but excluding temporary surge; `V` counts its complete target-version Ready instances. Use `p=V/N`, never `V/(N-partition)`. Protected instances already on the target count in V when Ready. Missing, terminating, unknown-history and non-Ready instances do not count as target Ready. Sparse stable identity and expansion/replacement ordering remain unchanged.

```text
allowedStarted(role) = min(N, ceil((slowestReadyProgress + maxSkew) * N))
remainingStart(role) = max(0, allowedStarted(role) - formalTargetStarts)
```

Formal target starts include non-Ready targets and reserved replacements without double counting. This capacity ledger is separate from the partition-limited stable replacement ledger; map remaining starts to lawful old candidates instead of subtracting partition twice. Roles whose template changed and N>0 remain in the baseline even after reaching their partition stop or having no eligible update work. Only N=0 is excluded for zero capacity. If only one Role changes template, there is no additional cross-Role progress limit. A Role with N=4, partition=2 and two target Ready instances has 50% version capacity despite completing its two eligible replacements.

**Partition compatibility at admission (2.7)**

Selected Roles must admit one common nominal retained fraction q, with each Role independently using `ceil(q*N)`. An explicit percentage fixes q. Integer P=0 requires q=0; integer P>0 permits `(P-1)/N < q <= P/N`. Intersect these intervals and fixed percentages exactly. N=0/P=0 imposes no ratio constraint. This allows 8/4/10 replicas with 10% partitions resolving independently to 1/1/1, and compatible integer/percentage mixtures; it rejects 4/4/4 with 2/0/0. It does not require exact equality of rounded P/N.

Validate creation and updates that change selected replicas, partition or Role templates. Legacy incompatible objects may still receive unrelated metadata/status/budget updates, but cannot initiate another such rollout/configuration change until corrected. While reconciling an incompatible active rollout, report `IncompatiblePartitions` and stop selecting further old stable deletions; already-issued replacements may finish. Never rewrite or propagate partition. Existing dependency-capacity and budget validation still applies. Individual Role partition protection in section 4.3 is unchanged.

The presence of coordination makes stable old-instance selection descending without skipping, with no additional switch. Select a consecutive prefix satisfying maxScaleDown, maxHealthyScaleDown, remainingStart and dependency/old-capacity retention. Stop at a blocked highest outdated candidate; do not bypass it for a lower NotReady one. Protected and already-target instances are not outdated update candidates. The participant list scopes proportional/dependency constraints, not an exception to this mode's ordering.

maxSkew means a **percentage progress difference**, not same-index pairing or atomic version switching. Legal superseded surge cleanup follows section 2.3 separately from the stable prefix and does not refund stable starts. See the shared lookup tables for examples.

### 5.1 `maxSkew`

Path: `spec.rolloutStrategy.roleCoordination.maxSkew`

**Accepted values**

Required when `roleCoordination` is present. Accept only whole-number percentage strings from `"1%"` through `"100%"`. Integers, `"0%"`, fractional percentages, and values above `"100%"` are rejected. At least two Roles must participate, and each Role must independently satisfy its own rollout budget validation. Required; no default.

**Mutability**

Immutable as part of `roleCoordination`; increasing or decreasing it after creation is rejected.

**Meaning**

Limit the lead over the slowest participating Role’s normalized target-version Ready progress. This is a percentage, not same-index pairing.

**Reference snippet**

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
    roleCoordination:
      roles: [role-a, role-b]
      maxSkew: '25%'
```

**Behavior examples**

Role role-a has four formal replicas and Role role-b has two; both have partition=0. The table gives cumulative started-instance limits under `maxSkew="25%"`, subject to each Role's own rollout budget.

| Stage | role-a target-version Ready | role-b target-version Ready | Slowest Ready progress | Maximum started role-a / role-b | Effect of `maxSkew=25%` |
| --- | --- | --- | --- | --- | --- |
| Initial | 0/4 | 0/2 | 0% | 1 / 1 | role-a 25% allowance rounds up to one instance for each Role |
| 1 | 1/4 | 1/2 | 25% | 2 / 1 | role-b is already at 50% and cannot advance further yet |
| 2 | 2/4 | 1/2 | 50% | 3 / 2 | Once role-a catches up, both can advance |
| 3 | 3/4 | 2/2 | 75% | 4 / 2 | role-b is complete; role-a may start its final instance |
| Final | 4/4 | 2/2 | 100% | 4 / 2 | Both Roles are complete |

**Notes**

role-a started instance may still be creating or not Ready. Ceiling rounding allows one instance of Role role-b at the initial step, which is already 50% of its two replicas. The ceiling calculation converts the progress allowance into whole instances. It can permit a larger percentage step for a Role with few replicas. Coordination only tightens each Role's rollout budget.

**Full-rollout terminal quantization (2.7)**

The ordinary ceiling rule can deadlock with old dependency retention. At a full-rollout tail only, allow a caller to start its final old formal instance when: all changing nonzero Roles have partition=0, each has at most one old formal instance, every formal instance is observed and Ready with known history, no replacement is in flight, and no old temporary/excess instance remains. The caller must be blocked by its ordinary skew allowance, have no remaining direct old caller requiring its own old capacity, and depend on retained old capacity. Recheck these facts on every reconciliation; the permit is not persisted. Unavailable targets, missing capacity or unknown history cannot qualify.

This grants at most one final start per Role. Existing budgets, physical capacity, stable order, target dependency readiness and old dependency retention still apply. It never applies to partition canary stops. Actual p is unchanged and the controller logs the terminal permit. For 8/4/10 replicas, 10% skew and prefill→decode→fff, Ready 7/8, 3/4, 9/10 permits prefill's final step. Once prefill's old instance disappears, decode may finish, followed by fff. A Ready checkpoint can temporarily be 100% versus 75%: this explicit single-instance terminal rule can exceed maxSkew and is not a strict 10% bound.

### 5.2 `dependencies`

Path: `spec.rolloutStrategy.roleCoordination.dependencies`

**Accepted values**

Each entry requires `role` and `dependsOn`; owners must be unique. Owners and dependency names must exist in the coordinated Role set. Self-dependencies, repeated names in one `dependsOn` list, and cycles are rejected. On workload updates, a changed dependency must be able to start target-version capacity outside partition while retaining any required old request path. The check uses old and new desired replica counts and partition, not temporary `maxSurge` capacity. For example, a changed backend with one stable replica cannot retain that old replica and also supply a target replica for an old frontend that still depends on it; increase replicas or stage a scale-up first. Replica reductions and partition increases during an active coordinated rollout must preserve the required old and target slots. Omitted means no dependency edges.

**Mutability**

Immutable as part of `roleCoordination`, including each `role` and its `dependsOn` list. A completed rollout does not permit adding, removing, or redirecting an edge.

**Meaning**

Require Ready target-version dependency capacity before starting a dependent Role, while retaining old capacity that is still needed.

**Reference snippet**

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
    roleCoordination:
      roles: [frontend, backend]
      maxSkew: '100%'
      dependencies:
        - role: frontend
          dependsOn: [backend]
```

**Behavior examples**

Assume sufficient stable replica slots to retain both dependency versions. frontend may first start v2 only after backend has Ready v2 capacity.

| Stage | frontend | backend | Effect of `dependencies` |
| --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | Update has not started |
| 1 | v1 Ready | First v2 instance created and Ready | backend establishes target-version capacity first |
| 2 | First v2 instance starts | At least one v2 Ready; old v1 retained | frontend may first start v2 after the dependency is Ready |
| 3 | v1 and v2 coexist | v1 and v2 coexist | Retain old backend capacity while old frontend instances still need it |
| Final | v2 Ready | v2 Ready | Remaining old backend capacity can be released after frontend finishes |

**Notes**

The complete object must satisfy the cross-field rules in the parent section. Snippets are partial configurations.

### 5.3 `roles`

Path: `spec.rolloutStrategy.roleCoordination.roles`

**Accepted values**

Omitted or [] selects all defined Roles. The resolved set must contain at least two distinct existing names.

**Mutability**

The Role set is immutable; semantic set reordering is allowed.

**Meaning**

Select participants for proportional progress and dependency constraints.

**Reference snippet**

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
    roleCoordination:
      roles: [prefill, decode]
      maxSkew: "25%"
```

**Behavior examples**

| Configuration | Effect |
| --- | --- |
| roles: [] | Select all defined Roles |
| roles: [prefill, decode] | Both participate in progress coordination; other Roles are outside this progress set |

**Notes**

Once roleCoordination is present, stable old instances cannot skip in this Role mode. Being outside the list is not an ordering exception. maxSkew is required and dependency names must belong to this set.

## 6. Failure recovery

### 6.1 `recoveryPolicy`

Path: `spec.recoveryPolicy`

**Accepted values**

`ServingGroupRecreate`, `RoleRecreate`, or `None`; default `RoleRecreate`. ServingGroup rollout permits all three; Role rollout rejects ServingGroupRecreate.

**Mutability**

Mutable; does not create a template revision.

**Meaning**

Select the rebuild scope for a fault and whether unhealthy restarts or terminal Failed trigger proactive deletion.

**Reference snippet**

```yaml
spec:
  recoveryPolicy: None
  template:
    restartGracePeriodSeconds: 0
```

**Behavior examples**

| Policy | Unhealthy restart / terminal Failed | Actual Pod deletion |
| --- | --- | --- |
| `ServingGroupRecreate` | After applicable grace, recreate the affected ServingGroup, including other Roles and all entry/worker Pods | Recreate the affected ServingGroup; grace does not postpone deletion recovery |
| `RoleRecreate` | After applicable grace, recreate the affected Role **instance**, including its entry and workers; other instances/Roles/groups retain UIDs | Recreate that Role instance, irrespective of grace |
| `None` | Never proactively delete Pods for restart errors or Failed, regardless of legal grace; keep reporting unavailability | Recreate only the missing Pod slot; retain every other Pod UID |

**Notes**

An unhealthy restart means a currently non-Ready Pod with `RestartCount > 0` in a normal or init container; a terminal `Failed` Pod also qualifies. Historical restarts on a currently Ready Pod do not trigger rebuilding. Non-Ready alone, without a restart or Failed, does not establish this recovery trigger. Recovery and availability are separate: an unrepaired Pod remains unavailable even when deletion is disabled.

`None` permits kubelet/container recovery in place where restart policy permits. A terminal Failed Pod is not promised to recover by itself: a user or external actor may delete it, after which the missing Pod is recreated. This policy does not suppress desired-state reconciliation for a genuinely missing Pod.

A local fault repair retains the enclosing rollout unit’s applied template and worker layout; it must not apply the newest target merely because a member failed. The rollout unit is a complete SG in ServingGroupRollingUpdate and one Role instance in RoleRollingUpdate. Rebuilding a whole rollout unit selects the protected baseline or latest target using partition at action time. If partition rises around an existing v2 unit, a local Pod/Role repair still retains that unit’s applied v2; only a whole-unit rebuild reselects the protected baseline and may return to v1. If the relevant history cannot be established, wait and report rather than guess the latest version.

For an unprotected unit with applied v1 and target v2:

| Rollout mode | Recovery scope | First replacement version |
| --- | --- | --- |
| ServingGroupRollingUpdate | None: replace only one missing Pod | Keep the SG’s applied v1; retain other UIDs |
| ServingGroupRollingUpdate | RoleRecreate: rebuild one contained Role instance | Use the enclosing SG’s v1; retain other Role UIDs |
| ServingGroupRollingUpdate | ServingGroupRecreate: rebuild the complete SG | Select by current partition; v2 for this unprotected slot |
| RoleRollingUpdate | None: replace only one missing Pod | Keep that Role instance’s applied v1 |
| RoleRollingUpdate | RoleRecreate: rebuild the complete Role instance | Select by the Role’s current partition; v2 in this example |

After local v1 recovery, a later ordinary rollout may adopt v2 when budgets, order, partition and coordination permit. Distinguish that valid template replacement from stale recovery events deleting a new UID: the earlier recovery action grants no deletion permission for replacement UIDs and cannot expand its scope. None does not disable ordinary template rollout.

While one controller process remains active, a RoleRecreate or ServingGroupRecreate still completes its configured scope and uses original Pod UID/owner checks to isolate same-name replacements. After a controller restart, the controller need not continue the pre-restart deletion batch and must not reconstruct it from Pod annotations or a persisted deletion transaction. It converges from surviving Pods, the latest configuration, applied historical templates, partition and current budgets. If a whole-group recovery deleted only prefill before restart while decode remains healthy, the new controller may refill prefill from the SG's correct historical template and retain the decode UID. If the refilled workload is still faulty, a new recovery decision begins only under the current recoveryPolicy, grace and health conditions.

Terminating Pods and actual unavailable capacity still consume current capacity and deletion allowance, so restart grants no duplicate credit. Complete Ready classification, candidate order, roleCoordination, UID/owner fencing, stale-event isolation and historical-template rules remain unchanged. None and grace=-1 cannot be bypassed. See [SG A.4 post-restart deletion convergence](servinggroup-compound-rollout.en.md#legacy-deletion-records).

See the [restart convergence lookup](servinggroup-compound-rollout.en.md#restart-convergence-cases) for phase expectations, SG/Role cases and survivor UID checks. Retention constrains the obsolete recovery plan; after partition is released, required updates may still replace those instances through lawful rollout.

### 6.2 `restartGracePeriodSeconds`

Path: `spec.template.restartGracePeriodSeconds`

**Accepted values**

Optional signed int64, minimum -1, default 0; values below -1 are rejected on create and update, including under None.

**Mutability**

Mutable; excluded from template revision identity.

**Meaning**

Seconds to wait for in-place recovery after first observing the fault. The sentinel -1 tolerates unhealthy restarts and Failed indefinitely.

**Reference snippet**

```yaml
spec:
  recoveryPolicy: RoleRecreate
  template:
    restartGracePeriodSeconds: -1
```

**Behavior examples**

| Value | With `ServingGroupRecreate` / `RoleRecreate` | With `None` |
| --- | --- | --- |
| omitted / `0` | Handle the current unhealthy restart or Failed immediately at the policy scope | No proactive Pod deletion |
| positive | Measure from the first observed fault; preserve UIDs if Ready recovers within grace, otherwise rebuild at the policy scope | No proactive Pod deletion |
| `-1` | Tolerate restart errors and Failed indefinitely; do not schedule a grace deletion task | No proactive Pod deletion |

**Notes**

`-1` is a sentinel, not a negative/immediate timeout or a very large sleeping timer. Actual Pod deletion still triggers the policy's deletion scope even at `-1`.

Before acting on a delayed task, use the latest policy and grace and verify ModelServing and Pod identity. A switch to `None` or `-1` cancels the old deletion effect; increasing/decreasing finite grace uses the current configuration. An old ModelServing/Pod UID must never authorize deletion of its replacement. Controller restart must preserve these rules.

For example, when a worker in Role instance 0 restarts and remains non-Ready: `recoveryPolicy=None, restartGracePeriodSeconds=0` and `recoveryPolicy=RoleRecreate, restartGracePeriodSeconds=-1` retain all UIDs while availability drops. If that worker is actually deleted, `None` replaces only it, whereas `recoveryPolicy=RoleRecreate, restartGracePeriodSeconds=-1` replaces the entry and workers of instance 0. Other Role instances and groups retain their UIDs. A transient error that becomes Ready before a finite deadline also preserves the original UIDs.

For the same continuous fault on the same ModelServing/Pod UIDs, preserve the first-observed time across controller restarts and active-controller changes. Offline time counts toward grace; do not start another full waiting period. The deadline is the trusted first-observed time plus the latest finite grace. Ready recovery ends that fault episode; a later fault or new Pod UID cannot inherit the old deletion permission. Migration for old objects without a trusted start time requires a separate design; do not substitute Pod creation time or a guessed fault time.

For grace=60 seconds, a fault first observed at t=0, controller exit at t=40 and restart at t=50 leaves about 10 seconds. Restart at t=70 does not add another 60 seconds, but acting still requires rechecking current UIDs, health, policy and grace.

## 7. `evictionStrategy`

The object, presence, level and thresholds are mutable without creating a template revision. Both rollout modes support it, independently of rollout granularity. It applies only to Kubernetes pods/eviction requests: it neither sets rollout maxUnavailable nor creates surge. Direct Pod deletion and node loss bypass this budget. A complete Ready ServingGroup requires all desired Role instances and their entry/workers; a Ready Role instance requires its entry and all workers.

### 7.1 `protectionLevel`

Path: `spec.rolloutStrategy.evictionStrategy.protectionLevel`

**Accepted values**

ServingGroup or Role; default ServingGroup.

**Mutability**

Mutable; validate together with the complete evictionStrategy.

**Meaning**

Select the logical unit protected by eviction admission.

**Reference snippet**

```yaml
spec:
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: ServingGroup
      minAvailable: 2
```

**Behavior examples**

| Value | Effect |
| --- | --- |
| ServingGroup | Count complete Ready ServingGroups |
| Role | Count corresponding Role instances in the target Pod’s ServingGroup |

**Notes**

Supply a valid threshold for the new level in the same update. Extra inactive-level thresholds may remain but are ignored.

### 7.2 `minAvailable`

Path: `spec.rolloutStrategy.evictionStrategy.minAvailable`

**Accepted values**

Non-negative integer or whole percentage 0%–100%, rounded up; resolved value <= spec.replicas. Required for ServingGroup protection; no default threshold.

**Mutability**

Mutable; validate together with the complete evictionStrategy.

**Meaning**

Minimum complete Ready ServingGroups to retain after eviction.

**Reference snippet**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: ServingGroup
      minAvailable: "66%"
```

**Behavior examples**

| Stage | Request / event | Result |
| --- | --- | --- |
| 1 | Evict a Pod in group 2 | Allowed; ceil(3×66%)=2 and two groups remain Ready |
| 2 | Evict from group 1 before group 2 recovers | Rejected; fewer than two Ready groups would remain |
| 3 | Group 2 recovers Ready | Budget becomes available again |

**Notes**

Revalidate after scaling; charge accepted evictions to disruptions without reusing their allowance.

### 7.3 `roleMinAvailable`

Path: `spec.rolloutStrategy.evictionStrategy.roleMinAvailable`

**Accepted values**

Non-empty RoleName→IntOrString map required for Role protection; keys must name existing Roles. Non-negative integers or whole percentages 0%–100%, rounded up and bounded by that Role’s replicas.

**Mutability**

Mutable; validate together with the complete evictionStrategy.

**Meaning**

Minimum complete Ready instances of each listed Role in each ServingGroup.

**Reference snippet**

```yaml
spec:
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: Role
      roleMinAvailable:
        prefill: 1
```

**Behavior examples**

| Three initially Ready prefill instances | Result |
| --- | --- |
| Evict prefill-2 | Allowed; 2 remain Ready |
| Then evict prefill-1 | Allowed; 1 remains Ready |
| Then evict prefill-0 | Rejected; that instance stays Ready |

**Notes**

Unlisted decode has no protection from this budget. Other ServingGroups cannot supply Ready credit; revalidate after scaling.

## 8. `gangPolicy`

Path: `spec.template.gangPolicy`

**Accepted values**

Each key must name an existing Role, and each value must be an integer satisfying `0 <= minRoleReplicas[role] <= role.replicas`. The same check applies after scaling: an immutable minimum cannot be lowered to accommodate a scale-down request. Roles absent from the map continue to use their full desired replica count, so their derived minimum changes when replicas change. Both rollout modes are supported; scheduling behavior requires a compatible Volcano scheduler.

| Property | Description |
| --- | --- |
| Type | `map<RoleName, int32>` |
| Default | If `gangPolicy` or its map is omitted, all desired instances of each Role count toward the gang minimum |
| Value range | `0 <= minRoleReplicas[role] <= role.replicas` |
| Roles absent from the map | Use their full desired replica count, not zero |

**Mutability**

Immutable as a whole, including whether `gangPolicy` is present, whether `minRoleReplicas` is present, and every map key and value. Adding the policy to an existing object, removing it, or changing an empty policy into a populated one is rejected. This strengthens the baseline's partial immutability.

**Meaning**

Choose each Role’s minimum contribution to gang scheduling; the minimum does not cap its desired replicas.

**Reference snippet**

```yaml
spec:
  template:
    gangPolicy:
      minRoleReplicas:
        prefill: 1
    roles:
      - name: prefill
        replicas: 3
        workerReplicas: 0
      - name: decode
        replicas: 2
        workerReplicas: 0
```

**Behavior examples**

The YAML requires at least one prefill and, because decode is omitted from the map, both decode instances. With no workers, the minimum gang is three Pods.

| Stage | Capacity available for the minimum set | Scheduling result | Effect |
| --- | --- | --- | --- |
| 1 | Insufficient for 1 prefill + 2 decode | PodGroup remains Pending | The minimum set cannot start piecemeal |
| 2 | Sufficient for 1 prefill + 2 decode | These three instances can begin scheduling as a gang | Each Role's minimum requirement is satisfied |
| 3 | More resources become available | The remaining two prefill instances can schedule | The minimum does not cap the final replica count |

**Notes**

The Pod count of one Role instance is:

```text
1 entry Pod + workerReplicas worker Pods
```

The ServingGroup PodGroup's `minMember` is therefore approximately:

```text
sum(effectiveMinRoleReplicas[role] × (1 + workerReplicas[role]))
```

A value of `0` is valid and removes that Role's contribution to the gang minimum without removing its desired replicas. Scheduling requires a compatible Volcano scheduler, CRDs, and plugins.

## 9. Network topology boundaries: `groupPolicy` and `rolePolicy`

Root path: `spec.template.networkTopology`.

**Mutability.** The whole object is immutable, including its presence, both boundary policies, all affinity and anti-affinity terms, tier selectors, Role references, and weights. Adding a previously omitted policy or removing an existing policy is rejected. This immutability is already enforced at the production baseline.

**Validation.** For each boundary policy, `mode` is `hard` or `soft` and defaults to `hard`. `highestTierAllowed` is a non-negative integer; `highestTierName` is at most 253 characters. The documented boundary contract makes the two selectors mutually exclusive and requires one in hard mode. The baseline schema enforces the individual ranges but does not fully enforce those cross-field boundary requirements; see section 11.

`groupPolicy` constrains the topology boundary containing all Pods of a ServingGroup. `rolePolicy` is copied into Volcano SubGroupPolicy entries for finer placement of Role SubJobs.

Both objects have the same fields:

| Field | Mutability | Default / range | Meaning |
| --- | --- | --- | --- |
| `mode` | Immutable | Default `hard`; values `hard` or `soft` | A mandatory boundary or a preference that permits fallback |
| `highestTierAllowed` | Immutable | Non-negative integer; mutually exclusive with `highestTierName` | Highest numeric HyperNode tier the placement may span |
| `highestTierName` | Immutable | At most 253 characters; mutually exclusive with `highestTierAllowed` | Highest permitted named tier, referencing `HyperNode.spec.tierName` |

`highestTierAllowed` and `highestTierName` express the same boundary and are mutually exclusive according to the API semantics. Configure one in `hard` mode; the PodGroup remains Pending if the boundary cannot be satisfied. `soft` mode allows an unspecified boundary or fallback when the preferred boundary has insufficient capacity.

### 9.1 `groupPolicy`

Path: `spec.template.networkTopology.groupPolicy`

**Accepted values**

Use `hard` or `soft`; never set both tier selectors. Hard mode requires one valid selector; soft mode may omit both. Numeric tiers must be non-negative, and named tiers must be at most 253 characters. A syntactically valid policy does not guarantee that a matching HyperNode domain or sufficient capacity exists; those determine scheduling success. Omitted means no group boundary; mode defaults to hard.

**Mutability**

Immutable as part of `networkTopology`, including presence, `mode`, `highestTierAllowed`, and `highestTierName`.

**Meaning**

Constrain the topology boundary containing all Pods of a ServingGroup.

**Reference snippet**

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      groupPolicy:
        mode: hard
        highestTierName: rack
```

**Behavior examples**

All Pods of each ServingGroup must fit within one rack. Separate ServingGroups may share that rack.

| Group | prefill placement | decode placement | Effect of `groupPolicy` |
| --- | --- | --- | --- |
| servinggroup-0 | rack-a | rack-a | All Pods of servinggroup-0 fit within one rack boundary |
| servinggroup-1 | rack-a | rack-a | servinggroup-1 is placed independently; sharing servinggroup-0's rack is allowed |

**Notes**

If no rack can accommodate the required placement in hard mode, the workload remains Pending.

### 9.2 `rolePolicy`

Path: `spec.template.networkTopology.rolePolicy`

**Accepted values**

The same boundary rules as `groupPolicy` apply: valid mode, at most one tier selector, exactly one selector in hard mode, a non-negative numeric tier or a name of at most 253 characters. The scheduler must support Role SubGroupPolicy for this finer placement boundary to take effect. Omitted means no Role boundary; mode defaults to hard.

**Mutability**

Immutable as part of `networkTopology`, including presence, `mode`, `highestTierAllowed`, and `highestTierName`.

**Meaning**

Constrain each Role instance’s entry and worker Pods to a topology boundary through Volcano SubGroupPolicy.

**Reference snippet**

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      rolePolicy:
        mode: hard
        highestTierAllowed: 0
```

**Behavior examples**

Each Role instance's entry and workers fit within a Tier 0 domain. Different instances may use different domains.

| ServingGroup | Role instance | Entry / worker placement | Effect of `rolePolicy` |
| --- | --- | --- | --- |
| servinggroup-0 | prefill-0 | Tier 0 domain-a | This Role SubJob fits within the allowed boundary |
| servinggroup-0 | prefill-1 | Tier 0 domain-b | Another instance may use a different Tier 0 domain |
| servinggroup-0 | decode-0 | Tier 0 domain-c | The whole ServingGroup need not share a single domain |

**Notes**

The complete object must satisfy the cross-field rules in the parent section. Snippets are partial configurations.

## 10. ServingGroup and Role affinity / anti-affinity

**[NOTE] Planned feature: [issues#645](https://github.com/volcano-sh/kthena/issues/645).** The rules below describe the feature's API contract.

**Mutability.** All three objects, their presence, terms, weights, tier selectors, and Role lists are immutable as part of `networkTopology`.

**Validation.** These relationship rules require `spec.schedulerName: volcano`. If any of the three objects is present, they must collectively contain at least one term. Required terms must omit `weight`; preferred terms require an integer weight from `1` through `100`. Every term must set exactly one tier selector. Required Role affinity and anti-affinity must also pass the conflict checks described below.

All three objects are under `spec.template.networkTopology`:

| Field | Scope | Meaning |
| --- | --- | --- |
| `servingGroupAntiAffinity` | Different ServingGroups in one ModelServing | Place groups in separate topology domains |
| `roleAffinity` | Selected Roles within each ServingGroup | Place their SubJobs in the same topology domain |
| `roleAntiAffinity` | Selected Roles within each ServingGroup | A single-Role term spreads that Role's instances; a multi-Role term separates different Roles |

Each object supports:

- `required`: hard constraints; terms must not set `weight`.
- `preferred`: soft constraints; each term must set `weight` from `1` to `100`.

Each term must specify exactly one of:

- `topologyTierName`: a `HyperNode.spec.tierName`, at most 253 characters.
- `topologyTier`: a `HyperNode.spec.tier`, a non-negative `int32`.

Role terms also contain `roles`. Names must exist in `spec.template.roles` and must not repeat. A `roleAffinity` term requires at least two Roles; a `roleAntiAffinity` term requires at least one.

### 10.1 `servingGroupAntiAffinity`

Path: `spec.template.networkTopology.servingGroupAntiAffinity`

**Accepted values**

Requires `schedulerName: volcano`. Each term selects exactly one of a non-negative `topologyTier` or a `topologyTierName` of at most 253 characters. Required terms must omit weight; preferred terms require weight in `[1, 100]`. At least one term must exist across the three relationship objects. Peer ServingGroups are selected automatically; no Role list is used here. Omitted means no corresponding relationship.

**Mutability**

Immutable, including adding or removing the object and editing required or preferred terms.

**Meaning**

Separate different ServingGroups of the same ModelServing into distinct topology domains.

**Reference snippet**

```yaml
spec:
  schedulerName: volcano
  replicas: 3
  template:
    networkTopology:
      servingGroupAntiAffinity:
        required:
          - topologyTierName: rack
```

**Behavior examples**

Three ServingGroups use three different rack domains under the required rule.

| ServingGroup | Example placement | Effect                             |
| ------------ | ----------------- | ---------------------------------- |
| servinggroup-0         | rack-a            | Occupies one rack domain           |
| servinggroup-1         | rack-b            | Cannot share servinggroup-0's rack           |
| servinggroup-2         | rack-c            | Cannot share servinggroup-0's or servinggroup-1's rack |

**Notes**

With only two suitable racks, the third group remains Pending. A preferred rule with a weight can fall back to a shared rack.

### 10.2 `roleAffinity`

Path: `spec.template.networkTopology.roleAffinity`

**Accepted values**

Requires `schedulerName: volcano`. Each term must reference at least two distinct, existing Role names and exactly one valid tier selector. Required terms omit weight; preferred terms require weight in `[1, 100]`. Required affinity and anti-affinity are rejected when the numeric anti-affinity tier is broader than the affinity tier and they constrain overlapping active Roles incompatibly: at least two shared Roles have positive replicas, or a single-Role anti-affinity term targets a shared Role with more than one replica. This check also applies after replica changes. The baseline does not infer tier ordering from names or mixed numeric/name selectors. Omitted means no corresponding relationship.

**Mutability**

Immutable, including the Role lists, required or preferred terms, weights, and tier selectors.

**Meaning**

Place all SubJobs of the selected Roles in the same domain within each ServingGroup; instances are not paired by ordinal.

**Reference snippet**

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      roleAffinity:
        required:
          - roles: [prefill, decode]
            topologyTierName: rack
```

**Behavior examples**

Within each ServingGroup, prefill and decode share a rack.

| ServingGroup | prefill | decode | Effect                                                  |
| ------------ | ------- | ------ | ------------------------------------------------------- |
| servinggroup-0         | rack-a  | rack-a | The two selected Roles must share a rack                |
| servinggroup-1         | rack-a  | rack-a | The relationship is evaluated separately for each group |

**Notes**

This applies to all SubJobs of the selected Roles; it does not pair prefill and decode instances by ordinal.

### 10.3 `roleAntiAffinity`

Path: `spec.template.networkTopology.roleAntiAffinity`

**Accepted values**

Requires `schedulerName: volcano`. Each term must reference at least one existing Role, with no duplicate names, and exactly one valid tier selector. Required terms omit weight; preferred terms require weight in `[1, 100]`. The required affinity conflict check in section 10.2 also applies here and is re-evaluated when Role replicas change. A one-Role term and a multi-Role term have different spreading semantics, as illustrated below. Omitted means no corresponding relationship.

**Mutability**

Immutable, including the Role lists, required or preferred terms, weights, and tier selectors.

**Meaning**

A single-Role term spreads that Role’s instances; a multi-Role term separates different Roles.

**Reference snippet**

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      roleAntiAffinity:
        required:
          - roles: [prefill]
            topologyTierName: node
```

**Behavior examples**

A single-Role term spreads prefill instances across node topology domains. decode is outside the term.

| Instance in one ServingGroup | Example placement | Effect |
| --- | --- | --- |
| prefill-0 | node-a | Occupies one node topology domain |
| prefill-1 | node-b | Separated from prefill-0 |
| prefill-2 | node-c | Separated from the other prefill instances |
| decode-\* | Any | Not listed in the term; unaffected by this rule |

**Notes**

A multi-Role term such as `roles: [prefill, decode]` separates the different Roles. It does not additionally spread instances of prefill from one another. To spread instances of one Role, give that Role its own term.

If any affinity or anti-affinity object is present, the three objects together must contain at least one term. A configuration containing only empty objects is rejected.

## 11. Differences from the inspected production baseline

The historical comparison is pinned to `production/release-1.0@a011cd5a`; it is **not** a claim about the current production branch. The approved 043 rules and 041 recovery semantics below define runner contract 2.1. Runner PASS still requires execution against a separately recorded candidate image/commit.

| Topic | Current API contract / runner verdict | Historical baseline |
| --- | --- | --- |
| Role-name set | Immutable, including zero-replica Roles; reordering and valid per-name content changes allowed | No general name-set immutability check |
| `gangPolicy` | Whole object and presence immutable, including map presence/keys/values | Partial parent/map checks miss optional transitions |
| `roleCoordination` | Whole object and presence immutable; reordering semantic sets/maps allowed | Consistency/capacity checks without general whole-object immutability |
| Inactive budgets | ServingGroup ignores Role maxUnavailable/maxSurge/partition; Role ignores top-level maxUnavailable/maxSurge/partition. Basic format/nonnegative checks remain; active-level bounds/pair rules do not apply to inactive budgets | ServingGroup rejects Role maxSurge/partition but permits maxUnavailable; Role rejects top-level budgets |
| Double-zero budgets | Reject resolved active maxUnavailable/maxSurge=0/0 after defaults and rounding, including zero replicas and full partition protection | Checked only when replicas exceed partition |
| Small positive maxUnavailable percentage | Always floor without minimum 1; desiredReplicas=3/maxUnavailable=25%/maxSurge=0 is rejected | ServingGroup rounds small positive maxUnavailable up to at least 1 |
| Integer bounds | Active maxUnavailable/partition <= corresponding replicas; zero replicas allow default/explicit integer maxUnavailable=1 | No general ServingGroup maxUnavailable or integer partition upper bounds |
| Surge percentages | Above 100% allowed; replicas+resolved maxSurge <= signed int32 maximum | Percentage validator capped at 100% |
| Boundary selectors | hard exactly one; soft at most one, with individual field limits | Cross-field checks incomplete |
| Recovery grace | -1 permanent tolerance; <-1 rejected; finite tasks recheck current policy and identities | Negative grace lacks the revised sentinel semantics |
| `None` recovery | No proactive deletion for restart errors or Failed; only an actually missing Pod is recreated | Failed cleanup could still delete a Pod |

Executable API cases: `cases/api-contract/cases.json`. Recovery execution: `scripts/run-recovery-contract.py` and `cases/recovery-contract/suite.json`. Historical catalogue conflicts and their current replacements are recorded separately; an old design trace is not proof of admission under this contract.

## 12. Plugin configuration and effects

Path: `spec.plugins`

**Accepted values**

Optional ordered list; omitted or empty means no plugin chain.

| Field under spec.plugins[] | Accepted values and defaults | Purpose |
| --- | --- | --- |
| `name` | Required registered name; no duplicate names | Select the plugin implementation |
| `type` | BuiltIn only; defaults to BuiltIn | Use an implementation built into the controller |
| `config` | Optional JSON interpreted by the plugin | Supply plugin-specific settings |
| `scope.roles` | Optional Role-name allowlist; omitted or empty means all Roles | Restrict matching Roles |
| `scope.target` | All / Entry / Worker; default All | Restrict the Pod kinds receiving Pod hooks |

**Mutability**

Substantive contents are immutable after creation: adding even the first plugin, removing even all plugins, changing name/type/config/scope, or meaningfully reordering the chain is rejected. Both rollout modes, zero replicas, completed rollouts, unready Pods and updates by parent controllers follow this rule. Equivalent representations are listed below.

**Meaning**

Plugins execute in list order to customize newly created Pods or maintain auxiliary resources such as Headless Services, discovery ConfigMaps and ranktables. Scope narrows application; not every lifecycle plugin supports Entry-only or Worker-only selection.

**Reference snippet**

```yaml
spec:
  plugins:
    - name: demo-pod-tweaks
      type: BuiltIn
      scope:
        roles: [prefill]
        target: Entry
      config:
        annotations:
          example.com/plugin: enabled
        env:
          - name: CUSTOM_VAR
            value: custom-value
```

**Behavior examples**

| Operation / scenario | Observable effect |
| --- | --- |
| Create with the snippet | prefill entry Pods receive the annotation and environment variable; workers and other Roles are outside this plugin’s scope |
| Scale or replace missing Pods without plugin changes | New Pods use the original plugin declaration; other fields must still pass their existing validation |
| Change plugins together with replicas or an image | Reject the whole request; do not partially persist replicas/image or trigger a plugin-change rollout |
| Scale to zero, then remove a plugin | Removal remains rejected |
| Need different plugin configuration | Create a new ModelServing and migrate traffic after validating readiness |

| Original representation | Permitted equivalent |
| --- | --- |
| plugins 省略 / omitted | [] |
| type 省略 / omitted | BuiltIn |
| scope 省略 / omitted | Empty scope; no Role restriction and target=All |
| scope.roles: [prefill, decode] | [decode, prefill] or [prefill, decode, prefill] |
| config 省略 / omitted | null |
| config JSON object | Object-key order or whitespace changes; preserve array order |

**Notes**

Neither config={} versus omission nor numeric spelling changes are guaranteed equivalent. Plugin immutability alone does not freeze all external inputs. Separately, the template-protection rule confirmed in 2.4 requires webhook rejection of content changes to ranktable template ConfigMaps referenced by a ModelServing; such changes are not a legal hot-update input. This protects the source template, not generated membership/communication ConfigMaps maintained by the plugin. It does not broaden the rule to all ConfigMaps, deletion or metadata-only updates. Other consumed annotations, controller changes and pre-existing mixed Pods remain separate concerns. Enable the ModelServing validating webhook. Successful API creation does not prove plugin executability; registration, private config and lifecycle restrictions also require construction/runtime checks. headless-service requires scope.target omitted or All, may restrict scope.roles, and maintains Services for Role instances with a workerTemplate. Different plugins require a new object; interruption depends on capacity and traffic migration.

The plugin-field immutability portion restates the [final task 046 implementation and verification](../../../issues/features/046-modelserving-plugin-immutable-DONE/PROPOSAL_COMMIT.md) and [approved task 051 port](../../../issues/features/051-production-baseline-realignment-DONE/production-selected-20261006/README.md), not the earlier exploratory proposal. Documentation does not establish new runner plugin execution coverage.

### 12.1 Effects of common built-in plugins

The following combines inspected built-in effects with approved 2.4 requirements. Availability depends on the deployed controller registry; requirements and implementation coverage are recorded separately.

| Plugin | Effect | Boundary |
| --- | --- | --- |
| `demo-pod-tweaks` | Set runtimeClassName, merge annotations and inject environment variables into selected Pods | Verify or reuse Pod customization through its config fields |
| `headless-service` | Maintain a Headless Service for Role instances with workerTemplate; inject ENTRY_ADDRESS and Pod hostname/subdomain | Provide entry/worker DNS discovery; restore missing desired Services without making auxiliary resources determine Role existence |
| `pod-discovery` | Aggregate ServingGroup member Pod IPs into a ConfigMap mounted at /etc/pod-discovery/ips.json | A membership table for group initialization: publish member IPs while Running even if NotReady, avoiding a cycle where readiness itself requires those addresses |
| `ranktable` / `pod-ranktable` | Generate, maintain and mount communication ConfigMaps from templates and member information | Require their respective templates, parsers and runtime; an in-use ranktable template has separate webhook protection, while generated communication data still follows membership changes |
| `lws-standard-labels` | Add standard group/worker identity labels to Pods of LeaderWorkerSet-managed ModelServings | Support parent-resource compatibility; does not turn an ordinary ModelServing into a LeaderWorkerSet |

The user confirmed the 2.4 membership-publication and template-protection requirements; these do not establish candidate conformance or runner coverage. Inspection of production@4e5c9016 found template existence validation but did not locate a ConfigMap-update rejection handler. The user subsequently confirmed that the protection webhook is on the production branch and asked to set this issue aside. Preserve the requirement based on that confirmation and defer implementation lookup, without treating it as a current action item or claiming deployment verification; see task 052.
