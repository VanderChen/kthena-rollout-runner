# ModelServing 滚动升级全场景与质量加固验收基线

版本：2026-09-05。用途：在实施质量加固、编写自动化用例前，明确配置空间、可行性、预期动作和验收条件。

本文的“全场景”按行为等价类枚举：任意一个具体配置必须能够映射到下列配置存在性、数值、partition、coordination、生命周期状态的组合。整数和副本数没有有限枚举上限，不能用几个成功 YAML 代表全部配置。第 13 节给出组合生成与覆盖规则。

## 1. 源码基线与证据边界

| 基线 | 用途 | 本文如何使用 |
| --- | --- | --- |
| `production/release-1.0@4373b29e` | 本地 production 发布分支快照 | 当前配置默认值、校验和执行语义的主基线 |
| `feat/018-production-rollout-features@6fea34e0` | 上一轮 Kind 镜像 `dev-018-gapfill` 对应代码 | 引用已有运行证据；不得当作发布分支全场景测试通过 |
| bug 016，`5c2fdfbd` | production 上的 hash/历史模板判等修复 | 历史异常及防回归要求 |
| bug 021，`25109bba`，父提交为 bug 016 | ControllerRevision 生命周期修复 | 历史写入顺序、引用保护和恢复要求 |

上述发布分支和 integration 分支的 API、CRD、validator、budget helper、coordination 算法相同；主要 controller 差异包含周期同步与缓存恢复路径。bug 016/021 修复分支不改变这些配置 API，但改变版本识别和历史管理。修复分支已存在不等于发布分支已合入；验收必须固定实际镜像对应 commit。

本文区分三种陈述：

- **现行语义**：由上述 production 源码得出的配置和动作规则。
- **验收要求**：质量加固必须守住的性质；发现现有代码违反时应修复，不能把异常写成正常预期。
- **待验证风险**：源码推导出的组合风险，尚不能声称该具体场景在 Kind 已复现。

上一轮 5 个运行场景、admission 检查及保留 YAML 在 [验证记录](./PROPOSAL_COMMIT.md) 和 [Kind 目录说明](./kind/README.md)。本文扩展分析与验收范围，本轮没有执行新增矩阵的 Kind 测试。

## 2. 统一术语与通过标准

| 符号 | 含义 |
| --- | --- |
| `N` | `spec.replicas`，ServingGroup 期望数 |
| `Rᵢ` | `spec.template.roles[i].replicas`，每个 ServingGroup 内该 Role 的期望实例数 |
| `Wᵢ` | 该 Role 的 `workerReplicas`；一个 Role 实例包含 `1+Wᵢ` 个 Pod |
| `D` | 当前预算作用对象的期望数；SG 模式为 `N`，Role 模式为对应 `Rᵢ` |
| `U/S/P` | 解析为整数后的 `maxUnavailable/maxSurge/partition` |
| `∅` | 原始请求没有配置该字段，不等于显式 `0` |
| `A/B/C` | 语义不同的业务模板版本；不要求 hash 字符串固定 |
| `O` | 实际存在的实例 ordinal 集合，可能不连续 |
| `E` | 当前允许更新的旧模板实例集合：旧模板且 ordinal `>=P`，再应用 coordination 条件 |

判断一个组合时要分别回答：

1. **可提交**：CRD schema、默认化和 webhook 是否允许创建/更新？
2. **可启动**：存在旧实例需要升级时，是否能合法创建或删除第一个实例？
3. **可推进**：Ready、依赖和容量条件满足后，是否持续取得进展？
4. **到达目标**：是全量 B，还是 partition 约束下的 A/B 混合稳态？
5. **可恢复**：进程重启、事件丢失、删除重试后，是否仍到达相同目标？

“admission 允许”不能替代后四项。例如完全保护是合法暂停；资源不足时 `U=0,S>0` 合法但等待；协调算法与稀疏 ordinal 不一致则可能合法但无法自动完成。

质量验收要求所有允许推进的场景在前置条件恢复后自动收敛。通过判据不包含“手工重启一次 controller”；重启仅是独立的恢复注入场景。

## 3. 配置与不配置：默认行为清单

### 3.1 粒度选择及字段归属

| ID | 请求形态 | API 默认化后/现行语义 |
| --- | --- | --- |
| D01 | 完全省略 `rolloutStrategy` | 父对象可保持不存在；controller 有效模式为 SG，`U=1,S=0,P=0` |
| D02 | `rolloutStrategy: {}` | `type` 默认 SG；顶层配置对象仍可不存在；有效 `1/0/0` |
| D03 | 只写 `type: ServingGroupRollingUpdate` | 顶层预算缺失，仍为 `1/0/0` |
| D04 | 省略 type，只写 `rollingUpdateConfiguration: {}` 或其中字段 | type 默认 SG；配置对象中的 U 默认 1，S/P 缺失按 0 |
| D05 | `type: RoleRollingUpdate`，各 Role 不写 U/S/P | 每个 Role 被 CRD 默认为 U=1；S=0、P=0；各组各 Role 独立推进 |
| D06 | 只在 Role 写 U/S/P，没有显式 Role mode | 不会自动切换到 Role mode。Role U 被忽略；Role S/P 被拒绝 |
| D07 | Role mode，同时顶层 `rollingUpdateConfiguration: {}` | 即使为空也拒绝；不能叠加 SG budget |
| D08 | 只写 `roleCoordination`，省略 type | type 默认 SG，因此拒绝 |
| D09 | `type: ""` 或未知枚举 | 不是省略；CRD 枚举拒绝 |
| D10 | optional 字段显式 `null` | 当前 schema 非 nullable；按默认化/删除字段后的 API 结果判断，不作为第三种业务值。必须保留 server-returned spec 验证默认化 |
| D11 | 通过 patch 删除已有 U | 恢复默认 1，不是变为 0；删除 S/P 恢复 0；删除整个 strategy 恢复 SG mode |
| D12 | 模板/旧数据绕过当前 CRD，Role U 在内存中为 nil | helper 的历史回退是“未限制 Role 删除数”；正常 API 默认化应避免此分支，升级回归必须覆盖 |

PATCH/SSA 的“本次请求没有写字段”不保证已有字段消失，尤其字段由其他 field manager 管理时。D11 的断言以 API Server 实际存储值为准。Role mode 改回 SG 时，需同时清除 Role S/P 和 coordination；只删除 type 可能导致剩余字段不兼容而被拒绝。

### 3.2 周边字段默认与粒度

| 字段 | 未配置 | 显式配置/影响 |
| --- | --- | --- |
| `spec.replicas` | 1 | 0 可用于停服/预置模板；不是只暂停滚动 |
| Role `replicas` | 1 | 可为 0，但受 Role U、gang、dependency 等额外校验影响，见 §5.3 |
| Role `workerReplicas` | **必填，无 CRD 默认** | 无 worker 必须写 0；大于 0 必须提供 workerTemplate |
| `recoveryPolicy` | `RoleRecreate` | None/RoleRecreate/ServingGroupRecreate 的粒度见 §8 |
| `restartGracePeriodSeconds` | 0 | 是故障恢复宽限期；不是滚动批次间隔，也不是 Pod terminationGracePeriodSeconds |
| `revisionHistoryLimit` | API 默认 10 | 验收要求保留仍被引用的历史，不计入非 live 历史上限；production 基线的历史缺陷见 §11 |
| `roleCoordination` | 关闭 | 配置对象后 maxSkew 必填；空对象不是默认启用 |
| `roleCoordination.roles` | 全部 Role | 空数组也选择全部；非空是参与集合，其余 Role 仍独立滚动 |
| `roleCoordination.dependencies` | 无依赖边 | 仍有 maxSkew 限制，不等于关闭 coordination |
| `evictionStrategy` | 无该策略提供的 eviction 保护 | 配置后独立保护 eviction，不是 rollout 的附加预算 |
| `schedulerName` | `volcano` | fixture 固定该值；调度结果决定新容量能否 Ready |
| `gangPolicy` | 默认按组内全部 Role 的 gang 要求调度 | 不提供跨 Role 版本依赖关系；不能替代 coordination |

