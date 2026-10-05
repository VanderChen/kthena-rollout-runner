# ModelServing 滚动升级：默认行为、边界值、版本比较与不可变字段

基于本地 `production/release-1.0@a011cd5aa4dc8442f3aba1aa12cb419380dc23f0`，整理日期：2026-09-11。API 为 `workload.serving.volcano.sh/v1alpha1`。本文核对了该版本的 CRD、webhook 和 controller 实际调用路径；没有重新运行 Kind。其他分支的实现可能不同。

2026-09-15 补充第 4 部分：不可变字段及更新限制；第 5 部分：需要拒绝的配置组合。源码基线不变。

先记住：**默认整组更新；副本数控制规模，模板控制版本，滚动参数控制推进速度和范围。**

一个 ServingGroup 是一套完整的服务实例，里面可以有 prefill、decode 等 Role。每种 Role 可以有多个实例；每个 Role 实例包含 **1 个 entry Pod + `workerReplicas` 个 worker Pod**。

**1. 默认值、默认行为**

| 配置 | 默认值 / 省略时的行为 | 怎么理解 |
|---|---|---|
| `spec.replicas` | `1` | 默认创建 1 个 ServingGroup |
| `spec.template.roles[].replicas` | `1` | 每组内，该 Role 默认有 1 个实例 |
| `rolloutStrategy.type` | `ServingGroupRollingUpdate` | 一个 Role 模板变化，按整个 ServingGroup 替换 |
| 生效层级的 `maxUnavailable` | `1` | 更新期间允许 1 个更新单位不可用 |
| 生效层级的 `maxSurge` | `0` | 默认不创建超过期望数的临时实例 |
| 生效层级的 `partition` | `0` | 所有编号都可参与模板更新 |
| `roleCoordination` | 不启用 | Role 之间默认没有升级依赖或进度差约束 |
| `recoveryPolicy` | `RoleRecreate` | 故障恢复默认重建受影响的 Role 实例 |
| `spec.template.restartGracePeriodSeconds` | `0` | 故障恢复宽限期为 0；它不是滚动批次间隔 |
| `revisionHistoryLimit` | `10` | 最多保留 10 份不再被引用的历史；仍被引用的历史另行保留 |
| `schedulerName` | `volcano` | 默认交给 Volcano 调度 |
| `roles[].workerReplicas` | **必填，没有 CRD 默认值** | 没有 worker 时显式写 `0` |

完全省略 `rolloutStrategy`、写 `rolloutStrategy: {}`，或者只写 `type: ServingGroupRollingUpdate`，正常经过当前 API 默认化后，有效滚动行为都是 `maxUnavailable=1、maxSurge=0、partition=0`。GET 返回的 YAML 不一定会把所有默认父对象和字段补出来。

例如有 3 个健康 ServingGroup，默认更新流程是：删除一个旧组 → 创建一个新组 → 等新组 Ready → 继续下一组。**如果只有 1 个 ServingGroup，默认更新会出现整组不可用的窗口。**

| 更新方式 | 参数写在哪里 | 更新单位及影响范围 |
|---|---|---|
| `ServingGroupRollingUpdate` | `spec.rolloutStrategy.rollingUpdateConfiguration` | 整个 ServingGroup；其中一个 Role 的模板变化，也会替换整组 |
| `RoleRollingUpdate` | `spec.template.roles[]` 内直接写 `maxUnavailable/maxSurge/partition` | 每个 ServingGroup 中的对应 Role 实例；未变更的 Role 模板不因此重建 |

Role 模式下，预算在“每个 ServingGroup × 每种 Role”内独立计算，并且各组可以同时更新。`maxUnavailable: 1` 不是整个 ModelServing 只允许一个 Role 实例不可用；一个 Role 实例有 worker 时，替换它会涉及多个 Pod。

配置归属要注意：

- Role 模式禁止顶层 `rollingUpdateConfiguration`，即使写成 `{}` 也不行。
- SG 模式下，Role 的 `maxUnavailable` 会被忽略；Role 的 `maxSurge`、`partition` 被拒绝，显式写 `0` 也一样。
- 只在 Role 上写参数不会自动切换到 Role 模式，必须显式设置 `type: RoleRollingUpdate`。
- `evictionStrategy` 保护外部 eviction 请求，不参与上述滚动预算计算。

