# ModelServing API 参考

版本 2.7 · 2026-10-08 · [English](modelserving-api-reference.en.md)

本文规定字段路径、默认值、可变性、合法范围、百分比取整和跨字段约束。Mutable 表示更新后的完整对象通过校验时可原地修改；Immutable 表示值和可选对象的存在性均不可变，即使滚动完成或副本数为零也不例外。

## 1. 核心概念

`spec.replicas` 是 ServingGroup 数量；每个 ServingGroup 包含全部 Role。`spec.template.roles[].replicas` 是每个 ServingGroup 内该 Role 的实例数量；`workerReplicas` 是每个实例中单个 entry Pod 附带的 worker Pod 数。

### 1.1 字段路径与默认值

| 功能 | 路径 | 省略时 |
| --- | --- | --- |
| 滚动粒度 | `spec.rolloutStrategy.type` | `ServingGroupRollingUpdate` |
| ServingGroup 预算 | `spec.rolloutStrategy.rollingUpdateConfiguration.*` | `maxUnavailable=1`、`maxSurge=0`、`partition=0` |
| Role 预算 | `spec.template.roles[].maxUnavailable/maxSurge/partition` | Role 模式每个 Role：`maxUnavailable=1`、`maxSurge=0`、`partition=0` |
| Role 协调 | `spec.rolloutStrategy.roleCoordination` | 各 Role 独立滚动 |
| 故障恢复 | `spec.recoveryPolicy` | `RoleRecreate` |
| 恢复宽限 | `spec.template.restartGracePeriodSeconds` | 0 秒 |
| 驱逐保护 | `spec.rolloutStrategy.evictionStrategy` | 不启用驱逐预算 |
| Gang 最小副本 | `spec.template.gangPolicy.minRoleReplicas` | 各 Role 全部期望实例计入最小集合 |
| 拓扑边界 | `spec.template.networkTopology.groupPolicy/rolePolicy` | 不配置对应边界 |
| 拓扑关系 | 同一对象下 `servingGroupAntiAffinity/roleAffinity/roleAntiAffinity` | 不配置对应关系；原参考将其标为计划能力 [#645](https://github.com/volcano-sh/kthena/issues/645) |
| 插件链 | `spec.plugins` | 不启用插件链；创建后实质内容不可修改，见 §12 |

### 1.2 整数、百分比与取整

`maxUnavailable`、`maxSurge`、`partition` 和驱逐阈值使用 IntOrString。整数必须非负；百分比必须为整数字符串，例如 `"25%"`。数字字符串 `"1"`、小数百分比 `"2.5%"`、负数和溢出均拒绝。

| 字段 | 百分比基数 | 取整 | 基数 3、25% |
| --- | --- | --- | --- |
| ServingGroup maxUnavailable | `spec.replicas` | floor，不补 1 | 0 |
| Role maxUnavailable | 对应 Role 的 replicas | floor，不补 1 | 0 |
| maxSurge、partition、驱逐阈值 | 对应层期望副本数 | ceil | 1 |

maxUnavailable/partition/驱逐百分比范围为 0%–100%。maxSurge 可以超过 100%，但生效层的 `replicas + resolved maxSurge <= 2147483647`。整数 maxUnavailable/partition 不得超过生效层 replicas；零副本允许默认或显式整数 maxUnavailable=1。

先应用 maxUnavailable=1、maxSurge=0 默认值，再取整，生效层 maxUnavailable/maxSurge 实际结果不得同时为零，**零副本、全部 partition 保护、省略 maxSurge 也不豁免**。ServingGroup 和 Role 使用相同规则，扩缩容须按新副本数重新校验。

| 期望副本数 | maxUnavailable 配置 | maxSurge 配置 | 解析后的 maxUnavailable | 结果 |
| --- | --- | --- | --- | --- |
| 3 | 25% | 0 | 0 | 拒绝，两个预算均为 0 |
| 3 | 25% | 1 | 0 | 允许，maxSurge 为正 |
| 4 | 25% | 0 | 1 | 允许，maxUnavailable 为正 |

非生效层预算允许保留但不参与滚动；仍检查基础格式、百分比范围和非负性，副本上界与双零组合只检查生效层。

### 1.3 Role 名称

路径：`spec.template.roles[].name`

**取值范围**

定义 1–4 个 Role；名称唯一，符合 DNS-1035、长度不超过 12：小写字母开头，小写字母或数字结尾，中间只允许小写字母、数字和连字符。

**是否可修改**

名称集合不可修改，包括零副本 Role；数组顺序可变，可在既有名称之间交换合法的可变内容，按名称关联对象。

**含义**

Role 名称是更新时关联配置的身份标识。

**参考片段**

```yaml
spec:
  template:
    roles:
      - name: prefill
      - name: decode
```

**行为举例**

| 操作 | 结果 |
| --- | --- |
| prefill → prefill-v2 | 拒绝，更换名称需新建 ModelServing |
| [prefill, decode] → [decode, prefill] | 允许纯重排，不改变语义 revision |
| 交换两个既有 Role 的可变模板 | 其他校验通过后按策略滚动 |

**注意事项**

拼接 ModelServing 名称和实例序号后的 Pod 名称仍须符合 DNS-1035 且不超过 63 字符。修改可变字段不能顺带增删名称。

### 1.4 副本数与 Role 模板

扩缩容仍须兼容不可变 gangPolicy、networkTopology 和 roleCoordination。例如固定 gang 最小实例数为 2，不能把该 Role 缩到 1。以下 YAML 均为局部片段，创建时需补齐其他必填字段。

#### 1.4.1 `spec.replicas`

路径：`spec.replicas`

**取值范围**

非负 int32，默认 1。

**是否可修改**

可修改；需通过更新后完整对象的校验。

**含义**

ServingGroup 数量。

**参考片段**

```yaml
spec:
  replicas: 3
```

**行为举例**

| 操作 | 效果 |
| --- | --- |
| 1 → 3 | 新增两个 ServingGroup；原保留组不因副本数变化创建模板 revision |

**注意事项**

重新校验 ServingGroup 预算、partition 和驱逐阈值；缩容可保留稀疏序号。

#### 1.4.2 `spec.template.roles[].replicas`

路径：`spec.template.roles[].replicas`

**取值范围**

非负 int32，默认 1。

**是否可修改**

可修改；需通过更新后完整对象的校验。

**含义**

每个 ServingGroup 内该 Role 的实例数量。

**参考片段**

```yaml
spec:
  template:
    roles:
      - name: prefill
        replicas: 3
```

**行为举例**

| 操作 | 效果 |
| --- | --- |
| 1 → 3 | 每组增加两个 prefill 实例；数量扩缩独立于模板 revision |

**注意事项**

仅限既有 Role；重新校验 Role 预算、gang 最小数、驱逐阈值、拓扑冲突和依赖容量。

#### 1.4.3 `spec.template.roles[].entryTemplate`

路径：`spec.template.roles[].entryTemplate`

**取值范围**

必填 Pod 模板；渲染后的 Pod 必须通过 Kubernetes 校验。

**是否可修改**

可修改；需通过更新后完整对象的校验。

**含义**

每个 Role 实例的 entry Pod 模板。

**参考片段**

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

**行为举例**

| 操作 | 效果 |
| --- | --- |
| 镜像 v1 → v2 / image v1 → v2 | 参与 revision 比较，按所选粒度滚动 |

**注意事项**

ServingGroup 模式会替换整组；Role 模式保留未改模板的 Role。示例镜像为占位值。

#### 1.4.4 `spec.template.roles[].workerReplicas`

路径：`spec.template.roles[].workerReplicas`

**取值范围**

非负 int32；无 worker 时显式填 0。

**是否可修改**

可修改；需通过更新后完整对象的校验。

**含义**

单个 entry Pod 附带的 worker Pod 数量。

**参考片段**

```yaml
spec:
  template:
    roles:
      - name: prefill
        workerReplicas: 2
```

**行为举例**

| 操作 | 效果 |
| --- | --- |
| 0 → 2 | 每实例布局变为 1 entry + 2 worker；布局变化触发滚动 |

**注意事项**

正数要求 workerTemplate；与 Role.replicas 的数量扩缩不同。

#### 1.4.5 `spec.template.roles[].workerTemplate`

路径：`spec.template.roles[].workerTemplate`

**取值范围**

workerReplicas>0 时必填；渲染后的 Pod 必须通过 Kubernetes 校验。

**是否可修改**

可修改；需通过更新后完整对象的校验。

**含义**

每个 Role 实例的 worker Pod 模板。

**参考片段**

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

**行为举例**

| 操作 | 效果 |
| --- | --- |
| 镜像 v1 → v2 / image v1 → v2 | 模板变化参与 revision 并按策略滚动 |

**注意事项**

workerReplicas>0 时不能移除此模板；示例镜像为占位值。

## 2. `rolloutStrategy.type`

路径：`spec.rolloutStrategy.type`

**取值范围**

`ServingGroupRollingUpdate` 或 `RoleRollingUpdate`；默认前者，省略整个 rolloutStrategy 也按此默认。

**是否可修改**

可修改，单独切换不创建模板 revision；重新校验新生效层预算。

**含义**

决定模板滚动以整个 ServingGroup 还是单个 Role 实例为单位。

**参考片段**

```yaml
spec:
  rolloutStrategy:
    type: ServingGroupRollingUpdate
```

**行为举例**

| 取值 | 仅 prefill 模板变化时的效果 | 生效预算 |
| --- | --- | --- |
| ServingGroupRollingUpdate | 替换整个 ServingGroup，decode 也获得新 Pod UID | rollingUpdateConfiguration |
| RoleRollingUpdate | 仅替换 prefill 实例，decode 保留原 Pod UID | roles[].maxUnavailable / maxSurge / partition |

**注意事项**

ServingGroup 模式允许但忽略 Role 预算，禁止 roleCoordination；Role 模式允许但忽略顶层预算（含空对象），禁止 ServingGroupRecreate。已有 roleCoordination 不可移除，因此不能切回 ServingGroup 模式。Role 预算按每个 ServingGroup 内的每种 Role 独立计算；例如三个组、Role maxUnavailable=1，可各有一个不可用实例。单组足够时，原参考建议搭配 spec.replicas=1。详细阶段见 §2.1–2.2。

### 2.1 `ServingGroupRollingUpdate`

路径：`spec.rolloutStrategy.type`

**取值范围**

`ServingGroupRollingUpdate` 或 `RoleRollingUpdate`；默认前者，省略整个 rolloutStrategy 也使用此默认值。

**是否可修改**

可修改；单独切换策略不创建模板 revision，切换后重新校验生效预算并保留所有不可变字段。

**含义**

以完整 ServingGroup 为滚动单元；任一 Role 模板变化会替换整组。

**参考片段**

```yaml
spec:
  rolloutStrategy:
    type: ServingGroupRollingUpdate
```

**行为举例**

仅修改 prefill 模板，初始 prefill/decode 均为 v1 Ready。

| 阶段 | prefill | decode | 配置效果 |
| --- | --- | --- | --- |
| 初始 | v1 Ready，原 Pod UID | v1 Ready，原 Pod UID | 尚未滚动 |
| 替换 | 删除并重建 | 删除并重建 | 整组暂不可用 |
| 完成 | v2 Ready，新 Pod UID | v1 Ready，新 Pod UID | decode 模板没变，但 Pod 随组替换 |

**注意事项**

仅顶层 rollingUpdateConfiguration 生效；Role 的 maxUnavailable、maxSurge、partition 允许配置但忽略。roleCoordination 必须省略；三种 recoveryPolicy 都允许。

### 2.2 `RoleRollingUpdate`

路径：`spec.rolloutStrategy.type`

**取值范围**

`ServingGroupRollingUpdate` 或 `RoleRollingUpdate`；默认前者，省略整个 rolloutStrategy 也使用此默认值。

**是否可修改**

可修改；单独切换策略不创建模板 revision，切换后重新校验生效预算并保留所有不可变字段。

**含义**

每个 ServingGroup 内只滚动模板发生变化的 Role，未变 Role 保留原 Pod UID。

**参考片段**

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
```

**行为举例**

仅修改 prefill 模板，初始 prefill/decode 均为 v1 Ready。

| 阶段 | prefill | decode | 配置效果 |
| --- | --- | --- | --- |
| 初始 | v1 Ready，原 Pod UID | v1 Ready，原 Pod UID | 尚未滚动 |
| 替换 | 删除并重建 | v1 Ready，原 Pod UID | 仅 prefill 暂不可用 |
| 完成 | v2 Ready，新 Pod UID | v1 Ready，原 Pod UID | decode 没有重建 |

**注意事项**

每个 ServingGroup 内每个 Role 独立计算预算，不跨组汇总；顶层 rollingUpdateConfiguration 允许保留（含空对象）但忽略。只允许 RoleRecreate/None。roleCoordination 可选，但创建后不能新增；已有协调对象也阻止切换到 ServingGroupRollingUpdate。

### 2.3 两层共用的预算与默认选择顺序

本节同时适用于 ServingGroup 和 Role 滚动，**不新增顺序开关**。desiredReplicas/maxUnavailable/maxSurge/partition 均取最新 spec 的生效层；ServingGroup 以完整 ServingGroup 为单位，Role 以每个 ServingGroup 内同一种 Role 的完整实例为单位。Role 的 entry 和所有必需 worker Ready 才计一个 Ready；各 Role、各 ServingGroup 不互借预算，未改模板的 Role 保留 UID。

| 滚动模式 | 默认旧实例选择 | 是否允许跳过健康高位旧实例 |
| --- | --- | --- |
| ServingGroup | 合法旧 NotReady 优先，同类按 ordinal 高到低，再处理旧 Ready | 允许，直接生效 |
| Role，未配置 roleCoordination | 与 ServingGroup 相同 | 允许，直接生效 |
| Role，配置 roleCoordination | 稳定旧实例按 ordinal 高到低；最高候选受阻就等待 | 不允许，无开关；参与名单另决定 maxSkew/依赖作用范围 |

**全量账本先计算，候选资格后判断。** protected、未参与协调和暂时不能删除的实例不能从其所属预算范围的可用性统计中消失。

```text
minAvailable         = max(0, desiredReplicas-maxUnavailable)
maxScaleDown         = max(0, activeReplicas-minAvailable-unavailableTargetReplicas-inFlightReservations)
maxHealthyScaleDown  = max(0, readyReplicas-minAvailable)
```

| 完整名称 | 定义 |
| --- | --- |
| desiredReplicas | 最新 spec 的生效层期望副本数：ServingGroup 层取 spec.replicas；Role 层取对应 Role.replicas |
| maxUnavailable / maxSurge / partition | 最新 spec 生效层字段经默认与百分比取整后的实际整数值 |
| minAvailable | 滚动期间的 Ready 容量底线；此处为派生值，不是驱逐配置字段 |
| maxScaleDown | 本轮允许清理旧实例的总额度，健康删除还须服从 maxHealthyScaleDown |
| activeReplicas | 实际活动实例数，包含实际已创建 surge；Deleting 完全消失前仍占物理容量，不预支未创建的 maxSurge |
| readyReplicas | 全部完整 Ready 容量，包含 protected、旧版和可用 surge；排除已承诺删除的容量 |
| unavailableTargetReplicas | 最新目标版本的不可用实例数，包含目标 surge；不能把所有旧 NotReady 都计入 unavailableTargetReplicas |
| inFlightReservations | 已占用、尚未由 activeReplicas 减少或 unavailableTargetReplicas 增加反映的在途额度；与 activeReplicas/unavailableTargetReplicas 的变化只记一次，稳定检查点为 0 |
| maxHealthyScaleDown | 健康删除上限；与 ServingGroup 附录的空洞集合是不同概念 |

例如旧对象删除已发但仍在 activeReplicas 且不在 unavailableTargetReplicas 中，先占 inFlightReservations；对象消失后 activeReplicas 减少，不再重复扣该 inFlightReservations；最新目标替代实例出现但未 Ready，由 unavailableTargetReplicas 占用。若替代槽受 partition 保护、补建的是历史模板，也不能因其不属于最新目标而提前释放尚未恢复的在途额度。目标改变时重分类，不能把已过期坏版本永久锁在旧的在途计数中。所有动作共享同一轮拟执行账本；创建还须遵守包含在途创建预约的 desiredReplicas+maxSurge 上限。

默认允许跳过时，令 eligibleOldNotReady/eligibleOldReady 为满足 partition、版本、身份和在途约束的旧 NotReady/旧 Ready 候选：

```text
oldNotReadyToReplace     = min(|eligibleOldNotReady|, maxScaleDown)
oldReadyToReplace = min(|eligibleOldReady|, maxScaleDown-oldNotReadyToReplace, maxHealthyScaleDown)
```

maxScaleDown 是总清理上界，**不是任意删除健康实例的许可**。每次健康删除仍重新核验 readyReplicas；外部故障已使 readyReplicas<minAvailable 时可在 maxScaleDown 内修复合法旧坏版本，但不得再因滚动降低 readyReplicas。已是当前目标的 NotReady 不反复模板滚动；明确 recoveryPolicy 的故障恢复另行适用。零更新量不形成永久滚动进度，desiredReplicas=0 的规模缩减按缩容处理。

有 coordination 时，使用相同 maxScaleDown/maxHealthyScaleDown，再叠加 §5 的剩余启动数、依赖和稳定实例有序前缀。废弃旧 NotReady **临时 surge** 的合法回收消耗同一 maxScaleDown，但不属于稳定实例跳过，也不增加或返还稳定启动额度；须确认未转正式、不承担必要依赖、无冲突在途动作。健康旧 surge 还受 maxHealthyScaleDown 限制，不能提前删除仍承担底线的容量。不能只凭 ordinal≥desiredReplicas 判定临时身份。

逐阶段例子及模式对照见 [ServingGroup/Role 共用速查表](servinggroup-compound-rollout.zh-CN.md#budget-lookup)。每个完整单元 Ready 后，按最新状态重新计算 maxScaleDown、maxHealthyScaleDown 和 inFlightReservations；存在合法候选且预算、partition、身份、物理容量及协调条件允许时继续推进，不等待原批次其他单元全部 Ready。Ready 回落或其他故障会重新消耗额度，不能把一次 Ready 事件当作永久或重复信用。协调 Role 仍遵守稳定候选顺序、dependencies 和 maxSkew。

## 3. ServingGroup 滚动配置

路径：`spec.rolloutStrategy.rollingUpdateConfiguration`；以下字段可变，不单独创建模板 revision。在 Role 模式下允许配置但忽略，仍满足 §1.2 基础类型约束。

### 3.1 `maxUnavailable`

路径：`spec.rolloutStrategy.rollingUpdateConfiguration.maxUnavailable`

**取值范围**

默认 `1`。生效整数范围为 `[0, spec.replicas]`；零副本允许默认或显式整数 `1`。百分比为 `"0%"`–`"100%"`，以 spec.replicas 为基数向下取整，不补 1。

**是否可修改**

可修改；只调整滚动预算，不创建模板 revision。

**含义**

相对于期望副本数允许的最大不可用 ServingGroup 数。总清理额度与健康删除上限按 §2.3 分别计算。

**参考片段**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxUnavailable: 2
```

**行为举例**

三个健康 ServingGroup，`maxUnavailable=2`、`maxSurge=0`，提交 v2。

| 阶段 | servinggroup-0 | servinggroup-1 | servinggroup-2 | 完整 Ready 数 | 效果 |
| --- | --- | --- | --- | --- | --- |
| 初始 | v1 Ready | v1 Ready | v1 Ready | 3 | 尚未滚动 |
| 第一批 | v1 Ready | 删除并重建 | 删除并重建 | 1 | 最多替换两个 |
| 第一批就绪 | v1 Ready | v2 Ready | v2 Ready | 3 | 恢复可用容量 |
| 下一批 | 删除并重建 | v2 Ready | v2 Ready | 2 | 替换剩余旧实例 |
| 完成 | v2 Ready | v2 Ready | v2 Ready | 3 | 全部采用 v2 |

**注意事项**

仅 ServingGroupRollingUpdate 生效；另一滚动模式允许配置但忽略，仍校验基础格式、百分比范围和非负性。实际 maxUnavailable=0 要求实际 maxSurge>0；零副本、全 partition 保护均不豁免双零拒绝。三副本的 25% 得到 0，必须另有正 maxSurge。partition 只筛候选，保护区故障仍计入全量 Ready 账本；合法旧 NotReady 按 §2.3 优先，配置 roleCoordination 时遵守有序前缀。表中是全健康的一条允许过程；单个完整单元 Ready 后即可按 §2.3 重算额度继续，不要求整批等待。

### 3.2 `maxSurge`

路径：`spec.rolloutStrategy.rollingUpdateConfiguration.maxSurge`

**取值范围**

默认 `0`。非负整数或整数百分比，百分比按 spec.replicas 向上取整；允许超过期望副本数或 100%，但 `spec.replicas + resolved maxSurge <= 2147483647`。

**是否可修改**

可修改；调整临时容量，不创建模板 revision。

**含义**

滚动期间允许在期望数量之上额外创建的 ServingGroup 数量，活动容量上限为期望副本数加解析后的 maxSurge。

**参考片段**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxSurge: 1
      maxUnavailable: 0
```

**行为举例**

期望三个 ServingGroup，`maxSurge=1`、`maxUnavailable=0`，提交 v2。

| 阶段 | 三个正式实例 | 临时额外实例 | 活动数 / Ready 数 | 效果 |
| --- | --- | --- | --- | --- |
| 初始 | 全部 v1 Ready | 无 | 3 / 3 | 等待滚动 |
| 创建额外容量 | 全部 v1 Ready | v2 NotReady | 4 / 3 | 不能删除健康旧实例 |
| 额外容量就绪 | 全部 v1 Ready | v2 Ready | 4 / 4 | 获得健康删除余量 |
| 逐个替换 | 依次将旧实例替换为 v2 | v2 Ready | 3–4 / 至少 3 | 每次删除仍核验 Ready 底线 |
| 完成 | 全部 v2 Ready | 已清理 | 3 / 3 | 恢复期望数量 |

**注意事项**

仅 ServingGroupRollingUpdate 生效；另一滚动模式允许配置但忽略，仍校验基础格式、百分比范围和非负性。实际 maxSurge=0 要求实际 maxUnavailable>0。容量上限不保证集群能够调度，新实例不 Ready 时示例等待。原始连续布局最终保留序号 0–2；既有健康目标高位可以稀疏保留（SG-S03），过期高位替换的版本按 §3.3 新槽位绝对序号判断。

### 3.3 `partition`

路径：`spec.rolloutStrategy.rollingUpdateConfiguration.partition`

**取值范围**

默认 `0`。生效整数范围 `[0, spec.replicas]`，百分比 `"0%"`–`"100%"`，向上取整；扩缩容后重新校验。

**是否可修改**

可修改；降低值释放待更新旧实例，升高值保护尚未更新的实例；不回滚已更新实例，也不创建模板 revision。

**含义**

保护绝对序号小于解析后 partition 的旧 ServingGroup；不是任意保留若干实例。

**参考片段**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      partition: 1
```

**行为举例**

三个 ServingGroup，`partition=1`、`maxUnavailable=1`、`maxSurge=0`，提交 v2。

| 阶段 | servinggroup-0 | servinggroup-1 | servinggroup-2 | 效果 |
| --- | --- | --- | --- | --- |
| 初始 | v1 Ready | v1 Ready | v1 Ready | 序号 0 受保护 |
| 替换序号 2 | v1 Ready（保护） | v1 Ready | 删除并重建 | 先处理最高旧序号 |
| 替换序号 1 | v1 Ready（保护） | 删除并重建 | v2 Ready | 序号 0 不参与模板滚动 |
| 灰度停点 | v1 Ready（保护） | v2 Ready | v2 Ready | 降低 partition 才释放序号 0 |

| 稀疏场景（SG-P11；Role 同规则） | 预期效果 |
| --- | --- |
| 期望副本数 2，现有序号 {0:v1, 3:v1}，partition=2、maxUnavailable=1、maxSurge=0，提交 v2 | 可替换过期序号 3，补低位空槽 1 |
| 新槽位 1 小于 partition=2 | 按历史模板创建 v1，最终 {0:v1, 1:v1} |
| 全部期望实例仍是 v1 | 是 partition 暂停；currentRevision=v1、updateRevision=v2、updatedReplicas=0 |

**注意事项**

仅 ServingGroupRollingUpdate 生效；另一滚动模式允许配置但忽略，仍校验基础格式、百分比范围和非负性。2026-10-06 稀疏澄清保持不变：单纯扩缩留下的空洞不触发滚动；真实模板滚动中，本来过期需替换的高位可补低位空洞，版本取决于新槽位绝对序号。受保护空槽使用历史模板，不能因原高位不受保护而强行用目标模板；所有动作仍受预算约束。

## 4. Role 滚动配置

路径：`spec.template.roles[]` 下直接的 maxUnavailable/maxSurge/partition；各 Role 在每个 ServingGroup 内独立计算。ServingGroup 模式下允许但忽略。改变预算本身不创建模板 revision。预算公式与 ServingGroup 相同，使用 §2.3 的 maxScaleDown/maxHealthyScaleDown/inFlightReservations 和默认顺序；协调只进一步限制候选，不扩大预算。

### 4.1 Role `maxUnavailable`

路径：`spec.template.roles[].maxUnavailable`

**取值范围**

默认 `1`。生效整数范围为 `[0, 该 Role 的 replicas]`；零副本允许默认或显式整数 `1`。百分比为 `"0%"`–`"100%"`，以 该 Role 的 replicas 为基数向下取整，不补 1。

**是否可修改**

可修改；只调整滚动预算，不创建模板 revision。

**含义**

相对于期望副本数允许的最大不可用 Role 实例 数。总清理额度与健康删除上限按 §2.3 分别计算。

**参考片段**

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

**行为举例**

三个健康 Role 实例，`maxUnavailable=2`、`maxSurge=0`，提交 v2。decode 模板未变，所有阶段保留原 Pod UID。

| 阶段 | prefill-0 | prefill-1 | prefill-2 | 完整 Ready 数 | 效果 |
| --- | --- | --- | --- | --- | --- |
| 初始 | v1 Ready | v1 Ready | v1 Ready | 3 | 尚未滚动 |
| 第一批 | v1 Ready | 删除并重建 | 删除并重建 | 1 | 最多替换两个 |
| 第一批就绪 | v1 Ready | v2 Ready | v2 Ready | 3 | 恢复可用容量 |
| 下一批 | 删除并重建 | v2 Ready | v2 Ready | 2 | 替换剩余旧实例 |
| 完成 | v2 Ready | v2 Ready | v2 Ready | 3 | 全部采用 v2 |

**注意事项**

仅 RoleRollingUpdate 生效；另一滚动模式允许配置但忽略，仍校验基础格式、百分比范围和非负性。实际 maxUnavailable=0 要求实际 maxSurge>0；零副本、全 partition 保护均不豁免双零拒绝。三副本的 25% 得到 0，必须另有正 maxSurge。partition 只筛候选，保护区故障仍计入全量 Ready 账本；合法旧 NotReady 按 §2.3 优先，配置 roleCoordination 时遵守有序前缀。表中是全健康的一条允许过程；单个完整单元 Ready 后即可按 §2.3 重算额度继续，不要求整批等待。

### 4.2 Role `maxSurge`

路径：`spec.template.roles[].maxSurge`

**取值范围**

默认 `0`。非负整数或整数百分比，百分比按 该 Role 的 replicas 向上取整；允许超过期望副本数或 100%，但 `该 Role 的 replicas + resolved maxSurge <= 2147483647`。

**是否可修改**

可修改；调整临时容量，不创建模板 revision。

**含义**

滚动期间允许在期望数量之上额外创建的 Role 实例 数量，活动容量上限为期望副本数加解析后的 maxSurge。

**参考片段**

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

**行为举例**

期望三个 Role 实例，`maxSurge=1`、`maxUnavailable=0`，提交 v2。decode 模板未变，所有阶段保留原 Pod UID。

| 阶段 | 三个正式实例 | 临时额外实例 | 活动数 / Ready 数 | 效果 |
| --- | --- | --- | --- | --- |
| 初始 | 全部 v1 Ready | 无 | 3 / 3 | 等待滚动 |
| 创建额外容量 | 全部 v1 Ready | v2 NotReady | 4 / 3 | 不能删除健康旧实例 |
| 额外容量就绪 | 全部 v1 Ready | v2 Ready | 4 / 4 | 获得健康删除余量 |
| 逐个替换 | 依次将旧实例替换为 v2 | v2 Ready | 3–4 / 至少 3 | 每次删除仍核验 Ready 底线 |
| 完成 | 全部 v2 Ready | 已清理 | 3 / 3 | 恢复期望数量 |

**注意事项**

仅 RoleRollingUpdate 生效；另一滚动模式允许配置但忽略，仍校验基础格式、百分比范围和非负性。实际 maxSurge=0 要求实际 maxUnavailable>0。容量上限不保证集群能够调度，新实例不 Ready 时示例等待。原始连续布局最终保留序号 0–2；既有健康目标高位可以稀疏保留（SG-S03），过期高位替换的版本按 §3.3 新槽位绝对序号判断。临时额外实例不能替代 §5.2 要求的稳定依赖容量。

### 4.3 Role `partition`

路径：`spec.template.roles[].partition`

**取值范围**

默认 `0`。生效整数范围 `[0, 该 Role 的 replicas]`，百分比 `"0%"`–`"100%"`，向上取整；扩缩容后重新校验。

**是否可修改**

可修改；降低值释放待更新旧实例，升高值保护尚未更新的实例；不回滚已更新实例，也不创建模板 revision。

**含义**

保护绝对序号小于解析后 partition 的旧 Role 实例；不是任意保留若干实例。

**参考片段**

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

**行为举例**

三个 Role 实例，`partition=1`、`maxUnavailable=1`、`maxSurge=0`，提交 v2。decode 模板未变，所有阶段保留原 Pod UID。

| 阶段 | prefill-0 | prefill-1 | prefill-2 | 效果 |
| --- | --- | --- | --- | --- |
| 初始 | v1 Ready | v1 Ready | v1 Ready | 序号 0 受保护 |
| 替换序号 2 | v1 Ready（保护） | v1 Ready | 删除并重建 | 先处理最高旧序号 |
| 替换序号 1 | v1 Ready（保护） | 删除并重建 | v2 Ready | 序号 0 不参与模板滚动 |
| 灰度停点 | v1 Ready（保护） | v2 Ready | v2 Ready | 降低 partition 才释放序号 0 |

| 稀疏场景（SG-P11；Role 同规则） | 预期效果 |
| --- | --- |
| 期望副本数 2，现有序号 {0:v1, 3:v1}，partition=2、maxUnavailable=1、maxSurge=0，提交 v2 | 可替换过期序号 3，补低位空槽 1 |
| 新槽位 1 小于 partition=2 | 按历史模板创建 v1，最终 {0:v1, 1:v1} |
| 全部期望实例仍是 v1 | 是 partition 暂停；currentRevision=v1、updateRevision=v2、updatedReplicas=0 |

**注意事项**

仅 RoleRollingUpdate 生效；另一滚动模式允许配置但忽略，仍校验基础格式、百分比范围和非负性。2026-10-06 稀疏澄清保持不变：单纯扩缩留下的空洞不触发滚动；真实模板滚动中，本来过期需替换的高位可补低位空洞，版本取决于新槽位绝对序号。受保护空槽使用历史模板，不能因原高位不受保护而强行用目标模板；所有动作仍受预算约束。协调依赖须保留旧版及目标版所需稳定槽位，提高 partition 不得移除必要容量。

## 5. `roleCoordination`

路径：`spec.rolloutStrategy.roleCoordination`

**取值范围**

可省略；仅 RoleRollingUpdate 允许。存在时 maxSkew 必填，roles 省略或 [] 选择全部 Role，解析结果至少两个不同既有 Role；dependencies 须满足 §5.2。

**是否可修改**

整体及存在性不可修改，包括 roles、maxSkew、全部依赖。完成滚动也不能新增、移除或编辑；语义集合/映射重排允许。Role 副本和预算仍可在其他约束满足时修改。

**含义**

先满足各 Role 独立预算，再叠加百分比进度和依赖约束；省略表示独立滚动，不是可切换的布尔开关。

**参考片段**

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
    roleCoordination:
      roles: [prefill, decode]
      maxSkew: "25%"
```

**行为举例**

| 配置 | 效果 |
| --- | --- |
| 省略 / omitted | 各 Role 独立，允许合法旧 NotReady 优先 |
| 已配置 / configured | 稳定旧实例按高到低前缀，最高候选受阻就等待；同时遵守进度和依赖 |

**注意事项**

进度衡量实际目标版本容量：N 是最新期望正式 Role 容量，包含 partition 保护实例和正式扩容成员，不含临时 surge；V 是其中完整目标版本 Ready 数。使用 p=V/N，不能使用 V/(N-partition)。protected 实例已经是目标版本且 Ready 时也计入 V；缺失、Terminating、未知历史和非 Ready 实例不计目标 Ready。稀疏稳定身份、扩缩和替换顺序不变。

```text
allowedStarted(role) = min(N, ceil((slowestReadyProgress + maxSkew) * N))
remainingStart(role) = max(0, allowedStarted(role) - formalTargetStarts)
```

正式目标启动数包含非 Ready 目标和替换预约，不重复计数。版本容量账本与受 partition 限制的稳定替换账本分开，再将剩余启动额度映射到合法旧候选，不能重复扣除 partition。模板变化且 N>0 的 Role 在到达 partition 停点或无可更新任务后仍参与基准；仅 N=0 因零容量而排除。只有一个 Role 改模板时不额外施加跨 Role 进度限制。N=4、partition=2、两个目标 Ready 时，局部两次替换虽已完成，真实版本比例仍为 50%。

**配置阶段 partition 比例校验（2.7）**

协调集合须兼容同一个名义保留比例 q，各 Role 独立按 ceil(q*N) 取整。显式百分比固定 q；整数 P=0 要求 q=0，P>0 要求 (P-1)/N < q <= P/N；用精确区间交集判断。N=0/P=0 不约束比例。8/4/10 副本均配置 10% 时可独立取整为 1/1/1；兼容的整数/百分比混合合法，4/4/4 配置 2/0/0 拒绝。不要求取整后 P/N 完全相等。

创建及修改参与 Role 的副本数、partition、模板时校验。已有不兼容对象仍可接受无关 metadata/status/预算更新，但在显式修正前不能再次触发上述滚动/配置变更。控制器处理已有不兼容滚动时报告 IncompatiblePartitions，停止选择更多稳定旧实例删除，已发替换允许完成。不改写、不传播 partition；原有依赖容量与预算校验仍生效，§4.3 的单 Role partition 保护不变。

存在 coordination 时，Role 模式的稳定旧实例不可跳过，无额外开关；从高到低取满足 maxScaleDown、maxHealthyScaleDown、remainingStart 和依赖保留规则的连续候选前缀。最高待更新旧实例被挡住即等待，不因低位 NotReady 绕行。已经是目标版及 protected 实例不是待更新旧候选。协调名单限定百分比/依赖参与者，不能用未入名单绕过该模式的默认顺序。

maxSkew 是**百分比进度差**，不是同 index 配对或原子版本切换承诺。合法废弃旧 surge 按 §2.3 单独回收，不占稳定序号前缀、不返还稳定启动额度。典型场景见共用速查表。

### 5.1 `maxSkew`

路径：`spec.rolloutStrategy.roleCoordination.maxSkew`

**取值范围**

必填，只接受 `"1%"`–`"100%"` 整数字符串；整数、0%、小数百分比和超过 100% 均拒绝。

**是否可修改**

不可修改，随 roleCoordination 整体及存在性固定。

**含义**

按各 Role 完整正式容量归一化，限制相对最慢 Role 实际目标版本 Ready 比例的领先程度。

**参考片段**

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
    roleCoordination:
      roles: [role-a, role-b]
      maxSkew: '25%'
```

**行为举例**

role-a 有 4 个正式实例，role-b 有 2 个，partition 均为 0，maxSkew=25%。

| role-a 目标 Ready | role-b 目标 Ready | 最慢 Ready 进度 | 累计可启动 role-a / role-b | 效果 |
| --- | --- | --- | --- | --- |
| 0/4 | 0/2 | 0% | 1 / 1 | 向上取整，初始各可启动一个 |
| 1/4 | 1/2 | 25% | 2 / 1 | role-b 暂不能继续领先 |
| 2/4 | 1/2 | 50% | 3 / 2 | role-a 追上后可继续 |
| 3/4 | 2/2 | 75% | 4 / 2 | role-a 可启动最后一个 |
| 4/4 | 2/2 | 100% | 4 / 2 | 全部完成 |

**注意事项**

至少两个 Role 参与，各自预算仍有效；已启动不等于 Ready，剩余启动数还要减去已启动数。向上取整使两个实例的 Role 首步一个就达到 50%；这不承诺同序号配对或原子版本切换。只有一个 Role 改模板时不额外限制跨 Role 进度差；N=0 不作分母，非零容量的 partition 停点仍参与。

**全量滚动末步量化（2.7）**

普通 ceil 门控可能与旧依赖保留形成循环。仅全量收尾时可许可调用方最后一个正式旧实例：所有模板变化且非零容量 Role 的 partition 均为 0，各自至多一个正式旧实例，全部正式实例可观测、完整 Ready 且历史已知，无在途替换，也无额外临时/超额旧实例。调用方自身已达到普通 skew 额度，没有直接旧调用方要求保留自己的旧容量，同时依赖仍保留的旧容量。每次 reconcile 重查事实，许可不持久化；目标不可用、缺员、未知历史均不能获得许可。

每个 Role 最多额外许可这一末步，原有预算、物理容量、有序候选、目标依赖 Ready 和旧依赖保留规则不变，不适用于 partition 灰度停点。真实 p 不变，controller 日志明确末步许可。8/4/10、10%、prefill→decode→fff 的目标 Ready 为 7/8、3/4、9/10 时，允许 prefill 最后一步；旧 prefill 消失后 decode 可完成，再完成 fff。Ready 检查点可能短暂为 100% 对 75%，此明确的单实例末步规则可超过 maxSkew，不能宣称严格保持 10%。

### 5.2 `dependencies`

路径：`spec.rolloutStrategy.roleCoordination.dependencies`

**取值范围**

可省略，默认无依赖。每项必有 role/dependsOn；owner 唯一，名称属于协调集合；禁止自依赖、重复依赖和环。

**是否可修改**

不可修改，包括 owner 和 dependsOn 列表；滚动完成后也不能增删或改向。

**含义**

目标版本先满足依赖 Ready 再启动依赖方，同时保留旧请求路径仍需使用的依赖容量。

**参考片段**

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

**行为举例**

frontend 依赖 backend，稳定副本足以同时保留必要的旧版和目标版容量。

| 阶段 | frontend | backend | 效果 |
| --- | --- | --- | --- |
| 初始 | v1 Ready | v1 Ready | 尚未滚动 |
| 依赖先就绪 | 保持 v1 | 首个 v2 Ready | 建立目标依赖容量 |
| 依赖方启动 | 首个 v2 启动 | 保留必要 v1，并有 v2 Ready | frontend 此时才可启动 v2 |
| 滚动中 | v1/v2 共存 | v1/v2 共存 | 旧 frontend 尚需的 backend v1 不能删除 |
| 完成 | 全部 v2 Ready | 全部 v2 Ready | frontend 完成后释放余下旧依赖 |

**注意事项**

依赖容量校验使用新旧期望副本数与 partition，不借用临时 maxSurge。只有一个稳定 backend 时不能既保留旧 backend 又提供目标 backend，应先扩稳定副本；滚动中缩容或提高 partition 也须保留必要槽位。

### 5.3 `roles`

路径：`spec.rolloutStrategy.roleCoordination.roles`

**取值范围**

可省略或填 []，两者均选择全部已定义 Role；解析结果至少包含两个不同既有名称。

**是否可修改**

不可修改 Role 集合；仅语义集合重排允许。

**含义**

指定百分比进度和依赖约束的参与 Role。

**参考片段**

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
    roleCoordination:
      roles: [prefill, decode]
      maxSkew: "25%"
```

**行为举例**

| 配置 | 效果 |
| --- | --- |
| roles: [] | 选择全部已定义 Role |
| roles: [prefill, decode] | 两者参与进度协调，其他 Role 不计入此进度集合 |

**注意事项**

整个 Role 模式只要配置了 roleCoordination，稳定旧实例就不能跳过；未入名单不构成绕过顺序的例外。maxSkew 必填，依赖名称必须属于此集合。

## 6. 故障恢复

### 6.1 `recoveryPolicy`

路径：`spec.recoveryPolicy`

**取值范围**

`ServingGroupRecreate`、`RoleRecreate`、`None`，默认 `RoleRecreate`。ServingGroupRollingUpdate 支持三者，RoleRollingUpdate 禁止 ServingGroupRecreate。

**是否可修改**

可修改，不创建模板 revision。

**含义**

决定故障恢复重建的范围，以及是否主动处理不健康重启或终态 Failed。

**参考片段**

```yaml
spec:
  recoveryPolicy: None
  template:
    restartGracePeriodSeconds: 0
```

**行为举例**

| 策略 | 不健康重启或 Failed | 真实 Pod 删除 |
| --- | --- | --- |
| ServingGroupRecreate | 按宽限规则重建所在完整 ServingGroup，含其他 Role、entry、worker | 立即按 ServingGroup 范围恢复，不等待 grace |
| RoleRecreate | 按宽限规则重建所在 Role **实例**的 entry/全部 worker，其他实例保留 UID | 按该实例范围恢复，不等待 grace |
| None | 不论合法 grace 为何，都不因重启错误或 Failed 主动删除 Pod；继续报告不可用 | 只补缺失的 Pod，其他 UID 全保留 |

**注意事项**

触发故障是：Pod 当前不 Ready 且普通或 init 容器 RestartCount>0，或者 Pod 为终态 Failed。Ready Pod 的历史重启计数不触发；没有重启/Failed 的纯 NotReady 不足以证明恢复触发条件。None 允许符合 restartPolicy 的容器由 kubelet 原地恢复，但不保证终态 Failed 自行恢复；真实缺失 Pod 仍须补建。不主动删除不等于把故障算作可用。

局部故障修复沿用所属滚动单元已经应用的模板和 worker 布局，不借修复提前应用最新目标。ServingGroupRollingUpdate 的滚动单元是完整 SG，RoleRollingUpdate 的滚动单元是一个 Role 实例；完整滚动单元重建才按动作当时的 partition 选择受保护基线或最新目标。已有 v2 单元因 partition 提高而被保护时，仅修其中一个 Pod/Role 仍保持该单元已应用的 v2；只有整个单元重建才重新按保护区基线选择，可能回到 v1。历史无法可靠确定时等待并报告，不猜测最新版本。

以已应用 v1、目标为 v2 的非保护单元为例：

| 滚动模式 | 恢复范围 | 首次补建版本 |
| --- | --- | --- |
| ServingGroupRollingUpdate | None：仅补一个缺失 Pod | 沿用该 SG 已应用的 v1，其他 UID 保留 |
| ServingGroupRollingUpdate | RoleRecreate：重建其中一个 Role 实例 | 该实例仍用所属 SG 的 v1；其他 Role UID 保留 |
| ServingGroupRollingUpdate | ServingGroupRecreate：整个 SG 重建 | 按当前 partition 选版本；本例非保护槽使用 v2 |
| RoleRollingUpdate | None：仅补一个缺失 Pod | 沿用该 Role 实例已应用的 v1 |
| RoleRollingUpdate | RoleRecreate：完整 Role 实例重建 | 按 Role 当前 partition 选版本；本例使用 v2 |

局部补回 v1 后，仍可在预算、顺序、partition 和协调允许时正常滚动为 v2。这次合法模板替换与旧恢复事件误删新 UID 必须区分：旧恢复动作不能自动获得新 UID 的删除许可，恢复范围也不得扩大。None 不关闭正常模板滚动。

controller 存活期间，一次 RoleRecreate/ServingGroupRecreate 仍按配置范围完成，并以原始 Pod UID/owner 校验隔离同名替代对象。controller 重启后不要求继续重启前的整批删除计划，也不从 Pod annotation 或持久化删除事务恢复该计划；应按现存 Pod、最新配置、已应用历史模板、partition 和当前预算重新收敛。若整组恢复在重启前只删除了 prefill，decode 仍健康，重启后允许按该 SG 正确历史模板补齐 prefill 并保留 decode UID。补齐后仍故障时，才按当前 recoveryPolicy、grace 和健康条件开始新的恢复判断。

Terminating Pod 和实际不可用容量仍计入当前容量与删除预算，不能因重启重复取得额度；完整 Ready、候选顺序、roleCoordination、UID/owner、旧事件隔离和历史模板规则不变。None 与 grace=-1 不能被绕过。具体边界见 [SG A.4 重启后删除收敛](servinggroup-compound-rollout.zh-CN.md#legacy-deletion-records)。

部分恢复重启的阶段表、SG/Role 用例及健康 UID 判定见 [重启收敛速查](servinggroup-compound-rollout.zh-CN.md#restart-convergence-cases)。保留健康幸存者仅约束旧恢复计划；解除 partition 后需要更新的实例仍可按正常滚动替换。

### 6.2 `restartGracePeriodSeconds`

路径：`spec.template.restartGracePeriodSeconds`

**取值范围**

可省略的 int64，默认 `0`，最小 `-1`；小于 -1 在创建和更新时均拒绝，None 也不豁免。

**是否可修改**

可修改，不参与模板 revision。

**含义**

从首次观察到符合恢复条件的故障开始，等待原地恢复的秒数；-1 表示永久容忍重启错误/Failed。

**参考片段**

```yaml
spec:
  recoveryPolicy: RoleRecreate
  template:
    restartGracePeriodSeconds: -1
```

**行为举例**

| 值 | ServingGroupRecreate / RoleRecreate | None |
| --- | --- | --- |
| 省略或 0 | 当前不健康重启/Failed 立即按策略范围处理 | 不主动删除 |
| 正数 | 从首次观察故障计时；期限内恢复 Ready 则保留 UID，否则按策略范围重建 | 不主动删除 |
| -1 | 永久容忍重启错误/Failed，不创建宽限删除任务 | 不主动删除 |

| worker 故障场景 | None，宽限 0 | RoleRecreate，宽限 -1 |
| --- | --- | --- |
| 重启后一直不 Ready | 保留所有 Pod UID，availableReplicas 降低 | 保留所有 Pod UID，availableReplicas 降低 |
| 该 worker 真实被删除 | 只补缺失 worker | 重建该 Role 实例的 entry/全部 worker |
| 其他实例及 ServingGroup | 保持原 UID | 保持原 UID |

**注意事项**

-1 是哨兵值，不是立即超时或超长睡眠。真实 PodDeleted 不等待宽限；有限宽限内恢复 Ready 则保留 UID。延迟任务执行前读取最新 policy/grace 并验证 ModelServing/Pod UID；切到 None/-1 必须取消旧任务的删除效果，有限宽限增减按最新配置判断。旧对象事件不能删除同名新 UID，controller 重启后仍须保持这些规则。

同一 ModelServing/Pod UID 的同一持续故障，首次观察时间必须跨 controller 重启和活动 controller 切换保留；离线时间计入宽限，不重新领取完整等待期。到期时间为可信首次观察时间加最新有限 grace。恢复 Ready 结束该次故障；后续新故障、新 Pod UID 不能继承旧删除许可。旧对象没有可信起点时的迁移方式须另行明确，不能拿 Pod 创建时间或猜测时间替代已观察故障起点。

例如 grace=60 秒：t=0 首次观察故障，t=40 controller 退出，t=50 恢复时只剩约 10 秒；若 t=70 才恢复，不再另等 60 秒，但仍须按最新 UID、健康、policy 和 grace 复核后才可动作。

## 7. `evictionStrategy`

整体、存在性、保护层和阈值可修改，不创建模板 revision。两种滚动模式都支持，驱逐粒度与滚动粒度独立。仅约束 Kubernetes `pods/eviction` 请求，不设置滚动 maxUnavailable，也不主动创建额外实例；直接删除 Pod 或节点丢失不经过此预算。完整 ServingGroup Ready 要求全部期望 Role 实例及其 entry/worker 存在且 Ready；完整 Role 实例要求 entry 及全部 worker Ready。

### 7.1 `protectionLevel`

路径：`spec.rolloutStrategy.evictionStrategy.protectionLevel`

**取值范围**

ServingGroup/Role，默认 ServingGroup。

**是否可修改**

可修改；与完整 evictionStrategy 一同校验。

**含义**

选择驱逐保护的逻辑单位。

**参考片段**

```yaml
spec:
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: ServingGroup
      minAvailable: 2
```

**行为举例**

| 取值 | 效果 |
| --- | --- |
| ServingGroup | 按完整 ServingGroup 计算可用性 |
| Role | 在目标 Pod 所在 ServingGroup 内按对应 Role 实例计数 |

**注意事项**

切换层级时同一请求必须提供新层级合法阈值。额外非生效层阈值允许保留，仅使用选中层阈值。

### 7.2 `minAvailable`

路径：`spec.rolloutStrategy.evictionStrategy.minAvailable`

**取值范围**

非负整数或 0%–100% 整数百分比，向上取整；结果不超过 spec.replicas。ServingGroup 层必填，无默认阈值。

**是否可修改**

可修改；与完整 evictionStrategy 一同校验。

**含义**

驱逐后至少保留的完整 Ready ServingGroup 数。

**参考片段**

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: ServingGroup
      minAvailable: "66%"
```

**行为举例**

| 阶段 | 请求 / 事件 | 结果 |
| --- | --- | --- |
| 1 | 驱逐组 2 的 Pod | 允许；66% 向上取整为 2，剩余两个 Ready 组 |
| 2 | 组 2 未恢复时驱逐组 1 | 拒绝；会低于两个 Ready 组 |
| 3 | 组 2 恢复 Ready | 预算恢复 |

**注意事项**

扩缩容后重新校验；已接受的驱逐须计入扰动，不能重复使用额度。

### 7.3 `roleMinAvailable`

路径：`spec.rolloutStrategy.evictionStrategy.roleMinAvailable`

**取值范围**

RoleName→IntOrString；Role 层必填且非空，键为既有 Role。各值非负整数或 0%–100%，向上取整，结果不超过该 Role.replicas。

**是否可修改**

可修改；与完整 evictionStrategy 一同校验。

**含义**

每个 ServingGroup 内，各列出 Role 至少保留的完整 Ready 实例数。

**参考片段**

```yaml
spec:
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: Role
      roleMinAvailable:
        prefill: 1
```

**行为举例**

| 同组原有三个 Ready prefill | 结果 |
| --- | --- |
| 驱逐 prefill-2 | 允许，剩余 2 Ready |
| 再驱逐 prefill-1 | 允许，剩余 1 Ready |
| 再驱逐 prefill-0 | 拒绝，该实例保持 Ready |

**注意事项**

未列出的 decode 不受此预算保护；不能借用其他 ServingGroup 的 Ready 容量。副本变化重新校验。

## 8. `gangPolicy`

路径：`spec.template.gangPolicy`

**取值范围**

可省略对象。minRoleReplicas 为 RoleName→int32；键必须为既有 Role，`0 <= minRoleReplicas[role] <= role.replicas`。未列入的 Role 按全部期望实例计入最小集合。

**是否可修改**

整体及存在性不可修改，包括 minRoleReplicas 是否存在、全部键值；不能将省略改为 `{}`，也不能在创建后填充空对象。

**含义**

设定 gang 调度的最小 Role 实例集合。一个实例包含 1 个 entry Pod 和 workerReplicas 个 worker Pod；ServingGroup PodGroup 的 minMember 约为各 Role 有效最小实例数 × (1 + workerReplicas) 之和。

**参考片段**

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

**行为举例**

prefill 期望 3、最小 1，decode 期望 2 且未指定最小值，两者均无 worker。

| 阶段 | 可用资源 | 调度结果 | 效果 |
| --- | --- | --- | --- |
| 资源不足 | 不足以容纳 1 prefill + 2 decode | PodGroup Pending | 最小集合不能零散启动 |
| 满足最小集合 | 可容纳上述 3 个 Pod | 最小集合可 gang 调度 | 未配置的 decode 按全部 2 个计入 |
| 后续资源到位 | 可容纳其余 2 个 prefill | 继续调度到期望副本数 | 最小数不是最终上限 |
| 某 Role 最小数为 0 | 不要求其贡献最小集合 | 仍保留该 Role 的期望实例 | 0 不等于删除 Role |

**注意事项**

两种滚动模式均支持；实际调度依赖兼容的 Volcano、相关自定义资源定义和插件。扩缩容重新校验最小数，不能通过修改不可变最小数来缩容；未列入 map 的 Role 的派生最小数随其期望副本变化。

## 9. 拓扑边界：`groupPolicy` 与 `rolePolicy`

根路径：`spec.template.networkTopology`。整个对象和存在性不可变，含边界、亲和/反亲和条目、selector、Role 引用和 weight；此不可变性历史基线已经实施。

| 字段 | 范围 | 语义 |
| --- | --- | --- |
| mode | hard/soft，默认 hard | 强制边界或允许回退的偏好 |
| highestTierAllowed | 非负整数 | 可跨越的最高数值 HyperNode tier |
| highestTierName | 不超过 253 字符 | 引用 HyperNode.spec.tierName |

两 selector 互斥；hard 恰好一个，soft 至多一个且可以省略。字段合法不保证资源和 HyperNode 存在；hard 不满足时 Pending，soft 可以回退。

### 9.1 `groupPolicy`

路径：`spec.template.networkTopology.groupPolicy`

**取值范围**

可省略，省略不配置此边界。mode 为 hard/soft，默认 hard；highestTierAllowed 为非负整数，highestTierName 长度不超过 253。两 selector 互斥；hard 恰好一个，soft 至多一个且可省略。

**是否可修改**

随 networkTopology 整体及存在性不可修改，包含 mode 和两个 selector。

**含义**

限制一个 ServingGroup 的所有 Pod 所在拓扑边界。

**参考片段**

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      groupPolicy:
        mode: hard
        highestTierName: rack
```

**行为举例**

| 对象 | 示例放置 | 效果 |
| --- | --- | --- |
| ServingGroup 0 的 prefill/decode | 都在 rack-a | 同组全部 Pod 在同一 rack |
| ServingGroup 1 的 prefill/decode | 也可都在 rack-a | 不同组独立，可共享 rack |

**注意事项**

语法合法不保证 HyperNode 或资源存在；hard 无法满足时 Pending，soft 可回退。groupPolicy 不隐含 ServingGroup 反亲和。

### 9.2 `rolePolicy`

路径：`spec.template.networkTopology.rolePolicy`

**取值范围**

可省略，省略不配置此边界。mode 为 hard/soft，默认 hard；highestTierAllowed 为非负整数，highestTierName 长度不超过 253。两 selector 互斥；hard 恰好一个，soft 至多一个且可省略。

**是否可修改**

随 networkTopology 整体及存在性不可修改，包含 mode 和两个 selector。

**含义**

复制到 Volcano SubGroupPolicy，限制每个 Role 实例的 entry/worker 所在边界。

**参考片段**

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      rolePolicy:
        mode: hard
        highestTierAllowed: 0
```

**行为举例**

| 同一 ServingGroup 内的实例 | entry/worker 放置 | 效果 |
| --- | --- | --- |
| prefill-0 | tier 0 域一 | 实例内全部 Pod 在同一域 |
| prefill-1 | tier 0 域二 | 不同实例可用不同域 |
| decode-0 | tier 0 域三 | 整个 ServingGroup 无需共用一域 |

**注意事项**

语法合法不保证 HyperNode 或资源存在；hard 无法满足时 Pending，soft 可回退。调度器必须支持 Role SubJob/SubGroupPolicy；其余边界校验与 groupPolicy 相同。

## 10. ServingGroup 与 Role 亲和 / 反亲和

原参考标注为计划能力 [#645](https://github.com/volcano-sh/kthena/issues/645)，以下为其 API 契约，不代表本 runner 验证了实际拓扑放置。

三对象都位于 `spec.template.networkTopology`，整体及存在性、条目、Role 列表、weight 和 selector 均不可变；要求 schedulerName=volcano。只要存在任一对象，三者合计至少有一个 term；仅空对象拒绝。

required 是硬约束，禁止 weight；preferred 是软约束，必须有 1–100 整数 weight。**每个 term 恰好一个** selector：非负 int32 topologyTier，或不超过 253 字符的 topologyTierName。Role 名称须既有且不重复。

### 10.1 `servingGroupAntiAffinity`

路径：`spec.template.networkTopology.servingGroupAntiAffinity`

**取值范围**

可省略。required 是硬约束，禁止 weight；preferred 是软约束，必须有 1–100 整数 weight。每 term 恰好一个 selector：非负 int32 topologyTier，或不超过 253 字符的 topologyTierName。不使用 roles。

**是否可修改**

随 networkTopology 整体及存在性不可修改，包括条目、Role 列表、weight 和 selector。

**含义**

将同一 ModelServing 的不同 ServingGroup 分开到不同拓扑域；自动选择对等组，不使用 Role 列表。

**参考片段**

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

**行为举例**

| ServingGroup | required/rack 示例放置 | 效果 |
| --- | --- | --- |
| 0 | rack-a | 占用一个 rack |
| 1 | rack-b | 不能与 0 同 rack |
| 2 | rack-c | 不能与 0/1 同 rack |

**注意事项**

原参考标为计划能力；此处是 API 契约，不代表 runner 已验证实际拓扑放置。要求 schedulerName=volcano；三个关系对象只要存在任一对象，合计至少一个 term，仅空对象拒绝。required/rack 只有两个适合 rack 时，第三组 Pending；preferred 可回退共用 rack。

### 10.2 `roleAffinity`

路径：`spec.template.networkTopology.roleAffinity`

**取值范围**

可省略。required 是硬约束，禁止 weight；preferred 是软约束，必须有 1–100 整数 weight。每 term 恰好一个 selector：非负 int32 topologyTier，或不超过 253 字符的 topologyTierName。每 term 至少两个不同既有 Role。

**是否可修改**

随 networkTopology 整体及存在性不可修改，包括条目、Role 列表、weight 和 selector。

**含义**

每个 ServingGroup 内，所选 Role 的全部 SubJob 共用拓扑域，不按序号配对实例。

**参考片段**

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

**行为举例**

| ServingGroup | prefill | decode | 效果 |
| --- | --- | --- | --- |
| 0 | rack-a | rack-a | 同组所选 Role 共域 |
| 1 | rack-a | rack-a | 各组独立，也可共享 rack |

**注意事项**

原参考标为计划能力；此处是 API 契约，不代表 runner 已验证实际拓扑放置。要求 schedulerName=volcano；三个关系对象只要存在任一对象，合计至少一个 term，仅空对象拒绝。required 亲和/反亲和数值 tier 可比较：反亲和 tier 比亲和更宽，且至少两个重叠 Role 均正副本，或单 Role 反亲和命中有多个实例的重叠 Role 时，冲突拒绝。副本变化重新校验；名称 tier 或数值/名称混用不推断层级顺序。

### 10.3 `roleAntiAffinity`

路径：`spec.template.networkTopology.roleAntiAffinity`

**取值范围**

可省略。required 是硬约束，禁止 weight；preferred 是软约束，必须有 1–100 整数 weight。每 term 恰好一个 selector：非负 int32 topologyTier，或不超过 253 字符的 topologyTierName。每 term 至少一个既有 Role，名称不能重复。

**是否可修改**

随 networkTopology 整体及存在性不可修改，包括条目、Role 列表、weight 和 selector。

**含义**

单 Role term 分散该 Role 的多个实例；多 Role term 分隔不同 Role，不额外分散同一 Role 的实例。

**参考片段**

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

**行为举例**

| 同一 ServingGroup 内的实例 | 示例放置 | 效果 |
| --- | --- | --- |
| prefill-0 | node-a | 占用一个 node 域 |
| prefill-1 | node-b | 与 prefill-0 分离 |
| prefill-2 | node-c | 与其他 prefill 分离 |
| decode | 任意 | 未列入 term，不受此规则限制 |

**注意事项**

原参考标为计划能力；此处是 API 契约，不代表 runner 已验证实际拓扑放置。要求 schedulerName=volcano；三个关系对象只要存在任一对象，合计至少一个 term，仅空对象拒绝。required 亲和/反亲和数值 tier 可比较：反亲和 tier 比亲和更宽，且至少两个重叠 Role 均正副本，或单 Role 反亲和命中有多个实例的重叠 Role 时，冲突拒绝。副本变化重新校验；名称 tier 或数值/名称混用不推断层级顺序。若既需 Role 间隔离又需某 Role 内实例分散，应为该 Role 另列单 Role term。

## 11. 与所检查历史 production 基线的区别

固定历史对照为 `production/release-1.0@a011cd5a`，**不是当前 production 状态**。043 最终校验和 041 恢复语义组成 runner 2.1 契约；PASS 须对单独记录的候选 commit/镜像实际执行。

| 项目 | 当前契约 / runner 判定 | 历史差异 |
| --- | --- | --- |
| Role 名称集合 | 不可变，零副本也适用；重排和合法内容修改允许 | 无通用名称集合不可变检查 |
| gangPolicy | 整体和存在性、map presence/键值不可变 | 部分父对象/map 检查遗漏 optional 转换 |
| roleCoordination | 整体和存在性不可变，语义集合/映射重排允许 | 主要校验一致性/依赖容量 |
| 非生效层预算 | ServingGroup 忽略 Role maxUnavailable/maxSurge/partition；Role 忽略顶层 maxUnavailable/maxSurge/partition；基础格式/非负仍验 | ServingGroup 曾拒绝 Role maxSurge/partition，Role 曾拒绝顶层预算 |
| 双零预算 | 默认与取整后生效 maxUnavailable/maxSurge=0/0 拒绝，含零副本和全 partition | 只在 replicas>partition 时检查 |
| 小百分比 maxUnavailable | 始终向下取整、不补 1；replicas=3、maxUnavailable=25%、maxSurge=0 拒绝 | ServingGroup 曾最小补 1 |
| 整数上界 | 生效 maxUnavailable/partition <= replicas，零副本 maxUnavailable=1 例外 | ServingGroup maxUnavailable、整数 partition 缺少通用上界 |
| Surge 百分比 | 可超过 100%，replicas+maxSurge 不得超 int32 | 百分比上限 100% |
| 边界 selector | hard 恰好一个，soft 至多一个 | 跨字段检查不完整 |
| restartGracePeriodSeconds | -1 永久容忍，<-1 拒绝；延迟任务核对最新配置/UID | 无新的负值哨兵语义 |
| None | 不因重启/Failed 主动删，只补真实缺失 Pod | Failed 清理仍可能删 Pod |

可执行 API 用例见 `cases/api-contract/cases.json`；恢复执行器见 `scripts/run-recovery-contract.py` 和 `cases/recovery-contract/suite.json`。历史用例冲突及替代覆盖单列记录；旧设计轨迹不证明现在能通过 admission。

## 12. Plugin 配置与效果

路径：`spec.plugins`

**取值范围**

可选有序插件列表，省略或空列表表示不启用插件链。

| 字段（spec.plugins[] 下） | 取值范围与默认值 | 作用 |
| --- | --- | --- |
| `name` | 已注册的插件名，必填；同名不重复 | 选择插件实现 |
| `type` | 仅 BuiltIn，默认 BuiltIn | 使用 controller 内置实现 |
| `config` | 可选 JSON，由对应插件解析 | 传递插件私有配置 |
| `scope.roles` | 可选 Role 名称白名单；省略或空列表表示全部 Role | 限制生效 Role |
| `scope.target` | All / Entry / Worker，默认 All | 限制 Pod 钩子的目标类型 |

**是否可修改**

创建后实质内容不可修改：新增（包括第一个插件）、删除（包括全部插件）、name/type/config/scope 变化和有意义的顺序变化都拒绝。两种滚动模式、零副本、滚动完成、Pod 尚未 Ready、上层控制器写入均无例外。语义等价调整见下表。

**含义**

插件按列表顺序执行，用于创建 Pod 时注入配置，或维护 Headless Service、成员发现 ConfigMap、ranktable 等附属资源。scope 限定作用范围；不承诺每种生命周期插件都支持仅 Entry/Worker。

**参考片段**

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

**行为举例**

| 操作 / 场景 | 可观察效果 |
| --- | --- |
| 创建上述配置 | prefill 的 entry Pod 得到注解和环境变量，worker 与其他 Role 不受此插件影响 |
| 插件不变，仅扩容或缺 Pod 恢复 | 新 Pod 继续使用创建时的插件声明；其他合法字段仍需通过原有校验 |
| 修改插件，同时修改 replicas 或镜像 | 整次请求拒绝，不会只写入副本数或镜像，也不会因插件变更启动滚动 |
| 缩容到 0，再删除插件 | 删除仍拒绝 |
| 需要不同插件配置 | 创建新的 ModelServing，验证就绪后按业务方式迁移流量 |

| 原表达 | 允许的等价表达 |
| --- | --- |
| plugins 省略 / omitted | [] |
| type 省略 / omitted | BuiltIn |
| scope 省略 / omitted | 空 scope；无 Role 限制且 target=All |
| scope.roles: [prefill, decode] | [decode, prefill] 或 [prefill, decode, prefill] |
| config 省略 / omitted | null |
| config JSON 对象 | 键顺序或空白变化；数组顺序仍须保留 |

**注意事项**

config 的 `{}` 与省略不承诺等价，数值表达差异也不自动放行。spec.plugins 不可变规则本身不冻结所有外部输入；另按 2.4 确认的模板保护规则，被 ModelServing 使用的 ranktable 模板 ConfigMap 内容修改应被 webhook 拒绝，不作为合法热更新输入。该限制针对模板源，不阻止插件维护生成的成员通信配置 ConfigMap；不据此扩大到所有 ConfigMap、删除或纯 metadata 更新。插件读取的其他 annotations、controller 实现变化及升级前混用仍需分别考虑。需启用 ModelServing validating webhook；API 对象创建成功不等于插件配置一定可执行，注册名、私有配置和生命周期限制还需插件构建及运行时检查。headless-service 仅允许 scope.target 省略或 All，其 scope.roles 可限制 Role；它按存在 workerTemplate 的 Role 实例维护 Service。更换插件需新建对象；迁移是否中断取决于资源和切流。

本节插件字段不可变部分复述 [046 最终实现与验证](../../../issues/features/046-modelserving-plugin-immutable-DONE/PROPOSAL_COMMIT.md) 和 [051 已批准移植记录](../../../issues/features/051-production-baseline-realignment-DONE/production-selected-20261006/README.md)，不采用 046 前期探索方案。本文补齐说明不代表 runner 新增了插件执行覆盖。

### 12.1 常见内置插件的效果

以下结合内置实现效果摘要及已批准的 2.4 行为要求；实际可用插件以部署的 controller 所注册实现为准，要求与实现覆盖分开记录。

| 插件 | 效果 | 使用边界 |
| --- | --- | --- |
| `demo-pod-tweaks` | 为选中 Pod 设置 runtimeClassName、合并注解、注入环境变量 | 验证或复用 Pod 定制；config 使用对应字段 |
| `headless-service` | 为带 workerTemplate 的 Role 实例维护 Headless Service，注入 ENTRY_ADDRESS 和 Pod hostname/subdomain | 提供实例内 entry/worker 的 DNS 发现；Service 意外丢失可重建，附属资源不决定 Role 是否存在 |
| `pod-discovery` | 将 ServingGroup 成员 Pod IP 汇总到 ConfigMap，挂载 /etc/pod-discovery/ips.json | 作为组网成员表；成员已有 IP 且 Running 时，即使 NotReady 也继续公布，避免进程等待成员 IP 才能 Ready 的循环依赖 |
| `ranktable` / `pod-ranktable` | 按模板及成员信息生成、维护并挂载通信配置 ConfigMap | 依赖各自模板、解析器及运行环境；被 ModelServing 使用的 ranktable 模板内容受独立 webhook 保护；生成的通信配置仍需随成员变化维护 |
| `lws-standard-labels` | 为 LeaderWorkerSet 管理的 ModelServing Pod 补齐标准组/worker 身份标签 | 用于上层资源兼容；不会把普通 ModelServing 自动变为 LeaderWorkerSet |

2.4 的成员发布和模板保护由用户确认；它们不意味着当前候选已完整实现或通过 runner。只读核对的 production@4e5c9016 中，ModelServing validator 检查模板存在性，尚未定位 ConfigMap 更新拦截处理器。用户随后确认保护 webhook 在生产分支，并要求暂时忽略此问题；按该确认保留模板保护预期，暂停实现定位，不将其列为当前待办，也不宣称已验证部署。证据与答复见 052。