### 3.3 U/S/P 的全部 8 种“写/不写”组合

下面每一行都必须分别应用于 SG 顶层和 Role 内联字段。`u/s/p` 是用户显式值，再按 §4/§5 分类；Role mode 下顶层对象始终必须缺失。

| ID | U | S | P | 生效值 | 行为含义 |
| --- | --- | --- | --- | --- | --- |
| M000 | ∅ | ∅ | ∅ | `1,0,0` | 默认先删后建 |
| M100 | u | ∅ | ∅ | `u,0,0` | 自定义 unavailable；u=0 且有更新目标时拒绝 |
| M010 | ∅ | s | ∅ | `1,s,0` | s>0 为 hybrid；**只写 surge 不代表零不可用** |
| M001 | ∅ | ∅ | p | `1,0,p` | partition 外按默认 unavailable 滚动 |
| M110 | u | s | ∅ | `u,s,0` | 自定义两个 budget，全量目标 |
| M101 | u | ∅ | p | `u,0,p` | partition 外先删后建，或合法冻结 |
| M011 | ∅ | s | p | `1,s,p` | 默认 unavailable + surge + partition |
| M111 | u | s | p | `u,s,p` | 完整显式配置，按下面组合逻辑执行 |

每个 Role 可以选不同的一行；不会从相邻 Role 继承预算。若生产要求全部 Role 零主动容量损失，就要给每个可能变更的 Role 显式写 `maxUnavailable: 0` 并提供可行的 surge。

## 4. 数值等价类、百分比与预算定义

### 4.1 取整及边界

| ID | 输入类 | U：SG | U：Role | S：两层 | P：两层 |
| --- | --- | --- | --- | --- | --- |
| V01 | ∅ | 1 | API 默认 1 | 0 | 0 |
| V02 | 整数 0 | 0 | 0 | 0 | 0 |
| V03 | `"0%"` | 0 | 0 | 0 | 0 |
| V04 | 正百分比，乘 D 后小于 1 | D>0 时 clamp 为 1 | 向下到 0 | 向上到 1 | 向上到 1 |
| V05 | 正百分比，乘 D 后非整数 | 向下，最小 1 | 向下 | 向上 | 向上 |
| V06 | `"100%"` | D | D | D | D；稀疏集合不一定全保护 |
| V07 | 正整数，`0<v<D` | v | v | v | v |
| V08 | 正整数，`v=D` | v | v | v | v |
| V09 | 正整数，`v>D` | 允许，无最低可用保证 | 拒绝 | 允许，需准备相应容量 | 允许，但仍按实际 ordinal 判断 |
| V10 | D=0，百分比 | 0 | 0 | 0 | 0 |
| V11 | 负数、>100%、“1.5%”、“25”（带引号但无百分号）、非法类型 | 按字段 schema/webhook 拒绝 | 同左 | 同左 | 同左 |

V11 对生效字段成立。SG mode 的 Role U 有兼容性例外：validator 直接跳过其值检查，CRD 只约束 int-or-string 类型；合法类型的无效字符串也可能被忽略而接收。切换到 Role mode 后会开始校验，不能依靠这个例外配置无效值。

数值示例（两个更新粒度都须测试）：

| D | `U=20%`：SG/Role | `S=20%` | `P=20%` |
| --- | --- | --- | --- |
| 0 | 0 / 0 | 0 | 0 |
| 1 | 1 / 0 | 1 | 1 |
| 3 | 1 / 0 | 1 | 1 |
| 5 | 1 / 1 | 1 | 1 |
| 6 | 1 / 1 | 2 | 2 |

百分比始终相对于最新期望数 D，**不是**实际数 `D+S`，也不是 `D-P`。运行中修改副本数必须重算三个值。

### 4.2 可用预算和额外容量

无外部故障、期望副本固定、普通模板替换时，验收约束为：

```text
最低可用容量 = max(D-U, 0)
活动容量上限 = D+S
可主动删除健康旧实例数 <= 当前可用容量 - 最低可用容量
```

production 删除 helper 的计算形式为：

```text
SG:   maxScaleDown = 实际 group 数 - (N-U) - newUnavailableGroups
Role: maxScaleDown = 实际 Role 数 - max(R-U,0) - newUnavailableRoles
```

负预算不执行删除。Role 的 deleting 实例也计入 newUnavailable；SG 对新 revision 且非 Running 的组做扣减。旧实例已不健康、protected group 不健康、deleting、缓存滞后必须单独覆盖，不能把这个计数公式直接当作对所有故障的可用性证明。

surge 只有在仍有可更新的旧模板时才启用；初次创建没有旧模板，不应因为写了 S 就直接创建 D+S。准备了 S 不代表 S 个实例必然同时创建，也不保证集群能调度它们。

统计必须分开记录：活动实例、terminating 实例、实际 Pod 总数、Ready 容量。Pod 终止宽限期和异步 GC 会使 `kubectl get pods` 的瞬时总数超过逻辑 `D+S`；此时应检查是否重复分配了活动容量，不能只凭总行数判定违反 surge。

Role budget 的单位是整个 Role 实例，不是 Pod。W=3 时，U=1 允许一个 Role 的 4 个 Pod 一起替换。SG budget 的单位是整个 ServingGroup，U=1 允许该组内全部 Role/Pod 被替换。

## 5. U/S/P 所有可行主模式

### 5.1 九类 budget 输入 × partition 状态

此表假设 `D>0` 且稳定 ordinal 连续为 `[0,D)`。`+` 表示解析后大于 0；`0` 包括整数零、0%、Role 小百分比向下取零。表中“全保护”的稀疏例外见 §6。

| ID | U 输入 | S 输入 | 生效 U/S | P=∅或0：全量更新 | 0<P<D：部分更新 | P>=D：全部已存在旧实例受保护 |
| --- | --- | --- | --- | --- | --- | --- |
| B01 | ∅ | ∅ | 1/0 | 先删后建 | 只替换 partition 外 | 合法暂停 |
| B02 | ∅ | 0 | 1/0 | 同 B01 | 同 B01 | 合法暂停 |
| B03 | ∅ | + | 1/S | hybrid | partition 外 hybrid | 合法暂停，不额外扩 surge |
| B04 | 0 | ∅ | 0/0 | 拒绝 | 拒绝 | 允许冻结 |
| B05 | 0 | 0 | 0/0 | 拒绝 | 拒绝 | 允许冻结 |
| B06 | 0 | + | 0/S | 先增后减，Ready 后才删健康旧实例 | 仅替换 partition 外，保留容量 | 合法暂停 |
| B07 | + | ∅ | U/0 | 最多消费 U 后等待补齐 | 同规则且排除保护实例 | 合法暂停 |
| B08 | + | 0 | U/0 | 同 B07 | 同 B07 | 合法暂停 |
| B09 | + | + | U/S | hybrid；新容量 Ready 前也可消费 U | partition 外 hybrid | 合法暂停 |