常见组合如下。`U/S/P` 分别表示解析后的 `maxUnavailable/maxSurge/partition`。

| 配置 | 运行含义 |
|---|---|
| `U=1, S=0, P=0` | 默认先删后建 |
| `U=0, S=1, P=0` | 先增加新实例，Ready 后再删旧实例；没有可用的额外资源时会等待 |
| `U=1, S=1, P=0` | 既可增加临时实例，也可消耗不可用预算 |
| `P>0` | 先排除编号小于 P 的实例，再按预算更新剩余实例 |

**只写 `maxSurge: 1`，仍然默认允许 1 个单位不可用。** 想让滚动流程先获得新 Ready 容量再删除健康旧实例，需要显式写 `maxUnavailable: 0`。这控制的是主动更新行为，不是对节点故障、业务请求无损切换的承诺。

下面是 SG 模式的零主动容量损失配置片段；Role 模式则将两个预算字段写到每个需要更新的 Role 下。

```yaml
spec:
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxUnavailable: 0
      maxSurge: 1
      partition: 0
```

**2. 边界值**

令 `D` 表示当前更新单位的期望数量：SG 模式用 `spec.replicas`，Role 模式用当前 Role 的 `replicas`。百分比根据这个数量计算，不根据 Pod 总数，也不根据加上 surge 后的数量计算。

| 字段 | 接受的值 | 百分比取整 | 特别边界 |
|---|---|---|---|
| SG `maxUnavailable` | 非负整数、`"0%"`～`"100%"` | 向下取整 | D>0 时，正百分比计算成 0 会保底为 1；`"0%"` 仍为 0 |
| Role `maxUnavailable` | 同上 | 向下取整 | **不保底为 1**；解析结果不能大于该 Role 的 replicas |
| `maxSurge` | 非负整数、`"0%"`～`"100%"` | 向上取整 | 整数可以大于 D；表示允许的额外容量，不保证实际资源可调度 |
| `partition` | 非负整数、`"0%"`～`"100%"` | 向上取整 | 整数可以大于 D；实际保护的是 ordinal < P 的实例 |

负数、`"101%"`、`"1.5%"`、带引号却不带百分号的 `"25"` 都不是有效配置。整数请写 `25`，百分比写 `"25%"`。SG 模式被忽略的 Role `maxUnavailable` 有兼容性校验例外，不应利用它存放无效配置。

相同的 `20%`，在不同字段上效果不同：

| D | SG `maxUnavailable=20%` | Role `maxUnavailable=20%` | `maxSurge=20%` | `partition=20%` |
|---|---:|---:|---:|---:|
| 0 | 0 | 0 | 0 | 0 |
| 1 | 1 | 0 | 1 | 1 |
| 3 | 1 | 0 | 1 | 1 |
| 5 | 1 | 1 | 1 | 1 |
| 6 | 1 | 1 | 2 | 2 |

| 边界情况 | 结果 |
|---|---|
| D>P，U=0 且 S=0 | admission 拒绝：配置没有可推进的更新预算 |
| D≤P，U=0 且 S=0 | admission 允许；但是否保护了所有实际实例，还要看编号 |
| Role replicas=3，U=`"20%"`，S=0，P=0 | U 取整为 0，因此被拒绝 |
| SG U 大于 replicas | 当前校验允许；可能一次更新全部旧组，不再提供正的最低可用数量 |
| Role U 大于 replicas | 被拒绝 |
| 顶层 `spec.replicas=0` | 允许缩到 0；partition 不保护显式缩容 |
| Role `replicas=0` | 副本数字本身合法；Role 模式下默认 U=1 会超过 0，需要同时把 U 改为 `0` 或 `"0%"`，还要满足 gang/eviction/coordination 等已配置约束 |
| `workerReplicas=0` | 只有 entry Pod；workerTemplate 可以省略 |
| `workerReplicas>0` | 必须提供 workerTemplate |
| `revisionHistoryLimit=0` | 清理不再被引用的历史；当前版本、目标版本及仍被实例引用的历史继续保留 |
| `RoleRollingUpdate` + `ServingGroupRecreate` | admission 拒绝；其余 `RoleRecreate`、`None` 可用于 Role 模式 |

