# ModelServing API 参考

版本 2.2 · 2026-10-07 · [English](modelserving-api-reference.en.md)

本文规定字段路径、默认值、可变性、合法范围、百分比取整和跨字段约束。Mutable 表示更新后的完整对象通过校验时可原地修改；Immutable 表示值和可选对象的存在性均不可变，即使滚动完成或副本数为零也不例外。第 11 节对比的是历史 `production/release-1.0@a011cd5a`，不代表当前 production 的实现状态。片段 YAML 需补齐其他必填字段；示例过程服从 §2.3 的默认选择规则，异步事件不要求逐行全序。

## 1. 核心概念

`spec.replicas` 是 ServingGroup（SG）数量；每个 SG 包含全部 Role。`spec.template.roles[].replicas` 是每个 SG 内该 Role 的实例数量；`workerReplicas` 是每个实例中单个 entry Pod 附带的 worker Pod 数。

### 1.1 字段路径与默认值

| 功能 | 路径 | 省略时 |
| --- | --- | --- |
| 滚动粒度 | `spec.rolloutStrategy.type` | `ServingGroupRollingUpdate` |
| SG 预算 | `spec.rolloutStrategy.rollingUpdateConfiguration.*` | U/S/P = 1/0/0 |
| Role 预算 | `spec.template.roles[].maxUnavailable/maxSurge/partition` | Role 模式每个 Role 的 U/S/P = 1/0/0 |
| Role 协调 | `spec.rolloutStrategy.roleCoordination` | 各 Role 独立滚动 |
| 故障恢复 | `spec.recoveryPolicy` | `RoleRecreate` |
| 恢复宽限 | `spec.template.restartGracePeriodSeconds` | 0 秒 |
| 驱逐保护 | `spec.rolloutStrategy.evictionStrategy` | 不启用驱逐预算 |
| Gang 最小副本 | `spec.template.gangPolicy.minRoleReplicas` | 各 Role 全部期望实例计入最小集合 |
| 拓扑边界 | `spec.template.networkTopology.groupPolicy/rolePolicy` | 不配置对应边界 |
| 拓扑关系 | 同一对象下 `servingGroupAntiAffinity/roleAffinity/roleAntiAffinity` | 不配置对应关系；原参考将其标为计划能力 [#645](https://github.com/volcano-sh/kthena/issues/645) |

### 1.2 整数、百分比与取整

U=`maxUnavailable`、S=`maxSurge`、P=`partition` 和驱逐阈值使用 IntOrString。整数必须非负；百分比必须为整数字符串，例如 `"25%"`。数字字符串 `"1"`、小数百分比 `"2.5%"`、负数和溢出均拒绝。

| 字段 | 百分比基数 | 取整 | 基数 3、25% |
| --- | --- | --- | --- |
| SG U | `spec.replicas` | floor，不补 1 | 0 |
| Role U | 对应 Role 的 replicas | floor，不补 1 | 0 |
| S、P、驱逐阈值 | 对应层期望副本数 | ceil | 1 |

U/P/驱逐百分比范围为 0%–100%。S 可以超过 100%，但生效层的 `replicas + resolved S <= 2147483647`。整数 U/P 不得超过生效层 replicas；零副本允许默认或显式整数 U=1。

先应用 U=1、S=0 默认值，再取整，生效层 U/S 实际结果不得同时为零，**零副本、全部 partition 保护、省略 S 也不豁免**。SG 和 Role 均满足：3/25%/0 拒绝；3/25%/1 允许；4/25%/0 允许。扩缩容须按新副本数重新校验。

非生效层预算允许保留但不参与滚动；仍检查基础格式、百分比范围和非负性，副本上界与双零组合只检查生效层。

### 1.3 Role 名称

路径：`spec.template.roles[].name`。Role 名称**集合不可变**，包括零副本 Role；增删或替换名称需要新建 ModelServing。顺序可变，可在既有名称之间交换合法的可变内容；按名称关联对象。

Role 数量 1–4，名称唯一、符合 DNS-1035、长度不超过 12：小写字母开头，小写字母或数字结尾，中间只允许小写字母、数字和连字符。拼接 ModelServing 名称、实例序号后的 Pod 名称仍须符合 DNS-1035 且不超过 63 字符。纯重排不改变语义 revision；模板内容交换按策略滚动。

### 1.4 副本数与 Role 模板

| 路径 | 可变性与校验 | 效果 |
| --- | --- | --- |
| `spec.replicas` | 可变，非负 int32，默认 1 | 仅扩缩 SG，不创建模板 revision；重验 SG 预算、partition、驱逐阈值 |
| `spec.template.roles[].replicas` | 既有 Role 可变，非负 int32，默认 1 | Role 扩缩独立于模板 revision；重验 Role 预算、gang、驱逐、拓扑冲突和依赖容量 |
| `entryTemplate` | 可变、必填；渲染后的 Pod 须通过 Kubernetes 校验 | 模板变化参与 revision 比较并触发策略滚动 |
| `workerReplicas` | 可变、非负 int32；无 worker 显式填 0 | 正数要求 workerTemplate；布局变化触发滚动 |
| `workerTemplate` | 可变；workerReplicas>0 时必填且不能移除 | 渲染后的 Pod 须通过 Kubernetes 校验；模板变化参与 revision |

扩缩须兼容不可变 gangPolicy、networkTopology 和 roleCoordination。例如 gang 最小实例数为 2 时不能将该 Role 缩到 1。修改可变字段不允许顺带改变名称集合或不可变策略。

## 2. `rolloutStrategy.type`

路径：`spec.rolloutStrategy.type`；可变，单独改变策略不创建模板 revision；省略整个 rolloutStrategy 也默认 SG 模式。切换时重新校验新生效层预算，非生效层预算可保留。

| 值 | 滚动单元 | 生效预算 |
| --- | --- | --- |
| `ServingGroupRollingUpdate` | 整个 SG | 顶层 rollingUpdateConfiguration |
| `RoleRollingUpdate` | 模板变化的 Role 实例 | 每个 Role 直接配置的 U/S/P |

Role 模式作用于**每个 SG**，每个 SG 内每个 Role 独立预算，不跨 SG 汇总。例如三个 SG、某 Role U=1，可各自有一个不可用实例。原参考建议单组足够时搭配 `spec.replicas=1`。

SG 模式允许但忽略 Role U/S/P，禁止 roleCoordination。Role 模式允许但忽略顶层预算（含 `{}`），禁止 `ServingGroupRecreate`。若创建时有 roleCoordination，不能切换到 SG 模式，因为协调对象不可变且仅允许 Role 模式。

### 2.1 `ServingGroupRollingUpdate`

策略可变，仍须满足所有不可变字段约束。仅顶层预算生效；Role 预算忽略，roleCoordination 必须省略，三种 recoveryPolicy 均合法。修改 prefill 模板会重建完整 SG：prefill 换到 v2，decode 即使模板仍为 v1 也获得新 Pod UID。

参考配置（局部片段）：

```yaml
spec:
  rolloutStrategy:
    type: ServingGroupRollingUpdate
```

### 2.2 `RoleRollingUpdate`

仅 Role 预算生效；顶层对象可保留但忽略。Recovery 只允许 RoleRecreate/None。协调可选，但不能在创建后新增。只修改 prefill 模板时，prefill 实例滚到 v2，decode 保留原 UID。

参考配置（局部片段）：

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
```

### 2.3 两层共用的预算与默认选择顺序

本节同时适用于 SG 和 Role 滚动，**不新增顺序开关**。N/U/S/P 均取最新 spec 的生效层；SG 以完整 SG 为单位，Role 以每个 SG 内同一种 Role 的完整实例为单位。Role 的 entry 和所有必需 worker Ready 才计一个 Ready；各 Role、各 SG 不互借预算，未改模板的 Role 保留 UID。

| 滚动模式 | 默认旧实例选择 | 是否允许跳过健康高位旧实例 |
| --- | --- | --- |
| SG | 合法旧 NotReady 优先，同类按 ordinal 高到低，再处理旧 Ready | 允许，直接生效 |
| Role，未配置 roleCoordination | 与 SG 相同 | 允许，直接生效 |
| Role，配置 roleCoordination | 稳定旧实例按 ordinal 高到低；最高候选受阻就等待 | 不允许，无开关；参与名单另决定 maxSkew/依赖作用范围 |

**全量账本先计算，候选资格后判断。** protected、未参与协调和暂时不能删除的实例不能从其所属预算范围的可用性统计中消失。

```text
M = minAvailable         = max(0, N-U)
Q = maxScaleDown         = max(0, C-M-V-I)
B = maxHealthyScaleDown  = max(0, R-M)
```

| 符号 | 定义 |
| --- | --- |
| C | 实际活动实例数，包含实际已创建 surge；Deleting 完全消失前仍占物理容量，不预支未创建的 S |
| R | 全部完整 Ready 容量，包含 protected、旧版和可用 surge；排除已承诺删除的容量 |
| V | 最新目标版本的不可用实例数，包含目标 surge；不能把所有旧 NotReady 都计入 V |
| I | 已占用、尚未由 C 减少或 V 增加反映的在途额度；与 C/V 的变化只记一次，稳定检查点为 0 |
| B | 健康删除上限；不是 SG 附录中表示空洞集合的 H |

例如旧对象删除已发但仍在 C 且不在 V 中，先占 I；对象消失后 C 减少，不再重复扣该 I；最新目标替代实例出现但未 Ready，由 V 占用。若替代槽受 P 保护、补建的是历史模板，也不能因其不属于最新目标而提前释放尚未恢复的在途额度。目标改变时重分类，不能把已过期坏版本永久锁在旧的在途计数中。所有动作共享同一轮拟执行账本；创建还须遵守包含在途创建预约的 N+S 上限。

默认允许跳过时，令 Ebad/Ehealthy 为满足 partition、版本、身份和在途约束的旧 NotReady/旧 Ready 候选：

```text
dBad     = min(|Ebad|, Q)
dHealthy = min(|Ehealthy|, Q-dBad, B)
```

Q 是总清理上界，**不是任意删除健康实例的许可**。每次健康删除仍重新核验 R；外部故障已使 R<M 时可在 Q 内修复合法旧坏版本，但不得再因滚动降低 R。已是当前目标的 NotReady 不反复模板滚动；明确 recoveryPolicy 的故障恢复另行适用。零更新量不形成永久滚动进度，N=0 的规模缩减按缩容处理。

有 coordination 时，使用相同 Q/B，再叠加 §5 的剩余启动数、依赖和稳定实例有序前缀。废弃旧 NotReady **临时 surge** 的合法回收消耗同一 Q，但不属于稳定实例跳过，也不增加或返还稳定启动额度；须确认未转正式、不承担必要依赖、无冲突在途动作。健康旧 surge 还受 B 限制，不能提前删除仍承担底线的容量。不能只凭 ordinal≥N 判定临时身份。

逐阶段例子及模式对照见 [SG/Role 共用速查表](servinggroup-compound-rollout.zh-CN.md#budget-lookup)。Q/B 给出预算上界，不单独决定批次调度时点；既有“整批等待/逐 Ready 推进”的执行口径差异见该表的覆盖边界，本次不据此新增批次语义。

## 3. ServingGroup 滚动配置

路径：`spec.rolloutStrategy.rollingUpdateConfiguration`；以下字段可变，不单独创建模板 revision。在 Role 模式下允许配置但忽略，仍满足 §1.2 基础类型约束。

### 3.1 `maxUnavailable`

默认 1，百分比按 SG 期望数向下取整、不补 1。生效整数范围 `[0, replicas]`，零副本默认/显式 U=1 例外；百分比 0%–100%。实际 U=0 要求实际 S>0，包括零副本和全 partition 情形。

U 表示相对于期望 SG 数允许的最大不可用量。统一计算见 §2.3：`Q=max(0,C-max(0,N-U)-V-I)`，健康删除另受 `B=max(0,R-max(0,N-U))` 限制。partition 只约束候选，不能将保护区故障从全量 Ready 账本中排除。

三个健康 SG、U=2/S=0 时，可以先替换最高的两组，等待它们 Ready，再替换余下一组；存在合法旧 NotReady 时按 §2.3 优先处理它们。

参考配置（局部片段）：

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxUnavailable: 2
```

### 3.2 `maxSurge`

默认 0，向上取整。非负整数或整数百分比，不以 replicas 或 100% 为上限，但 `replicas + resolved S` 必须在 int32 范围内。生效 S=0 要求 U>0。

活动 SG 数上限 N+S 是容量边界，不保证集群能调度额外组。N=3/U=0/S=1 时，先创建第四组；新组未 Ready 不得删除健康旧组；新组 Ready 才释放删除容量，持续替换后退回三个目标组。原始连续布局应清理临时 surge 回到 0..2；既有健康目标高位可保持稀疏（SG-S03），过期高位可以替换到低位空洞，版本按 §3.3 判定。

参考配置（局部片段）：

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxSurge: 1
      maxUnavailable: 0
```

### 3.3 `partition`

默认 0，向上取整。生效整数范围 `[0, replicas]`，百分比 0%–100%；扩缩后重验。降低 P 释放旧实例，升高 P 保护尚未更新的实例，不回滚已经更新的实例，不创建模板 revision。

P 是**绝对序号边界**，保护 ordinal<P 的旧组，不是任意保留 P 个组。N=3/P=1 时，sg-0 保留 v1，其余两组可滚到 v2。

**2026-10-06 稀疏澄清保持不变：**单纯扩缩留下的空洞不触发滚动。在真实模板滚动中，本来过期需替换的高位组可以补低位空洞；版本取决于**新槽位的绝对 ordinal**。受保护的空槽用历史模板重建。

例如 N=2、`{sg-0:v1, sg-3:v1}`、P=2/U=1/S=0，提交 v2 后可删除过期 sg-3，在 sg-1 创建历史 v1；最终两个组都为 v1。这是 partition 暂停，currentRevision 保持 v1、updateRevision 为 v2、updatedReplicas 为 0；降低 P 才释放 v2 更新。所有创建删除仍受预算约束，不能因为旧高位不受保护就让受保护的新槽强行使用 v2。详见 SG-P11。

参考配置（局部片段）：

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      partition: 1
```

## 4. Role 滚动配置

路径：`spec.template.roles[]` 下直接的 U/S/P；各 Role 在每个 SG 内独立计算。SG 模式下允许但忽略。改变预算本身不创建模板 revision。预算公式与 SG 相同，使用 §2.3 的 Q/B/I 和默认顺序；协调只进一步限制候选，不扩大预算。

### 4.1 Role `maxUnavailable`

默认 1；按该 Role replicas 向下取整、不补 1。生效整数不得超过 Role replicas，零副本默认/显式整数 U=1 例外；百分比 0%–100%。三副本 U=25% 得到 0，必须有正 S；全 partition 或零副本不豁免实际双零。三个 prefill、U=2 时可同时替换两个实例，Ready 后替换最后一个；未改模板的 decode 保留 UID。

参考配置（局部片段）：

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

### 4.2 Role `maxSurge`

默认 0，ceil；非负整数或整百分比可超过 100%，但 `role.replicas + S <= 2147483647`。实际 S=0 要求 U>0。N=3/U=0/S=1 可先建第四个 prefill，Ready 后再逐个替换旧实例，最终保留三个 v2；decode 不扩容、不重建。临时 surge 不能替代 §5.2 所要求的稳定依赖容量。

参考配置（局部片段）：

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

### 4.3 Role `partition`

默认 0，ceil；生效整数 `[0, role.replicas]`，百分比 0%–100%。保护每个 SG 中该 Role 的 `[0,P)`，不回滚已更新实例。副本变化重新校验，协调中的依赖须保留旧版和目标版所需槽位；提高 P 不得移除必要容量。

三 prefill、P=1 时保留 prefill-0:v1，其余滚 v2，decode 保留原 UID。§3.3 的绝对序号/历史模板规则同样适用：过期高位替换到受保护低位后，可以所有期望 Role 实例仍是历史版本。

参考配置（局部片段）：

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

## 5. `roleCoordination`

路径：`spec.rolloutStrategy.roleCoordination`。**整体及存在性不可变**，含 roles、maxSkew、全部依赖；完成滚动也不能新增、移除或修改。语义集合/映射的重排允许。仅 Role 模式允许；省略表示不协调，不是可切换的布尔开关。

roles 省略或 `[]` 解析为全部已定义 Role，结果必须至少两个不同的既有名称。maxSkew 必填。先满足各 Role 预算，再按协调进一步约束启动；参与 Role 不要求副本数相同。

```text
allowedStarted(role) = min(role.totalToUpdate,
                           ceil((slowestReadyProgress + maxSkew) × role.totalToUpdate))
remainingStart(role) = max(0, allowedStarted(role) - role.alreadyStarted)
```

进度按各 Role 可更新稳定实例数归一化，以参与本轮的最慢 Role 的**目标版本 Ready**进度为基准；已启动不等于 Ready，零更新量不作分母。只有一个 Role 实际更新时不额外施加跨 Role 进度差限制。

存在 coordination 时，Role 模式的稳定旧实例不可跳过，无额外开关；从高到低取满足 Q、B、remainingStart 和依赖保留规则的连续候选前缀。最高待更新旧实例被挡住即等待，不因低位 NotReady 绕行。已经是目标版及 protected 实例不是待更新旧候选。协调名单限定百分比/依赖参与者，不能用未入名单绕过该模式的默认顺序。

maxSkew 是**百分比进度差**，不是同 index 配对或原子版本切换承诺。合法废弃旧 surge 按 §2.3 单独回收，不占稳定序号前缀、不返还稳定启动额度。典型场景见共用速查表。

### 5.1 `maxSkew`

不可变，只接受 `"1%"`–`"100%"` 整数字符串。整数、0%、小数、超过 100% 均拒绝。至少两个 Role，各自预算仍有效。

例：A 有 4 个待更新实例，B 有 2 个，maxSkew=25%。最慢 Ready 进度为 0/25/50/75/100% 时，累计允许启动 A/B 分别为 1/1、2/1、3/2、4/2、4/2。ceil 使少副本 Role 的单步比例可能超过百分比，例如最初启动一个 B 已是 50%；它仍受到自己的滚动预算约束。

参考配置（局部片段）：

```yaml
spec:
  rolloutStrategy:
    type: RoleRollingUpdate
    roleCoordination:
      roles: [role-a, role-b]
      maxSkew: '25%'
```

### 5.2 `dependencies`

作为协调对象一部分不可变，含 owner 及 dependsOn 列表。每项必须有 role/dependsOn；owner 唯一，名称属于协调集合，禁止自依赖、重复依赖、环。

有变化的依赖必须在 partition 外能提供目标版容量，同时保留旧请求路径所需容量。校验使用新旧**期望副本**及 P，不借用临时 surge。只有一个稳定 backend 实例时，无法既保留旧 backend 又提供目标版给依赖它的旧 frontend；应先扩稳定副本。滚动中缩容或升高 P 仍需保留必要槽位。

典型顺序：backend 先有 Ready v2 → frontend 首个 v2 才可启动 → frontend v1 尚存时保留必要 backend v1 → frontend 完成后才能释放余下旧依赖。

参考配置（局部片段）：

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

## 6. 故障恢复

### 6.1 `recoveryPolicy`

路径：`spec.recoveryPolicy`；可变，默认 RoleRecreate，不创建模板 revision。合法值 ServingGroupRecreate/RoleRecreate/None；SG 模式都支持，Role 模式禁止 ServingGroupRecreate。

| 策略 | 不健康重启或 Failed | 真实 Pod 删除 |
| --- | --- | --- |
| ServingGroupRecreate | 按宽限规则重建所在完整 SG，含其他 Role、entry、worker | 立即按 SG 范围恢复，不等待 grace |
| RoleRecreate | 按宽限规则重建所在 Role **实例**的 entry/全部 worker，其他实例保留 UID | 按该实例范围恢复，不等待 grace |
| None | 不论合法 grace 为何，都不因重启错误或 Failed 主动删除 Pod；继续报告不可用 | 只补缺失的 Pod，其他 UID 全保留 |

触发故障是：Pod 当前不 Ready 且普通或 init 容器 RestartCount>0，或者 Pod 为终态 Failed。已经 Ready 的历史重启计数不触发；没有重启/Failed 的纯 NotReady 不足以证明此恢复触发条件。

None 允许符合 restartPolicy 的容器由 kubelet 原地恢复；不能承诺终态 Failed 自行恢复，它可能需要用户或外部删除后才补建。禁止主动删除不等于把故障算作可用，也不抑制真正缺失 Pod 的期望状态恢复。

参考配置（局部片段）：

```yaml
spec:
  recoveryPolicy: None
  template:
    restartGracePeriodSeconds: 0
```

### 6.2 `restartGracePeriodSeconds`

路径：`spec.template.restartGracePeriodSeconds`；可变、可省略 int64，默认 0，最小 -1；小于 -1 在创建/更新中拒绝，None 也不豁免。不参与模板 revision。

| 值 | SGRecreate / RoleRecreate | None |
| --- | --- | --- |
| 省略或 0 | 当前不健康重启/Failed 立即按策略范围处理 | 不主动删除 |
| 正数 | 从首次观察故障计时；期限内恢复 Ready 则保留 UID，否则按策略范围重建 | 不主动删除 |
| -1 | 永久容忍重启错误/Failed，不创建宽限删除任务 | 不主动删除 |

-1 是哨兵值，不是立即超时，也不是一个超长睡眠任务。真实 PodDeleted 在 -1 下仍按策略范围恢复。

延迟任务执行前读取最新 policy/grace，验证 ModelServing/Pod UID。切到 None/-1 必须取消旧任务的删除效果；有限宽限增减按最新配置判断。旧对象事件不能删除同名新 UID，controller 重启也须保持这些语义。

例：worker 重启后一直不 Ready，None/0 与 RoleRecreate/-1 都保留所有 UID，availableReplicas 降低。若真实删除该 worker，前者只补 worker，后者重建该实例的 entry/worker；其他实例和 SG 不动。有限宽限内恢复 Ready 也保留原 UID。

参考配置（局部片段）：

```yaml
spec:
  recoveryPolicy: RoleRecreate
  template:
    restartGracePeriodSeconds: -1
```

## 7. `evictionStrategy`

路径：`spec.rolloutStrategy.evictionStrategy`；整体、存在性、保护层和阈值可变，不创建模板 revision。两种 rollout 都支持，驱逐粒度与滚动粒度独立。改变层级须在同一请求提供新层级合法阈值。

仅约束 Kubernetes `pods/eviction` 请求，不设置滚动 U，不主动创建 surge；直接删除 Pod 或节点丢失不经过此 admission 预算。

| 字段 | 默认/范围 | 规则 |
| --- | --- | --- |
| protectionLevel | 默认 ServingGroup；ServingGroup/Role | 决定逻辑预算单位 |
| minAvailable | 非负整数或 0%–100%，ceil，结果 <= SG replicas | SG 层必填 |
| roleMinAvailable | RoleName→IntOrString；各值非负整数或 0%–100%，ceil，结果 <= 该 Role replicas | Role 层必填非空；键必须是既有 Role |

只使用选中层的阈值，额外非生效层阈值允许保留。扩缩后重新校验。未列入 Role map 的 Role 没有此预算保护。

SG Ready 要求全部期望 Role 实例及其 entry/worker 存在且 Ready；Role 实例 Ready 要求 entry 和全部 worker Ready。Role 阈值在目标 Pod 所在 SG 独立评估，不能借用其他 SG 容量。

三个 SG、minAvailable=66% 得到 2：第一次驱逐一组可接受，在它恢复前再驱逐另一组应拒绝。三个 prefill、roleMinAvailable.prefill=1：可依次驱逐两个实例，第三次拒绝；被拒绝的实例保持 Ready，未列出的 decode 不受此预算约束。

参考配置（局部片段）：

```yaml
spec:
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: ServingGroup
      minAvailable: '66%'
```

```yaml
spec:
  rolloutStrategy:
    evictionStrategy:
      protectionLevel: Role
      roleMinAvailable:
        prefill: 1
```

## 8. `gangPolicy`

路径：`spec.template.gangPolicy` 及 minRoleReplicas。整体及存在性不可变，包括 map 是否存在、全部键值；不能将省略变成 `{}`，不能创建后新增、移除或填充空对象。

map 键为既有 Role，值为 int32 且 `0 <= minRoleReplicas[role] <= role.replicas`；缩容也重验，不能通过修改不可变最小数来缩容。未列入 map 的 Role 使用全部期望实例，因此其派生最小数随扩缩变化。

```text
Role 实例 Pod 数 = 1 entry + workerReplicas
SG PodGroup minMember ≈ sum(effectiveMinRoleReplicas[role] × (1 + workerReplicas[role]))
```

0 合法，只移除该 Role 对 gang 最小集合的贡献，不移除其期望实例。两种 rollout 都支持；实际调度需兼容的 Volcano、CRD 和插件。例如 prefill 三实例、最小 1，decode 两实例但未配置最小值、无 worker：最小集合为三 Pod；资源不足时 Pending，满足后最小集合可 gang 调度，其余 prefill 等资源，不将最小值当最终上限。

参考配置（局部片段）：

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

## 9. 拓扑边界：`groupPolicy` 与 `rolePolicy`

根路径：`spec.template.networkTopology`。整个对象和存在性不可变，含边界、亲和/反亲和条目、selector、Role 引用和 weight；此不可变性历史基线已经实施。

| 字段 | 范围 | 语义 |
| --- | --- | --- |
| mode | hard/soft，默认 hard | 强制边界或允许回退的偏好 |
| highestTierAllowed | 非负整数 | 可跨越的最高数值 HyperNode tier |
| highestTierName | 不超过 253 字符 | 引用 HyperNode.spec.tierName |

两 selector 互斥；hard 恰好一个，soft 至多一个且可以省略。字段合法不保证资源和 HyperNode 存在；hard 不满足时 Pending，soft 可以回退。

### 9.1 `groupPolicy`

`spec.template.networkTopology.groupPolicy` 限制一个 SG 的所有 Pod 所在边界，字段随 networkTopology 不可变。例如 hard/rack 要求同 SG 的 prefill/decode 在同一 rack；不同 SG 可以共享该 rack，不隐含 SG 反亲和。

参考配置（局部片段）：

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      groupPolicy:
        mode: hard
        highestTierName: rack
```

### 9.2 `rolePolicy`

`spec.template.networkTopology.rolePolicy` 复制到 Volcano SubGroupPolicy，要求 scheduler 支持 Role SubJob。与 groupPolicy 同样校验。hard/tier0 时，一个 Role 实例的 entry/worker 必须在一个 tier0 域，但不同实例可用不同域，整个 SG 不必在同域。

参考配置（局部片段）：

```yaml
spec:
  schedulerName: volcano
  template:
    networkTopology:
      rolePolicy:
        mode: hard
        highestTierAllowed: 0
```

## 10. SG 与 Role 亲和 / 反亲和

原参考标注为计划能力 [#645](https://github.com/volcano-sh/kthena/issues/645)，以下为其 API 契约，不代表本 runner 验证了实际拓扑放置。

三对象都位于 `spec.template.networkTopology`，整体及存在性、条目、Role 列表、weight 和 selector 均不可变；要求 schedulerName=volcano。只要存在任一对象，三者合计至少有一个 term；仅空对象拒绝。

required 是硬约束，禁止 weight；preferred 是软约束，必须有 1–100 整数 weight。**每个 term 恰好一个** selector：非负 int32 topologyTier，或不超过 253 字符的 topologyTierName。Role 名称须既有且不重复。

### 10.1 `servingGroupAntiAffinity`

同一 ModelServing 的不同 SG 分开拓扑域，自动选择对等 SG，不使用 Role 列表。三 SG required/rack 需要三个 rack，只有两个时第三组 Pending；preferred 可回退共用 rack。

参考配置（局部片段）：

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

### 10.2 `roleAffinity`

每 term 至少两个不同既有 Role，约束每个 SG 内这些 Role 的全部 SubJob 共用域，不是按序号配对实例。不同 SG 独立，同样可共用一个 rack。

required affinity 与 required anti-affinity 的数值 tier 可比较：反亲和 tier 比亲和更宽且活动 Role 冲突时拒绝。冲突包括至少两个重叠 Role 均正副本，或单 Role 反亲和命中重叠 Role 且它有多个实例。副本变化重验。名称 tier 或数值/名称混用不推断层级顺序。

参考配置（局部片段）：

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

### 10.3 `roleAntiAffinity`

每 term 至少一个既有 Role，遵守上述 selector/weight 及 §10.2 冲突规则。单 Role term 分散该 Role 的多个实例，例如 prefill-0/1/2 在不同 node 域；未列入的 decode 不受影响。多 Role term 分隔不同 Role，不额外分散同一 Role 的实例；需要后者时为它单列 term。

参考配置（局部片段）：

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

## 11. 与所检查历史 production 基线的区别

固定历史对照为 `production/release-1.0@a011cd5a`，**不是当前 production 状态**。043 最终校验和 041 恢复语义组成 runner 2.1 契约；PASS 须对单独记录的候选 commit/镜像实际执行。

| 项目 | 当前契约 / runner 判定 | 历史差异 |
| --- | --- | --- |
| Role 名称集合 | 不可变，零副本也适用；重排和合法内容修改允许 | 无通用名称集合不可变检查 |
| gangPolicy | 整体和存在性、map presence/键值不可变 | 部分父对象/map 检查遗漏 optional 转换 |
| roleCoordination | 整体和存在性不可变，语义集合/映射重排允许 | 主要校验一致性/依赖容量 |
| 非生效层预算 | SG 忽略 Role U/S/P；Role 忽略顶层 U/S/P；基础格式/非负仍验 | SG 曾拒绝 Role S/P，Role 曾拒绝顶层预算 |
| 双零预算 | 默认与取整后生效 U/S=0/0 拒绝，含零副本和全 partition | 只在 replicas>P 时检查 |
| 小百分比 U | 始终 floor、不补 1，3/25%/0 拒绝 | SG 曾最小补 1 |
| 整数上界 | 生效 U/P <= replicas，零副本 U=1 例外 | SG U、整数 P 缺少通用上界 |
| Surge 百分比 | 可超过 100%，replicas+S 不得超 int32 | 百分比上限 100% |
| 边界 selector | hard 恰好一个，soft 至多一个 | 跨字段检查不完整 |
| grace | -1 永久容忍，<-1 拒绝；延迟任务核对最新配置/UID | 无新的负值哨兵语义 |
| None | 不因重启/Failed 主动删，只补真实缺失 Pod | Failed 清理仍可能删 Pod |

可执行 API 用例见 `cases/api-contract/cases.json`；恢复执行器见 `scripts/run-recovery-contract.py` 和 `cases/recovery-contract/suite.json`。历史用例冲突及替代覆盖单列记录；旧设计轨迹不证明现在能通过 admission。