这些模式对 SG 与独立 Role 均成立；Role U>D 先被拒绝，coordination 再对 Role 可行动作加约束。admission 的 0/0 检查使用 `D>P`，并不读取实际 O。

### 5.2 两个粒度的具体执行与终态

以 `D=3`、U/S 的正值取 1、P 的部分值取 1、初始 A 全 Ready 为例：

| 模式 | ServingGroupRollingUpdate | RoleRollingUpdate |
| --- | --- | --- |
| U=1,S=0,P=0 | 3 组中删 1 组，创建 B 并等 Ready，继续；终态 3 组 B | 每组内对 changed Role 3 个实例做同样替换；其他 Role UID 不变 |
| U=0,S=1,P=0 | 创建第 4 组 B，Ready 后删旧；无主动容量损失；最后回到 3 组 B | 对每个 changed Role 创建第 4 个实例，Ready 后删旧，最后回到 R=3 |
| U=1,S=1,P=0 | 总活动组最多 4；可用下限 2；允许创建与删除交错 | 每个 changed Role 活动数最多 4、可用下限 2；各组各 Role 独立 |
| 上述任一模式，P=1 | group 0 保留 A；其余切 B；最终 3 组、至少一个旧组 | 各 Role ordinal 0 保留其历史模板；只更新其余实例；整组可保持混合版本 |
| P=3，连续 O | 全部 3 组保留 A，不因为 template=B 启动 surge | 该 Role 全部保留 A；其他未完全保护 Role 仍可更新 |

在两 Role、N=3 的部署中，Role 模式可能让三个 ServingGroup 同时有更新中的 Role。因此不能以“每 Role U=1”推导“整个 ModelServing 最多一个 SG 不可用”。

静态同构部署的资源估算：SG surge 的 Pod 上限约为 `(N+S) × Σ[Rᵢ×(1+Wᵢ)]`；Role surge 约为 `N × Σ[(Rᵢ+Sᵢ)×(1+Wᵢ)]`，其中只有实际变更的 Role 才需要 Sᵢ。A/B worker 数不同或正在终止时，需按每个实际版本逐项累加。

### 5.3 零/单副本及无实际变更

| ID | 组合 | 预期行为与现行约束 |
| --- | --- | --- |
| Z01 | N=0，任意合法顶层 budget/P | 无 SG/Pod；更新模板只是预置，不应产生 surge。仍执行 schema/Role/dependency 校验 |
| Z02 | N 从 0 扩至 >0 | 按最新目标和有效 partition 创建；若没有历史 A，初次创建直接使用当前 B，不应等待不存在的 A |
| Z03 | Role R=0，Role U 省略 | CRD 默认 U=1，现行 Role validator 因 1>0 拒绝；“R 可为 0”不等于省略 budget 的组合可提交 |
| Z04 | Role R=0，显式 U=0 | 0/0 且无 updateable 实例允许；还须满足 gang minimum、dependency update 容量校验 |
| Z05 | D=1，默认 U=1,S=0 | 允许完整替换，但期间该粒度容量为 0；不是高可用模式 |
| Z06 | D=1,U=0,S=1，无依赖保留要求 | 新 Ready 再删旧，具备零主动容量损失的前提 |
| Z07 | D=1,P=1 或正百分比 P | 连续稳定实例全部被保护；不存在“20% 只保留小部分” |
| Z08 | spec 反复 apply，template 语义无变化 | 不应更新 Pod UID、启动时间、IP 或版本标签；不得仅因 S>0 产生额外容量 |
| Z09 | 只有副本数或 budget/P/coordination 变化 | 无新业务模板；按最新容量/策略处理已有未完成工作，不应制造一次新的全量模板滚动 |

## 6. Partition 与 ordinal：不能遗漏的组合

实际代码使用 `ordinal < P` 保护实例。API 注释“保护升序排列后的前 N 个现存副本”与该实现不等价；质量验收按明确的 ordinal 规则断言，并把注释差异登记为文档问题。

| ID | O、D 和 P | 可更新集合/预期行为 |
| --- | --- | --- |
| P01 | O={0,1,2},D=3,P=0 或省略 | 所有旧实例都可更新 |
| P02 | O={0,1,2},D=3,P=1 | 保护 0，更新 1/2 |
| P03 | O={0,1,2},D=3,P=3/4 | 没有可更新旧实例，合法暂停 |
| P04 | O={0,3,4},D=3,P=1 | 保护 0，更新 3/4；不能按数组下标或填洞冲动删除健康实例 |
| P05 | O={0,3,4},D=3,P=3 或100% | **仍可更新 3/4**；P=D 不保证全保护 |
| P06 | O={0,3,4},D=3,P=4 | 仍可更新 4；全保护须覆盖所有现存 ordinal，而非只大于 D |
| P07 | P05/P06 + U=0,S=0 | admission 因 D<=P 可能允许，但实际仍有 E，预算无前进手段；待验证的校验/运行时不一致 |
| P08 | O={0,2,3},D=3，没有 template 或 count 变化 | 数量满足，保留 O；不能为了补齐 1 或消除 3 发起滚动 |
| P09 | 更新期间 P 降低 | 新解锁的旧 ordinal 可继续更新；无须再次修改业务模板 |
| P10 | 更新期间 P 提高 | 对未来动作扩大保护；已经发送的删除不能撤销，已到 B 的实例不会因此自动回滚到 A |
| P11 | P 用百分比，运行中扩缩 D | P 重新向上取整；保护集合可能变动，不能固定为首次 rollout 的 P |
| P12 | 保护实例缺 Pod/实例 | 应从正确历史模板补齐；保护不等于不恢复；历史不可用时安全报错重试 |
| P13 | SG P>0，同时改 Role replicas 和模板/W | 受保护组：历史模板及历史 W + 最新 Role replicas；未保护组：最新模板/W + 最新 replicas |
| P14 | 真实缩容必须移除受保护实例 | partition 优先保护滚动候选，不禁止期望数缩小；非保护先删，不足时可删保护实例 |

P13 的 manage、readiness、PodGroup、Ranktable 必须使用同一“历史模板 + 最新 Role 数”投影，否则会再现旧代码 `2/3` 永远不收敛。

partition 稳态有两类：SG mode 可以在 protected group=A、其他=B 时停止 rollout；Role mode 只要组内仍有 protected 旧模板，aggregate revision 可保留 A。验收首先确认全部允许更新的实例已完成、容量恢复和保护边界正确，不能对两类稳态统一强制 `currentRevision==updateRevision`。

## 7. Role coordination：默认、可行组合与完成性

### 7.1 配置存在性的全部组合

以下仅适用于 Role mode。`roles=∅/[]` 均选全部，`dependencies=∅/[]` 均无依赖边。K 表示 maxSkew。

| ID | roles | K | dependencies | 结果 |
| --- | --- | --- | --- | --- |
| C-OFF | coordination 整体不存在 | — | — | 独立 Role 滚动，没有默认的比例/依赖限制 |
| C000 | ∅ | ∅ | ∅ | 空对象拒绝：K 必填 |
| C001 | ∅ | ∅ | 配置 | 拒绝：不能只配置依赖而省略 K |
| C010 | ∅ | 配置 | ∅ | 全部 Role 参与比例控制，无依赖边 |
| C011 | ∅ | 配置 | 配置 | 全部 Role 参与比例及依赖控制 |
| C100 | 配置 | ∅ | ∅ | 拒绝：K 必填 |
| C101 | 配置 | ∅ | 配置 | 拒绝：K 必填 |
| C110 | 配置 | 配置 | ∅ | 指定集合比例控制；集合外独立 |
| C111 | 配置 | 配置 | 配置 | 指定集合比例及依赖控制；集合外独立 |