`partition` 最容易被误解。**这个 production 版本按绝对实例编号保护，不是按当前列表的前几个保护。**

例如 replicas=3、partition=2：

| 当前存在的编号 | 受保护 | 可更新 |
|---|---|---|
| `0、1、2` | `0、1` | `2` |
| `0、3、4` | `0` | `3、4` |

因此 `partition: "100%"` 在 replicas=3 时只得到 P=3。如果实例编号是 `0、3、4`，编号 3、4 仍然可以更新。**不能仅凭 P≥replicas 判断滚动已完全冻结。** 此处按实际 controller 行为解释；该版本 API/CRD 注释中“前 N 个现有实例”的描述与稀疏编号行为不一致。

另外：

- 调大 partition 不会把已更新实例回滚；调小 partition 会释放更多旧实例进入更新候选。
- partition 约束模板更新，不禁止显式缩容，也不关闭故障恢复。已有受保护实例恢复时，应沿其记录的历史模板恢复。
- 改变 replicas 会重新计算百分比预算和 partition，即使没有产生新模板版本，允许推进的范围也可能变化。
- 新实例一直不 Ready，会占用预算；`U=0` 的更新通常会等待新容量 Ready 后继续。
- 保留旧模板的 partition 灰度状态可以长期存在，`currentRevision != updateRevision` 本身不等于故障。

可选的 Role coordination 再增加几道限制：`maxSkew` 必填，只接受 `"1%"`～`"100%"`；不接受整数或 `"0%"`。`roles` 省略/空数组表示全部 Role，但最终至少要选中两个。依赖必须在所选 Role 内，不能自依赖、重复或成环。

`maxSkew` 控制不同 Role 的相对更新进度差，并受整数实例的取整影响；它不是全局不可用预算。依赖链入口 Role 首次启动目标版本前，要等待依赖链的目标兼容容量 Ready；链内 Role 可以并行启动。旧调用方仍存在时，控制器还会保留旧依赖实例。`maxSkew: "100%"` 也不会取消依赖、旧版本保留或各 Role 自身的 U/S/P 限制。更新校验还可能要求先增加依赖 Role 的副本，为新旧版本共存留出空间。

**3. 哪些字段参与 ControllerRevision 的滚动比较**

当前 production 实际比较的范围可以完整写成：

```text
spec.template.roles[]，先按 name 排序
  ├─ name
  ├─ entryTemplate（完整模板：metadata + spec）
  ├─ workerReplicas
  └─ workerTemplate（完整模板：metadata + spec）
```

| 字段 / 变更 | 参与比较？ | 影响 |
|---|---|---|
| Role `name`，增加/删除 Role | 是 | Role 集合或身份变化 |
| `roles[].entryTemplate.metadata.labels/annotations` | 是 | 修改 Pod 模板标签、注解属于模板变化 |
| `roles[].entryTemplate.spec` 内所有字段 | 是 | 包括 image、command、args、env、resources、探针、volumes、调度约束、securityContext 等 |
| `roles[].workerReplicas` | **是** | worker 数量属于 Role 实例内部结构；0→1、1→2、2→1 都是模板变化 |
| `roles[].workerTemplate.metadata/spec` | 是 | 与 entryTemplate 一样，比较完整模板 |
| `spec.replicas` | 否 | 单独做 ServingGroup 扩缩容 |
| `roles[].replicas` | 否 | 单独做 Role 实例扩缩容 |
| 顶层及 Role 的 `maxUnavailable/maxSurge/partition` | 否 | 调整已有版本更新的预算和范围 |
| `rolloutStrategy.type/roleCoordination/evictionStrategy` | 否 | 调整更新或驱逐规则；仍需通过校验 |
| `recoveryPolicy`、`restartGracePeriodSeconds` | 否 | 调整故障恢复行为 |
| `revisionHistoryLimit` | 否 | 调整历史保留数量 |
| 顶层 `spec.schedulerName`、`spec.plugins` | **否，当前 production 路径不包含** | 不能依靠单独修改它们触发已有实例模板滚动 |
| `spec.template.networkTopology` | 否 | 整体不可变，创建后不能增加、删除或修改，详见第 4 部分 |
| `spec.template.gangPolicy` | 否 | 设置后不能删除父对象；minRoleReplicas 的键值有不可变约束，可选字段边界见第 4 部分 |
| ModelServing 自身的 `metadata.labels/annotations`、`status` | 否 | 与 Pod 模板中的 metadata 是两个不同位置 |
| 仅调整 `roles[]` 的排列顺序 | 否 | 比较前按 Role name 排序 |
| 引用的 ConfigMap/Secret 的内容、相同镜像 tag 背后的内容 | 否 | 外部内容不在该模板快照中；修改模板内引用名或 image 字符串才进入比较 |

