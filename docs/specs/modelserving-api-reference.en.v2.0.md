# ModelServing API Reference

This reference lists field paths, semantics, defaults, accepted values, percentage rounding, validation constraints, and minimal reference YAML.

**Contract status.** Mutability and validation statements describe the revised API contract, including the agreed immutable fields. **Mutable** means an in-place update is allowed if the resulting object passes all validation. **Immutable** means the value and, for an optional object, its presence cannot change after creation. Immutability still applies after rollout completion or scaling to zero. Some rules strengthen the inspected production baseline, `production/release-1.0@a011cd5a`; section 11 lists those differences so target requirements are not mistaken for implemented checks.

The YAML snippets are partial configurations that illustrate individual fields. Supply the remaining required fields when creating a ModelServing. Behavior tables illustrate allowed sequences; they do not guarantee a particular ordinal order.

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
| Role rollout budgets | `spec.template.roles[].maxUnavailable/maxSurge/partition` | Effective values of `1/0/0` for each Role under `RoleRollingUpdate` |
| Role coordination | `spec.rolloutStrategy.roleCoordination` | Roles roll independently |
| Failure recovery scope | `spec.recoveryPolicy` | `RoleRecreate` |
| Recovery grace period | `spec.template.restartGracePeriodSeconds` | `0` seconds; immediate handling |
| Eviction protection | `spec.rolloutStrategy.evictionStrategy` | No eviction budget is enabled |
| Gang minimum replicas | `spec.template.gangPolicy.minRoleReplicas` | All instances of each Role count toward the gang minimum |
| Topology placement boundary | `spec.template.networkTopology.groupPolicy/rolePolicy` | No corresponding boundary is configured |
| Topology relationships **\[NOTE\] Planned Feature [issues#645](https://github.com/volcano-sh/kthena/issues/645)** | `spec.template.networkTopology.servingGroupAntiAffinity/roleAffinity/roleAntiAffinity` | No corresponding relationship is configured |

### 1.2 Integers, percentages, and rounding

`maxUnavailable`, `maxSurge`, `partition`, and eviction protection thresholds use Kubernetes `IntOrString`: a non-negative integer or a percentage string. Quote percentage values in YAML, for example `"25%"`.

| Field | Percentage base | Rounding | Example: base `3`, value `"25%"` |
| --- | --- | --- | --- |
| ServingGroup `maxUnavailable` | `spec.replicas` | Round down; a positive percentage resolves to at least `1` when replicas are positive | `max(1, floor(3 × 25%)) = 1` |
| Role `maxUnavailable` | The corresponding Role's `replicas` | Round down; no minimum-of-one adjustment | `floor(3 × 25%) = 0` |
| `maxSurge` | Desired replicas at the corresponding level | Round up | `ceil(3 × 25%) = 1` |
| `partition` | Desired replicas at the corresponding level | Round up | `ceil(3 × 25%) = 1` |
| Eviction `minAvailable` / `roleMinAvailable` | Desired replicas at the corresponding level | Round up | `ceil(3 × 25%) = 1` |

`maxUnavailable`, `partition`, and eviction thresholds accept non-negative integers or whole-number percentages from `"0%"` through `"100%"`, subject to their replica-count limits. This revised reference retains the broader `maxSurge` range: non-negative integers or whole-number percentages with no API-contract upper bound of `100%`. The production baseline still rejects percentages above `100%`; see section 11. Use an integer such as `1`, not a numeric string such as `"1"` or a fractional percentage such as `"2.5%"`.

For a rollout that has eligible instances (`replicas > resolved partition`), validate budgets after applying defaults and percentage rounding: resolved `maxUnavailable` and `maxSurge` must not both be `0`. The revised contract also prohibits explicitly configuring both budgets as `0` or `"0%"`, even when partition currently pauses all updates. Zero desired replicas do not themselves require rollout progress. The Role-level rounding rule matters: with three Role replicas, `maxUnavailable: "25%"` resolves to `0`, so a positive resolved `maxSurge` is required when updates are eligible.

### 1.3 Role names

Path: `spec.template.roles[].name`

**Mutability.** The set of Role names is immutable. Adding, removing, or replacing a name requires a new ModelServing, including when the affected Role has zero replicas. Array order is mutable. Exchanging mutable contents between existing names, or swapping names between two entries while preserving the name set, is allowed; compare and update each Role by name.

**Validation.** Define between one and four Roles with unique names. Each name must be a DNS-1035 label of at most 12 characters: start with a lowercase letter, end with a lowercase letter or digit, and contain only lowercase letters, digits, and hyphens. Generated Pod names must also remain valid DNS-1035 labels of at most 63 characters, including the ModelServing name and replica ordinals. An update must preserve `set(new.roles[].name) == set(old.roles[].name)` in addition to name uniqueness.

For example, `{prefill, decode} -> {prefill, decode-v2}` is rejected. Reordering `{prefill, decode}` or exchanging the two Roles' mutable templates is allowed if all other validation succeeds. A pure reorder does not change the semantic revision; a template exchange follows the selected rollout strategy.

### 1.4 Replica counts and Role templates

| Field path | Mutability | Validation and update effect |
| --- | --- | --- |
| `spec.replicas` | Mutable | Non-negative `int32`; default `1`. Changes scale ServingGroups without creating a template revision. Revalidate ServingGroup budget, partition, and eviction threshold limits against the new count. |
| `spec.template.roles[].replicas` | Mutable for an existing named Role | Non-negative `int32`; default `1`. Changes scale that Role independently of revision identity. Revalidate Role budgets, partition, gang minimums, eviction thresholds, required topology conflicts, and dependency capacity against the new count. |
| `spec.template.roles[].entryTemplate` | Mutable | Required. The rendered Pod must satisfy Kubernetes Pod validation. Template changes participate in ControllerRevision comparison and trigger the selected rollout strategy. |
| `spec.template.roles[].workerReplicas` | Mutable | Non-negative `int32`; explicitly set `0` for no workers. A positive value requires `workerTemplate`. Changes alter the Role's Pod layout and trigger the selected rollout strategy. |
| `spec.template.roles[].workerTemplate` | Mutable | Required when `workerReplicas > 0`; it cannot be removed while workers remain desired. The rendered Pod must satisfy Kubernetes Pod validation. Template changes participate in revision comparison. |

**Cross-field validation.** Scaling must remain compatible with immutable `gangPolicy`, `networkTopology`, and `roleCoordination`. For example, a fixed gang minimum of two instances prevents scaling that Role to one. A dependency may also need capacity for both old and target versions, as described in section 5.2. Updating an otherwise mutable field does not permit changing the Role-name set or an immutable policy.

## 2. `rolloutStrategy.type`

Path: `spec.rolloutStrategy.type`

**Mutability.** Mutable, subject to the complete configuration remaining valid. Changing the strategy alone does not create a new template revision. Update incompatible budget fields in the same request. If `roleCoordination` was configured at creation, switching to `ServingGroupRollingUpdate` is not allowed: coordination is immutable and is valid only with `RoleRollingUpdate`.

| Value | Meaning | Budget configuration | Typical use |
| --- | --- | --- | --- |
| `ServingGroupRollingUpdate` | Replace a complete ServingGroup as one rollout unit | `spec.rolloutStrategy.rollingUpdateConfiguration` | Tightly coupled Roles or updates that replace the entire group |
| `RoleRollingUpdate` | Replace instances of Roles whose templates changed | Fields directly under `spec.template.roles[]` | Preserve unchanged Roles; commonly used with one ServingGroup |

The default is `ServingGroupRollingUpdate`, including when the entire `rolloutStrategy` object is omitted.

`RoleRollingUpdate` operates in every ServingGroup, with a separate budget for each Role in each group. For example, with `spec.replicas=3` and Role `maxUnavailable=1`, each of the three ServingGroups may simultaneously have one unavailable instance of that Role. The limit is not shared across groups. The original reference therefore recommends pairing this mode with `spec.replicas=1` when a single group is sufficient.

Configuration constraints:

- `ServingGroupRollingUpdate` uses the top-level `rollingUpdateConfiguration`. Do not configure Role `maxUnavailable`, `maxSurge`, or `partition`; all three are forbidden by the revised contract, including explicit zero values. `roleCoordination` is also forbidden.
- `RoleRollingUpdate` rejects the top-level `rollingUpdateConfiguration`. Place the three budget fields directly under each Role, without a nested `rollingUpdateConfiguration` object.
- Combining `ServingGroupRecreate` with `RoleRollingUpdate` is rejected.

### 2.1 `ServingGroupRollingUpdate`

**Mutability.** The strategy value may be selected or changed in place only when the resulting configuration satisfies section 2 and preserves all immutable fields.

**Validation.** Use only the top-level rollout budget object; omit all three Role rollout fields and `roleCoordination`. Any of the three recovery policies is compatible with this mode. The top-level budgets must satisfy the range, rounding, and nonzero-budget rules in section 3.

Reference configuration:

```yaml
spec:
  rolloutStrategy:
    type: ServingGroupRollingUpdate
```

**Simple behavior example.** Only the prefill template changes. Both prefill and decode Pods are recreated because the rollout unit is the whole ServingGroup.

| Stage | prefill | decode | ServingGroup state | Effect |
| --- | --- | --- | --- | --- |
| Initial | v1 Ready (Pod UID P1) | v1 Ready (Pod UID D1) | Ready | The old group is running |
| 1 | Deleting / recreating | Deleting / recreating | Unavailable | Changing one Role replaces the whole group |
| Final | v2 Ready (new Pod UID P2) | v1 Ready (new Pod UID D2) | Ready | The decode template is unchanged, but its Pods are recreated with the group |

### 2.2 `RoleRollingUpdate`

**Mutability.** The strategy value may be selected or changed in place only when the resulting configuration satisfies section 2. Changing the mode cannot add an immutable `roleCoordination` object that was absent at creation.

**Validation.** Omit top-level `rollingUpdateConfiguration`, including an empty `{}` object. Place budgets directly under each Role. `recoveryPolicy` must be `RoleRecreate` or `None`; `ServingGroupRecreate` is rejected. Coordination is optional, but must have been declared at creation if it is needed.

Reference configuration:

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
```

**Simple behavior example.** Only the prefill template changes. The unchanged decode Pods retain their UIDs.

| Stage | prefill | decode | ServingGroup state | Effect |
| --- | --- | --- | --- | --- |
| Initial | v1 Ready (Pod UID P1) | v1 Ready (Pod UID D1) | Ready | Both Roles use their original templates |
| 1 | Deleting / recreating | v1 Ready (same Pod UID D1) | Partially available | Only the changed Role is replaced |
| Final | v2 Ready (new Pod UID P2) | v1 Ready (same Pod UID D1) | Ready | The unchanged decode Pods are preserved |

## 3. ServingGroup rollout configuration

Configure these fields under `spec.rolloutStrategy.rollingUpdateConfiguration`.

**Mutability.** The object and its budget fields are mutable. They control rollout progress and are not inputs to template revision identity.

**Validation.** The object is allowed only with `ServingGroupRollingUpdate`, including that mode's default when `type` is omitted. In this section, `N = spec.replicas`. Apply defaults and percentage rounding before checking the budget pair. `maxUnavailable` and `maxSurge` cannot both be explicitly zero; when `N > resolved partition`, their resolved values must not both be zero either.

### 3.1 `maxUnavailable`

Path: `spec.rolloutStrategy.rollingUpdateConfiguration.maxUnavailable`

**Mutability.** Mutable; changes the availability budget of an existing rollout without creating a new revision.

**Validation.** Valid only with `ServingGroupRollingUpdate`. An explicitly configured integer must be in `[0, spec.replicas]`; a percentage must be in `"0%"`–`"100%"`. If this budget resolves to `0` while updates are eligible, `maxSurge` must resolve above `0`. Explicitly setting both budgets to zero is rejected. The integer upper bound and unconditional explicit-zero restriction strengthen the baseline; see section 11.

| Property | Description |
| --- | --- |
| Type | Non-negative integer [0, replica] or percentage [0%, 100%] |
| Default | `1` |
| Percentage rounding | Round down; a positive percentage resolves to at least `1` when `spec.replicas > 0` |
| Meaning | The maximum allowed unavailability relative to the desired ServingGroup count during a rollout |

The maximum scale-down instance count per iteration is calculated using the following formula:

```text
maxScaleDown = len(liveServingGroups) - (replicas - maxUnavailable) - newServingGroupUnavailableCount
```

**Reference configuration:**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxUnavailable: 2
```

**Simple behavior example.** With three groups, `maxUnavailable=2`, and the default `maxSurge=0`, two groups may be replaced together.

| Stage | sg-0 | sg-1 | sg-2 | Available groups | Effect of `maxUnavailable=2` |
| --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | 3 | Update has not started |
| 1 | v1 Ready | Deleting / recreating | Deleting / recreating | 1 | Up to two groups may be unavailable |
| 2 | v1 Ready | v2 Ready | v2 Ready | 3 | The two Ready groups release budget |
| 3 | Deleting / recreating | v2 Ready | v2 Ready | 2 | Update the remaining old group |
| Final | v2 Ready | v2 Ready | v2 Ready | 3 | Rollout complete |

The illustrated ordinal order is one possible sequence; the budget does not guarantee deletion order.

### 3.2 `maxSurge`

Path: `spec.rolloutStrategy.rollingUpdateConfiguration.maxSurge`

**Mutability.** Mutable; adjusts temporary rollout capacity without creating a new revision.

**Validation.** Valid only with `ServingGroupRollingUpdate`. Accept a non-negative integer or whole-number percentage; the revised range is not capped by `spec.replicas` or `100%`. If this budget resolves to `0` while updates are eligible, `maxUnavailable` must resolve above `0`. Explicitly setting both budgets to zero is rejected. Percentages above `100%` require the baseline validation change identified in section 11.

| Property | Description |
| --- | --- |
| Type | Non-negative integer or percentage [0%, $\infty$) |
| Default | `0` |
| Percentage rounding | Round up |
| Meaning | The temporary number of additional ServingGroups allowed above `spec.replicas` while old groups remain eligible for update |

For resolved surge `S`, the controller's live group count is bounded by `N + S`. This is a capacity limit, not a guarantee that the cluster can schedule the extra groups.

**Reference configuration:**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxSurge: 1
      maxUnavailable: 0
```

**Simple behavior example.** With three desired groups, `maxSurge=1`, and the fixed prerequisite `maxUnavailable=0`, create Ready replacement capacity before deleting a healthy old group.

| Stage | sg-0 | sg-1 | sg-2 | sg-3 (surge) | Live / available groups | Effect of `maxSurge=1` |
| --- | --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | Absent | 3 / 3 | Update has not started |
| 1 | v1 Ready | v1 Ready | v1 Ready | v2 creating | 4 / 3 | Create a fourth group before deleting a healthy old group |
| 2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | 4 / 4 | The Ready new group provides capacity for an old group to be removed |
| 3 | v1 Ready | v1 Ready | Deleting / recreating | v2 Ready | 3–4 / 3 | Delete one old group |
| 4 | v1 Ready | Deleting / recreating | v2 Ready | v2 Ready | 3–4 / 3 | Delete one old group |
| 5 | Deleting / recreating | v2 Ready | v2 Ready | v2 Ready | 3–4 / 3 | Delete one old group |
| Final | v2 Ready | v2 Ready | v2 Ready | removed | 3 / 3 | Retain three v2 groups |

Repeat the create-and-replace cycle until all old groups have been replaced, then return to three groups. If the new group never becomes Ready, this example stops at stage 1. The final retained ordinals need not be `sg-0..sg-2`.

### 3.3 `partition`

Path: `spec.rolloutStrategy.rollingUpdateConfiguration.partition`

**Mutability.** Mutable; lowering it can release protected groups for update, while raising it protects eligible old groups. It does not roll back already updated groups or create a new revision.

**Validation.** Valid only with `ServingGroupRollingUpdate`. An integer must be in `[0, spec.replicas]`; a percentage must be in `"0%"`–`"100%"`. Revalidate this limit when `spec.replicas` changes. Lowering partition to expose rollout candidates requires nonzero resolved rollout capacity. The integer upper bound strengthens the baseline; see section 11.

| Property | Description |
| --- | --- |
| Type | Non-negative integer [0, replica] or percentage [0%, 100%] |
| Default | `0` |
| Percentage rounding | Round up |
| Meaning | Protect old ServingGroups whose absolute ordinal is less than the resolved partition from template-driven replacement |

**Reference configuration:**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      partition: 1
```

Partition is an absolute ordinal boundary, not a count of arbitrary groups to retain.

**Simple behavior example.** With `partition=1` and the default budgets, ordinal 0 remains on v1 while the other two groups update.

| Stage | sg-0 | sg-1 | sg-2 | Effect of `partition=1` |
| --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | Update has not started |
| 1 | v1 Ready (protected) | v1 Ready | Deleting / recreating | Ordinal 0 is excluded from rollout candidates |
| 2 | v1 Ready (protected) | v1 Ready | v2 Ready | sg-2 has completed |
| 3 | v1 Ready (protected) | Deleting / recreating | v2 Ready | Update sg-1 |
| Final | v1 Ready (protected) | v2 Ready | v2 Ready | Ordinals in `[0, 1)` retain the old version |

## 4. Role rollout configuration

Role fields are placed directly under `spec.template.roles[]` and are evaluated separately for each Role in each ServingGroup.

**Mutability.** Each existing Role's rollout fields are mutable and excluded from template revision identity.

**Validation.** All three fields are valid only with `RoleRollingUpdate`; omit them under `ServingGroupRollingUpdate`, including explicit zero values. Do not wrap them in a per-Role `rollingUpdateConfiguration`. In this section, `N` is that Role's `replicas`; apply defaults and rounding separately for each Role. Explicitly zeroing both budgets is rejected. When `N > resolved partition`, the resolved budgets must not both be zero either.

### 4.1 Role `maxUnavailable`

Path: `spec.template.roles[].maxUnavailable`

**Mutability.** Mutable; affects only this Role's rollout budget in each ServingGroup.

**Validation.** Valid only with `RoleRollingUpdate`. An explicitly configured integer must be in `[0, role.replicas]`; a percentage must be in `"0%"`–`"100%"`. Revalidate after replica changes. Percentage rounding can produce `0` even for a positive percentage: with `replicas: 3`, `maxUnavailable: "25%"`, and `partition: 0`, a positive resolved `maxSurge` is required. Explicitly configuring both budgets as zero is rejected. Rejecting this field under `ServingGroupRollingUpdate` is a target change; see section 11.

| Property | Description |
| --- | --- |
| Type | Non-negative integer [0, replica] or percentage [0%, 100%] |
| Default | `1` |
| Percentage rounding | Round down, without the ServingGroup minimum-of-one adjustment |
| Additional limit | The resolved value must not exceed that Role's `replicas` |
| Meaning | Maximum allowed unavailability for this Role's desired instances in each ServingGroup |

**Reference configuration:**

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

**Simple behavior example.** Within one ServingGroup, update three prefill instances with `maxUnavailable=2`. The unchanged decode Pods retain their UIDs.

| Stage | prefill-0 | prefill-1 | prefill-2 | decode | Effect of `maxUnavailable=2` |
| --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | v1 Ready (original UIDs) | Update has not started |
| 1 | v1 Ready | Deleting / recreating | Deleting / recreating | v1 Ready (original UIDs) | Up to two prefill instances in this group may be unavailable |
| 2 | v1 Ready | v2 Ready | v2 Ready | v1 Ready (original UIDs) | Two Ready instances release budget |
| 3 | Deleting / recreating | v2 Ready | v2 Ready | v1 Ready (original UIDs) | Update the last old instance |
| Final | v2 Ready | v2 Ready | v2 Ready | v1 Ready (original UIDs) | decode Pods have not been recreated |

### 4.2 Role `maxSurge`

Path: `spec.template.roles[].maxSurge`

**Mutability.** Mutable; adjusts this Role's temporary rollout capacity in each ServingGroup.

**Validation.** Valid only with `RoleRollingUpdate`. Accept a non-negative integer or whole-number percentage, without the revised contract imposing a `role.replicas` or `100%` upper bound. If it resolves to `0` while updates are eligible, Role `maxUnavailable` must resolve above `0`. Explicitly configuring both budgets as zero is rejected. Surge does not replace the stable replica capacity required by dependency validation. Percentages above `100%` differ from the baseline; see section 11.

| Property | Description |
| --- | --- |
| Type | Non-negative integer or percentage [0%, $\infty$) |
| Default | `0` |
| Percentage rounding | Round up |
| Meaning | Temporary additional instances of this Role allowed above `role.replicas` in each ServingGroup |

**Reference configuration:**

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

**Simple behavior example.** Create a fourth prefill instance using `maxSurge=1` and the fixed prerequisite `maxUnavailable=0`. decode does not scale out or restart.

| Stage | prefill-0 | prefill-1 | prefill-2 | prefill-3 (surge) | decode | Effect of `maxSurge=1` |
| --- | --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | Absent | v1 Ready (original pod UIDs) | Update has not started |
| 1 | v1 Ready | v1 Ready | v1 Ready | v2 creating | v1 Ready (original pod UIDs) | Add a fourth prefill instance |
| 2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v1 Ready (original pod UIDs) | Wait for new prefill capacity before deleting an old instance |
| 3 | v1 Ready | v1 Ready | Deleting / recreating | v2 Ready | v1 Ready (original pod UIDs) | Replace prefill-2; decode does not scale out |
| 4 | v1 Ready | Deleting / recreating | v2 Ready | v2 Ready | v1 Ready (original pod UIDs) | Replace prefill-1; decode does not scale out |
| 5 | Deleting / recreating | v2 Ready | v2 Ready | v2 Ready | v1 Ready (original pod UIDs) | Replace prefill-0; decode does not scale out |
| Final | v2 Ready | v2 Ready | v2 Ready | removed | v1 Ready (original UIDs) | Retain three v2 prefill instances |

Repeat the replacement cycle and scale back to three v2 prefill instances. Each ServingGroup applies its Role budget independently.

### 4.3 Role `partition`

Path: `spec.template.roles[].partition`

**Mutability.** Mutable; changes the protected ordinal range for this Role without creating a new revision or rolling back already updated instances.

**Validation.** Valid only with `RoleRollingUpdate`. An integer must be in `[0, role.replicas]`; a percentage must be in `"0%"`–`"100%"`. Revalidate after replica changes. When coordination needs a changed dependency's target-version capacity, partition must leave a usable target slot; increasing partition must not remove required capacity during an active coordinated rollout. Lowering partition must also leave nonzero resolved rollout capacity. The integer upper bound strengthens the baseline; see sections 5.2 and 11.

| Property | Description |
| --- | --- |
| Type | Non-negative integer [0, replica] or percentage [0%, 100%] |
| Default | `0` |
| Percentage rounding | Round up |
| Meaning | In each ServingGroup, protect instances of this Role whose ordinals are in `[0, partition)` |

For resolved partition `P`, Role instances with ordinals in `[0, P)` are protected independently in each ServingGroup.

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

**Simple behavior example.** With three prefill instances and `partition=1`, prefill-0 retains v1. The other prefill instances update; decode is unchanged.

| Stage | prefill-0 | prefill-1 | prefill-2 | decode | Effect of `partition=1` |
| --- | --- | --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | v1 Ready | v1 Ready (original UIDs) | Update has not started |
| 1 | v1 Ready (protected) | v1 Ready | Deleting / recreating | v1 Ready (original UIDs) | prefill ordinal 0 is not updated |
| 2 | v1 Ready (protected) | v1 Ready | v2 Ready | v1 Ready (original UIDs) | prefill-2 has completed |
| 3 | v1 Ready (protected) | Deleting / recreating | v2 Ready | v1 Ready (original UIDs) | Update prefill-1 |
| Final | v1 Ready (protected) | v2 Ready | v2 Ready | v1 Ready (original UIDs) | Only prefill-0 retains the old prefill version |

## 5. `roleCoordination`

Path: `spec.rolloutStrategy.roleCoordination`

**Mutability.** Immutable as a whole, including its presence, `roles`, `maxSkew`, and all dependency entries. It cannot be added, removed, enabled, disabled, or edited after creation, even after a rollout completes. Role replicas and rollout budgets remain mutable subject to dependency-capacity validation. This whole-object immutability is a target change.

**Validation.** Allowed only with `RoleRollingUpdate`. `maxSkew` is required. An omitted or empty `roles` list resolves to all defined Roles; the resulting coordinated set must contain at least two distinct, existing Role names. Each dependency owner may appear only once, all referenced names must belong to the coordinated set, and the dependency graph must have no self-reference, repeated dependency names, or cycles. Update requests must also preserve the capacity needed to start the target version while retaining required old-version dependencies.

This object is allowed only with `RoleRollingUpdate`. Omitting it disables coordination; it is not a Boolean switch. When enabled, the controller first applies each Role's own `maxUnavailable`, `maxSurge`, and `partition`, then uses coordination to further constrain how many instances may begin updating.

| Field | Required | Mutability | Default / range | Meaning |
| --- | --- | --- | --- | --- |
| `roles` | No | Immutable | Omitted or `[]` selects all Roles; the resolved set must contain at least two Roles | Roles participating in proportional rollout; names must exist and be unique |
| `maxSkew` | Yes | Immutable | Percentage strings from `"1%"` to `"100%"` only | Allowed lead over the slowest normalized Ready progress, subject to integer rounding |
| `dependencies` | No | Immutable | No dependencies | Startup dependencies for the target version; the graph must be acyclic |
| `dependencies[].role` | Yes | Immutable | Must belong to the coordinated set; unique across dependency entries | The Role whose target-version startup is constrained |
| `dependencies[].dependsOn` | Yes | Immutable | Role names in the coordinated set; no self-reference, duplicates, or cycles | Roles that must provide Ready target-version capacity before this Role first starts its target version |

Progress is normalized by each Role's number of instances eligible for update; participating Roles do not need equal replica counts. The slowest Role's target-version **Ready** progress is the baseline:

$$
allowedStarted(role)
  = ceil((slowestReadyProgress + maxSkew) × role.totalToUpdate)
$$

### 5.1 `maxSkew`

Path: `spec.rolloutStrategy.roleCoordination.maxSkew`

**Mutability.** Immutable as part of `roleCoordination`; increasing or decreasing it after creation is rejected.

**Validation.** Required when `roleCoordination` is present. Accept only whole-number percentage strings from `"1%"` through `"100%"`. Integers, `"0%"`, fractional percentages, and values above `"100%"` are rejected. At least two Roles must participate, and each Role must independently satisfy its own rollout budget validation.

Reference configuration:

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
    roleCoordination:
      roles: [role-a, role-b]
      maxSkew: '25%'
```

**Simple behavior example.** Role A has four instances to update and Role B has two. The table gives cumulative started-instance limits under `maxSkew="25%"`, subject to each Role's own rollout budget.

| Stage | A target-version Ready | B target-version Ready | Slowest Ready progress | Maximum started A / B | Effect of `maxSkew=25%` |
| --- | --- | --- | --- | --- | --- |
| Initial | 0/4 | 0/2 | 0% | 1 / 1 | A 25% allowance rounds up to one instance for each Role |
| 1 | 1/4 | 1/2 | 25% | 2 / 1 | B is already at 50% and cannot advance further yet |
| 2 | 2/4 | 1/2 | 50% | 3 / 2 | Once A catches up, both can advance |
| 3 | 3/4 | 2/2 | 75% | 4 / 2 | B is complete; A may start its final instance |
| Final | 4/4 | 2/2 | 100% | 4 / 2 | Both Roles are complete |

A started instance may still be creating or not Ready. Ceiling rounding allows one instance of Role B at the initial step, which is already 50% of its two replicas. The ceiling calculation converts the progress allowance into whole instances. It can permit a larger percentage step for a Role with few replicas. Coordination only tightens each Role's rollout budget.

### 5.2 `dependencies`

Path: `spec.rolloutStrategy.roleCoordination.dependencies`

**Mutability.** Immutable as part of `roleCoordination`, including each `role` and its `dependsOn` list. A completed rollout does not permit adding, removing, or redirecting an edge.

**Validation.** Each entry requires `role` and `dependsOn`; owners must be unique. Owners and dependency names must exist in the coordinated Role set. Self-dependencies, repeated names in one `dependsOn` list, and cycles are rejected. On workload updates, a changed dependency must be able to start target-version capacity outside partition while retaining any required old request path. The check uses old and new desired replica counts and partition, not temporary `maxSurge` capacity. For example, a changed backend with one stable replica cannot retain that old replica and also supply a target replica for an old frontend that still depends on it; increase replicas or stage a scale-up first. Replica reductions and partition increases during an active coordinated rollout must preserve the required old and target slots.

Reference configuration:

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

**Simple behavior example.** Assume sufficient stable replica slots to retain both dependency versions. frontend may first start v2 only after backend has Ready v2 capacity.

| Stage | frontend | backend | Effect of `dependencies` |
| --- | --- | --- | --- |
| Initial | v1 Ready | v1 Ready | Update has not started |
| 1 | v1 Ready | First v2 instance created and Ready | backend establishes target-version capacity first |
| 2 | First v2 instance starts | At least one v2 Ready; old v1 retained | frontend may first start v2 after the dependency is Ready |
| 3 | v1 and v2 coexist | v1 and v2 coexist | Retain old backend capacity while old frontend instances still need it |
| Final | v2 Ready | v2 Ready | Remaining old backend capacity can be released after frontend finishes |

## 6. Failure recovery

### 6.1 `recoveryPolicy`

Path: `spec.recoveryPolicy`

**Mutability.** Mutable; changes failure recovery policy without creating a template revision. It must remain compatible with the selected rollout mode.

**Validation.** Only `ServingGroupRecreate`, `RoleRecreate`, and `None` are accepted. `ServingGroupRecreate` cannot be combined with `RoleRollingUpdate`; the other two values work with either rollout mode. Compatibility is checked after applying defaults and on updates as well as creation.

Default: `RoleRecreate`.

| Value | Recovery scope after Pod deletion, failure, or persistent unhealthiness | Usage notes |
| --- | --- | --- |
| `ServingGroupRecreate` | Delete and recreate all Roles and Pods in the affected ServingGroup | Restart the whole group together; incompatible with `RoleRollingUpdate` |
| `RoleRecreate` | Delete and recreate the affected Role instance, including its entry and worker Pods | Default; isolates recovery to one Role instance |
| `None` | Let kubelet recover surviving Pods; replace missing Pods individually | The runtime must support independent recovery; terminal failed Pods may still be deleted and replaced |

This policy controls failure recovery scope. Rollout batch size is controlled separately. `RoleRecreate` is valid with `ServingGroupRollingUpdate`, and `None` is valid with either rollout mode.

#### `ServingGroupRecreate`

**Mutability and validation.** Mutable, but valid only with `ServingGroupRollingUpdate`. A request that leaves `RoleRollingUpdate` configured is rejected.

Reference configuration:

```yaml
spec:
  recoveryPolicy: ServingGroupRecreate
```

**Simple behavior example.** A prefill Pod fails and triggers recovery. The whole ServingGroup is recreated, including the healthy decode Pods.

| Stage | prefill | decode | Effect of `ServingGroupRecreate` |
| --- | --- | --- | --- |
| Initial | Ready (Pod UID P1) | Ready (Pod UID D1) | The group is healthy |
| Failure | A prefill Pod becomes unhealthy | Ready (Pod UID D1) | The fault occurs in prefill |
| Recovery | Deleting / recreating | Deleting / recreating | Recreate the entire ServingGroup |
| Final | Ready (new Pod UID P2) | Ready (new Pod UID D2) | The healthy decode Pod is also replaced |

#### `RoleRecreate`

**Mutability and validation.** Mutable and valid with either rollout mode. This is the default; it does not require selecting `RoleRollingUpdate`.

Reference configuration:

```yaml
spec:
  recoveryPolicy: RoleRecreate
```

**Simple behavior example.** A Pod in one prefill instance fails and triggers recovery. Recreate that instance's entry and workers; preserve other Role instances.

| Stage | prefill | decode | Effect of `RoleRecreate` |
| --- | --- | --- | --- |
| Initial | Ready (Pod UID P1) | Ready (Pod UID D1) | Both Roles are healthy |
| Failure | A Pod in one Role instance fails | Ready (Pod UID D1) | The fault is localized to that prefill instance |
| Recovery | That instance's entry and workers are recreated | Ready (same Pod UID D1) | Recovery covers one complete Role instance |
| Final | Ready (new Pod UID P2) | Ready (same Pod UID D1) | Other Roles are preserved |

#### `None`

**Mutability and validation.** Mutable and valid with either rollout mode. The runtime's ability to recover independently is an operational prerequisite, not a separate admission check.

Reference configuration:

```yaml
spec:
  recoveryPolicy: None
```

**Simple behavior example.** Recovery stays at the individual Pod level. Missing Pods are still replaced.

| Failure | Controller behavior | Other Pods |
| --- | --- | --- |
| A container restarts in a surviving Pod | Allow kubelet to recover it within the same Pod | Preserved |
| One Pod is missing | Recreate only the missing Pod | Preserved |
| A Pod reaches a terminal failed state | May delete it after the grace period, then replace it as a missing Pod | Recovery does not expand to the Role or group |

### 6.2 `restartGracePeriodSeconds`

Path: `spec.template.restartGracePeriodSeconds`

**Mutability.** Mutable; changes recovery timing without creating a template revision.

**Validation.** Supply an `int64` number of seconds, not a duration string such as `"30s"`. Use non-negative values. A non-negative minimum is a recommendation in this reference, not a newly introduced admission requirement; the baseline accepts negative values and handles them like zero. No incompatibility with a particular rollout mode is imposed.

| Property | Description |
| --- | --- |
| Type | `int64`, in seconds |
| Default | `0` |
| Recommended range | Non-negative integers |
| Validation at the documented production baseline | Neither CRD nor webhook enforces `minimum: 0`; negative values are accepted and treated like zero for immediate handling |

When the controller detects that a previously running Pod is unhealthy, it removes the Pod from the available set and waits for this grace period:

- If the Pod becomes Ready during the grace period, preserve it.
- If it remains unhealthy after the grace period, delete it; `recoveryPolicy` determines whether to replace that Pod, recreate its Role instance, or recreate the ServingGroup.
- A value of `0` or less allows immediate handling of the unhealthy Pod.

With `recoveryPolicy=None`, a surviving non-terminal Pod is left to kubelet for recovery. Expiration of this grace period does not cause the controller to delete that Pod. A terminal failed Pod may still be deleted and replaced after the grace period.

This field is not a delay between rollout batches and does not pause planned template updates for the specified duration.

```yaml
spec:
  template:
    restartGracePeriodSeconds: 30
```

**Simple behavior example.** With a 30-second grace period, a previously running Pod can recover without replacement before the period expires.

| Time / stage | Pod state | Controller behavior |
| --- | --- | --- |
| `t=0s` | A previously running Pod becomes NotReady | Exclude it from availability; do not delete it yet |
| `t<30s` | Pod becomes Ready again | Preserve its UID and finish recovery |
| At grace-period expiry | Pod is still unhealthy | Delete the failed Pod when reconciliation processes the expiry |
| Afterwards | Pod is missing | Recover the Pod, Role instance, or entire group according to `recoveryPolicy` |

With `recoveryPolicy=None`, a surviving non-terminal Pod remains under kubelet recovery even after the grace period expires.

## 7. `evictionStrategy`

Path: `spec.rolloutStrategy.evictionStrategy`

**Mutability.** Mutable as a whole, including its presence, `protectionLevel`, `minAvailable`, and `roleMinAvailable`. It may be added, removed, or updated without creating a template revision. Changing the protection level requires a valid threshold for the new level in the same request.

**Validation.** Allowed with either rollout mode and independent of rollout granularity. `protectionLevel` accepts only `ServingGroup` or `Role`, defaulting to `ServingGroup`. ServingGroup protection requires `minAvailable`; Role protection requires a non-empty `roleMinAvailable` map. Validate active thresholds after percentage rounding, and revalidate them when the corresponding replica counts change. Only the thresholds for the selected protection level are used; the baseline does not reject an additional threshold belonging to the inactive level.

Although nested under `rolloutStrategy`, this object applies only to requests to the Kubernetes `pods/eviction` subresource. It does not set the rolling-update `maxUnavailable` budget or proactively create surge capacity for an eviction. Direct Pod deletion and node loss do not pass through this admission budget.

For ServingGroup protection:

```yaml
spec:
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: ServingGroup
      minAvailable: '66%'
```

**Simple behavior example.** For three ServingGroups, `minAvailable="66%"` rounds up to two. Disrupted units are charged to accepted evictions; the table shows state after each admission decision.

| Stage | sg-0 | sg-1 | sg-2 | Request / event | Result |
| --- | --- | --- | --- | --- | --- |
| Initial | Ready | Ready | Ready | None | Three complete Ready groups |
| 1 | Ready | Ready | Disrupted | Evict a Pod in sg-2 | Allowed; two Ready groups remain |
| 2 | Ready | Ready | Disrupted | Evict a Pod in sg-1 | Rejected; accepting it would violate `minAvailable=2` |
| 3 | Ready | Ready | Ready | sg-2 recovers | Disruption is cleared and budget becomes available again |

For Role protection:

```yaml
spec:
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: Role
      roleMinAvailable:
        prefill: 1
```

**Simple behavior example.** Assume three Ready prefill instances in the target ServingGroup and `roleMinAvailable.prefill=1`. Each accepted eviction consumes one instance's availability.

| Stage | prefill-0 | prefill-1 | prefill-2 | decode | Request | Result |
| --- | --- | --- | --- | --- | --- | --- |
| Initial | Ready | Ready | Ready | Not listed; no budget protection | None | Three Ready prefill instances |
| 1 | Ready | Ready | Disrupted | Unaffected | Evict a prefill-2 Pod | Allowed; two Ready instances remain |
| 2 | Ready | Disrupted | Disrupted | Unaffected | Evict a prefill-1 Pod | Allowed; one Ready instance remains |
| 3 | Ready | Disrupted | Disrupted | Unaffected | Evict a prefill-0 Pod | Rejected; prefill-0 remains Ready because the request is denied |

The denied request leaves prefill-0 Ready. Ready instances in another ServingGroup cannot satisfy this group's threshold.

| Field | Mutability | Default / range | Meaning |
| --- | --- | --- | --- |
| `protectionLevel` | Mutable | Default `ServingGroup`; values `ServingGroup` or `Role` | The logical unit protected by the eviction budget; changing it requires a valid threshold for the new level |
| `minAvailable` | Mutable | Non-negative integer or `"0%"`–`"100%"`; round percentages up; resolved value must not exceed `spec.replicas` | Required for ServingGroup protection; minimum complete Ready ServingGroups to retain |
| `roleMinAvailable` | Mutable, including keys and values | `map<RoleName, IntOrString>`; values are non-negative integers or `"0%"`–`"100%"`; round percentages up; each resolved value must not exceed that Role's `replicas` | Required and non-empty for Role protection; keys must reference existing Roles; minimum Ready instances of each listed Role in each ServingGroup |

ServingGroup protection without `minAvailable` is rejected. Role protection with an empty `roleMinAvailable` map is rejected. Map keys must name Roles in `spec.template.roles`; Roles absent from the map have no protection under this eviction budget.

Readiness is evaluated for the complete logical unit:

- A ServingGroup counts as one Ready unit only when all desired Role instances and their entry and worker Pods exist and are Ready.
- A Role instance counts as one Ready unit only when its entry and all worker Pods exist and are Ready.
- A Role threshold is evaluated in the target Pod's ServingGroup, without aggregating capacity from other ServingGroups.

## 8. `gangPolicy`

Paths: `spec.template.gangPolicy` and `spec.template.gangPolicy.minRoleReplicas`

**Mutability.** Immutable as a whole, including whether `gangPolicy` is present, whether `minRoleReplicas` is present, and every map key and value. Adding the policy to an existing object, removing it, or changing an empty policy into a populated one is rejected. This strengthens the baseline's partial immutability.

**Validation.** Each key must name an existing Role, and each value must be an integer satisfying `0 <= minRoleReplicas[role] <= role.replicas`. The same check applies after scaling: an immutable minimum cannot be lowered to accommodate a scale-down request. Roles absent from the map continue to use their full desired replica count, so their derived minimum changes when replicas change. Both rollout modes are supported; scheduling behavior requires a compatible Volcano scheduler.

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

**Simple behavior example.** The YAML requires at least one prefill and, because decode is omitted from the map, both decode instances. With no workers, the minimum gang is three Pods.

| Stage | Capacity available for the minimum set | Scheduling result | Effect |
| --- | --- | --- | --- |
| 1 | Insufficient for 1 prefill + 2 decode | PodGroup remains Pending | The minimum set cannot start piecemeal |
| 2 | Sufficient for 1 prefill + 2 decode | These three instances can begin scheduling as a gang | Each Role's minimum requirement is satisfied |
| 3 | More resources become available | The remaining two prefill instances can schedule | The minimum does not cap the final replica count |

| Property | Description |
| --- | --- |
| Type | `map<RoleName, int32>` |
| Default | If `gangPolicy` or its map is omitted, all desired instances of each Role count toward the gang minimum |
| Value range | `0 <= minRoleReplicas[role] <= role.replicas` |
| Roles absent from the map | Use their full desired replica count, not zero |
| Mutability | The entire `gangPolicy` object and its presence are immutable from creation, including all map entries |

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

**Mutability.** Immutable as part of `networkTopology`, including presence, `mode`, `highestTierAllowed`, and `highestTierName`.

**Validation.** Use `hard` or `soft`; never set both tier selectors. Hard mode requires one valid selector; soft mode may omit both. Numeric tiers must be non-negative, and named tiers must be at most 253 characters. A syntactically valid policy does not guarantee that a matching HyperNode domain or sufficient capacity exists; those determine scheduling success.

Reference configuration:

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      groupPolicy:
        mode: hard
        highestTierName: rack
```

**Simple behavior example.** All Pods of each ServingGroup must fit within one rack. Separate ServingGroups may share that rack.

| Group | prefill placement | decode placement | Effect of `groupPolicy` |
| --- | --- | --- | --- |
| sg-0 | rack-a | rack-a | All Pods of sg-0 fit within one rack boundary |
| sg-1 | rack-a | rack-a | sg-1 is placed independently; sharing sg-0's rack is allowed |

If no rack can accommodate the required placement in hard mode, the workload remains Pending.

### 9.2 `rolePolicy`

Path: `spec.template.networkTopology.rolePolicy`

**Mutability.** Immutable as part of `networkTopology`, including presence, `mode`, `highestTierAllowed`, and `highestTierName`.

**Validation.** The same boundary rules as `groupPolicy` apply: valid mode, at most one tier selector, exactly one selector in hard mode, a non-negative numeric tier or a name of at most 253 characters. The scheduler must support Role SubGroupPolicy for this finer placement boundary to take effect.

Reference configuration:

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      rolePolicy:
        mode: hard
        highestTierAllowed: 0
```

**Simple behavior example.** Each Role instance's entry and workers fit within a Tier 0 domain. Different instances may use different domains.

| ServingGroup | Role instance | Entry / worker placement | Effect of `rolePolicy` |
| --- | --- | --- | --- |
| sg-0 | prefill-0 | Tier 0 domain A | This Role SubJob fits within the allowed boundary |
| sg-0 | prefill-1 | Tier 0 domain B | Another instance may use a different Tier 0 domain |
| sg-0 | decode-0 | Tier 0 domain C | The whole ServingGroup need not share a single domain |

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

**Mutability.** Immutable, including adding or removing the object and editing required or preferred terms.

**Validation.** Requires `schedulerName: volcano`. Each term selects exactly one of a non-negative `topologyTier` or a `topologyTierName` of at most 253 characters. Required terms must omit weight; preferred terms require weight in `[1, 100]`. At least one term must exist across the three relationship objects. Peer ServingGroups are selected automatically; no Role list is used here.

Reference configuration:

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

**Simple behavior example.** Three ServingGroups use three different rack domains under the required rule.

| ServingGroup | Example placement | Effect                             |
| ------------ | ----------------- | ---------------------------------- |
| sg-0         | rack-a            | Occupies one rack domain           |
| sg-1         | rack-b            | Cannot share sg-0's rack           |
| sg-2         | rack-c            | Cannot share sg-0's or sg-1's rack |

With only two suitable racks, the third group remains Pending. A preferred rule with a weight can fall back to a shared rack.

### 10.2 `roleAffinity`

Path: `spec.template.networkTopology.roleAffinity`

**Mutability.** Immutable, including the Role lists, required or preferred terms, weights, and tier selectors.

**Validation.** Requires `schedulerName: volcano`. Each term must reference at least two distinct, existing Role names and exactly one valid tier selector. Required terms omit weight; preferred terms require weight in `[1, 100]`. Required affinity and anti-affinity are rejected when the numeric anti-affinity tier is broader than the affinity tier and they constrain overlapping active Roles incompatibly: at least two shared Roles have positive replicas, or a single-Role anti-affinity term targets a shared Role with more than one replica. This check also applies after replica changes. The baseline does not infer tier ordering from names or mixed numeric/name selectors.

Reference configuration:

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

**Simple behavior example.** Within each ServingGroup, prefill and decode share a rack.

| ServingGroup | prefill | decode | Effect                                                  |
| ------------ | ------- | ------ | ------------------------------------------------------- |
| sg-0         | rack-a  | rack-a | The two selected Roles must share a rack                |
| sg-1         | rack-a  | rack-a | The relationship is evaluated separately for each group |

This applies to all SubJobs of the selected Roles; it does not pair prefill and decode instances by ordinal.

### 10.3 `roleAntiAffinity`

Path: `spec.template.networkTopology.roleAntiAffinity`

**Mutability.** Immutable, including the Role lists, required or preferred terms, weights, and tier selectors.

**Validation.** Requires `schedulerName: volcano`. Each term must reference at least one existing Role, with no duplicate names, and exactly one valid tier selector. Required terms omit weight; preferred terms require weight in `[1, 100]`. The required affinity conflict check in section 10.2 also applies here and is re-evaluated when Role replicas change. A one-Role term and a multi-Role term have different spreading semantics, as illustrated below.

Reference configuration:

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

**Simple behavior example.** A single-Role term spreads prefill instances across node topology domains. decode is outside the term.

| Instance in one ServingGroup | Example placement | Effect |
| --- | --- | --- |
| prefill-0 | node-a | Occupies one node topology domain |
| prefill-1 | node-b | Separated from prefill-0 |
| prefill-2 | node-c | Separated from the other prefill instances |
| decode-\* | Any | Not listed in the term; unaffected by this rule |

A multi-Role term such as `roles: [prefill, decode]` separates the different Roles. It does not additionally spread instances of prefill from one another. To spread instances of one Role, give that Role its own term.

If any affinity or anti-affinity object is present, the three objects together must contain at least one term. A configuration containing only empty objects is rejected.

## 11. Differences from the inspected production baseline

| Topic | Revised API contract | Inspected baseline |
| --- | --- | --- |
| Role-name set | Immutable; order and valid per-name content changes remain allowed | No general name-set immutability check |
| `gangPolicy` | Entire object and its presence immutable | Existing parent cannot be removed; child-map equality checks do not cover every optional-field add/remove transition |
| `roleCoordination` | Entire object and its presence immutable | Consistency and dependency-capacity checks exist, but no general whole-object immutability rule |
| Role budgets under `ServingGroupRollingUpdate` | Reject Role `maxUnavailable`, `maxSurge`, and `partition`, even if explicitly zero | Rejects `maxSurge` and `partition`; Role `maxUnavailable` is not rejected and does not control group rollout |
| Explicitly zero `maxUnavailable` and `maxSurge` | Reject the pair, including a fully partition-protected configuration | The resolved-zero rejection applies only when `replicas > resolved partition`; zero replicas or a fully protected range bypass it |
| Budget percentages resolving to zero | When updates are eligible, reject if both resolved budgets are zero | Already checked after level-specific rounding |
| Integer upper bounds | Configured `maxUnavailable` and `partition` stay within the corresponding replica count | Explicit Role `maxUnavailable` is bounded; ServingGroup integer `maxUnavailable` and integer partitions at both levels do not have that general upper bound |
| `maxSurge` percentages | Non-negative whole-number percentages, including values above `100%` | The shared percentage validator rejects values above `100%` |
| Boundary tier selectors | `highestTierAllowed` and `highestTierName` are mutually exclusive; hard mode requires one | Individual field ranges are enforced, but the inspected ModelServing schema/webhook do not fully enforce these cross-field boundary rules |