每个允许行还必须满足：K 为百分比字符串 `1%..100%`；至少两个不同的合法 Role 名；图中所有端点在参与集合中；无重复、自依赖和环。当前 CRD 最多 4 种 Role，图测试覆盖 2/3/4 个节点即可，副本数不受这一“Role 种类数”限制。

### 7.2 与 U/S/P 的组合关系

coordination 按 ServingGroup 独立计算；其输入包含当前版本的 Role 状态、旧 revision 的副本基线和用户 partition。

```text
stableEndᵢ = min(旧副本基线ᵢ, 当前 Rᵢ)
Tᵢ = max(stableEndᵢ - Pᵢ, 0)
progressᵢ = Ready 的目标版本稳定实例数 / Tᵢ
allowᵢ = min(Tᵢ, ceil((最慢活动 progress + K) × Tᵢ))
```

仅剩一个有待更新的参与 Role 时，不再由其他已完成 Role 施加比例限制。`startedCount` 包括已预留/删除旧稳定实例后的启动进度，不能只统计 Ready Pod。

正常执行关系如下，顺序表示约束的合成，不表示所有 Pod 必须严格串行创建：

1. 用户 P 设定本次允许更新的范围。
2. 处在请求入口的 root Role（没有其他 Role 依赖它）的第一次目标版本启动，要等待依赖闭包有目标版本 Ready 容量。中间 Role 可同时起步，从下游向上游逐步 Ready。
3. K 生成动态的 effective partition 和稳定替换的剩余删除额度；它不是全体目标版本 Pod 占比的硬上限。
4. 仍有旧直接调用方时，dependency 至少保留一个旧副本；用户 P>0 已保留的旧路径也要计入。
5. Role U 限制可消耗的可用容量；Role S 限制额外实例。所有适用约束共同限制动作。

**surge 与 K 的区别**：现行 creation 路径主要用 dependency 的 `allowTargetStart` 放行额外实例；K 通过稳定范围的替换预算限制推进。因此可能已经有多个高 ordinal 的 B surge Pod，但稳定版本推进仍受 K 限制。不能用“B Pod 总数 / 实际 Pod 总数 <= K”作为断言。

小副本量时存在量化：T=2,K=10%，最慢 Ready=0，允许启动数为 `ceil(0.1×2)=1`，一次即是 50% 稳定进度。K 表示计算额度使用的百分比点差，受向上取整影响，不是小副本下绝对精确的比例差。

### 7.3 可行运行模式与边界

| ID | 组合 | 预期行为/可行性条件 |
| --- | --- | --- |
| C-R01 | 无 dependency，所有参与 Role 为默认 U=1,S=0 | 比例控制分批先删后建；默认 Role replicas=1 时可发生所有相关 Role 暂时不可用 |
| C-R02 | 无 dependency，U=0,S>0 | 各 Role 可先准备 surge；Ready 释放稳定替换预算；完成后回到 R |
| C-R03 | 无 dependency，U>0,S>0 | hybrid + K 稳定替换限制；不能超 Role budget |
| C-R04 | A→B，两者变更，B 稳定容量足够 | B 先建立稳定范围内的目标 Ready 容量，A 再首次启动目标；旧 A 消失前保留旧 B |
| C-R05 | A→B→C 链 | root A 等依赖闭包；B/C 可以起步，不能简单断言每条边上的所有创建完全串行 |
| C-R06 | A→{B,C} 分叉 | A 首次启动须同时具备 B/C 的目标 Ready 容量；任一下游未满足都不得越过 gate |
| C-R07 | {A,B}→C 汇聚 | 两个旧调用方均消失前保留旧 C；不能只检查某一个 caller |
| C-R08 | A→{B,C}，B/C→D 菱形 | 去重依赖闭包、保留共享旧 D、按 Role budget/K 推进；禁止环 |
| C-R09 | 只改 caller，dependency template 未变 | 语义等价且 Ready 的 dependency 可满足新请求路径，不为统一 revision 标签重建它 |
| C-R10 | 只改 dependency，caller template 不变 | 等价 caller 不应仅因旧 revision 标签而被判定必须重建；dependency 可按自己的budget推进。另测 caller 明确保留旧模板的停点，见 C-R13 |
| C-R11 | 只改集合外 Role | 该 Role 独立 budget；参与集合不应被无关变更拖进全量滚动 |
| C-R12 | 有参与 Role 完全被 P 保护 | 比例总量排除保护范围；若它还是发生变化的必需 dependency，update admission 可能拒绝其缺少目标容量 |
| C-R13 | caller 的 P>0 保留旧版本 | 即使 caller 的 partition 外已完成，旧路径仍存在，dependency 可能继续保留旧副本；这是目标约束，不能靠强制删旧解锁 |
| C-R14 | Role R 不同、P 不同 | 比较各自 T 上的归一化进度，不能比较原始副本数或同一绝对 P |
| C-R15 | K=1%、小 T | 至少一个离散启动额度；验证量化公式，避免误判违反比例 |
| C-R16 | K=100% | 比例限制基本放开；dependency readiness 和旧路径保留仍生效，不等于 C-OFF |
| C-R17 | 两组及以上 N>1 | 每组单独满足依赖/K；某组阻塞不应消耗另一组的独立 Role budget |
| C-R18 | 高 ordinal surge Ready，尚无稳定范围目标 Ready | 不足以放行 root caller；surge 是额外容量，不自动成为 dependency stable slot |
| C-R19 | 终态保留高 ordinal，例如 R=2、O={1,2} | 验收要求稳定容量已由正确 B 满足后自动完成；现行 coordinator 固定 ordinal 计数存在收敛风险，见 §11 |
| C-R20 | 没有任何 template 变化，仅配置/移除 coordination | 不应重建健康 Role；已有 rollout 则按新策略继续，移除后不再承诺原依赖保护 |

C-R09/C-R10 中版本等价由该 Role 自身模板及历史状态判定，不是比较 Pod 的全局 revision 标签。须分别测试“未变 Role 仍为等价 target”和“Role 明确保留另一个旧模板/受 P 保护”两种状态。业务 RPC 兼容性本身不由 readiness probe 自动证明。

### 7.4 dependency 容量的 create/update 差异

创建一个只有单副本 dependency 的对象可以通过；后续更新该 dependency template 可能被拒绝。对需要保留旧路径的 dependency，当前 update validator 计算：

```text
retainFloor = 需要保留旧路径 ? 1 : 0
replacement = max(min(oldR,newR) - max(P,retainFloor), 0)
expansion   = max(newR - max(oldR,P), 0)
需要 P < newR 且 replacement + expansion > 0
```

| ID | 组合 | 结果/预期 |
| --- | --- | --- |
| C-A01 | oldR=newR=1,P=0，依赖需同时留旧和出新 | 拒绝；加 S=1 不改变 stable capacity 检查 |
| C-A02 | oldR=newR=2,P=0 | 有一个替换槽；U/S 还要提供实际前进手段 |
| C-A03 | oldR=1,newR=2，同时改 template | 可由 expansion 提供目标稳定槽；最终仍需 runtime/K/Ready 验证，不应笼统说必须两次提交 |
| C-A04 | 先扩 1→2 并 Ready，再改 template | 明确可复现的分阶段路径，旧 baseline 已更新 |
| C-A05 | P>=newR 且该 dependency 需变更 | 拒绝，即使 U/S 为正 |
| C-A06 | active rollout 中缩 dependency 或提高 P，移除目标启动槽 | update 校验应拒绝不再可行的容量配置；不能只验证 create |
| C-A07 | graph 新增/移除边、参与集合变动 | 静态图校验 + active rollout 容量检查 + 运行中重算；合法提交不代表此前的依赖保护承诺仍保留 |