“完整 Pod 模板”还意味着：顶层 `spec.schedulerName` 不参与，但 `entryTemplate.spec.schedulerName` 位于模板内部，会参与。模板里的普通环境变量及控制器保留环境变量也没有在这条 production 比较路径中被剔除。即使 `workerReplicas=0`，一个已填写的 workerTemplate 仍参与比较。

**保存进 ControllerRevision，不等于参与比较。** 该版本的 `ControllerRevision.data` 保存的是包在 `data` 下的完整 Roles 快照，因此可以看到 Role replicas、滚动参数等字段。但是，版本计算和语义比较只抽取上面四个字段；只改 replicas/预算时会复用历史，不覆盖旧快照。历史里看到的 replicas 可能因此不是当前期望规模。

**参与比较，也不等于立即重建全部 Pod。** 实际处理顺序是：

1. 抽取 Role 模板字段，与已有历史作语义比较；有等价历史就复用对应版本。
2. 找到模板确实不同的实例。hash 相同可快速判断；hash 不同还要读取实例自己的历史模板，作语义比较。
3. 按更新模式决定粒度：SG 模式替换整个旧组；Role 模式替换对应的变更 Role 实例。
4. 再受 partition、不可用/额外容量预算、Ready 状态和可选 coordination 限制。

因此，升级 controller 或依赖库后 hash 表示变化，不应该单独成为重建理由。Kubernetes 语义相同的资源量，如 CPU `1` 与 `1000m`，也按等价模板处理。模板真正不同但碰巧与更早历史相同，可以复用更早版本，不一定新增一份历史。

这里的“语义比较”并不等于把模板渲染成最终 Pod 后比较。它不会统一所有省略值和显式默认值，不能假定所有“运行效果相同”的 YAML 都等价；Role 列表以外的有序数组也不能任意重排。

历史缺失或损坏时，控制器通常把相关模板视为“未知”，暂停受影响实例的模板驱动删除并重试；不会直接用当前 spec 猜测旧模板。缺失历史的特殊恢复分支需要可信的前次目标记录，能够证明发生了真实模板变更。

删除整个 Role 是特殊情况：Role 模式会直接清理已经不在目标 spec 中的旧 Role 实例，不能期待已删除 Role 上原来的 U/P 参数继续保护它。

最后，源码里虽然存在 `BuildRevisionData()`，会包含 scheduler/plugins 并做额外归一化，但 **它不是本次 production reconcile 使用的版本生成路径**。判断当前行为要沿 `desiredRevision → CreateControllerRevision / EqualRoleTemplatesForRevision` 看，不能仅凭该 helper 的注释扩展上面的字段范围。

**4. Immutable：哪些字段不能原地修改**

**不可变字段的修改会在 API 更新校验阶段被拒绝，不会进入滚动升级。** “不参与 Revision 比较”与“是否允许修改”是两个独立问题。