## 8. recovery、eviction、调度与插件的组合

### 8.1 全部 recoveryPolicy 配对

| recoveryPolicy | SG mode | Role mode | 单个 Pod 故障/外部删除的恢复含义 |
| --- | --- | --- | --- |
| 省略 | 允许，有效 RoleRecreate | 允许，有效 RoleRecreate | 按 Role 恢复 |
| RoleRecreate | 允许 | 允许 | 同一 Role 实例内 entry/worker 一起恢复 |
| ServingGroupRecreate | 允许 | 拒绝 | 同 ServingGroup 一起恢复；与独立 Role 滚动不兼容 |
| None | 允许 | 允许 | 活着的容器重启由 kubelet 处理；真正缺失/终态 Pod 可被补建 |

recoveryPolicy 控制故障恢复粒度，rollout type 控制主动模板更新粒度。默认 `RoleRecreate+SG` 是合法组合。现行不兼容报错只列两对合法值，遗漏 `RoleRecreate+SG` 与 None，是文案问题。

每个允许配对都要与 §5 的先删后建、纯 surge、hybrid、partition 组合；再分别注入 entry/worker 故障。故障可能已经使容量跌破 `D-U`，此时验收的是“不再越过预算主动删除额外健康实例、恢复后继续”，不能承诺 U 会防止节点宕机。

### 8.2 evictionStrategy 配置与省略

| ID | 配置 | 预期 |
| --- | --- | --- |
| E01 | 完全省略 | 不提供 ModelServing eviction budget 保护；rollout 仍用 U/S/P |
| E02 | SG protection + minAvailable | 保护外部 eviction 的完整 Ready SG 数；不成为 Role/SG rollout 的第二个 U |
| E03 | Role protection + roleMinAvailable | 对指定 Role readiness 的 eviction 保护；同样独立于 rollout budget |
| E04 | eviction minAvailable=D，rollout U>0 | rollout 仍可主动替换；不能认为 minAvailable=D 会冻结 rollout |
| E05 | eviction minAvailable 较低，rollout U=0,S>0 | 主动滚动仍不能先删健康旧实例；eviction 允许不等于 rollout 放宽 |
| E06 | rollout 与 drain 同时发生 | 两条路径共同改变实际容量；不能宣称两个独立 budget 自动构成单一全局上限。须检查共享 readiness、in-flight eviction 计数和健康副本选择 |

evictionStrategy 对象存在但缺少对应 minAvailable/roleMinAvailable 会被拒绝；minAvailable 不得超过其期望副本数。基线没有因 eviction 等待而自动启动通用 surge 的保证，不能把尚在设计的 proactive eviction surge 当作现有能力。

### 8.3 调度/插件组合

| ID | 条件 | 预期及验收重点 |
| --- | --- | --- |
| X01 | gang 全量默认或显式部分 minimum | PodGroup 期望与当前有效模板/Role 数一致；rollout 不得因旧 PodGroup 阻止新版本调度 |
| X02 | S>0，但 GPU/CPU/配额/拓扑容纳不了额外实例 | U=0 时等待；U>0 可以合法消费 U，但不能突破下限清空旧版本 |
| X03 | A/B 的 workerReplicas 不同 | 新旧 Role 分别按自己的布局判 Ready，不能把旧 Role 套入新 W 后无限恢复 |
| X04 | SG partition + Role 扩缩 + worker 布局变更 | PodGroup 必须与“历史 W + 最新 R”一致；与 P13 同一强制组合 |
| X05 | 无插件 / Headless Service / Ranktable / 二者都有 | 更新策略不因插件选择变更；创建、删除、恢复、surge 清理均执行适用 hook |
| X06 | 插件 hook/API 失败或重试 | 不重复创建活跃实例，不丢失删除重试，不泄漏旧/surge 资源；错误恢复后自动推进 |
| X07 | gang minimum 或 networkTopology 运行中改变 | 先检查 immutable 规则；现有 `minRoleReplicas` 值不可修改，已设置 gangPolicy 不可移除；networkTopology 不可更新 |

## 9. 哪些变更应触发滚动，哪些只影响策略或容量

静态 YAML 的排列组合不足以覆盖异常滚动。每个允许模式都要再与变更类型组合。

| ID | 用户动作 | SG mode 预期 | Role mode 预期 |
| --- | --- | --- | --- |
| T01 | 只修改一个 Role 的 image/command/env/resources/probe/Pod template metadata | eligible SG 整组换模板 | 只替换这个 changed Role |
| T02 | 同时修改多个 Role 模板 | eligible SG 整组换模板 | 各 changed Role 依其 budget；有 coordination 时再受集合/图限制 |
| T03 | 改 workerReplicas 或 workerTemplate | 视为 template 变化 | 该 Role template 变化，实例内 entry/worker 按有效版本布局恢复 |
| T04 | 只改 N | 扩缩 SG；无理由替换未被缩容选中的健康 UID | 同左；不应诱发各 Role 无关滚动 |
| T05 | 只改一个 R | 所有保留组包括 protected group 按最新 R 扩缩 | 对该 Role 扩缩；其他 Role UID 保持 |
| T06 | 同一提交改 N/R 和模板 | 按最新容量与版本规划，不能把规模变化当成整个旧群必须重建 | 同左；coordination 使用正确前后副本基线 |
| T07 | 只改 U/S/P | 不生成新模板版本；但会改变当前未完成 rollout 的可行动作和临时容量 | 同左，对对应 Role 生效 |
| T08 | 只改 K/依赖/参与集合 | SG 不允许 coordination | 重算门控，不为策略本身重建等价模板 |
| T09 | 只改 recovery、eviction、restartGracePeriod | 不应触发模板滚动；未来对应动作使用最新策略 | 同左 |
| T10 | 只改 ModelServing 外层 labels/annotations | 无 Pod 模板变化；不应滚动 | 同左 |
| T11 | 只改 schedulerName 或顶层 plugins/config | 基线 revision 只覆盖 Role 模板，不能期待自动滚动所有存量 Pod；插件动态同步由 hook 决定 | 同左；若业务要求所有存量应用，必须单独定义发布动作 |
| T12 | 只重排 Role 列表、nil/空值等语义等价变换 | 基线 raw hash 可能变化；验收目标是不因纯表示变化误删健康 Pod，须与 bug016 判等一起验证 | 各 Role 模板相同应保留 UID；不能为了统一 group hash 重建 |
| T13 | 新增 Role | 最新未保护组应包含新 Role；保护组维持历史成员集合 | 新建该 Role，已有等价 Role 保持；coordination/gang 引用必须合法 |
| T14 | 删除/重命名 Role | 最新组成员变化；保护组遵守历史模板 | 删除旧成员、新名称视为新增；现行删除不存在于 spec 的 Role 可直接执行，不受已移除 Role 的旧 U/P 常规预算保护 |
| T15 | SG→Role 或 Role→SG 模式切换 | 按目标粒度接管未完成工作，不能只凭 type 变化启动无关滚动 | 同左；提交中必须清除目标 mode 禁止的字段 |
| T16 | A→B 未完又改 C | 最终收敛到最新期望 C，旧 A/B 都有正确历史和预算 | 逐 Role 识别 A/B/C，不把 group 多数 revision 当成每个 Role 的历史 |
| T17 | A→B 后回滚 A，含只回滚部分 Role | 回滚也是受预算/partition 约束的模板变更 | 未回滚 Role 保持自身版本；正确使用各 Role 所引历史 |