| 字段 | 限制 | 直观例子 / 校验位置 |
|---|---|---|
| ModelServing `metadata.name`、`metadata.namespace` | 同一个对象不能改名或迁移 namespace | 属于 Kubernetes 对象身份；换名字创建的是另一个对象 |
| `spec.template.networkTopology` 整体 | 创建后不能补填、删除或修改其内容 | webhook 比较整个新旧对象；报错 `field is immutable` |
| `networkTopology.groupPolicy`、`rolePolicy` | 随整个 networkTopology 一起不可变 | 不能把 `mode: hard` 改为 `soft`，也不能改变拓扑层级限制 |
| `networkTopology.servingGroupAntiAffinity` | 随整个 networkTopology 一起不可变 | 不能增加、删除或调整 required/preferred 规则、拓扑层级及权重 |
| `networkTopology.roleAffinity`、`roleAntiAffinity` | 随整个 networkTopology 一起不可变 | 不能变更参与 Role、亲和/反亲和规则或层级 |
| `spec.template.gangPolicy` 的存在性 | 初次可以不配置；后续允许补填；一旦存在，不能删除整个父对象 | CRD CEL：`!has(oldSelf.gangPolicy) || has(self.gangPolicy)`；报错 `gangPolicy is required once set` |
| `spec.template.gangPolicy.minRoleReplicas` 的已有 map | 新旧对象都保留该字段时，整张 map 必须相等，不能改值、增键或删键 | CRD CEL：`self == oldSelf`；报错 `minRoleReplicas is immutable`；可选字段增删的边界见下文 |

上表中缩写的 `networkTopology.*` 均位于 `spec.template.networkTopology` 下。其子字段的限制来自**整个父对象不可变**，不是每个子字段各有一条独立规则。创建时没有 networkTopology，之后也不能补上，即使只补一个空对象 `{}`。

`gangPolicy` 则不能简单理解为“整个配置创建后永远不变”。当前 CRD 的精确边界是：

- 已有 `minRoleReplicas: {prefill: 1, decode: 1}`，直接改成 `{prefill: 2, decode: 1}`，会被拒绝；在仍然保留 map 的情况下删除 `decode` 键，也会被拒绝。
- `minRoleReplicas` 是可选子字段。它的 `self == oldSelf` 规则没有覆盖字段从缺失到存在、或从存在到缺失的情况；父级规则只保证 gangPolicy 本身不被删除。
- 因此，**保留 `gangPolicy: {}` 而移除整个 minRoleReplicas，当前不可变规则本身不会拦截；原先没有 minRoleReplicas 时补填也不会被这条规则拦截。** 新配置仍需通过 Role 名称、副本数等其他校验。这是 CRD 与 CEL 执行逻辑的源码结论，本次未做集群验证，不能把该字段描述为覆盖所有增删路径的严格 immutable。

还要区分三种容易混淆的情况：

| 情况 | 应该如何理解 |
|---|---|
| 修改 `roles[].entryTemplate/workerTemplate` 中的 command、env、volumes 等 | 这些 ModelServing 模板字段可以变化并触发重建。内嵌 PodSpec 文档中的“不可变”描述，不能直接当作 ModelServing 模板字段的更新禁令 |
| 修改 replicas、滚动预算、partition、rolloutStrategy.type、roleCoordination | 没有统一的 immutable 限制；但更新后的字段归属、预算和依赖容量必须合法。例如切换 Role 模式需要去掉顶层滚动配置 |
| `ControllerRevision.data` 历史快照 | 当前 controller 保持已有快照内容不变：模板等价则复用；同名历史对应不同模板则报错，不覆盖旧快照。这是历史管理规则，不代表 ModelServing 的模板不允许更新 |

例如，想改拓扑分布时，应为新配置创建新的 ModelServing 并安排迁移；调整 partition 或滚动预算不能放开 networkTopology 的不可变限制。只改镜像、命令或 worker 数量，则走前文的正常模板更新流程。

**5. 需要被拒绝的配置组合**

下面列出当前 production 的 CRD / webhook 明确拒绝的组合。除注明“仅更新”的情形外，创建与更新都会校验；判断对象是**经过 API 默认化后的配置**。这里的拒绝指 ModelServing 创建或更新请求不被接受，不包括已经提交成功、随后等待资源或 Ready 的情况。

沿用 `D` 表示当前层级期望副本数，`U/S/P` 表示取整后的 maxUnavailable/maxSurge/partition。表中的 `A → B` 表示 Role A 依赖 Role B。

**5.1 更新模式、恢复策略与预算组合**

| 需要拒绝的组合 | 例子 / 原因 | 调整方式 |
|---|---|---|
| Role 模式 + 顶层 `rollingUpdateConfiguration` | 即使顶层只写 `{}`，也属于不允许的配置层级 | 去掉顶层对象，把预算写到各 Role 下 |
| SG 模式 + 任意 Role 显式设置 `maxSurge` 或 `partition` | 即使写 `0` 也拒绝；省略更新方式时默认 SG，同样适用 | 使用顶层参数，或显式切换到 Role 模式 |
| SG 模式 + `roleCoordination` | coordination 只适用于 Role 模式；只写 coordination 而省略 type 也会被拒绝 | 设置 Role 模式，或移除 coordination |
| Role 模式 + `recoveryPolicy: ServingGroupRecreate` | Role 更新与整组故障恢复的粒度不兼容 | 改用 `RoleRecreate` / `None`，或采用 SG 更新 |
| D>P，同时 U=0、S=0 | 例如 replicas=3、partition=0、U=0、未写 S；未写 S 等于 0 | 让 U 或 S 的有效值大于 0 |
| Role 模式中，小百分比 U 向下取整为 0，同时 S=0、D>P | 例如 Role replicas=3、U=`"20%"`、S=0、P=0；显式百分比没有保底 1 | 使用有效的整数/百分比预算，或增加 surge |
| Role 模式中 U>D | 例如 Role replicas=1、U=2；surge 再大也不能抵消这个上限 | U 不超过该 Role 的期望数 |
| Role 模式中 replicas=0，但 U 省略或保持 1 | API 默认 U=1，随后发现 1>0 而拒绝 | 同时设置 U=0 或 `"0%"`，并检查其他容量约束 |

恢复策略的完整兼容关系如下，不能仅根据错误信息中列举的示例推断为“一一对应”：

| recoveryPolicy | SG 更新 | Role 更新 |
|---|---|---|
| `RoleRecreate`（默认） | 允许 | 允许 |
| `ServingGroupRecreate` | 允许 | **拒绝** |
| `None` | 允许 | 允许 |

**5.2 Role coordination 的配置组合**

以下均以已经选择 Role 模式为前提。

| 需要拒绝的组合 | 例子 / 原因 |
|---|---|
| 配置 coordination，但不提供有效 maxSkew | `{}`、整数 `10`、`"0%"`、`"101%"`、`"1.5%"` 都不合法；必须为 `"1%"`～`"100%"` |
| 实际参与协调的 Role 少于两个 | 显式只选一个；或者 roles 留空、但 ModelServing 总共只有一个 Role |
| 参与集合包含未知或重复 Role | 例如选择 `[prefill, missing]` 或 `[prefill, prefill]` |
| 依赖任一端不在参与集合中 | 只选择 A、B，却设置 A → C；即使 C 在 ModelServing 中存在也不允许 |
| 自依赖、循环依赖 | A → A，或 A → B → A |
| 重复定义同一 Role 的依赖记录，或 dependsOn 内重复 | 两条 `role: a` 记录，或 `dependsOn: [b, b]` |

`roles` 省略或 `[]` 表示选择全部 Role，不表示关闭 coordination。关闭时要移除 `roleCoordination` 对象。

**5.3 仅更新时才会拒绝的组合**

| 旧状态 + 本次修改 | 拒绝原因 / 如何理解 |
|---|---|
| 已存在 A → B，A、B 模板都发生真实变化，B 的新 partition≥新 replicas | 被依赖的 B 没有 partition 外的目标版本位置；即使 S>0，也会被依赖容量校验拒绝 |
| 已存在 A → B，A、B 模板都变化，B 旧、新 replicas 都为 1 | B 要保留旧版本供旧 A 使用，却没有另一个位置启动新 B；应增加 B 的 replicas，例如先扩到 2 并就绪后再改模板 |
| 上一行只增加 B 的 maxSurge，不增加 replicas | 当前依赖容量校验只按旧/新 replicas、partition 和旧版本保留需求计算，**不把 surge 计入可启动容量**，仍会拒绝 |
| 协调更新尚未确认完成，把依赖 B 的 replicas 从 2 降到 1 | 丢失新旧依赖实例共存空间；即使本次没有再改模板，也会被拒绝 |
| 协调更新尚未确认完成，调整依赖 Role 的 replicas / partition 后，新 replicas≤max(P,1) | 例如 replicas=2，把 partition 改为 2；无法同时保留一个旧实例并留出目标位置 |
| 切换 SG → Role，但保留顶层滚动配置；或切换 Role → SG，但保留 Role 的 S/P 或 coordination | 切换后的完整配置违反字段归属；需要在同一次更新里清理不兼容字段 |
| 缩小 Role replicas，却让现有 U 或 gang minRoleReplicas 超过新副本数 | 例如原 replicas=3、U=2，缩到 1 却不调整 U；不可变 gang 下限还可能限制可缩到的最小规模 |
| 缩小 SG / Role replicas，却让对应 eviction 下限超过新副本数 | 例如 SG minAvailable=2，spec.replicas 从 3 改为 1 |
| 删除或改名 Role 后，gang、生效的 Role eviction 预算、coordination 或拓扑规则仍引用旧名字 | 新对象存在悬空引用；其中不可变的 gang/topology 配置需要预先规划，不能假定都可以随 Role 一起修改 |
| 修改第 4 部分的不可变字段，或违反 gangPolicy 设置后不得删除的约束 | 更新直接拒绝；预算、surge 或 partition 不能解除不可变限制 |