production 当前 `ModelServingRevision()` 只对 Role 列表算 hash，并排除 Role replicas 与内联 U/S/P；`CalRoleTemplateHash()` 同样排除 replicas/U/S/P。外层 scheduler/plugins 不在该 hash 中，不能因为文件中存在另一套 revision helper 就假设 controller 已使用它。

T14 是“成员删除”而非固定成员下的普通滚动。禁止在验收中用 U=0 推断“显式从 spec 删除 Role 后仍必须保留它”；需要将用户明确移除容量与 controller 意外删除分开记录。

## 10. 运行中修改、失败与恢复的必测序列

下列序列与 SG/Role 三种推进模式交叉。序列在所有无关字段不变、完整保存前后 spec 的前提下执行。

| ID | 状态/动作序列 | 验收要求 |
| --- | --- | --- |
| L01 | Ready A → 更新 B → 全部 Ready | 正常预算、保护边界、最终容量及 revision 状态一致 |
| L02 | 新 B 长期 Pending / ImagePullBackOff / Running 但 NotReady | 停在预算允许的边界；不得把 Running 当 Ready；修复镜像/就绪后自动继续 |
| L03 | B Ready 后又 NotReady | 撤回其可用贡献；暂停需要它的健康旧实例删除；恢复后继续 |
| L04 | 旧 A 非保护实例故障 | 故障恢复和 rollout 不能重复创建/重复消费同一实例；不主动扩大故障范围 |
| L05 | protected A 故障或缺失 | 按历史模板恢复，不能趁故障突破 P 应用 B |
| L06 | 删除 entry、删除 worker、同时删除二者 | recovery 粒度正确；延迟/重复 tombstone 不得把刚补建的新 UID 当成旧 UID 再删 |
| L07 | Pod 卡 terminating、有 finalizer、插件删失败 | 等删除确认并重试；不得因缓存中暂时缺失就忘记删除意图，或无限保留 Deleting 状态 |
| L08 | N:3→1→3，第二次修改发生在旧 Pod 删除中 | 最终期望 3 自动补齐；只删除第一步已选择对象，不误删新 UID |
| L09 | R:3→1→3，同样快速往返 | 同一 Role 自动恢复完整实例；与别的 Role 的新旧版本不相互污染 |
| L10 | S:1→2 或 2→0，更新进行中 | 按最新 S 重算临时容量，避免双重 surge；收缩不应突破需要保护的健康容量，边界应验证 |
| L11 | U:1→0，保留 S>0；或 U:0→1 | 之后的新主动动作服从新预算；已发起删除不可撤销，不能用改配置瞬间重置 in-flight 计数 |
| L12 | U/S 同时改为0/0，P仍允许更新 | 拒绝；连同 P 提高到无可更新范围时测试暂停与稀疏例外 |
| L13 | P降低/提高/删除，或百分比P随扩缩变化 | 只改变允许范围；不会自动反转已完成/已发出的更新 |
| L14 | 开启/关闭 coordination、调K、增删边/参与Role | 静态/active update 校验正确；不丢失已有启动 reservation，不重复释放旧路径保护 |
| L15 | controller 停在启动surge后、旧实例删除中、最后cleanup后，逐阶段重启 | 从持久化事实恢复到等价状态；不超预算，不永久等待，不重滚已经完成的模板 |
| L16 | controller leader切换，重复/乱序 watch 事件 | 幂等，不重复删除，不依赖某个事件恰好只来一次 |
| L17 | informer暂未看到MS/Pod、API List失败、一次Create/Delete/Status失败 | 不能把错误当空集合或已完成；返回可诊断错误并自动重试 |
| L18 | 扩缩与模板变更/回滚交错，A/B/C混合 | 按最新期望及每个实例所引历史决策；终态不要求各Role所有Pod revision标签相同 |
| L19 | 所有模板目标已Ready，最后一个旧/临时实例消失 | 无手工干预完成状态晋升和清理；特别覆盖高ordinal保留 |
| L20 | N降至0，再改template、再扩容 | 不保留幽灵SG/Role/deleting记录；恢复使用可追溯的正确模板 |
| L21 | 同名ModelServing删除后重建、旧UID的Pod/CR仍在 | 归属按owner UID判定；不得采用旧对象的历史/就绪状态作为新对象证据 |
| L22 | 正常rollout同时drain/故障恢复/插件变更 | 分别记录删除原因与预算来源；任何reconcile重入不能扩大原动作授权的实例集合 |

“最终收敛”只有在调度资源、Ready条件、外部finalizer及API可用等前置条件恢复后才要求有界完成。外部条件尚未满足时，通过标准是稳定等待、原因可诊断、没有反复删除健康旧实例。测试超时取自明确的重试/探针/终止窗口，不把长期轮询后重启 controller 当成通过。

## 11. 历史异常 → 质量不变量 → 必须覆盖的组合

### 11.1 无意滚动和 ControllerRevision

| ID | 已分析异常/风险 | 质量加固必须成立的预期 | 必测组合 |
| --- | --- | --- | --- |
| H01 | [bug016：controller 升级导致同一业务模板 hash 漂移](../../bugs/016-upgrade-sparse-ordinals-pod-recreation-DONE/DESCRIPTION.md) | 历史模板语义相等时保留旧 Pod 名称、UID、启动时间、IP、原 revision/hash 标签；不因版本变化重建 | SG/Role；连续/稀疏 O；无/有 P；默认U/纯surge/hybrid；升级后再重启 |
| H02 | 将 sparse ordinal 当作缺副本/临时副本处理 | 数量正确且模板无变化时不归一化名称；真正欠副本才分配新实例 | O={0,3,4} 与连续控制组；P=0/1/D；只改controller/只改副本/真实改模板三类分别验证 |
| H03 | 升级兼容逻辑按“Running老Pod”永久豁免 | 等价性与健康、删除意图独立判断；真实模板变更、缩容选中、故障恢复仍执行 | H01基线 + NotReady、实例不完整、已经Deleting；升级后实际滚B验证不被永久保留 |
| H04 | [bug021：独立Role滚动缺少目标ControllerRevision](../../bugs/021-role-rollingupdate-controllerrevision-DONE/DESCRIPTION.md) | 任何新模板实例创建或旧模板主动删除前，目标历史可读取且归属正确 | C-OFF/C-ON；默认U/纯surge；P=0/部分/全保护；N=0/1/多组 |
| H05 | Role部分更新后只保留group revision，删掉未变Role仍引用的历史 | 保留 current/update 及所有live SG/Role/Pod引用；包括NotReady、terminating和surge | 只改decode，prefill不变；A/B/C混合；historyLimit=0/默认10；状态晋升后重启 |
| H06 | policy/replica变化使同名CR.Data不同，误当冲突或改写历史 | 同模板复用不可变历史，mutable replica baseline单独更新；真实冲突安全失败 | T05/T07/T08 + coordination；AlreadyExists竞态；错误owner UID/损坏数据 |
| H07 | protected恢复时历史缺失，静默套用最新模板并标旧revision | 历史不可靠时不得伪造旧版本Pod或凭hash差异授权删除；可诊断重试 | P12/P13；历史缺失/不可读/缺Role/owner错误；恢复后自动继续 |
| H08 | 保留策略只看历史条数或只看Ready实例 | live引用永远优先于historyLimit；读取引用失败不视为空 | A/B/C、old termination、surge、partial update；limit=0/1/默认10 |

H01/H04/H05 在本工作区已有对应 fix 分支及独立验证记录。本文将它们作为后续每次发布的回归要求，不将旧分支缺陷反向当作新代码一定仍有问题。

### 11.2 计数、恢复与推进

| ID | 已分析异常/风险 | 必须守住的预期 | 覆盖入口 |
| --- | --- | --- | --- |
| H09 | [bug013：partition保护组沿用旧Role replicas，持续2/3](../../bugs/013-role-replica-mismatch-recovery-DONE/DESCRIPTION.md) | 历史模板+最新R在manage/readiness/PodGroup中一致；重试能真实补齐 | P13、X04、T06；W也变化；protected实例缺失再恢复 |
| H10 | [bug008：快速缩后扩缺Pod](../../bugs/008-manual-replica-scale-pod-missing-DONE/DESCRIPTION.md) | deleting意图与最新desired共存，最终补齐；不重复使用未终止名称 | L08/L09；SG/Role；默认/纯surge；混合A/B |
| H11 | [bug010：RoleDeleting卡住](../../bugs/010-role-deleting-recovery-hardening-IP/DESCRIPTION.md) | cache滞后/事件丢失后能确认删除并重新入队，插件清理可重试 | L06/L07/L15/L17；entry+worker、多Role、P保护 |
| H12 | [bug014：正U百分比小副本向下到0](../../bugs/014-maxunavailable-percent-minimum-one-DONE/DESCRIPTION.md) | admission与controller使用一致的有效预算；保留production的SG最小1/Role不clamp区别，或另行评审统一 | V04；SG和Role分别测D=1/3/6；S省略/0/正；P=0/全保护 |
| H13 | [bug002：N=0被partition校验错误拒绝](../../bugs/002-replica-0-DONE/DESCRIPTION.md) | N=0可预置合法模板，随后正常扩容；无surge/幽灵对象 | Z01/Z02/Z04；0/0预算；P=0/正；snapshot存在性 |
| H14 | [task020：协调Role完成后状态不晋升](./PROPOSAL_COMMIT.md) | 所有允许更新实例完成、容量清理后无需重启自动完成；重启前后得出相同完成性 | C-R19/L19；O={0,1}、{1,2}；C-OFF作为对照；S=0/1；不同R/P |
| H15 | ordinal partition与“前N个现存实例”注释不一致 | 明确并固定同一语义；admission暂停判据和runtime候选一致 | P04/P05/P06/P07；U=0,S=0以及有surge对照 |
| H16 | protected/旧NotReady容量漏扣、跨路径重复使用budget | 不再主动扩大已低于可用下限的健康容量损失；in-flight动作不能重复计数 | L02/L03/L04/L05/E06，配合U=0/1、S=0/1 |

H14 的源码推导比上一轮“可能缺少重入”更具体：假设旧/新 R=2、P=0，最终留下目标版本 Role ordinal `{1,2}`。`resolveRoleRolloutState` 仍取 `stableEnd=2,T=2`，仅 ordinal 1 计入 `stableTargetReady`，ordinal 2 因 `ordinal>=desired` 被跳过，于是 `readyCount=1<T=2`，`inProgress=true`。`rolesToDeleteForRoleRollingUpdate` 用该 inProgress 初始化 hasOutdatedRoles，即使没有旧模板，也不能晋升 group revision。controller 重启后从全B的Pod恢复group revision，可能绕开旧revision的coordination计算，才表现为完成。

这是由已观察的终态和共用算法得到的原因推导，尚未在本轮新增定向单测。加固应验证“最终保留的高ordinal合法实例”与“比例推进使用的稳定身份”如何统一，不能只通过增加requeue掩盖计数模型冲突。

### 11.3 状态必须分层验收

| ID | 情况 | 状态判据 |
| --- | --- | --- |
| S01 | 无partition、资源清理完毕、全部目标Ready | replicas/available/updated等于N，current/update晋升到相容目标，coordination为RolloutComplete |
| S02 | SG partition保留A | updated按实际B组计数，available可为N，current/update允许不同；无可更新旧组时不得继续无意义删除 |
| S03 | Role partition保留A | 有旧Role的group可不算updated；available仍按完整SG readiness统计；用“允许集合完成”判断合法停点 |
| S04 | 未变Role保留A标签，changed Role已B | 模板等价的未变Role不阻碍组完成；A历史仍需保留，不要求强制改标签 |
| S05 | 有surge或terminating | total、available、updated可暂时不等于desired，不能提前宣告资源已完全收敛 |
| S06 | current行为Available=False但availableReplicas=N | 现行Available与UpdateInProgress互斥；记录该语义，监控不要只看条件名判断物理容量；是否解耦需另行API语义评审 |

S06 不应直接变成“本轮要求改Available”的实现决定。文档先固定两类断言：实际可用容量、发布进度；随后产品状态语义可以独立评审。

## 12. 可复制的配置模板与省略字段示例

下面使用 production API，省略字段的例子刻意保持省略。可执行测试 YAML 继续保存在 `kind/`，运行测试应使用带 readiness/资源限制和可区分A/B模板的完整fixture。

### 12.1 完全默认的最小对象

```yaml
apiVersion: workload.serving.volcano.sh/v1alpha1
kind: ModelServing
metadata:
  name: rolling-default
spec:
  template:
    roles:
      - name: serving
        workerReplicas: 0
        entryTemplate:
          spec:
            containers:
              - name: serving
                image: busybox:1.36
                command: ["sh", "-c", "sleep 3600"]
```

有效值：N=1，Role R=1，scheduler=volcano，recovery=RoleRecreate，SG mode，U=1/S=0/P=0。更新这个单组对象允许短暂没有可用组。`workerReplicas: 0` 必须显式写，不能作为默认省略。

### 12.2 SG只写surge与显式零不可用的差别

```yaml
# M010：有效U=1，属于hybrid
rolloutStrategy:
  rollingUpdateConfiguration:
    maxSurge: 1
```

```yaml
# M110：有效U=0，先有新Ready容量再主动删健康旧组
rolloutStrategy:
  type: ServingGroupRollingUpdate
  rollingUpdateConfiguration:
    maxUnavailable: 0
    maxSurge: 1
```

```yaml
# M111：三组连续ordinal中保留group0
replicas: 3
rolloutStrategy:
  type: ServingGroupRollingUpdate
  rollingUpdateConfiguration:
    maxUnavailable: 1
    maxSurge: 1
    partition: 1
```

### 12.3 Role全部默认、不同Role混合预算

```yaml
# 两个Role都不写budget时，各自默认U=1/S=0/P=0
rolloutStrategy:
  type: RoleRollingUpdate
# template.roles中的replicas可分别设置，budget可完全省略。
```

```yaml
# 以下为spec片段；entryTemplate等完整内容见保留fixture
rolloutStrategy:
  type: RoleRollingUpdate
template:
  roles:
    - name: frontend
      replicas: 3
      maxUnavailable: 0
      maxSurge: 1
      partition: 1
      # entryTemplate + workerReplicas 必填内容略
    - name: backend
      replicas: 3
      # U/S/P全部省略：backend仍是1/0/0，不继承frontend。
      # entryTemplate + workerReplicas 必填内容略
```

### 12.4 比例协调与依赖协调

```yaml
# 全部Role参与，只限制比例；没有隐式dependency
rolloutStrategy:
  type: RoleRollingUpdate
  roleCoordination:
    maxSkew: "25%"
```

```yaml
# 指定集合 + dependency；预算仍在每个Role内设置
rolloutStrategy:
  type: RoleRollingUpdate
  roleCoordination:
    roles: [frontend, backend]
    maxSkew: "50%"
    dependencies:
      - role: frontend
        dependsOn: [backend]
```