“尚未确认完成”也包括 `status.observedGeneration < metadata.generation` 的情况。即使旧条件曾显示 `RolloutComplete`，controller 还没观察到最新配置时，依赖缩容校验仍会保守地按可能正在更新处理。

前面三条依赖容量限制针对需要提供新版本容量的依赖 Role；不能推广成“所有单副本 Role 都禁止更新”。只有资源 Quantity 的等价写法变化，也不构成这里的真实模板变化。

**5.4 worker、gang、eviction 与拓扑的关联约束**

| 需要拒绝的组合 | 例子 / 原因 |
|---|---|
| workerReplicas>0，但 workerTemplate 缺失 | 例如 workerReplicas=2，却没有 worker Pod 模板 |
| gang minRoleReplicas 引用不存在的 Role，或该值超过对应 Role replicas | 例如 decode replicas=1，却设置 minRoleReplicas.decode=2 |
| 配置 eviction，保护层级为 ServingGroup，但 minAvailable 缺失 | `evictionStrategy: {}` 也会遇到默认层级 ServingGroup 的此项校验；只填 roleMinAvailable 不能替代 minAvailable |
| eviction 保护层级为 Role，但 roleMinAvailable 缺失或为空 | 只填 minAvailable 不能替代 Role 预算 |
| 生效的 eviction 下限超过对应期望数，或 Role 预算引用未知 Role | 百分比按期望数向上取整，再校验上限 |
| 设置任意一个拓扑亲和/反亲和对象，但 schedulerName 不是 volcano | 适用于 servingGroupAntiAffinity、roleAffinity、roleAntiAffinity；不能泛化成任何 networkTopology 字段都触发这条校验 |
| 设置了上述亲和/反亲和对象，但所有 required/preferred 都为空 | 例如只写 `roleAffinity: {}`；至少要有一条拓扑规则 |
| 同一亲和/反亲和 term 同时设置 topologyTierName 和 topologyTier，或两者都不设置 | 必须二选一；数字 topologyTier 不能为负数 |
| required term 设置 weight；或 preferred term 未设置 weight / weight 不在 1～100 | required 不允许权重，preferred 必须有合法权重 |
| roleAffinity term 少于两个不同 Role；roleAntiAffinity term 少于一个 Role；引用未知/重复 Role | Role 亲和与反亲和的最小参与数不同 |
| 数字层级的硬约束发生当前校验可识别的冲突 | 例如两个 Role 副本数都>0，对它们设置 required affinity tier=0，同时 required anti-affinity tier=1，会被拒绝 |
| 启用 ranktable，但配置无法解析、未提供 template，或对应模板 ConfigMap 无法读取 | ConfigMap 在 controller 配置的模板 namespace 查找，并非任意 namespace 同名即可 |

拓扑冲突校验的范围有限：当前只比较数字 tier 的特定关系；使用 tier 名称时，层级关系交给 Volcano 处理，不能把所有调度上难以满足的组合都写成“ModelServing admission 会拒绝”。

基础格式错误也会拒绝：负数副本数 / workerReplicas / 生效预算，非法百分比，负数 revisionHistoryLimit，未知策略枚举，缺少必填模板字段，以及 Role 数量不在 1～4、Role 名重复或命名不合法等。具体数值格式见第 2 部分。