上述依赖场景使用frontend/backend各R=2、U=0、S=1是可启动的代表配置。只有R=1时不能假设加surge就通过依赖template更新校验。完整fixture：[v1](./kind/role-coordinated-v1.yaml)、[v2](./kind/role-coordinated-v2.yaml)。

## 13. 如何生成完整测试组合，而不遗漏默认或非法配置

### 13.1 配置分类规则

对任何待测 YAML，按下面顺序生成场景标识和预期：

```text
原始请求形态D/M/C
 → CRD默认化后的有效Spec
 → 字段归属/枚举/数值/schema校验
 → budget的9种输入类B及数值边界V
 → actual ordinal的P类（含稀疏）
 → recovery合法配对
 → Role coordination集合/图/容量可行性
 → 本次变更T与当前生命周期L
 → 前/中/后状态S + 历史/质量不变量H
```

schema/webhook拒绝的组合记录“预期拒绝且旧spec/UID不变”；可提交但有合法保护目标的组合记录“预期停在保护边界”；外部依赖不足记录“等待并能恢复”；其余要求自动完成。发现源码与验收要求冲突时标为待修缺陷，不能把期望改成异常结果。

### 13.2 必须做全交叉的核心维度

1. **SG和Role分别覆盖M000…M111全部8种字段存在性。** 每个显式字段再覆盖V02…V11相关值类，省略由V01覆盖。特别断言“写S省略U”仍为U=1。
2. **每个粒度覆盖B01…B09 × P省略/显式0/部分保护/连续全保护。** 原始省略与显式值即使运行等价，也要独立检查API默认化。
3. **每种预算主模式覆盖D=0/1/3/6、整数/百分比、不整除与临界边界。** Role D=0的默认U拒绝独立于SG N=0；验证同名字段两层差异。
4. **Role的每种允许主模式交叉C-OFF、C010/C110、C011/C111。** 同构Role之外，再覆盖两Role不同M/B/P/R；不能只测所有Role预算相等。
5. **所有允许recovery配对交叉三个推进模式与有/无P。** None需要分别测容器重启和Pod真正删除。
6. **C011/C111交叉2/3/4节点DAG形状、单/多SG、相同/不同比例副本、P=0/部分/全保护。** C-A01…C-A07覆盖create和update差异；图或容量不合法的组合不进入运行阶段。
7. **H01/H04/H05/H14/H15全交叉默认/纯surge/hybrid、连续/稀疏、C-OFF/C-ON适用项。** 这些是历史误滚动/不收敛的强制防回归组合，不能仅采用pairwise抽样。

其余插件、调度、独立瞬时API故障可以在上述基线上按风险交叉；但每个L事件都必须落到至少一个确定的scenario，而不是只在文档上打勾。A/B布局变化、删除中重启、多版本历史丢失和混合预算按本文指定的强制组合执行。

示例标识：`ROLE-M011-B03-P04-C111-T06-L15`。场景记录必须带每个Role的实际参数、K、图、版本和ordinal，不能只保存这个类别ID。

### 13.3 每个运行用例的统一记录与断言

| 阶段 | 必留证据 | 必查内容 |
| --- | --- | --- |
| Before | 原始v1 YAML、server-defaulted Spec、Pod/Role/SG身份、CR.Data及owner、镜像commit | 有效默认值、A模板、稳定容量、初始ordinal集合 |
| Trigger | v2/v3或patch、旧/新generation、故障注入目标UID和时间 | 变更属于T哪类；预算/partition是否重新计算 |
| During | 连续watch/轮询、删除时间戳、事件/日志、Ready与terminating分别计数 | 容量上下限、精确保护实例、dependency/K、禁止无意滚动、历史引用完整 |
| After | 最新Spec、全部实际模板/身份、status、CR、插件资源 | 允许目标完成、期望容量、正确版本来源、无残余Deleting/surge泄漏 |
| Recovery | controller重启/故障恢复前后相同证据 | 相同业务意图下等价终态；已完成健康实例UID不额外改变 |

K比较使用参与Role稳定推进模型及量化公式，不能用Pod Ready总数替代。旧Pod的UID/IP保持是“无意滚动”用例的核心断言，不能仅看最终副本数正确。历史清理必须检查Data/owner/引用关系，不能只看CR名字或条数。

### 13.4 已有证据与新增覆盖的边界

| 类别 | 当前证据 | 本文新增的覆盖要求 |
| --- | --- | --- |
| SG默认、SG U+S+P、Role默认部分更新、Role S+P | task020有5场景中的4场景Kind记录和v1/v2 YAML | 补齐存在性、百分比、零副本、稀疏P、运行中修改 |
| coordination + S + dependency + K | task020已有运行记录，发现完成性偏差 | 补无dependency、集合外Role、混合budget、DAG、stable/high ordinal反例 |
| admission | task020已有6允许+9拒绝+1 update-only结果 | 补null/default/patch、Role0、稀疏0/0、C的全部存在性和active变更 |
| 升级hash漂移、CR生命周期 | bug016/021有独立production修复验证 | 固定集成commit后重放强制交叉，不能把未集成fix测试当release通过 |
| 运行中故障和状态变换 | 历史bug各有局部证据 | 将L01…L22纳入统一矩阵，保留失败原因及前置条件 |

本阶段交付是完整场景分类和预期行为文档。后续质量加固应先依据这里的场景ID建立可执行用例，再以准确commit对应的Kind环境执行；没有实际证据的行始终标“未验证”，不沿用上一轮“代表场景通过”来填充。

## 14. 代码核对索引

路径相对 `kthena-production-linear/`；配置代码与`4373b29e`一致，controller行号以下以production发布快照为准。需要还原时使用 `git show 4373b29e:<path>`，避免读到后续fix分支后混淆行为。

| 规则 | 源码入口 |
| --- | --- |
| 默认/字段归属 | `pkg/apis/workload/v1alpha1/model_serving_types.go`，`servinggroup_types.go`；Helm workload ModelServing CRD |
| U/S/P合法性、recover配对 | `pkg/model-serving-controller/webhook/validator.go`：validateRollingUpdateConfiguration、validateMaxUnavailableForRoles、validateRecoveryPolicyAndRolloutStrategy |
| coordination默认/图/容量 | `webhook/role_coordination_validator.go`：validateRoleCoordination、validateRoleCoordinationUpdate、validateInProgressDependencyCapacity |
| 取整/最小1 | `utils/utils.go:632`：GetMaxUnavailable、GetMaxUnavailableForRole、GetMaxSurge/GetMaxSurgeForRole |
| 模板hash排除项 | `utils/revision_util.go:43`：ModelServingRevision、CalRoleTemplateHash |
| reconcile与SG/Role预算 | production `controller/model_serving_controller.go:603`、`:1602`、`:1779`、`:3518` |
| 保护模板+最新Role副本 | production `controller/model_serving_controller.go:954`：syncRoleReplicas、mergeLatestRoleReplicas |
| coordination稳定计数/额度 | `controller/role_rolling_update_coordinator.go:337`、`:545`、`:667` |
| 状态与Available互斥 | production `controller/model_serving_controller.go:2620`；`utils/utils.go:437` |
| 历史已知缺陷及修复 | bug016/021的DESCRIPTION和PROPOSAL_COMMIT，见§11链接 |

后续改动若改变这些规则，应同步更新该规则对应的场景ID、预期和测试YAML；尤其不要只改取整helper、admission或状态中的一处。