**5.5 这些情况不能误列为“必须拒绝”**

| 配置 / 情况 | 当前行为 |
|---|---|
| SG 模式下设置 Role maxUnavailable | 被忽略；不能与 Role maxSurge / partition 的拒绝规则混淆 |
| SG U>D，或任一模式 S>D | 整数配置允许；Role U>D 则拒绝 |
| U=0、S=0，但 D≤P | 普通预算校验允许；依赖更新可能另行拒绝，稀疏编号也不保证全部被保护 |
| 同时设置 U>0、S>0 | 合法，可以组合使用 |
| coordination 不配置 dependencies，但有合法 maxSkew 和至少两个参与 Role | 合法，只应用进度协调，不要求必须配置依赖边 |
| eviction 同时包含 minAvailable 和 roleMinAvailable | 当前 webhook 按 protectionLevel 校验对应一项，没有仅因“两项同时存在”就拒绝的互斥规则 |
| 新版本不 Ready、GPU 不足、实际依赖尚未 Ready | 通常是提交后的等待或运行失败，不是上述配置组合的 admission 拒绝 |

这部分根据当前源码及已有测试用例整理，未新增执行 Go / Kind 验证。

**源码依据**

| 内容 | 对应源码 |
|---|---|
| API 默认值、两种模式 | [model_serving_types.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/apis/workload/v1alpha1/model_serving_types.go:36)、[servinggroup_types.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/apis/workload/v1alpha1/servinggroup_types.go:70) |
| 实际数值取整 | [utils.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/utils/utils.go:632) |
| 预算及兼容性校验 | [validator.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/webhook/validator.go:256) |
| networkTopology 整体不可变及更新用例 | [validator.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/webhook/validator.go:204)、[validator_test.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/webhook/validator_test.go:148) |
| gangPolicy / minRoleReplicas 不可变规则 | [CRD 子字段规则](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/charts/kthena/charts/workload/crds/workload.serving.volcano.sh_modelservings.yaml:318)、[CRD 父对象存在性规则](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/charts/kthena/charts/workload/crds/workload.serving.volcano.sh_modelservings.yaml:17977) |
| 可选字段 CEL 规则执行边界（本地依赖源码） | [旧字段缺失时跳过 transition rule](/Users/vanderchen/go/pkg/mod/k8s.io/apiextensions-apiserver@v0.34.2/pkg/apiserver/schema/cel/validation.go:399)、[只遍历新对象中存在的子字段](/Users/vanderchen/go/pkg/mod/k8s.io/apiextensions-apiserver@v0.34.2/pkg/apiserver/schema/cel/validation.go:832) |
| name / namespace 不可变（本地依赖源码） | [objectmeta.go](/Users/vanderchen/go/pkg/mod/k8s.io/apimachinery@v0.34.2/pkg/api/validation/objectmeta.go:252) |
| coordination 校验与执行 | [role_coordination_validator.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/webhook/role_coordination_validator.go:32)、[role_rolling_update_coordinator.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/controller/role_rolling_update_coordinator.go:592) |
| 更新时的依赖容量拒绝规则与现有用例 | [role_coordination_validator.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/webhook/role_coordination_validator.go:143)、[role_coordination_validator_test.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/webhook/role_coordination_validator_test.go:153) |
| gang / 拓扑 / worker / recovery / eviction 关联校验 | [validator.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/webhook/validator.go:428) |
| partition 和滚动删除 | [model_serving_controller.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/controller/model_serving_controller.go:1630) |
| 四字段投影、hash 与语义比较 | [revision_util.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/utils/revision_util.go:34) |
| 目标历史选择、实例模板比较 | [revision_resolver.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/controller/revision_resolver.go:97) |
| 实际历史存储与清理 | [controller_revision.go](/Users/vanderchen/workspace/dev/kthena-workspace/kthena-production-linear/kthena-production-release/pkg/model-serving-controller/utils/controller_revision.go:49) |

此前实际 Kind 复验覆盖的场景与结果见 [029 验证记录](/Users/vanderchen/workspace/dev/kthena-workspace/issues/bugs/029-production-023-027-backport-DONE/PROPOSAL_COMMIT.md)。本文新增内容为源码说明，不代表重新执行了全部边界组合。
