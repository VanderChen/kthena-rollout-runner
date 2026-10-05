# ModelServing Rolling Update：独立测试场景表

版本：2026-09-07（按现有分类顺序重新编排RUN编号；初版2026-09-05）。**不新增、删除用例或改变测试语义；RUN-001～RUN-060及DENY编号保持不变。**

## 总览

**共768个独立用例 = 671个运行用例 + 97个预期拒绝用例。**

| 大类 | 运行 | 拒绝 | 合计 | 编号范围 |
| --- | ---: | ---: | ---: | --- |
| 1. 正常流程 | 303 | 0 | 303 | RUN-001～RUN-303 |
| 2. 故障恢复场景 | 236 | 0 | 236 | RUN-304～RUN-539 |
| 3. 边界及拒绝场景 | 72 | 97 | 169 | RUN-540～RUN-611；DENY-001～DENY-097 |
| 4. Controller升级场景 | 60 | 0 | 60 | RUN-612～RUN-671 |
| **总计** | **671** | **97** | **768** | RUN-001～RUN-671；DENY-001～DENY-097 |

这四大类能够覆盖现有全部用例。为避免遗漏，在“故障恢复场景”下补充 **2.3 关联资源与插件故障**；1.2命名为“滚动中的动态变更”，下设扩缩容和再次变更。每个ID只有一个主归属，不因为同时涉及partition、历史或重启就在多处重复计数。

### 分类索引

1.1为下列1.1.1～1.1.7的小计，父项与子项不重复累计。

| 分类 | 数量 | 编号范围 | 范围说明 |
| --- | ---: | --- | --- |
| 1.1 基础组合 | 159 | RUN-001～RUN-159 | 基础预算、默认值、协调、Gang/插件、普通模板变更、无操作及正常完成性 |
| 1.1.1 基础预算组合 | 60 | RUN-001～RUN-060 | 原核心U/S/P配置矩阵，RUN-001～RUN-060 |
| 1.1.2 默认值与提交形态 | 16 | RUN-061～RUN-076 | 字段省略、空对象、null及恢复默认 |
| 1.1.3 Role协调与正常灰度组合 | 30 | RUN-077～RUN-106 | 合法协调图、比例门控、参与集合及灰度停点 |
| 1.1.4 Gang、Eviction预算与插件正常组合 | 16 | RUN-107～RUN-122 | 不注入故障或外部驱逐时的预算与插件组合 |
| 1.1.5 成员与worker布局变更 | 9 | RUN-123～RUN-131 | Role新增、删除、改名及worker布局更新 |
| 1.1.6 无操作、等价提交与非模板策略变更 | 21 | RUN-132～RUN-152 | 重复提交、语义等价变更及不应重建Pod的策略调整 |
| 1.1.7 历史复用与正常完成性 | 7 | RUN-153～RUN-159 | 合法历史复用及连续ordinal终态自动收敛 |
| 1.2.1 滚动过程中扩缩容 | 115 | RUN-160～RUN-274 | 原有35个相关对照/交互用例 + 上一轮新增80个运行用例 |
| 1.2.2 滚动过程中再次变更 | 29 | RUN-275～RUN-303 | A→B→C/回滚、U/S/P和coordination变更 |
| 2.1 Pod故障 | 131 | RUN-304～RUN-434 | 删除/缺失、容器重启、探针/镜像/调度失败、终止阻塞及外部驱逐 |
| 2.2 Controller故障 | 28 | RUN-435～RUN-462 | 同版本重启、leader切换、Watch/缓存及API读写失败 |
| 2.3 关联资源与插件故障 | 77 | RUN-463～RUN-539 | ControllerRevision持久化/读取/内容故障及插件hook失败 |
| 3.1 数值与副本边界 | 33 | RUN-540～RUN-572 | 单/零副本、百分比取整、极值及零副本扩容 |
| 3.2 稀疏ordinal、partition与完成性边界 | 37 | RUN-573～RUN-609 | 高ordinal、稳定槽、稀疏停点与状态收敛 |
| 3.3 历史命名与对象身份边界 | 2 | RUN-610～RUN-611 | 等价CR冲突复用、同名新UID隔离 |
| 3.4 预期拒绝 | 97 | DENY-001～DENY-097 | 所有DENY用例，包括扩缩容和coordination引发的拒绝 |
| 4.1 健康旧实例的跨版本升级 | 54 | RUN-612～RUN-665 | 更换controller/CRD版本，保持业务模板不变后检查误滚动 |
| 4.2 异常旧实例的跨版本升级 | 6 | RUN-666～RUN-671 | 旧实例NotReady、缺worker或Deleting时升级 |
| **合计（只累计本表叶级范围）** | **768** | RUN-001～RUN-671；DENY-001～DENY-097 | 与上方四大类总数一致 |

### 编号规则

本轮按既有分类和类内出现顺序，依次分配RUN-001～RUN-671；第3类的DENY行不占RUN序号。正文、附录和JSON中的运行用例引用均使用新编号，配置、初态、动作、预期、执行方式及验证状态不变。

已实现的核心60例仍是RUN-001～RUN-060，因此runner代码、case YAML文件名及已有Kind报告无需迁移。场景仍在梳理阶段，统一以JSON的`id`和本表当前编号为准，不保留旧编号或映射。原始分析和归档设计稿保持原文，不作为当前用例编号的依据。

### 配置缩写与实际字段对应关系

配置列使用以下简写，默认值均按本表固定的production基线解释；完整默认化、百分比取整和公共fixture说明见§A.2。

| 表中写法 | 对应YAML字段 | 如何理解 |
| --- | --- | --- |
| 粒度`SG` / `Role` | `spec.rolloutStrategy.type` | 分别为`ServingGroupRollingUpdate` / `RoleRollingUpdate`；整个strategy省略时有效粒度为SG |
| `N=3` | `spec.replicas: 3` | 期望3个ServingGroup；省略默认1，不是总Pod数 |
| `f` / `b` / `c` / `d` / `x` | `spec.template.roles[].name` | 分别代指`frontend` / `backend` / `role-c` / `role-d` / `extra`，不是API字段名 |
| `f:R=1` | `spec.template.roles[]`中`name: frontend`的`replicas: 1` | 每个SG内有1个frontend Role副本；R省略默认1 |
| `W=0` | 对应Role的`workerReplicas: 0` | 每个Role副本有0个worker，仍有1个entry Pod；W必填，无默认值 |
| 顶层`U/S/P` | `spec.rolloutStrategy.rollingUpdateConfiguration`下的`maxUnavailable` / `maxSurge` / `partition` | SG滚动预算，省略时有效值`1/0/0`；Role模式禁止配置这个顶层对象 |
| Role内联`U/S/P`，如`f:…U/S/P=…` | 对应`spec.template.roles[]`条目直接填写`maxUnavailable` / `maxSurge` / `partition` | Role模式下每个SG内、每种Role分别计算预算，省略时有效值`1/0/0`；不是再嵌套一个`rollingUpdateConfiguration`对象。SG模式忽略Role U、拒绝Role S/P |
| `协调` | `spec.rolloutStrategy.roleCoordination` | 整个对象省略表示关闭；不是填写一个值为`false`的开关 |
| 协调`roles` / `K` / `依赖` | `spec.rolloutStrategy.roleCoordination`下的`roles` / `maxSkew` / `dependencies` | 协调参与集合、最大进度偏差和依赖关系；启用时K必填，roles省略或`[]`表示全部Role |
| `恢复=RoleRecreate` | `spec.recoveryPolicy: RoleRecreate` | 故障恢复策略；省略默认RoleRecreate，不随SG/Role滚动粒度自动切换 |

`U`是允许不可用的预算（maxUnavailable），`S`是允许超过期望副本数的额外活动容量（maxSurge），`P`是保护旧版本的ordinal阈值（partition，ordinal小于P的实例受保护）。它们的单位随粒度变化：SG模式按组计算，Role模式按每个SG内对应Role的副本计算，**都不是直接数单个Pod**；验收时由实际Pod聚合这些单位的可用性，不以ModelServing的status计数代替。

符号`∅`表示本次完整提交中未填写该字段，**不是YAML字面量，也不等于显式`0`、`"0%"`或`null`**。`→`在“原始配置→生效值”中表示默认化/预算计算后的结果，不表示一次滚动动作，也不保证API会把所有生效值回填到对象中；动作列中的`A→B`才表示业务模板版本变化。只写子字段为`∅`，并不能区分父对象省略还是`{}`，具体以用例的提交形态为准。

例如SG粒度下的`N=3；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate`：

- 建立3个SG，每组仅有1个frontend Role副本，每个副本只有entry、没有worker；健康初态共3个Pod。
- 顶层U/S/P均未填写，实际按U=1、S=0、P=0执行：普通无故障滚动时Ready组容量至少2，不增加surge活动组，没有partition保护的旧实例；P=0不是“停止滚动”。
- 未启用Role协调；故障恢复按RoleRecreate处理，与此次模板更新采用SG粒度是两个独立配置。

下面只展示配置对应关系，省略了`entryTemplate`等必需内容，**不是可直接apply的完整测试YAML**；完整工作负载仍使用保留的fixture。一种省略U/S/P的写法为：

```yaml
spec:
  replicas: 3
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration: {} # 内部U/S/P均省略
    # roleCoordination未配置，即关闭
  # recoveryPolicy未配置，默认RoleRecreate
  template:
    roles:
      - name: frontend
        replicas: 1
        workerReplicas: 0
```

其生效滚动/恢复策略可对照下面的显式写法（N与Role布局沿用上例，协调仍省略）。二者在本例中的生效策略相同，但“省略字段”和“显式填写默认值”仍是不同的提交形态测试，不能合并用例：

```yaml
spec:
  rolloutStrategy:
    type: ServingGroupRollingUpdate
    rollingUpdateConfiguration:
      maxUnavailable: 1
      maxSurge: 0
      partition: 0
  recoveryPolicy: RoleRecreate
```

### 归类与保留规则

- **按主验证目标归类。** 预期API拒绝统一在第3类；明确更换controller/CRD版本的在第4类；Pod/控制器/关联资源故障注入按对象落在第2类；其余正常流程与专门的数值/身份边界分别归第1、3类。
- **同版本重启不等于升级。** RUN-435～RUN-442是第2类；RUN-612～RUN-671才是第4类。ControllerRevision中的业务版本A/B/C也不是controller二进制版本。RUN-153及RUN-154等在正常流程完成后才重启核对，不因此改为故障或升级测试。
- **Ready门控不等于Pod故障。** 为观察正常过程而暂不放行Ready，或仅用finalizer固定扩缩交错窗口，仍可归正常流程；专门验证探针失败、Ready回退、Pod丢失或终止失败恢复的才归Pod故障。坏镜像改C用于故障修复，不搬到正常“再次变更”。
- **混合用例不拆成重复行。** RUN-463～RUN-522每行都注入目标历史创建失败，主归属2.3.1，同时保留混合历史/清理/重启断言；DENY-078、DENY-094～DENY-097虽涉及扩缩，主归属仍是拒绝类。RUN-237/RUN-241的终止门控服务于连续缩扩的时序验证，保留在1.2.1。
- **RUN按分类连续编号，执行方式不变。** 按正文出现顺序编号为RUN-001～RUN-671；DENY-001～DENY-097不重编号。已实现的RUN-001～RUN-060仍为原核心60例；用例统一使用当前编号。上一轮84项扩展仍为48个HOLD_READY、20个AUTO_READY_INTERLEAVE、12个ATOMIC_SPEC_SCALE运行及4个拒绝；受控Ready、正常自动Ready和单请求同时改副本/PodSpec不能互相代替。
- **基础72输入不变。** 2粒度 × 3种U输入 × 3种S输入 × 4种P输入，60个合法运行在1.1.1，12个拒绝在3.4.1（该小节另含1个百分比拒绝）。其他696个是显式边界/风险用例，不把全表再做隐含笛卡尔积。
- [机器可读清单](./ROLLING_UPDATE_CASES.json)与正文顺序一致；`group`/`section`/`categoryPath`描述新分类，`legacyGroup`及`legacyStats`保留旧分类来源，原有`subgroup`和`executionProfile`不丢失。旧分类去向见附录B。
- 本轮仅更新RUN编号、索引和引用，**没有新增Kind验证或改变结果状态**。原始分析、原28份Kind YAML及runner核心60份case YAML保留；production基线、默认行为和执行证据要求集中放在附录A，不占用四大类编号。

这里的“全量”仅指明确列出的有限768个用例，不声称穷尽任意整数、PodSpec字段和全部事件排列。每行多步动作仍是一个用例，引用、统计和共同断言都不另计数。

## 1 正常流程：303个

不以异常恢复或controller版本替换为主目标的流程。基础配置保持原意；扩缩、正常自动Ready及同请求变更按独立时序验收。

### 1.1 基础组合：159个

包括从健康基线发起的合法更新，以及应不触发重建的合法变更；专门的数值/稀疏/身份边界移至第3类。

#### 1.1.1 基础预算组合：60个

核心U/S/P配置输入，包含省略与显式值；保持原60个基础用例，不因其中含partition全保护而拆散核心矩阵。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-001 | SG | N=3；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-002 | SG | N=3；顶层U/S/P=∅/∅/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-003 | SG | N=3；顶层U/S/P=∅/∅/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥2；最终1个受保护A+2个B，活动数回3 |
| RUN-004 | SG | N=3；顶层U/S/P=∅/∅/3→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-005 | SG | N=3；顶层U/S/P=∅/0/∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-006 | SG | N=3；顶层U/S/P=∅/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-007 | SG | N=3；顶层U/S/P=∅/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥2；最终1个受保护A+2个B，活动数回3 |
| RUN-008 | SG | N=3；顶层U/S/P=∅/0/3→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-009 | SG | N=3；顶层U/S/P=∅/1/∅→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-010 | SG | N=3；顶层U/S/P=∅/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-011 | SG | N=3；顶层U/S/P=∅/1/1→1/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤4、Ready容量≥2；最终1个受保护A+2个B，活动数回3 |
| RUN-012 | SG | N=3；顶层U/S/P=∅/1/3→1/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-013 | SG | N=3；顶层U/S/P=0/∅/3→0/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-014 | SG | N=3；顶层U/S/P=0/0/3→0/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-015 | SG | N=3；顶层U/S/P=0/1/∅→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-016 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-017 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤4、Ready容量≥3；最终1个受保护A+2个B，活动数回3 |
| RUN-018 | SG | N=3；顶层U/S/P=0/1/3→0/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-019 | SG | N=3；顶层U/S/P=2/∅/∅→2/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥1；最终0个受保护A+3个B，活动数回3 |
| RUN-020 | SG | N=3；顶层U/S/P=2/∅/0→2/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥1；最终0个受保护A+3个B，活动数回3 |
| RUN-021 | SG | N=3；顶层U/S/P=2/∅/1→2/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥1；最终1个受保护A+2个B，活动数回3 |
| RUN-022 | SG | N=3；顶层U/S/P=2/∅/3→2/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-023 | SG | N=3；顶层U/S/P=2/0/∅→2/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥1；最终0个受保护A+3个B，活动数回3 |
| RUN-024 | SG | N=3；顶层U/S/P=2/0/0→2/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥1；最终0个受保护A+3个B，活动数回3 |
| RUN-025 | SG | N=3；顶层U/S/P=2/0/1→2/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤3、Ready容量≥1；最终1个受保护A+2个B，活动数回3 |
| RUN-026 | SG | N=3；顶层U/S/P=2/0/3→2/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-027 | SG | N=3；顶层U/S/P=2/1/∅→2/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤4、Ready容量≥1；最终0个受保护A+3个B，活动数回3 |
| RUN-028 | SG | N=3；顶层U/S/P=2/1/0→2/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤4、Ready容量≥1；最终0个受保护A+3个B，活动数回3 |
| RUN-029 | SG | N=3；顶层U/S/P=2/1/1→2/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 活动组≤4、Ready容量≥1；最终1个受保护A+2个B，活动数回3 |
| RUN-030 | SG | N=3；顶层U/S/P=2/1/3→2/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-031 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-032 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/∅/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-033 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/∅/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥2；最终1个受保护A+2个B，活动数回3；b的UID保持 |
| RUN-034 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/∅/3→1/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-035 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/0/∅→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-036 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-037 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥2；最终1个受保护A+2个B，活动数回3；b的UID保持 |
| RUN-038 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/0/3→1/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-039 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/1/∅→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-040 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-041 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤4、Ready容量≥2；最终1个受保护A+2个B，活动数回3；b的UID保持 |
| RUN-042 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/1/3→1/1/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-043 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/∅/3→0/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-044 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/0/3→0/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-045 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/∅→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-046 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-047 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤4、Ready容量≥3；最终1个受保护A+2个B，活动数回3；b的UID保持 |
| RUN-048 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-049 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/∅/∅→2/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥1；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-050 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/∅/0→2/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥1；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-051 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/∅/1→2/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥1；最终1个受保护A+2个B，活动数回3；b的UID保持 |
| RUN-052 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/∅/3→2/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-053 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/0/∅→2/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥1；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-054 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/0/0→2/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥1；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-055 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/0/1→2/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤3、Ready容量≥1；最终1个受保护A+2个B，活动数回3；b的UID保持 |
| RUN-056 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/0/3→2/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-057 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/1/∅→2/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤4、Ready容量≥1；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-058 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/1/0→2/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤4、Ready容量≥1；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-059 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/1/1→2/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 活动f实例≤4、Ready容量≥1；最终1个受保护A+2个B，活动数回3；b的UID保持 |
| RUN-060 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/1/3→2/1/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 全部旧UID保留，不启动surge；目标停在P保护边界 |

#### 1.1.2 默认值与提交形态：16个

创建、空对象、null及恢复默认；这些是在健康基线配置或验证默认化，不是已有滚动中的再次变更。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-061 | SG | N=∅→1；strategy形态=∅；顶层预算对象∅→1/0/0；f:R=∅→1,W=0；协调∅→关；恢复=∅→RoleRecreate；schedulerName="omitted"；plugins="omitted" | 对象不存在；没有任何历史版本 | 创建A并Ready，然后将f模板A→B | N/R默认1、SG预算1/0/0；创建1组，滚动允许暂时0可用组 |
| RUN-062 | SG | N=3；strategy形态=∅；顶层预算对象∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效SG预算1/0/0；保留至少2组Ready，最终3组B |
| RUN-063 | SG | N=3；strategy形态={}；顶层预算对象∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效SG预算1/0/0；保留至少2组Ready，最终3组B |
| RUN-064 | SG | N=3；strategy形态=仅type；顶层预算对象∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效SG预算1/0/0；保留至少2组Ready，最终3组B |
| RUN-065 | SG | N=3；strategy形态=省略type；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | type默认SG，空配置对象U默认1；最终3组B，最低Ready2 |
| RUN-066 | SG | N=3；strategy形态=仅eviction；顶层预算对象∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"SG","min":2} | 连续ordinal；A全Ready | 将f模板A→B | type默认SG，预算1/0/0；eviction不改变rollout预算 |
| RUN-067 | SG | N=3；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0,内联U/S/P=2/∅/∅（U忽略；S/P若存在则非法）；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 接受Role U但SG滚动忽略它；顶层默认U=1，最低2组Ready |
| RUN-068 | SG | N=3；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0,内联U/S/P="invalid-budget"/∅/∅（U忽略；S/P若存在则非法）；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 接受Role U但SG滚动忽略它；顶层默认U=1，最低2组Ready |
| RUN-069 | SG | N=3；strategy形态=null；顶层预算对象∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，strategy原为显式SG | 将strategy置null，并将f模板A→B | server返回无strategy；有效SG 1/0/0，最终3组B |
| RUN-070 | SG | N=3；顶层U/S/P=null/null/null→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，预算原为显式1/0/0 | 将顶层U/S/P置null，并改f为B | 非nullable字段经API处理后U默认1，S/P有效0；按1/0/0滚动 |
| RUN-071 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=null/null/null→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，预算原为显式1/0/0 | 将f的U/S/P置null，并改f为B | 非nullable字段经API处理后U默认1，S/P有效0；按1/0/0滚动 |
| RUN-072 | Role | N=∅→1；b:R=∅→1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=∅→1,W=0,U/S/P=∅/∅/∅→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 对象不存在 | 创建A并Ready后，仅改f模板B | N与各Role R默认1，Role U默认1/S=P=0；f可出现0可用后补B，b UID保持；单副本默认不是零中断 |
| RUN-073 | SG | N=3；顶层U/S/P=2/1/0→2/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 使用JSON merge patch将U设为null，然后将f模板A→B | server U回默认1，不是0；后续按1/1/0滚动 |
| RUN-074 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 将S及P设为null，然后将f模板A→B | server删除S/P后有效0/0；结合U=1为全量先删后建 |
| RUN-075 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/1/0→2/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 使用JSON merge patch将U设为null，然后将f模板A→B | server U回默认1，不是0；后续按1/1/0滚动 |
| RUN-076 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 将S及P设为null，然后将f模板A→B | server删除S/P后有效0/0；结合U=1为全量先删后建 |

#### 1.1.3 Role协调与正常灰度组合：30个

合法协调图、比例门控、参与集合、正常灰度与初始创建。RUN-099中延迟Ready是观测门控，不作为故障注入。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-077 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤3、主动更新Ready下限2；K=50%按稳定槽取整限速；无依赖门控；最终f、b各3个B；x若存在UID不变 |
| RUN-078 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤3、主动更新Ready下限2；K=50%按稳定槽取整限速；f首次目标启动等待b稳定目标Ready；旧f存在时保留至少1个旧b；最终f、b各3个B；x若存在UID不变 |
| RUN-079 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；x:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；协调roles=["f","b"],K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤3、主动更新Ready下限2；K=50%按稳定槽取整限速；无依赖门控；最终f、b各3个B；x若存在UID不变 |
| RUN-080 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；x:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；协调roles=["f","b"],K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤3、主动更新Ready下限2；K=50%按稳定槽取整限速；f首次目标启动等待b稳定目标Ready；旧f存在时保留至少1个旧b；最终f、b各3个B；x若存在UID不变 |
| RUN-081 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤4、主动更新Ready下限3；K=50%按稳定槽取整限速；无依赖门控；最终f、b各3个B；x若存在UID不变 |
| RUN-082 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤4、主动更新Ready下限3；K=50%按稳定槽取整限速；f首次目标启动等待b稳定目标Ready；旧f存在时保留至少1个旧b；最终f、b各3个B；x若存在UID不变 |
| RUN-083 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；x:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；协调roles=["f","b"],K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤4、主动更新Ready下限3；K=50%按稳定槽取整限速；无依赖门控；最终f、b各3个B；x若存在UID不变 |
| RUN-084 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；x:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；协调roles=["f","b"],K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤4、主动更新Ready下限3；K=50%按稳定槽取整限速；f首次目标启动等待b稳定目标Ready；旧f存在时保留至少1个旧b；最终f、b各3个B；x若存在UID不变 |
| RUN-085 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤4、主动更新Ready下限2；K=50%按稳定槽取整限速；无依赖门控；最终f、b各3个B；x若存在UID不变 |
| RUN-086 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤4、主动更新Ready下限2；K=50%按稳定槽取整限速；f首次目标启动等待b稳定目标Ready；旧f存在时保留至少1个旧b；最终f、b各3个B；x若存在UID不变 |
| RUN-087 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；x:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；协调roles=["f","b"],K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤4、主动更新Ready下限2；K=50%按稳定槽取整限速；无依赖门控；最终f、b各3个B；x若存在UID不变 |
| RUN-088 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；x:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；协调roles=["f","b"],K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B；集合外x若存在则不变 | 每Role活动数≤4、主动更新Ready下限2；K=50%按稳定槽取整限速；f首次目标启动等待b稳定目标Ready；旧f存在时保留至少1个旧b；最终f、b各3个B；x若存在UID不变 |
| RUN-089 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=[]→全部,K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B | 空roles与省略均选全部Role；空dependencies与省略均无依赖；仅K=50%限速，各Role最终3个B |
| RUN-090 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=[]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B | 空roles与省略均选全部Role；空dependencies与省略均无依赖；仅K=50%限速，各Role最终3个B |
| RUN-091 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=[]→全部,K="50%",依赖=[]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B | 空roles与省略均选全部Role；空dependencies与省略均无依赖；仅K=50%限速，各Role最终3个B |
| RUN-092 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=["f","b"],K="50%",依赖=[]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B | 空roles与省略均选全部Role；空dependencies与省略均无依赖；仅K=50%限速，各Role最终3个B |
| RUN-093 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；c:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=b→[c],f→[b]；恢复=∅→RoleRecreate | 所有Role的A全Ready；连续ordinal | 所有Role模板同时A→B | 链依赖闭包生效；入口首次启动等全部必需下游稳定目标Ready，中间节点不要求绝对串行创建；每个旧直接调用方消失前保留共享旧依赖；各Role不主动损失Ready容量，最终全B |
| RUN-094 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；c:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b,c]；恢复=∅→RoleRecreate | 所有Role的A全Ready；连续ordinal | 所有Role模板同时A→B | 分叉依赖闭包生效；入口首次启动等全部必需下游稳定目标Ready，中间节点不要求绝对串行创建；每个旧直接调用方消失前保留共享旧依赖；各Role不主动损失Ready容量，最终全B |
| RUN-095 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；c:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=b→[c],f→[c]；恢复=∅→RoleRecreate | 所有Role的A全Ready；连续ordinal | 所有Role模板同时A→B | 汇聚依赖闭包生效；入口首次启动等全部必需下游稳定目标Ready，中间节点不要求绝对串行创建；每个旧直接调用方消失前保留共享旧依赖；各Role不主动损失Ready容量，最终全B |
| RUN-096 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；c:R=3,W=0,U/S/P=0/1/0→0/1/0；d:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=b→[d],c→[d],f→[b,c]；恢复=∅→RoleRecreate | 所有Role的A全Ready；连续ordinal | 所有Role模板同时A→B | 菱形依赖闭包生效；入口首次启动等全部必需下游稳定目标Ready，中间节点不要求绝对串行创建；每个旧直接调用方消失前保留共享旧依赖；各Role不主动损失Ready容量，最终全B |
| RUN-097 | Role | N=1；b:R=2,W=0,U/S/P=0/1/0→0/1/0；f:R=2,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="1%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B | T=2时ceil(1%×2)=1个启动额度，不断言进度差严格≤1%；仍遵守依赖/旧路径保护，最终全B |
| RUN-098 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="100%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b模板同时A→B | 比例额度放开，但f仍等b稳定目标Ready、旧f存在仍保留旧b；最终全B |
| RUN-099 | Role | N=3；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 3组A全Ready；准备仅延迟group0的b目标Ready | f、b同时A→B；group0受阻后恢复其b Ready | 各组独立计算K/依赖；group1、2继续推进，group0不借用其他组Ready绕过门控；恢复后3组全B |
| RUN-100 | Role | N=1；b:R=5,W=0,U/S/P=1/1/0→1/1/0；f:R=2,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b同时A→B | 按T_f=2、T_b=5归一化进度计算K，非按已更新个数比较；各自预算生效，最终2个f-B及5个b-B |
| RUN-101 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b同时A→B | f ordinal0保持A；f其余完成后旧调用路径仍在，b至少保留1个旧实例；不得强删旧b来追求全B或错误宣称全量完成 |
| RUN-102 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 仅f模板A→B | 仅f滚动；未变Role模板语义等价且UID保持，不因全局revision标签旧而强制重建；目标Role最终3个B |
| RUN-103 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 仅b模板A→B | 仅b滚动；未变Role模板语义等价且UID保持，不因全局revision标签旧而强制重建；目标Role最终3个B |
| RUN-104 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；x:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=["f","b"],K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 仅集合外x模板A→B | x按1/0/0独立完成；f、b UID保持，协调集合不被无关变更拉入滚动 |
| RUN-105 | Role | N=1；b:R=3,W=0,U/S/P=0/1/3→0/1/3；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready；连续ordinal | f、b同时A→B | 无依赖边；b被全保护，f应独立达到3个B；b保持3个A，不把受保护b当作必须推进的比例分母 |
| RUN-106 | Role | N=1；b:R=1,W=0,U/S/P=0/1/0→0/1/0；f:R=1,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 对象不存在 | 创建A并等Ready | create允许单副本依赖；只建期望容量，不因surge创建额外实例；不代表后续同容量模板更新也允许 |

#### 1.1.4 Gang、Eviction预算与插件正常组合：16个

未主动注入故障或外部Eviction的普通滚动；验证各预算与插件职责，不验收集群拓扑放置。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-107 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；plugins=[] | A全Ready；所列插件及ConfigMap已就绪 | 更新f为B并等surge/旧资源清理完成 | 插件不改变0/1/0滚动预算；仅执行所选插件hook，最终PodGroup/Service/Ranktable与有效实例一致，无旧/surge资源泄漏 |
| RUN-108 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service"] | A全Ready；所列插件及ConfigMap已就绪 | 更新f为B并等surge/旧资源清理完成 | 插件不改变0/1/0滚动预算；仅执行所选插件hook，最终PodGroup/Service/Ranktable与有效实例一致，无旧/surge资源泄漏 |
| RUN-109 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；plugins=["ranktable"] | A全Ready；所列插件及ConfigMap已就绪 | 更新f为B并等surge/旧资源清理完成 | 插件不改变0/1/0滚动预算；仅执行所选插件hook，最终PodGroup/Service/Ranktable与有效实例一致，无旧/surge资源泄漏 |
| RUN-110 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service","ranktable"] | A全Ready；所列插件及ConfigMap已就绪 | 更新f为B并等surge/旧资源清理完成 | 插件不改变0/1/0滚动预算；仅执行所选插件hook，最终PodGroup/Service/Ranktable与有效实例一致，无旧/surge资源泄漏 |
| RUN-111 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"SG","min":3,"role":"f"} | A全Ready；eviction minimum等于该层期望容量 | 仅更新f模板B，不发起外部eviction | eviction保护不是rollout第二个U；主动模板替换仍可按U=1执行，不因minimum=D冻结 |
| RUN-112 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"Role","min":1,"role":"f"} | A全Ready；eviction minimum等于该层期望容量 | 仅更新f模板B，不发起外部eviction | eviction保护不是rollout第二个U；主动模板替换仍可按U=1执行，不因minimum=D冻结 |
| RUN-113 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"Role","min":0,"role":"f"} | A全Ready | 提交f模板B，不发起外部eviction | 即使eviction允许全部驱逐，主动滚动仍按U=0先有新Ready再删健康旧实例 |
| RUN-114 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=3,W=0；协调∅→关；恢复=∅→RoleRecreate；gangPolicy={"minRoleReplicas":{"f":1}} | A全Ready；显式部分gang minimum | 将f模板A→B | 按显式minimum产生正确PodGroup，不将gang minimum当作coordination依赖；B可调度后按0/1/0完成 |
| RUN-115 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；plugins=[] | A全Ready；所列插件及ConfigMap已就绪 | 更新f为B并等surge/旧资源清理完成 | 插件不改变0/1/0滚动预算；仅执行所选插件hook，最终PodGroup/Service/Ranktable与有效实例一致，无旧/surge资源泄漏 |
| RUN-116 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service"] | A全Ready；所列插件及ConfigMap已就绪 | 更新f为B并等surge/旧资源清理完成 | 插件不改变0/1/0滚动预算；仅执行所选插件hook，最终PodGroup/Service/Ranktable与有效实例一致，无旧/surge资源泄漏 |
| RUN-117 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；plugins=["ranktable"] | A全Ready；所列插件及ConfigMap已就绪 | 更新f为B并等surge/旧资源清理完成 | 插件不改变0/1/0滚动预算；仅执行所选插件hook，最终PodGroup/Service/Ranktable与有效实例一致，无旧/surge资源泄漏 |
| RUN-118 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service","ranktable"] | A全Ready；所列插件及ConfigMap已就绪 | 更新f为B并等surge/旧资源清理完成 | 插件不改变0/1/0滚动预算；仅执行所选插件hook，最终PodGroup/Service/Ranktable与有效实例一致，无旧/surge资源泄漏 |
| RUN-119 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"SG","min":1,"role":"f"} | A全Ready；eviction minimum等于该层期望容量 | 仅更新f模板B，不发起外部eviction | eviction保护不是rollout第二个U；主动模板替换仍可按U=1执行，不因minimum=D冻结 |
| RUN-120 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"Role","min":3,"role":"f"} | A全Ready；eviction minimum等于该层期望容量 | 仅更新f模板B，不发起外部eviction | eviction保护不是rollout第二个U；主动模板替换仍可按U=1执行，不因minimum=D冻结 |
| RUN-121 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"Role","min":0,"role":"f"} | A全Ready | 提交f模板B，不发起外部eviction | 即使eviction允许全部驱逐，主动滚动仍按U=0先有新Ready再删健康旧实例 |
| RUN-122 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；gangPolicy={"minRoleReplicas":{"f":1}} | A全Ready；显式部分gang minimum | 将f模板A→B | 按显式minimum产生正确PodGroup，不将gang minimum当作coordination依赖；B可调度后按0/1/0完成 |

#### 1.1.5 成员与worker布局变更：9个

从健康基线发起成员新增、删除、改名或worker布局更新；没有要求先进入一轮滚动再提交第二次变更。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-123 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 新增Role x（R=1,W=0，模板A） | SG未保护组按新成员集合更新；Role模式只增x，原等价Role保持；必须满足最新合法gang/coordination引用 |
| RUN-124 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 从spec删除Role f | 显式成员删除应清理f，不受被删除Role的旧U/P永久保护；Role模式保留b UID，SG模式按新模板更新eligible组 |
| RUN-125 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 将Role f改名为x，模板内容不变 | 旧f作为成员删除，新x作为新增；不能将改名当作同一Role的无操作 |
| RUN-126 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 仅将f workerReplicas:0→1并添加workerTemplate B | 按更新粒度应用布局变更；A/B分别按自身W判断Ready，不把旧实例套入新W引起无限恢复 |
| RUN-127 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 新增Role x（R=1,W=0，模板A） | SG未保护组按新成员集合更新；Role模式只增x，原等价Role保持；必须满足最新合法gang/coordination引用 |
| RUN-128 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 从spec删除Role f | 显式成员删除应清理f，不受被删除Role的旧U/P永久保护；Role模式保留b UID，SG模式按新模板更新eligible组 |
| RUN-129 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 将Role f改名为x，模板内容不变 | 旧f作为成员删除，新x作为新增；不能将改名当作同一Role的无操作 |
| RUN-130 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 仅将f workerReplicas:0→1并添加workerTemplate B | 按更新粒度应用布局变更；A/B分别按自身W判断Ready，不把旧实例套入新W引起无限恢复 |
| RUN-131 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；group0受保护 | 新增Role x（R=1,W=0）并同时改f为B | group0按历史成员集不创建x；其余组使用新成员集f-B+x；不能把T13默认全量预期套在protected组 |

#### 1.1.6 无操作、等价提交与非模板策略变更：21个

健康对象的重复提交、语义等价字段、scheduler/recovery/模式/插件配置变化；重点是不意外重建。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-132 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 重复apply完全相同的A YAML | 所有健康UID保持，无surge、无多余ControllerRevision |
| RUN-133 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 重复apply完全相同的A YAML | 所有健康UID保持，无surge、无多余ControllerRevision |
| RUN-134 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 重复apply完全相同的A YAML | 所有健康UID保持，无surge、无多余ControllerRevision |
| RUN-135 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 只改ModelServing外层labels/annotations | Pod模板不变，健康UID全部保持 |
| RUN-136 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 只重排f、b的Role列表顺序 | 按模板语义判等，不能因列表顺序/hash表示变化删除任何健康Pod |
| RUN-137 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 仅把Pod模板中的可选空labels从省略改为{} | 若API存储语义等价则不触发无意滚动；保存server spec证明归一化结果 |
| RUN-138 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 仅修改schedulerName为已安装的default-scheduler | 顶层scheduler不在Role模板revision中，不承诺重建存量Pod；存量健康UID保持，后续新建使用最新有效调度配置 |
| RUN-139 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 只改recoveryPolicy=None、restartGracePeriodSeconds=30 | 不生成模板滚动；后续恢复采用新策略，存量健康UID保持 |
| RUN-140 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 重复apply完全相同的A YAML | 所有健康UID保持，无surge、无多余ControllerRevision |
| RUN-141 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 重复apply完全相同的A YAML | 所有健康UID保持，无surge、无多余ControllerRevision |
| RUN-142 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 重复apply完全相同的A YAML | 所有健康UID保持，无surge、无多余ControllerRevision |
| RUN-143 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 只改ModelServing外层labels/annotations | Pod模板不变，健康UID全部保持 |
| RUN-144 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 只重排f、b的Role列表顺序 | 按模板语义判等，不能因列表顺序/hash表示变化删除任何健康Pod |
| RUN-145 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 仅把Pod模板中的可选空labels从省略改为{} | 若API存储语义等价则不触发无意滚动；保存server spec证明归一化结果 |
| RUN-146 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 仅修改schedulerName为已安装的default-scheduler | 顶层scheduler不在Role模板revision中，不承诺重建存量Pod；存量健康UID保持，后续新建使用最新有效调度配置 |
| RUN-147 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 只改recoveryPolicy=None、restartGracePeriodSeconds=30 | 不生成模板滚动；后续恢复采用新策略，存量健康UID保持 |
| RUN-148 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=3,W=0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；f、b各R=3 | 仅切换为Role mode，删除顶层预算，保留内联U默认1/S/P省略 | 合法模式切换但模板不变；健康UID全部保持 |
| RUN-149 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 仅切换为SG mode并清除所有Role S/P及coordination，顶层预算改1/0/0 | 合法切换但不凭type变化重建；Role U在SG中不生效 |
| RUN-150 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready | A全Ready时移除再恢复相同coordination | 纯策略变更不重建任何健康UID |
| RUN-151 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service"] | A全Ready | 仅添加已就绪ranktable插件，再只改其配置ConfigMap | 顶层插件及其配置变化不等价于Role模板滚动；健康Pod UID保持，派生资源按hook/live audit同步 |
| RUN-152 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service"] | A全Ready | 仅添加已就绪ranktable插件，再只改其配置ConfigMap | 顶层插件及其配置变化不等价于Role模板滚动；健康Pod UID保持，派生资源按hook/live audit同步 |

#### 1.1.7 历史复用与正常完成性：7个

合法历史复用及连续ordinal终态自动收敛。完成后同镜像重启只是终态等价核对，不是以故障触发恢复，更不是controller跨版本升级。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-153 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready；已保存目标A CR.Data | 仅将f/b R从3→4，再改U/S为0/1、K为25%；保存每一步CR.Data后重启 | 模板相等应复用历史，不以新replicas/策略改写既有Data；可变replica baseline正确更新，后续协调使用最新已完成基线 |
| RUN-154 | Role | N=1；b:R=2,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=2,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={0,1}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-155 | Role | N=1；b:R=2,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=2,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={0,1}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-156 | Role | N=1；b:R=2,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=2,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={0,1}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-157 | Role | N=1；b:R=2,W=0,U/S/P=1/0/0→1/0/0；f:R=2,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={0,1}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-158 | Role | N=1；b:R=2,W=0,U/S/P=0/1/0→0/1/0；f:R=2,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={0,1}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-159 | Role | N=1；b:R=2,W=0,U/S/P=1/1/0→1/1/0；f:R=2,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={0,1}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |

### 1.2 滚动中的动态变更：144个

按用户操作分为扩缩容与再次变更。相关的纯扩缩、先扩缩后滚动和同请求变更作为对照放在1.2.1，不能将它们误称为已命中正常在途交错。

#### 1.2.1 滚动过程中扩缩容：115个

统一收纳扩缩容与滚动的交互，同时保留前置扩缩、灰度停驻后扩缩、正常/受控Ready下在途扩缩，以及同请求修改副本数和PodSpec的区别。

本节115个运行用例分为既有35个相关扩缩对照与交互、48个受控Ready、20个正常自动Ready在途交错、12个同请求变更。上一轮新增用例的共同阶段契约见§A.4；4个相关拒绝用例移至3.4.6，不在115中计数。

##### 既有扩缩容、前置对照与联合变更：35个

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-160 | Role | N=1；b:R=1,W=0,U/S/P=0/1/0→0/1/0；f:R=1,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 单副本f、b的A全Ready | 同一提交将f、b replicas由1→2并将两者模板A→B | 新稳定槽由expansion提供；允许更新，按1→2基线和依赖执行，最终各2个B |
| RUN-161 | Role | N=1；b:R=1,W=0,U/S/P=0/1/0→0/1/0；f:R=1,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 单副本f、b的A全Ready | 先将f、b replicas由1→2并等Ready，再同时将模板A→B | 扩容阶段不替换原健康UID；模板阶段使用已更新的oldR=2基线，最终各2个B |
| RUN-162 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 3组A全Ready；group0受保护；f旧R=1、W=1 | 同一提交将f R:1→2、W:1→2，并将f模板A→B | group0保留历史A及W=1，但f按最新R=2补齐，PodGroup/Ready按旧布局+新R计算；未保护组使用B、R=2、W=2；受保护原UID保持 |
| RUN-163 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将N扩大1后等Ready，再缩回原N | 仅增删组容量；保留组内原健康UID不变；组缩容与滚动预算分开判断 |
| RUN-164 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f R扩大1后等Ready，再缩回原R | 只改变f实例数；未被缩容选中的UID和b保持，不因replica变化滚动模板 |
| RUN-165 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B，同时把N扩大1、f R扩大1 | 按最新N/R规划容量；先持久化目标历史，最终最新容量B；不能重复计算扩容与surge |
| RUN-166 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将N降至1，旧Pod仍terminating时立即把N恢复为3 | 最终N=3并按最新B恢复；旧删除意图不扩展到新UID，不能长期缺Pod |
| RUN-167 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=3,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将f R降至1，旧Pod仍terminating时立即把f R恢复为3 | 最终每组f R=3按B补齐；删除幂等，不污染其他Role版本/UID |
| RUN-168 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将N扩大1后等Ready，再缩回原N | 仅增删组容量；保留组内原健康UID不变；组缩容与滚动预算分开判断 |
| RUN-169 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f R扩大1后等Ready，再缩回原R | 只改变f实例数；未被缩容选中的UID和b保持，不因replica变化滚动模板 |
| RUN-170 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B，同时把N扩大1、f R扩大1 | 按最新N/R规划容量；先持久化目标历史，最终最新容量B；不能重复计算扩容与surge |
| RUN-171 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将N降至1，旧Pod仍terminating时立即把N恢复为3 | 最终N=3并按最新B恢复；旧删除意图不扩展到新UID，不能长期缺Pod |
| RUN-172 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=3,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将f R降至1，旧Pod仍terminating时立即把f R恢复为3 | 最终每组f R=3按B补齐；删除幂等，不污染其他Role版本/UID |
| RUN-173 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将N扩大1后等Ready，再缩回原N | 仅增删组容量；保留组内原健康UID不变；组缩容与滚动预算分开判断 |
| RUN-174 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f R扩大1后等Ready，再缩回原R | 只改变f实例数；未被缩容选中的UID和b保持，不因replica变化滚动模板 |
| RUN-175 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B，同时把N扩大1、f R扩大1 | 按最新N/R规划容量；先持久化目标历史，最终最新容量B；不能重复计算扩容与surge |
| RUN-176 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将N降至1，旧Pod仍terminating时立即把N恢复为3 | 最终N=3并按最新B恢复；旧删除意图不扩展到新UID，不能长期缺Pod |
| RUN-177 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=3,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将f R降至1，旧Pod仍terminating时立即把f R恢复为3 | 最终每组f R=3按B补齐；删除幂等，不污染其他Role版本/UID |
| RUN-178 | SG | N=6；顶层U/S/P="20%"/"20%"/"20%"→1/2/2；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | D=6；A全Ready；连续ordinal | 将模板A→B，同时把预算作用的D扩大到10 | 百分比按最新D=10计算U=2/S=2/P=2，不按实际surge数或D-P计算；最终P保护目标正确 |
| RUN-179 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将N扩大1后等Ready，再缩回原N | 仅增删组容量；保留组内原健康UID不变；组缩容与滚动预算分开判断 |
| RUN-180 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f R扩大1后等Ready，再缩回原R | 只改变f实例数；未被缩容选中的UID和b保持，不因replica变化滚动模板 |
| RUN-181 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B，同时把N扩大1、f R扩大1 | 按最新N/R规划容量；先持久化目标历史，最终最新容量B；不能重复计算扩容与surge |
| RUN-182 | Role | N=3；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将N降至1，旧Pod仍terminating时立即把N恢复为3 | 最终N=3并按最新B恢复；旧删除意图不扩展到新UID，不能长期缺Pod |
| RUN-183 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将f R降至1，旧Pod仍terminating时立即把f R恢复为3 | 最终每组f R=3按B补齐；删除幂等，不污染其他Role版本/UID |
| RUN-184 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将N扩大1后等Ready，再缩回原N | 仅增删组容量；保留组内原健康UID不变；组缩容与滚动预算分开判断 |
| RUN-185 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f R扩大1后等Ready，再缩回原R | 只改变f实例数；未被缩容选中的UID和b保持，不因replica变化滚动模板 |
| RUN-186 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B，同时把N扩大1、f R扩大1 | 按最新N/R规划容量；先持久化目标历史，最终最新容量B；不能重复计算扩容与surge |
| RUN-187 | Role | N=3；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将N降至1，旧Pod仍terminating时立即把N恢复为3 | 最终N=3并按最新B恢复；旧删除意图不扩展到新UID，不能长期缺Pod |
| RUN-188 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将f R降至1，旧Pod仍terminating时立即把f R恢复为3 | 最终每组f R=3按B补齐；删除幂等，不污染其他Role版本/UID |
| RUN-189 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将N扩大1后等Ready，再缩回原N | 仅增删组容量；保留组内原健康UID不变；组缩容与滚动预算分开判断 |
| RUN-190 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f R扩大1后等Ready，再缩回原R | 只改变f实例数；未被缩容选中的UID和b保持，不因replica变化滚动模板 |
| RUN-191 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B，同时把N扩大1、f R扩大1 | 按最新N/R规划容量；先持久化目标历史，最终最新容量B；不能重复计算扩容与surge |
| RUN-192 | Role | N=3；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将N降至1，旧Pod仍terminating时立即把N恢复为3 | 最终N=3并按最新B恢复；旧删除意图不扩展到新UID，不能长期缺Pod |
| RUN-193 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；A/B混合时将f R降至1，旧Pod仍terminating时立即把f R恢复为3 | 最终每组f R=3按B补齐；删除幂等，不污染其他Role版本/UID |
| RUN-194 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P="20%"/"20%"/"20%"→1/2/2；协调∅→关；恢复=∅→RoleRecreate | D=6；A全Ready；连续ordinal | 将模板A→B，同时把预算作用的D扩大到10 | 百分比按最新D=10计算U=2/S=2/P=2，不按实际surge数或D-P计算；最终P保护目标正确 |

##### 受控Ready下的扩缩容与灰度停点：48个

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-195 | SG | N=3；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察旧SG2已删除、新2完整但NotReady，保持10秒后仅改N:3→5；门控放行至完成 | 默认U/S/P始终1/0/0；扩容新增3、4用B；扩容缺口不算额外滚动许可，原删除/Ready账本连续；最终5个B，全Ready，无额外实例 |
| RUN-196 | SG | N=3；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady、0/1仍为Ready A，保持10秒后仅改N:3→2；门控放行至完成 | 默认U/S/P=1/0/0；缩容先移除NotReady的2，另行记录随后对保留旧实例的滚动；最终0、1为B，不把删除2错误计作两个滚动动作 |
| RUN-197 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady，保持10秒后仅改N:3→5；门控放行至灰度停点 | 绝对P保持1；0的旧UID保留，新增3、4为B；没有Ready信用时不能继续删除保留旧实例；最终A{0}+B{1,2,3,4}并稳定30秒 |
| RUN-198 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady、A0/A1 Ready，保持10秒后仅改N:3→2；门控放行至灰度停点 | 合法缩容删除NotReady的2；滚动只替换保留的1，不能删除受保护0；最终A{0}+B{1}并稳定30秒 |
| RUN-199 | SG | N=4；顶层U/S/P=2/0/1→2/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察旧3、2均已开始删除且新3、2完整但NotReady，保持10秒后仅改N:4→6；门控放行至灰度停点 | U保持2，已有两个替换占用不可清零；新增4、5用B，NotReady不记Ready信用；最终A{0}+B{1,2,3,4,5}并稳定30秒 |
| RUN-200 | SG | N=4；顶层U/S/P=2/0/1→2/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新3、2完整但NotReady、A0/A1 Ready，保持10秒后仅改N:4→3；门控放行至灰度停点 | 同等删除成本时缩容先删NotReady的3；保留2的在途替换仍记账，不能重置计数后额外越界；最终A{0}+B{1,2}并稳定30秒 |
| RUN-201 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察旧f实例2已删除、新2完整但NotReady，保持10秒后仅改f.R:3→5；门控放行至完成 | 默认U/S/P始终1/0/0；扩容新增3、4用B；扩容缺口不算额外滚动许可，原删除/Ready账本连续；最终5个B，全Ready，无额外实例；b全程保持A及原UID |
| RUN-202 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady、0/1仍为Ready A，保持10秒后仅改f.R:3→2；门控放行至完成 | 默认U/S/P=1/0/0；缩容先移除NotReady的2，另行记录随后对保留旧实例的滚动；最终0、1为B，不把删除2错误计作两个滚动动作；b全程保持A及原UID |
| RUN-203 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady，保持10秒后仅改f.R:3→5；门控放行至灰度停点 | 绝对P保持1；0的旧UID保留，新增3、4为B；没有Ready信用时不能继续删除保留旧实例；最终A{0}+B{1,2,3,4}并稳定30秒；b全程保持A及原UID |
| RUN-204 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady、A0/A1 Ready，保持10秒后仅改f.R:3→2；门控放行至灰度停点 | 合法缩容删除NotReady的2；滚动只替换保留的1，不能删除受保护0；最终A{0}+B{1}并稳定30秒；b全程保持A及原UID |
| RUN-205 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=4,W=0,U/S/P=2/0/1→2/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察旧3、2均已开始删除且新3、2完整但NotReady，保持10秒后仅改f.R:4→6；门控放行至灰度停点 | U保持2，已有两个替换占用不可清零；新增4、5用B，NotReady不记Ready信用；最终A{0}+B{1,2,3,4,5}并稳定30秒；b全程保持A及原UID |
| RUN-206 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=4,W=0,U/S/P=2/0/1→2/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新3、2完整但NotReady、A0/A1 Ready，保持10秒后仅改f.R:4→3；门控放行至灰度停点 | 同等删除成本时缩容先删NotReady的3；保留2的在途替换仍记账，不能重置计数后额外越界；最终A{0}+B{1,2}并稳定30秒；b全程保持A及原UID |
| RUN-207 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改N:3→5；门控放行新增3、4 | 缩放前A{0}+B{1,2}；新增3、4为B，不重滚原B；最终A{0}+B{1,2,3,4}，原有0/1/2 UID保持，稳定30秒 |
| RUN-208 | SG | N=5；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改N:5→3 | 缩放前A{0}+B{1,2,3,4}；全部Ready、等删除成本时按4、3缩容；最终A{0}+B{1,2}，保留0/1/2 UID不变，稳定30秒 |
| RUN-209 | SG | N=5；顶层U/S/P=1/0/3→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改N:5→2，P仍为3 | 缩放前A{0,1,2}+B{3,4}；先删非保护4、3，再合法缩掉保护2；最终A{0,1}全Ready，原0/1 UID保留，不强求旧版本数=P或晋升到B，稳定30秒 |
| RUN-210 | SG | N=3；顶层U/S/P=1/0/5→1/0/5；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改N:3→6；门控放行新3、4、5 | 缩放前全保护A{0,1,2}，无B；P仍为5，缺失3、4须用受保护历史A，5用B；最终A{0,1,2,3,4}+B{5}，原0/1/2 UID保留，稳定30秒 |
| RUN-211 | SG | N=3；顶层U/S/P=1/0/"50%"→1/0/2；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改N:3→5；门控放行新增3、4 | P=ceil(50%×D)：2→3；缩放前A{0,1}+B{2}；2虽进入新保护范围，仍保留B及原UID，不能回退A；最终A{0,1}+B{2,3,4}，不是3个A，稳定30秒 |
| RUN-212 | SG | N=5；顶层U/S/P=1/0/"50%"→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改N:5→3；门控放行新解锁的2 | P：3→2；缩放前A{0,1,2}+B{3,4}；先缩容4、3，原A2因新P被解锁后按U=1更新；最终A{0,1}+B{2}，0/1 UID保留，稳定30秒 |
| RUN-213 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改f.R:3→5；门控放行新增3、4 | 缩放前A{0}+B{1,2}；新增3、4为B，不重滚原B；最终A{0}+B{1,2,3,4}，原有0/1/2 UID保持，稳定30秒；b全程保持A及原UID |
| RUN-214 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=5,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改f.R:5→3 | 缩放前A{0}+B{1,2,3,4}；全部Ready、等删除成本时按4、3缩容；最终A{0}+B{1,2}，保留0/1/2 UID不变，稳定30秒；b全程保持A及原UID |
| RUN-215 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=5,W=0,U/S/P=1/0/3→1/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改f.R:5→2，P仍为3 | 缩放前A{0,1,2}+B{3,4}；先删非保护4、3，再合法缩掉保护2；最终A{0,1}全Ready，原0/1 UID保留，不强求旧版本数=P或晋升到B，稳定30秒；b全程保持A及原UID |
| RUN-216 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/5→1/0/5；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改f.R:3→6；门控放行新3、4、5 | 缩放前全保护A{0,1,2}，无B；P仍为5，缺失3、4须用受保护历史A，5用B；最终A{0,1,2,3,4}+B{5}，原0/1/2 UID保留，稳定30秒；b全程保持A及原UID |
| RUN-217 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/"50%"→1/0/2；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改f.R:3→5；门控放行新增3、4 | P=ceil(50%×D)：2→3；缩放前A{0,1}+B{2}；2虽进入新保护范围，仍保留B及原UID，不能回退A；最终A{0,1}+B{2,3,4}，不是3个A，稳定30秒；b全程保持A及原UID |
| RUN-218 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=5,W=0,U/S/P=1/0/"50%"→1/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B并按门控达到所述灰度停点，稳定30秒后仅改f.R:5→3；门控放行新解锁的2 | P：3→2；缩放前A{0,1,2}+B{3,4}；先缩容4、3，原A2因新P被解锁后按U=1更新；最终A{0,1}+B{2}，0/1 UID保留，稳定30秒；b全程保持A及原UID |
| RUN-219 | SG | N=3；顶层U/S/P=1/0/"50%"→1/0/2；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady，保持10秒后仅改N:3→5；门控放行 | P：2→3；已开始替换的2不能因新保护范围而取消、回退或重建为A；新增3、4为B；最终A{0,1}+B{2,3,4}，稳定30秒 |
| RUN-220 | SG | N=3；顶层U/S/P=0/1/"50%"→0/1/2；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察surge 3完整但NotReady且原0/1/2未删除，保持10秒后仅改N:3→5；门控放行 | P：2→3；旧2尚未开始替换，新P必须阻止其后续滚动；原surge 3成为所需扩容容量，新增4为B，不因配置S=1额外无条件创建第6个实例；最终A{0,1,2}+B{3,4}，原A UID保留，稳定30秒 |
| RUN-221 | SG | N=4；顶层U/S/P="50%"/"25%"/1→2/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察旧3、2开始删除且所有新B仍NotReady、surge已占用，保持10秒后仅改N:4→6；门控逐个放行 | 有效U/S/P：2/1/1→3/2/1；原删除承诺继续计账，不能在扩容事件后重新获得3次无条件删除；按新容量与真实Ready校验下一次破坏动作；最终保留A0及5个B，活动数6，稳定30秒 |
| RUN-222 | SG | N=6；顶层U/S/P="20%"/"20%"/1→1/2/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察旧5开始删除、全部新B仍NotReady且两个surge容量已占用，保持10秒后仅改N:6→3；门控逐个放行 | 有效U/S/P：1/2/1→1/1/1；SG正百分比U向下取整为0时clamp到1；逐笔区分缩容回收、历史在途与后续滚动，不要求patch瞬间已满足新容量上限；最终A0及2个B，活动数3，稳定30秒 |
| RUN-223 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/"50%"→1/0/2；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady，保持10秒后仅改f.R:3→5；门控放行 | P：2→3；已开始替换的2不能因新保护范围而取消、回退或重建为A；新增3、4为B；最终A{0,1}+B{2,3,4}，稳定30秒；b全程保持A及原UID |
| RUN-224 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/"50%"→0/1/2；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察surge 3完整但NotReady且原0/1/2未删除，保持10秒后仅改f.R:3→5；门控放行 | P：2→3；旧2尚未开始替换，新P必须阻止其后续滚动；原surge 3成为所需扩容容量，新增4为B，不因配置S=1额外无条件创建第6个实例；最终A{0,1,2}+B{3,4}，原A UID保留，稳定30秒；b全程保持A及原UID |
| RUN-225 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=4,W=0,U/S/P="50%"/"25%"/1→2/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察旧3、2开始删除且所有新B仍NotReady、surge已占用，保持10秒后仅改f.R:4→6；门控逐个放行 | 有效U/S/P：2/1/1→3/2/1；原删除承诺继续计账，不能在扩容事件后重新获得3次无条件删除；按新容量与真实Ready校验下一次破坏动作；最终保留A0及5个B，活动数6，稳定30秒；b全程保持A及原UID |
| RUN-226 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P="20%"/"20%"/1→1/2/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察旧5开始删除、全部新B仍NotReady且两个surge容量已占用，保持10秒后仅改f.R:6→3；门控逐个放行 | 有效U/S/P：1/2/1→0/1/1；Role U取整为0但S=1，仍属合法更新；逐笔区分缩容回收、历史在途与后续滚动，不要求patch瞬间已满足新容量上限；最终A0及2个B，活动数3，稳定30秒；b全程保持A及原UID |
| RUN-227 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f的entry/worker模板B；观察SG2的新成员完整但NotReady后，仅改f.R:1→2；门控放行 | 保护SG0使用历史A且W=1、最新R=2，应有4个A Pod；SG1/2最终各4个B Pod；SG0原有entry/worker UID不变；扩容成员缺口与SG替换分账，未Ready不授予滚动信用；PodGroup/Ranktable与各组有效布局一致 |
| RUN-228 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=2,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f的entry/worker模板B；观察SG2的新成员完整但NotReady后，仅改f.R:2→1；门控放行 | 每组只按Role缩容规则移除多余f实例；保护SG0缩掉f1、保留f0原A entry/worker UID；最终SG0有2个A Pod、SG1/2各2个B Pod；不能将组内合法缩容误报为SG0滚动；无额外整组替换 |
| RUN-229 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B及W:1→2；门控达到SG0=A、SG1/2=B的停点并稳定30秒；仅改f.R:1→2，再门控放行 | SG0保持历史A/W=1但采用R=2，4个Pod；SG1/2采用B/W=2及R=2，各6个Pod；只新增Role容量，已有UID不因R变化而重滚；PodGroup成员及资源请求按4/6/6实际布局核对，不能按最新W把SG0误判缺worker |
| RUN-230 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=3,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B及W:1→2；门控达到SG0=A、SG1/2=B的停点并稳定30秒；仅改f.R:3→1 | 各组缩掉f2、f1，保留f0原UID；SG0最终A/W=1共2个Pod，SG1/2最终B/W=2各3个Pod；PodGroup/Ranktable同步2/3/3有效布局，不强行统一版本或worker数；稳定30秒 |
| RUN-231 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察两个SG的f2均已替换成完整NotReady的B，f0/f1与b仍Ready A，保持10秒后，仅改N:2→3；门控放行 | 已有SG0/1仍各保留受保护f0的A及UID，按各自R=3/U=1独立记账；新SG2按当前模板创建全部f为B（含f0）、b为A，不能把既有SG的旧版本保护解释成新SG必须生成旧f0；最终N=3，各f.R=3；旧SG的b UID保持 |
| RUN-232 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察两个SG的f2均已替换成完整NotReady的B，f0/f1与b仍Ready A，保持10秒后，仅改N:2→1；门控放行 | 同健康等级、等删除成本时缩掉整个SG1，包括其中受Role partition保护的f0；只在保留SG0校验f0的A及UID保护、f1/f2最终B与b原UID；Role P不能阻止顶层合法缩容；最终N=1并稳定30秒 |
| RUN-233 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察两个SG均达到f:A{0}+B{1,2}、b全A的停点并稳定30秒后，仅改N:2→3；门控放行 | 已有SG0/1仍各保留受保护f0的A及UID，按各自R=3/U=1独立记账；新SG2按当前模板创建全部f为B（含f0）、b为A，不能把既有SG的旧版本保护解释成新SG必须生成旧f0；最终N=3，各f.R=3；旧SG的b UID保持 |
| RUN-234 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察两个SG均达到f:A{0}+B{1,2}、b全A的停点并稳定30秒后，仅改N:2→1；门控放行 | 同健康等级、等删除成本时缩掉整个SG1，包括其中受Role partition保护的f0；只在保留SG0校验f0的A及UID保护、f1/f2最终B与b原UID；Role P不能阻止顶层合法缩容；最终N=1并稳定30秒 |
| RUN-235 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察surge 3完整但NotReady、全部旧A UID仍在，保持10秒后仅改N:3→5；门控逐个放行 | 原surge 3计入扩容后的所需容量，不另造重复stable槽；旧A0保护，下一次滚动删除后真实Ready必须仍≥5；新增NotReady不提供信用；最终A0及4个B、活动数5，稳定30秒 |
| RUN-236 | SG | N=5；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察surge 5完整但NotReady、全部旧A UID仍在，保持10秒后仅改N:5→3；门控逐个放行 | 缩容先选NotReady的5，再按非保护/删除成本/ordinal回收超额容量；U=0不禁止缩容，但保留集后续滚动每次删除后真实Ready须≥3；旧A0保护，最终A0及2个B、活动数3，稳定30秒 |
| RUN-237 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady后给该测试Pod UID加专用finalizer；改N:3→2；观察其deletionTimestamp后、删除完成前改N:2→3；移除本用例finalizer，门控放行 | 未完成的删除不因扩容而取消，不能复用terminating Pod UID/名称创建成功；不清空旧删除账本；待旧UID消失后正确补齐B2；最终A{0}+B{1,2}，无漏建、重复Pod或资源泄漏，稳定30秒；触发机制未实现时标MANUAL_REQUIRED，不能PASS |
| RUN-238 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；门控达到A{0}+B{1,2}并稳定30秒；仅改N:3→5，门控达到A{0}+B{1,2,3,4}并再次稳定30秒；最后仅改P:1→0 | 前两段不替换A0，也不重滚已有B；最后仅解锁并替换旧0，遵守U=1/S=0，无需再次提交B模板；最终5个B，原B1/2/3/4 UID保持，稳定30秒 |
| RUN-239 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察surge 3完整但NotReady、全部旧A UID仍在，保持10秒后仅改f.R:3→5；门控逐个放行 | 原surge 3计入扩容后的所需容量，不另造重复stable槽；旧A0保护，下一次滚动删除后真实Ready必须仍≥5；新增NotReady不提供信用；最终A0及4个B、活动数5，稳定30秒；b全程保持A及原UID |
| RUN-240 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=5,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察surge 5完整但NotReady、全部旧A UID仍在，保持10秒后仅改f.R:5→3；门控逐个放行 | 缩容先选NotReady的5，再按非保护/删除成本/ordinal回收超额容量；U=0不禁止缩容，但保留集后续滚动每次删除后真实Ready须≥3；旧A0保护，最终A0及2个B、活动数3，稳定30秒；b全程保持A及原UID |
| RUN-241 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；观察新2完整但NotReady后给该测试Pod UID加专用finalizer；改f.R:3→2；观察其deletionTimestamp后、删除完成前改f.R:2→3；移除本用例finalizer，门控放行 | 未完成的删除不因扩容而取消，不能复用terminating Pod UID/名称创建成功；不清空旧删除账本；待旧UID消失后正确补齐B2；最终A{0}+B{1,2}，无漏建、重复Pod或资源泄漏，稳定30秒；触发机制未实现时标MANUAL_REQUIRED，不能PASS；b全程保持A及原UID |
| RUN-242 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；所有后续新Pod默认NotReady，由runner按UID逐个放行 | 提交f模板B；门控达到A{0}+B{1,2}并稳定30秒；仅改f.R:3→5，门控达到A{0}+B{1,2,3,4}并再次稳定30秒；最后仅改P:1→0 | 前两段不替换A0，也不重滚已有B；最后仅解锁并替换旧0，遵守U=1/S=0，无需再次提交B模板；最终5个B，原B1/2/3/4 UID保持，稳定30秒；b全程保持A及原UID |

##### 正常自动Ready下的真实在途交错：20个

不要求NotReady停点；必须证明副本请求与尚未完成的原滚动真实重叠。窗口未命中或证据不足不得PASS，详见§A.4。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-243 | SG | N=6；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready自然推进中观察首个旧SG5开始删除，且原滚动尚未完成时立即单独改N:6→8；不等待或制造NotReady停点 | 副本patch接收时必须实际处于在途滚动；省略U/S/P，默认1/0/0；新增6、7按目标B创建；扩容前的在途动作继续计账；每条Pod事件检查允许动作与实际Ready信用，不漏掉快速连续替换；最终8个B，活动数8，全Ready并稳定30秒 |
| RUN-244 | SG | N=6；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready自然推进中观察首个旧SG5开始删除，且原滚动尚未完成时立即单独改N:6→4；不等待或制造NotReady停点 | 副本patch接收时必须实际处于在途滚动；省略U/S/P，默认1/0/0；按当时保护/健康/成本/ordinal约束回收容量，动态Ready下不预写唯一缩容序号；每条Pod事件检查允许动作与实际Ready信用，不漏掉快速连续替换；最终4个B，活动数4，全Ready并稳定30秒 |
| RUN-245 | SG | N=6；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready自然推进中观察首个旧SG5开始删除，且原滚动尚未完成时立即单独改N:6→8；不等待或制造NotReady停点 | 副本patch接收时必须实际处于在途滚动；U/S/P=1/0/1，保留旧0的A及UID；新增6、7按目标B创建；扩容前的在途动作继续计账；每条Pod事件检查允许动作与实际Ready信用，不漏掉快速连续替换；最终A0及7个B，活动数8，全Ready并稳定30秒 |
| RUN-246 | SG | N=6；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready自然推进中观察首个旧SG5开始删除，且原滚动尚未完成时立即单独改N:6→4；不等待或制造NotReady停点 | 副本patch接收时必须实际处于在途滚动；U/S/P=1/0/1，保留旧0的A及UID；按当时保护/健康/成本/ordinal约束回收容量，动态Ready下不预写唯一缩容序号；每条Pod事件检查允许动作与实际Ready信用，不漏掉快速连续替换；最终A0及3个B，活动数4，全Ready并稳定30秒 |
| RUN-247 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=∅/∅/∅→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready自然推进中观察首个旧f实例5开始删除，且原滚动尚未完成时立即单独改f.R:6→8；不等待或制造NotReady停点 | 副本patch接收时必须实际处于在途滚动；省略U/S/P，默认1/0/0；新增6、7按目标B创建；扩容前的在途动作继续计账；每条Pod事件检查允许动作与实际Ready信用，不漏掉快速连续替换；最终8个B，活动数8，全Ready并稳定30秒；b的A及UID保持 |
| RUN-248 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=∅/∅/∅→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready自然推进中观察首个旧f实例5开始删除，且原滚动尚未完成时立即单独改f.R:6→4；不等待或制造NotReady停点 | 副本patch接收时必须实际处于在途滚动；省略U/S/P，默认1/0/0；按当时保护/健康/成本/ordinal约束回收容量，动态Ready下不预写唯一缩容序号；每条Pod事件检查允许动作与实际Ready信用，不漏掉快速连续替换；最终4个B，活动数4，全Ready并稳定30秒；b的A及UID保持 |
| RUN-249 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready自然推进中观察首个旧f实例5开始删除，且原滚动尚未完成时立即单独改f.R:6→8；不等待或制造NotReady停点 | 副本patch接收时必须实际处于在途滚动；U/S/P=1/0/1，保留旧0的A及UID；新增6、7按目标B创建；扩容前的在途动作继续计账；每条Pod事件检查允许动作与实际Ready信用，不漏掉快速连续替换；最终A0及7个B，活动数8，全Ready并稳定30秒；b的A及UID保持 |
| RUN-250 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready自然推进中观察首个旧f实例5开始删除，且原滚动尚未完成时立即单独改f.R:6→4；不等待或制造NotReady停点 | 副本patch接收时必须实际处于在途滚动；U/S/P=1/0/1，保留旧0的A及UID；按当时保护/健康/成本/ordinal约束回收容量，动态Ready下不预写唯一缩容序号；每条Pod事件检查允许动作与实际Ready信用，不漏掉快速连续替换；最终A0及3个B，活动数4，全Ready并稳定30秒；b的A及UID保持 |
| RUN-251 | SG | N=4；顶层U/S/P=1/0/0→1/0/0；f:R=2,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察旧SG3开始删除且仍在途，立即单独改f.R:2→3 | N及SG预算不变；组内扩缩容与整组替换分账，新成员未实际Ready不提供整组信用；最终4个SG均用B、各f.R=3；PodGroup/Ranktable按有效成员数同步；稳定30秒 |
| RUN-252 | SG | N=4；顶层U/S/P=1/0/0→1/0/0；f:R=2,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察旧SG3开始删除且仍在途，立即单独改f.R:2→1 | N及SG预算不变；组内扩缩容与整组替换分账，新成员未实际Ready不提供整组信用；最终4个SG均用B、各f.R=1；PodGroup/Ranktable按有效成员数同步；稳定30秒 |
| RUN-253 | SG | N=4；顶层U/S/P=1/0/1→1/0/1；f:R=2,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察旧SG3开始删除且仍在途，立即单独改f.R:2→3 | N及SG预算不变；组内扩缩容与整组替换分账，新成员未实际Ready不提供整组信用；SG0保持历史A、采用新f.R=3，保留未被合法缩掉的旧成员UID；SG1/2/3最终用B；PodGroup/Ranktable按有效成员数同步；稳定30秒 |
| RUN-254 | SG | N=4；顶层U/S/P=1/0/1→1/0/1；f:R=2,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察旧SG3开始删除且仍在途，立即单独改f.R:2→1 | N及SG预算不变；组内扩缩容与整组替换分账，新成员未实际Ready不提供整组信用；SG0保持历史A、采用新f.R=1，保留未被合法缩掉的旧成员UID；SG1/2/3最终用B；PodGroup/Ranktable按有效成员数同步；稳定30秒 |
| RUN-255 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察任一SG的旧f5开始删除且仍在途，立即单独改N:2→3 | f的D仍6、U=1，各SG独立记账；新SG2按目标模板创建6个B f和A b，不重滚已有SG；所有保留SG最终f全B；保留原SG的b UID不变，最终N=3，每组f.R=6，稳定30秒 |
| RUN-256 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察任一SG的旧f5开始删除且仍在途，立即单独改N:2→1 | f的D仍6、U=1，各SG独立记账；按当时健康等级/成本/ordinal选择且只缩掉1个SG，不预设必须是SG1；所有保留SG最终f全B；保留原SG的b UID不变，最终N=1，每组f.R=6，稳定30秒 |
| RUN-257 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察任一SG的旧f5开始删除且仍在途，立即单独改N:2→3 | f的D仍6、U=1，各SG独立记账；新SG2按目标模板创建6个B f和A b，不重滚已有SG；保留的原SG最终f0为原A、f1～5为B；新增SG的f0也应为B；保留原SG的b UID不变，最终N=3，每组f.R=6，稳定30秒 |
| RUN-258 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察任一SG的旧f5开始删除且仍在途，立即单独改N:2→1 | f的D仍6、U=1，各SG独立记账；按当时健康等级/成本/ordinal选择且只缩掉1个SG，不预设必须是SG1；保留的原SG最终f0为原A、f1～5为B；新增SG的f0也应为B；保留原SG的b UID不变，最终N=1，每组f.R=6，稳定30秒 |
| RUN-259 | SG | N=6；顶层U/S/P=1/0/"50%"→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察旧5开始删除且仍在途，立即单独改N:6→8 | P：3→4；按修改时已观察到的旧UID/在途/已完成B集合判定新保护范围，既有B不回退，尚未开始且落入新保护范围的A不再启动；缩容仅删除允许集合；最终活动数8，所有保留的ordinal≥4均为B，低ordinal按历史及已发出动作验收，不强求A数=P；全Ready并稳定30秒 |
| RUN-260 | SG | N=6；顶层U/S/P=1/0/"50%"→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察旧5开始删除且仍在途，立即单独改N:6→4 | P：3→2；按修改时已观察到的旧UID/在途/已完成B集合判定新保护范围，既有B不回退，尚未开始且落入新保护范围的A不再启动；缩容仅删除允许集合；最终活动数4，所有保留的ordinal≥2均为B，低ordinal按历史及已发出动作验收，不强求A数=P；全Ready并稳定30秒 |
| RUN-261 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=1/0/"50%"→1/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察旧5开始删除且仍在途，立即单独改f.R:6→8 | P：3→4；按修改时已观察到的旧UID/在途/已完成B集合判定新保护范围，既有B不回退，尚未开始且落入新保护范围的A不再启动；缩容仅删除允许集合；最终活动数8，所有保留的ordinal≥4均为B，低ordinal按历史及已发出动作验收，不强求A数=P；全Ready并稳定30秒；b的A及UID保持 |
| RUN-262 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P=1/0/"50%"→1/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 先单独提交f PodSpec A→B；自动Ready滚动中观察旧5开始删除且仍在途，立即单独改f.R:6→4 | P：3→2；按修改时已观察到的旧UID/在途/已完成B集合判定新保护范围，既有B不回退，尚未开始且落入新保护范围的A不再启动；缩容仅删除允许集合；最终活动数4，所有保留的ordinal≥2均为B，低ordinal按历史及已发出动作验收，不强求A数=P；全Ready并稳定30秒；b的A及UID保持 |

##### 同请求修改副本数与PodSpec：12个

正常自动Ready；副本数和PodSpec必须在一个API请求中提交，不得拆成两次patch。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-263 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线直接用同一次Update/patch同时提交N:3→5和f PodSpec A→B；A/B正常自动Ready | 只允许服务端接收一个包含新副本数与B的spec快照；按新D=5解析P=1，不能先按旧D启动一轮更新；新增ordinal按新保护范围选择历史A或目标B；最终A{0}+B{1,2,3,4}，保留A UID不变；稳定30秒 |
| RUN-264 | SG | N=5；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线直接用同一次Update/patch同时提交N:5→3和f PodSpec A→B；A/B正常自动Ready | 只允许服务端接收一个包含新副本数与B的spec快照；按新D=3解析P=1，不能先按旧D启动一轮更新；先按新目标数量合法缩容，保留集再按新partition滚动；最终A{0}+B{1,2}，保留A UID不变；稳定30秒 |
| RUN-265 | SG | N=3；顶层U/S/P=1/0/"50%"→1/0/2；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线直接用同一次Update/patch同时提交N:3→5和f PodSpec A→B；A/B正常自动Ready | 只允许服务端接收一个包含新副本数与B的spec快照；按新D=5解析P=3，不能先按旧D启动一轮更新；新增ordinal按新保护范围选择历史A或目标B；最终A{0,1,2}+B{3,4}，保留A UID不变；原0/1/2全部保留A，区别于先滚到B2再扩容的A{0,1}+B{2,3,4}；稳定30秒 |
| RUN-266 | SG | N=5；顶层U/S/P=1/0/"50%"→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线直接用同一次Update/patch同时提交N:5→3和f PodSpec A→B；A/B正常自动Ready | 只允许服务端接收一个包含新副本数与B的spec快照；按新D=3解析P=2，不能先按旧D启动一轮更新；先按新目标数量合法缩容，保留集再按新partition滚动；最终A{0,1}+B{2}，保留A UID不变；稳定30秒 |
| RUN-267 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线直接用同一次Update/patch同时提交f.R:3→5和f PodSpec A→B；A/B正常自动Ready | 只允许服务端接收一个包含新副本数与B的spec快照；按新D=5解析P=1，不能先按旧D启动一轮更新；新增ordinal按新保护范围选择历史A或目标B；最终A{0}+B{1,2,3,4}，保留A UID不变；稳定30秒；b的A及UID保持 |
| RUN-268 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=5,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线直接用同一次Update/patch同时提交f.R:5→3和f PodSpec A→B；A/B正常自动Ready | 只允许服务端接收一个包含新副本数与B的spec快照；按新D=3解析P=1，不能先按旧D启动一轮更新；先按新目标数量合法缩容，保留集再按新partition滚动；最终A{0}+B{1,2}，保留A UID不变；稳定30秒；b的A及UID保持 |
| RUN-269 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/"50%"→1/0/2；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线直接用同一次Update/patch同时提交f.R:3→5和f PodSpec A→B；A/B正常自动Ready | 只允许服务端接收一个包含新副本数与B的spec快照；按新D=5解析P=3，不能先按旧D启动一轮更新；新增ordinal按新保护范围选择历史A或目标B；最终A{0,1,2}+B{3,4}，保留A UID不变；原0/1/2全部保留A，区别于先滚到B2再扩容的A{0,1}+B{2,3,4}；稳定30秒；b的A及UID保持 |
| RUN-270 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=5,W=0,U/S/P=1/0/"50%"→1/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线直接用同一次Update/patch同时提交f.R:5→3和f PodSpec A→B；A/B正常自动Ready | 只允许服务端接收一个包含新副本数与B的spec快照；按新D=3解析P=2，不能先按旧D启动一轮更新；先按新目标数量合法缩容，保留集再按新partition滚动；最终A{0,1}+B{2}，保留A UID不变；稳定30秒；b的A及UID保持 |
| RUN-271 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=2,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线用同一次Update/patch同时提交f.R:2→3、f entry/worker PodSpec A→B及W:1→2；A/B正常自动Ready | SG0受P=1保护，历史A/W=1与最新R=3组合，最终6个Pod；SG1/2用B/W=2及R=3，各9个Pod；只允许必要的组内缩容删除保护组成员，不允许借R/W合并更新重滚SG0；PodGroup/Ranktable按有效布局同步；稳定30秒 |
| RUN-272 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=2,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线用同一次Update/patch同时提交f.R:2→1、f entry/worker PodSpec A→B及W:1→2；A/B正常自动Ready | SG0受P=1保护，历史A/W=1与最新R=1组合，最终2个Pod；SG1/2用B/W=2及R=1，各3个Pod；只允许必要的组内缩容删除保护组成员，不允许借R/W合并更新重滚SG0；PodGroup/Ranktable按有效布局同步；稳定30秒 |
| RUN-273 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线用同一次Update/patch同时提交N:2→3和f PodSpec A→B；f.R=3及P=1不变，A/B正常自动Ready | 原SG0/1各保留f0旧A、更新f1/2为B，b UID保持；新SG2从当前模板创建f0/1/2全B、b为A；Role P不要求新SG生成旧版本f0；最终N=3，各f.R=3，稳定30秒 |
| RUN-274 | Role | N=2；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；沿用§A.2.2启动即创建ready文件及真实探针，A/B均自动Ready，runner不阻塞或逐个放行 | 从Ready A基线用同一次Update/patch同时提交N:2→1和f PodSpec A→B；f.R=3及P=1不变，A/B正常自动Ready | Ready A基线等健康等级/成本，先合法缩掉SG1，包括其f0；SG0保留f0旧A及b原UID，f1/2更新为B；不能因Role partition阻止顶层缩容；最终N=1，f.R=3，稳定30秒 |

#### 1.2.2 滚动过程中再次变更：29个

一轮模板更新已开始或到达灰度停点后，再提交模板、回滚、U/S/P或coordination变更；含正常回滚但不含故障修复时换镜像。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-275 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；尚有A时改C，等完成后回滚f为A | 按最新意图经历A/B/C混合；每个阶段使用正确历史，最终回A，不留下B/C幽灵实例 |
| RUN-276 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；尚有A时改C，等完成后回滚f为A | 按最新意图经历A/B/C混合；每个阶段使用正确历史，最终回A，不留下B/C幽灵实例 |
| RUN-277 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；尚有A时改C，等完成后回滚f为A | 按最新意图经历A/B/C混合；每个阶段使用正确历史，最终回A，不留下B/C幽灵实例 |
| RUN-278 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 提交f模板B并到达P=1混合停点；将P降为0 | 原受保护A此后允许更新；最终全B，不重滚已经完成的B |
| RUN-279 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 提交B并在仍有A时将P提高到3 | 新动作保护ordinal<3；已发出删除不可撤销、不回滚已B；只回收安全可回收临时容量 |
| RUN-280 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已经到达A/B混合P=1停点 | 用JSON merge patch将P设为null | API移除非nullable P、有效P=0；继续完成B，不把null解释为保留旧值 |
| RUN-281 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | B滚动中；存在Ready A和未Ready B | 将U从1降至0 | 仅后续新动作使用U=0；不重置已发出的删除额度，不要求撤销旧删除 |
| RUN-282 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | B滚动中；新B受资源阻塞 | 将U从0升至1并随后恢复调度 | 允许合法消费1个unavailable释放容量；最终全B |
| RUN-283 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | B滚动中；存在额外B | 将S从1提高为2并恢复所有B Ready | 只按新D+2上限增容，不双重计算旧surge；最终回D |
| RUN-284 | SG | N=3；顶层U/S/P=1/2/0→1/2/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | B滚动中；2个surge槽已占用且至少1个B未Ready | 将S从2降至0并恢复B Ready | 上限变化不使已在途对象瞬间消失；停止继续扩surge，安全缩回D，不额外越过U |
| RUN-285 | SG | N=3；顶层U/S/P=0/1/3→0/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续A全Ready；全保护；template期望B | 同一提交将U/S改为0/0并保持P=3 | 合法冻结，受保护UID全部保持，无新surge |
| RUN-286 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；尚有A时改C，等完成后回滚f为A | 按最新意图经历A/B/C混合；每个阶段使用正确历史，最终回A，不留下B/C幽灵实例 |
| RUN-287 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；尚有A时改C，等完成后回滚f为A | 按最新意图经历A/B/C混合；每个阶段使用正确历史，最终回A，不留下B/C幽灵实例 |
| RUN-288 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 提交f模板B；尚有A时改C，等完成后回滚f为A | 按最新意图经历A/B/C混合；每个阶段使用正确历史，最终回A，不留下B/C幽灵实例 |
| RUN-289 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 提交f模板B并到达P=1混合停点；将P降为0 | 原受保护A此后允许更新；最终全B，不重滚已经完成的B |
| RUN-290 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；连续ordinal | 提交B并在仍有A时将P提高到3 | 新动作保护ordinal<3；已发出删除不可撤销、不回滚已B；只回收安全可回收临时容量 |
| RUN-291 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate | 已经到达A/B混合P=1停点 | 用JSON merge patch将P设为null | API移除非nullable P、有效P=0；继续完成B，不把null解释为保留旧值 |
| RUN-292 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | B滚动中；存在Ready A和未Ready B | 将U从1降至0 | 仅后续新动作使用U=0；不重置已发出的删除额度，不要求撤销旧删除 |
| RUN-293 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | B滚动中；新B受资源阻塞 | 将U从0升至1并随后恢复调度 | 允许合法消费1个unavailable释放容量；最终全B |
| RUN-294 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | B滚动中；存在额外B | 将S从1提高为2并恢复所有B Ready | 只按新D+2上限增容，不双重计算旧surge；最终回D |
| RUN-295 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/2/0→1/2/0；协调∅→关；恢复=∅→RoleRecreate | B滚动中；2个surge槽已占用且至少1个B未Ready | 将S从2降至0并恢复B Ready | 上限变化不使已在途对象瞬间消失；停止继续扩surge，安全缩回D，不额外越过U |
| RUN-296 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调∅→关；恢复=∅→RoleRecreate | 连续A全Ready；全保护；template期望B | 同一提交将U/S改为0/0并保持P=3 | 合法冻结，受保护UID全部保持，无新surge |
| RUN-297 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | A全Ready | f、b同时A→B，完成后仅将b回滚A | f按0/1/1保留1个A且其余B；b按1/0/0先升级再回滚；两Role不继承预算，f不被b回滚重建 |
| RUN-298 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 从A更新f、b到B，尚有A/B混合 | 在A/B滚动中K从50%改100% | 重算额度，保留已启动reservation；依赖门控仍有效 |
| RUN-299 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 从A更新f、b到B，尚有A/B混合 | 在A/B滚动中K从50%改1% | 后续不超新额度，但不回滚已启动动作；恢复Ready后能推进 |
| RUN-300 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 从A更新f、b到B，尚有A/B混合 | 在A/B滚动中移除依赖f→b、保留K | 比例约束保留，依赖保护从此次合法提交后取消；不因配置变更额外重建等价Role |
| RUN-301 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 从A更新f、b到B，尚有A/B混合 | 在A/B滚动中移除整个coordination | 之后各Role按独立预算继续；不再承诺旧依赖约束，不生成仅策略变化的模板版本 |
| RUN-302 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；x:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=["f","b"],K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | f、b、x均从A向B滚动中 | 将参与集合从[f,b]改为[f,x]，保持无依赖边 | 合法集合变更重新计算K；b退出后独立，x加入后服从新额度；不重复启动已在途槽 |
| RUN-303 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | A/B滚动中；b仍有旧A且已具备至少1个稳定目标Ready | 新增依赖f→b | 通过当前容量检查后应用门控；保留旧路径且不撤销已发出的动作，最终全B |

## 2 故障恢复场景：236个

按故障的主对象组织。注入方式、恢复点和可用性损失都保留，归类不放宽任何滚动预算断言。

### 2.1 Pod故障：131个

业务Pod或容器的故障与恢复；controller暂停若只是故障预置手段，不改变主目标。

#### 2.1.1 Pod删除与recoveryPolicy组合：97个

entry/worker缺失、原地容器重启与恢复策略矩阵；暂停controller若仅用于构造故障窗口，不改变该例的Pod恢复主归属。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-304 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 3组A全Ready；group0受保护；f旧R=1、W=1 | 将f R:1→2、W:1→2、模板A→B；随后删除group0的f entry并等待恢复 | group0按历史A、W=1及最新R=2恢复，不能恢复成B或长期卡缺副本；其他组B收敛 |
| RUN-305 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-306 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-307 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-308 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-309 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-310 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-311 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-312 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-313 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-314 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-315 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-316 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-317 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-318 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-319 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-320 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-321 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-322 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-323 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-324 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-325 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-326 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-327 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-328 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-329 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-330 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-331 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-332 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-333 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-334 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-335 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-336 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-337 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-338 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-339 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-340 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=ServingGroupRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属完整SG；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-341 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-342 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-343 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-344 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-345 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-346 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-347 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-348 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-349 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-350 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-351 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-352 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；b:R=1,W=0；f:R=1,W=1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-353 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-354 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-355 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-356 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-357 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-358 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-359 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-360 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-361 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-362 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-363 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-364 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-365 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-366 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-367 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-368 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-369 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-370 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-371 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-372 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-373 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-374 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-375 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-376 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=RoleRecreate | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 故障恢复范围为所属f实例的entry和worker；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-377 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-378 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-379 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-380 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-381 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-382 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-383 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-384 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-385 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-386 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除未保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；故障恢复与模板更新去重，按当前允许目标恢复；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=0目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-387 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f entry Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-388 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=None | 连续ordinal；A全Ready；f每实例有entry+1 worker | 暂停controller；提交f模板B；删除受保护ordinal0的f worker Pod；恢复controller并允许新Pod Ready | 补齐真正缺失的Pod；不能把None解释为永不补建；受保护实例必须按历史A/W=1恢复，eligible部分升级B；区分外部删除与主动滚动损失，不再无预算删除额外健康实例；最终达到P=1目标、无缺失/重复实例；备注：Pod删除故障；不是容器原地重启 |
| RUN-389 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=1；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守1/0/0，None不等于禁用rollout |
| RUN-390 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=1；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守1/0/1，None不等于禁用rollout |
| RUN-391 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=1；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守0/1/0，None不等于禁用rollout |
| RUN-392 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=1；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守0/1/1，None不等于禁用rollout |
| RUN-393 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=1；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守1/1/0，None不等于禁用rollout |
| RUN-394 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；f:R=1,W=1；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守1/1/1，None不等于禁用rollout |
| RUN-395 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守1/0/0，None不等于禁用rollout |
| RUN-396 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守1/0/1，None不等于禁用rollout |
| RUN-397 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守0/1/0，None不等于禁用rollout |
| RUN-398 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守0/1/1，None不等于禁用rollout |
| RUN-399 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守1/1/0，None不等于禁用rollout |
| RUN-400 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=None | A全Ready；f entry配置restartPolicy=Always | 在f ordinal0原地杀死容器进程并观察kubelet恢复；恢复Ready后提交f模板B | 仅容器重启阶段Pod UID保持，None不触发controller重建；随后正常模板更新仍遵守1/1/1，None不等于禁用rollout |

#### 2.1.2 Pending、NotReady、Ready回退与镜像故障：24个

实际注入资源不足、持续探针失败、已Ready容量回退或坏镜像；改C用于修复故障的用例仍归此处。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-401 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Pending | 提交B并用测试资源约束令新B无法调度；记录预算停点后释放资源 | 资源不足时稳定等待，只能消费原U；U=0不得先删健康A；资源释放后自动完成；活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-402 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=NotReady | 提交可启动但readiness持续失败的B；记录停点后放开探针 | Running不能当Ready；未就绪B不提供健康旧实例的额外删除额度；恢复Ready后自动完成；活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-403 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Ready回退 | 提交B；首个B Ready后、仍有健康A时立即令该B NotReady；记录后恢复Ready | 撤销该B可用贡献，下一次主动删除重新计算预算；不主动扩大可用性损失，恢复后自动完成；活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-404 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=新镜像错误 | 提交不可拉取镜像B；出现ImagePullBackOff后改为可拉取的C | 不无限消耗健康A；C作为最新目标按预算收敛，A/B历史引用不串用；活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-405 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Pending | 提交B并用测试资源约束令新B无法调度；记录预算停点后释放资源 | 资源不足时稳定等待，只能消费原U；U=0不得先删健康A；资源释放后自动完成；活动组≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-406 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=NotReady | 提交可启动但readiness持续失败的B；记录停点后放开探针 | Running不能当Ready；未就绪B不提供健康旧实例的额外删除额度；恢复Ready后自动完成；活动组≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-407 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Ready回退 | 提交B；首个B Ready后、仍有健康A时立即令该B NotReady；记录后恢复Ready | 撤销该B可用贡献，下一次主动删除重新计算预算；不主动扩大可用性损失，恢复后自动完成；活动组≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-408 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=新镜像错误 | 提交不可拉取镜像B；出现ImagePullBackOff后改为可拉取的C | 不无限消耗健康A；C作为最新目标按预算收敛，A/B历史引用不串用；活动组≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-409 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Pending | 提交B并用测试资源约束令新B无法调度；记录预算停点后释放资源 | 资源不足时稳定等待，只能消费原U；U=0不得先删健康A；资源释放后自动完成；活动组≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-410 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=NotReady | 提交可启动但readiness持续失败的B；记录停点后放开探针 | Running不能当Ready；未就绪B不提供健康旧实例的额外删除额度；恢复Ready后自动完成；活动组≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-411 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Ready回退 | 提交B；首个B Ready后、仍有健康A时立即令该B NotReady；记录后恢复Ready | 撤销该B可用贡献，下一次主动删除重新计算预算；不主动扩大可用性损失，恢复后自动完成；活动组≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-412 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=新镜像错误 | 提交不可拉取镜像B；出现ImagePullBackOff后改为可拉取的C | 不无限消耗健康A；C作为最新目标按预算收敛，A/B历史引用不串用；活动组≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-413 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Pending | 提交B并用测试资源约束令新B无法调度；记录预算停点后释放资源 | 资源不足时稳定等待，只能消费原U；U=0不得先删健康A；资源释放后自动完成；活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-414 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=NotReady | 提交可启动但readiness持续失败的B；记录停点后放开探针 | Running不能当Ready；未就绪B不提供健康旧实例的额外删除额度；恢复Ready后自动完成；活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-415 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Ready回退 | 提交B；首个B Ready后、仍有健康A时立即令该B NotReady；记录后恢复Ready | 撤销该B可用贡献，下一次主动删除重新计算预算；不主动扩大可用性损失，恢复后自动完成；活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-416 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=新镜像错误 | 提交不可拉取镜像B；出现ImagePullBackOff后改为可拉取的C | 不无限消耗健康A；C作为最新目标按预算收敛，A/B历史引用不串用；活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-417 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Pending | 提交B并用测试资源约束令新B无法调度；记录预算停点后释放资源 | 资源不足时稳定等待，只能消费原U；U=0不得先删健康A；资源释放后自动完成；活动f实例≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-418 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=NotReady | 提交可启动但readiness持续失败的B；记录停点后放开探针 | Running不能当Ready；未就绪B不提供健康旧实例的额外删除额度；恢复Ready后自动完成；活动f实例≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-419 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Ready回退 | 提交B；首个B Ready后、仍有健康A时立即令该B NotReady；记录后恢复Ready | 撤销该B可用贡献，下一次主动删除重新计算预算；不主动扩大可用性损失，恢复后自动完成；活动f实例≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-420 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=新镜像错误 | 提交不可拉取镜像B；出现ImagePullBackOff后改为可拉取的C | 不无限消耗健康A；C作为最新目标按预算收敛，A/B历史引用不串用；活动f实例≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-421 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Pending | 提交B并用测试资源约束令新B无法调度；记录预算停点后释放资源 | 资源不足时稳定等待，只能消费原U；U=0不得先删健康A；资源释放后自动完成；活动f实例≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-422 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=NotReady | 提交可启动但readiness持续失败的B；记录停点后放开探针 | Running不能当Ready；未就绪B不提供健康旧实例的额外删除额度；恢复Ready后自动完成；活动f实例≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-423 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=Ready回退 | 提交B；首个B Ready后、仍有健康A时立即令该B NotReady；记录后恢复Ready | 撤销该B可用贡献，下一次主动删除重新计算预算；不主动扩大可用性损失，恢复后自动完成；活动f实例≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-424 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=新镜像错误 | 提交不可拉取镜像B；出现ImagePullBackOff后改为可拉取的C | 不无限消耗健康A；C作为最新目标按预算收敛，A/B历史引用不串用；活动f实例≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |

#### 2.1.3 Pod终止阻塞：6个

删除中的finalizer阻塞及解除后的回收与恢复；不与正常扩缩容中的受控终止窗口重复计数。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-425 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=终止阻塞 | 提交B；对首个待删旧实例Pod预置测试finalizer，确认terminating后移除finalizer | 把terminating与活动容量分开统计；不重复分配/遗忘删除意图；解除后自动清理并完成；活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-426 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=终止阻塞 | 提交B；对首个待删旧实例Pod预置测试finalizer，确认terminating后移除finalizer | 把terminating与活动容量分开统计；不重复分配/遗忘删除意图；解除后自动清理并完成；活动组≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-427 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=终止阻塞 | 提交B；对首个待删旧实例Pod预置测试finalizer，确认terminating后移除finalizer | 把terminating与活动容量分开统计；不重复分配/遗忘删除意图；解除后自动清理并完成；活动组≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-428 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=终止阻塞 | 提交B；对首个待删旧实例Pod预置测试finalizer，确认terminating后移除finalizer | 把terminating与活动容量分开统计；不重复分配/遗忘删除意图；解除后自动清理并完成；活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-429 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=终止阻塞 | 提交B；对首个待删旧实例Pod预置测试finalizer，确认terminating后移除finalizer | 把terminating与活动容量分开统计；不重复分配/遗忘删除意图；解除后自动清理并完成；活动f实例≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-430 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=终止阻塞 | 提交B；对首个待删旧实例Pod预置测试finalizer，确认terminating后移除finalizer | 把terminating与活动容量分开统计；不重复分配/遗忘删除意图；解除后自动清理并完成；活动f实例≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |

#### 2.1.4 容器重启与恢复宽限期：2个

运行中容器可恢复重启，校验restartGracePeriodSeconds不扩大重建范围。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-431 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=1；协调∅→关；恢复=RoleRecreate；restartGracePeriodSeconds=30 | A全Ready；entry容器可注入重启且worker健康 | 在同一轮B更新窗口中令旧entry容器发生一次可恢复重启，保持Pod存在；30秒内恢复健康 | 宽限期影响故障恢复决策，不是rollout批次间隔；不因短暂重启错误扩大重建范围，恢复后继续B |
| RUN-432 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=RoleRecreate；restartGracePeriodSeconds=30 | A全Ready；entry容器可注入重启且worker健康 | 在同一轮B更新窗口中令旧entry容器发生一次可恢复重启，保持Pod存在；30秒内恢复健康 | 宽限期影响故障恢复决策，不是rollout批次间隔；不因短暂重启错误扩大重建范围，恢复后继续B |

#### 2.1.5 滚动中外部Eviction：2个

Eviction本身是合法API操作，但这里主动制造Pod可用性损失，按故障恢复验证，区别于只配置Eviction预算的正常流程。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-433 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=3,W=0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"Role","min":1,"role":"f"} | A全Ready；f R=3 | 提交f模板B，在A/B混合时通过Eviction API驱逐1个Ready f entry | 外部eviction与rollout分别记账，共享最新readiness；不把两个预算当单一全局保证，下一轮不得基于过时可用量再删额外健康实例；恢复后完成 |
| RUN-434 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"Role","min":1,"role":"f"} | A全Ready；f R=3 | 提交f模板B，在A/B混合时通过Eviction API驱逐1个Ready f entry | 外部eviction与rollout分别记账，共享最新readiness；不把两个预算当单一全局保证，下一轮不得基于过时可用量再删额外健康实例；恢复后完成 |

### 2.2 Controller故障：28个

控制器进程、leader、事件/缓存和API访问链路故障；这些是同一版本的恢复能力验证。

#### 2.2.1 同版本重启与leader切换：8个

在滚动/删除/清理阶段重启相同镜像或切换leader，验证状态重建、继续推进和预算继承。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-435 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 更新f为B，在首个surge创建后终止并由Deployment重启同镜像controller | 从API持久化事实及历史重建状态，不重复消费预算、不误滚已完成B、不永久等待；最终无需再次人工重启完成 |
| RUN-436 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 更新f为B，在旧实例删除中终止并由Deployment重启同镜像controller | 从API持久化事实及历史重建状态，不重复消费预算、不误滚已完成B、不永久等待；最终无需再次人工重启完成 |
| RUN-437 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 更新f为B，在最后cleanup完成后终止并由Deployment重启同镜像controller | 从API持久化事实及历史重建状态，不重复消费预算、不误滚已完成B、不永久等待；最终无需再次人工重启完成 |
| RUN-438 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 更新f为B，在首个surge创建后终止并由Deployment重启同镜像controller | 从API持久化事实及历史重建状态，不重复消费预算、不误滚已完成B、不永久等待；最终无需再次人工重启完成 |
| RUN-439 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 更新f为B，在旧实例删除中终止并由Deployment重启同镜像controller | 从API持久化事实及历史重建状态，不重复消费预算、不误滚已完成B、不永久等待；最终无需再次人工重启完成 |
| RUN-440 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready | 更新f为B，在最后cleanup完成后终止并由Deployment重启同镜像controller | 从API持久化事实及历史重建状态，不重复消费预算、不误滚已完成B、不永久等待；最终无需再次人工重启完成 |
| RUN-441 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；controller双副本且启用leader election | 提交f模板B，在A/B混合时删除当前leader Pod，允许另一个实例接管 | leader切换后不重滚等价模板、不重复创建/删除，自动完成B |
| RUN-442 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；controller双副本且启用leader election | 提交f模板B，在A/B混合时删除当前leader Pod，允许另一个实例接管 | leader切换后不重滚等价模板、不重复创建/删除，自动完成B |

#### 2.2.2 Watch、事件与缓存故障：12个

丢通知、旧UID重复乱序事件、informer未同步和live audit恢复。涉及Pod删除的混合例以事件/缓存恢复为主目标。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-443 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=丢失删除通知 | 提交B；仅丢弃首个旧f实例全部Pod删除通知，API中实际完成删除；保持controller运行 | 依靠production周期live audit/API事实恢复队列，不能永久RoleDeleting；在已配置audit/retry窗口内无需重启完成；活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-444 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=丢失删除通知 | 提交B；仅丢弃首个旧f实例全部Pod删除通知，API中实际完成删除；保持controller运行 | 依靠production周期live audit/API事实恢复队列，不能永久RoleDeleting；在已配置audit/retry窗口内无需重启完成；活动组≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-445 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=丢失删除通知 | 提交B；仅丢弃首个旧f实例全部Pod删除通知，API中实际完成删除；保持controller运行 | 依靠production周期live audit/API事实恢复队列，不能永久RoleDeleting；在已配置audit/retry窗口内无需重启完成；活动组≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-446 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=丢失删除通知 | 提交B；仅丢弃首个旧f实例全部Pod删除通知，API中实际完成删除；保持controller运行 | 依靠production周期live audit/API事实恢复队列，不能永久RoleDeleting；在已配置audit/retry窗口内无需重启完成；活动f实例≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-447 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=丢失删除通知 | 提交B；仅丢弃首个旧f实例全部Pod删除通知，API中实际完成删除；保持controller运行 | 依靠production周期live audit/API事实恢复队列，不能永久RoleDeleting；在已配置audit/retry窗口内无需重启完成；活动f实例≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-448 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready；故障类型=丢失删除通知 | 提交B；仅丢弃首个旧f实例全部Pod删除通知，API中实际完成删除；保持controller运行 | 依靠production周期live audit/API事实恢复队列，不能永久RoleDeleting；在已配置audit/retry窗口内无需重启完成；活动f实例≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-449 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | A全Ready；P=1；准备原ordinal0 entry及worker UID | 暂停controller；提交B并同时删除受保护ordinal0的entry+worker；恢复controller，再注入这两个旧UID的重复乱序删除事件 | 按历史A恢复受保护实例；重复旧UID事件不能再删新UID；eligible部分完成B，最后无Deleting残留 |
| RUN-450 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=ModelServing informer暂未同步 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-451 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=Pod informer暂未同步 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-452 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | A全Ready；P=1；准备原ordinal0 entry及worker UID | 暂停controller；提交B并同时删除受保护ordinal0的entry+worker；恢复controller，再注入这两个旧UID的重复乱序删除事件 | 按历史A恢复受保护实例；重复旧UID事件不能再删新UID；eligible部分完成B，最后无Deleting残留 |
| RUN-453 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=ModelServing informer暂未同步 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-454 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=Pod informer暂未同步 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |

#### 2.2.3 Controller API读写失败：8个

Create/Delete/Status/List失败注入后重试，验证controller不把读取失败当空集合，不重复破坏容量。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-455 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=Create一次失败 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-456 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=Delete一次失败 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-457 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=Status一次失败 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-458 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=API List一次失败 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-459 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=Create一次失败 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-460 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=Delete一次失败 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-461 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=Status一次失败 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |
| RUN-462 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；测试注入点=API List一次失败 | 提交B；注入所列故障一次后恢复API/cache正常 | 错误不视为空对象集/成功完成；重试幂等、不重复创建或删除；原健康容量不被无预算清空；恢复后自动全B |

### 2.3 关联资源与插件故障：77个

补足Pod和controller进程之外的关联历史资源、读写异常及插件hook故障，仍属于滚动可靠性。

#### 2.3.1 历史持久化失败与live引用保护：60个

每行均显式注入目标ControllerRevision创建失败，兼测历史保留、混合版本、terminating引用及后续重启；不是单纯的正常再次变更或controller版本升级。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-463 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-464 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-465 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/3→1/0/3；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-466 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-467 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-468 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-469 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-470 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/3→1/0/3；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-471 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-472 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-473 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-474 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-475 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-476 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-477 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-478 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-479 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-480 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-481 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-482 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-483 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-484 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-485 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-486 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-487 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-488 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-489 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-490 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-491 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-492 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-493 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-494 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-495 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/3→1/0/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-496 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-497 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-498 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-499 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-500 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/3→1/0/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-501 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-502 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-503 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-504 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-505 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-506 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-507 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-508 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-509 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-510 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-511 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-512 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-513 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-514 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-515 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-516 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-517 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,1,2}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-518 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-519 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=1保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-520 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=3保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=0被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-521 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit=1 | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=1被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |
| RUN-522 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；revisionHistoryLimit="omitted" | A全Ready；f O={0,3,4}；b连续A；现有A历史可读 | 更新f为B时注入目标CR创建失败；确认无模板驱动创建/删除后恢复CR API；到达允许目标后再仅改f为C；保留任一实际产生的旧终止实例直至检查引用，随后释放并重启controller | 目标历史持久化先于模板驱动动作；b未变UID及其A历史保留；按P=0保护实际ordinal；所有live A/B/C、surge、terminating引用的CR不因historyLimit=默认10被回收，Data不可变；解除阻塞后自动达到最新允许目标，重启不误滚 |

#### 2.3.2 历史缺失、损坏、归属冲突与读取失败：13个

外部历史材料不可用、损坏或不等价冲突后的安全阻断与恢复。等价AlreadyExists和同名新UID边界分别放在3.3。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-523 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=缺失 | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-524 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=API读取失败 | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-525 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=Data损坏 | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-526 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=历史中缺f Role | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-527 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=owner UID错误 | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-528 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=缺失 | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-529 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=API读取失败 | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-530 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=Data损坏 | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-531 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=历史中缺f Role | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-532 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | 已到P=1的A/B停点；ordinal0受保护A；A历史状态=owner UID错误 | 删除protected ordinal0的f entry；观察失败边界，再恢复正确归属的原A历史/读取能力 | 历史未知时不伪造带A标签的B Pod、不据hash差异授权删除其他健康Pod；可诊断重试；历史恢复后按A/W=1补齐并保留保护语义 |
| RUN-533 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；crCollision="不同Data且本对象owner" | A全Ready；预置将与目标B命名冲突的CR | 提交f模板B，并让CR Create返回AlreadyExists | 不可复用或覆盖冲突历史，不授权模板驱动删除；报告可诊断冲突；清除测试冲突后自动完成B |
| RUN-534 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；crCollision="等价Data但外部owner" | A全Ready；预置将与目标B命名冲突的CR | 提交f模板B，并让CR Create返回AlreadyExists | 不可复用或覆盖冲突历史，不授权模板驱动删除；报告可诊断冲突；清除测试冲突后自动完成B |
| RUN-535 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；revisionHistoryLimit=0 | f为B、b仍A；A/B均被live对象引用 | 触发历史清理时让live引用List一次失败；恢复后再次reconcile | 不能把读取失败视为空引用；A/B历史均保留，恢复后清理仅无live引用历史 |

#### 2.3.3 插件hook失败：4个

headless-service/ranktable清理hook失败与恢复后的重试、幂等和无泄漏。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-536 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service","ranktable"] | A全Ready；测试注入点=headless-service删除hook | 提交B；令所列hook连续失败2次再恢复 | 保留清理/重试意图，状态不提前宣告清理完成；恢复后自动重新入队并清理，不泄漏旧/surge资源，不误删新UID |
| RUN-537 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service","ranktable"] | A全Ready；测试注入点=ranktable清理hook | 提交B；令所列hook连续失败2次再恢复 | 保留清理/重试意图，状态不提前宣告清理完成；恢复后自动重新入队并清理，不泄漏旧/surge资源，不误删新UID |
| RUN-538 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service","ranktable"] | A全Ready；测试注入点=headless-service删除hook | 提交B；令所列hook连续失败2次再恢复 | 保留清理/重试意图，状态不提前宣告清理完成；恢复后自动重新入队并清理，不泄漏旧/surge资源，不误删新UID |
| RUN-539 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；plugins=["headless-service","ranktable"] | A全Ready；测试注入点=ranktable清理hook | 提交B；令所列hook连续失败2次再恢复 | 保留清理/重试意图，状态不提前宣告清理完成；恢复后自动重新入队并清理，不泄漏旧/surge资源，不误删新UID |

## 3 边界及拒绝场景：169个

既包含合法但具有特殊边界语义的RUN，也包含必须被拒绝的DENY。不能把合法等待误记成拒绝，或把请求被意外接收记为通过。

### 3.1 数值与副本边界：33个

单/零副本、百分比取整、U/S/P上界及零副本再扩容；按边界主目标归类，合法场景仍保留RUN类型。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-540 | SG | N=1；顶层U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=1/0/0；活动组≤1、Ready容量≥0；最终0个受保护A+1个B，活动数回1 |
| RUN-541 | SG | N=1；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=0/1/0；活动组≤2、Ready容量≥1；最终0个受保护A+1个B，活动数回1 |
| RUN-542 | SG | N=1；顶层U/S/P=∅/0/"20%"→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=1/0/1；全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-543 | SG | N=3；顶层U/S/P="20%"/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=1/0/0；活动组≤3、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-544 | SG | N=3；顶层U/S/P="20%"/"20%"/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=1/1/0；活动组≤4、Ready容量≥2；最终0个受保护A+3个B，活动数回3 |
| RUN-545 | SG | N=5；顶层U/S/P="20%"/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=1/0/0；活动组≤5、Ready容量≥4；最终0个受保护A+5个B，活动数回5 |
| RUN-546 | SG | N=6；顶层U/S/P="20%"/"20%"/"20%"→1/2/2；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=1/2/2；活动组≤8、Ready容量≥5；最终2个受保护A+4个B，活动数回6 |
| RUN-547 | SG | N=3；顶层U/S/P="0%"/"100%"/0→0/3/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=0/3/0；活动组≤6、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-548 | SG | N=3；顶层U/S/P="100%"/0/0→3/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=3/0/0；活动组≤3、Ready容量≥0；最终0个受保护A+3个B，活动数回3 |
| RUN-549 | SG | N=3；顶层U/S/P=0/"100%"/0→0/3/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=0/3/0；活动组≤6、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-550 | SG | N=3；顶层U/S/P=1/0/"100%"→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=1/0/3；全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-551 | SG | N=3；顶层U/S/P=0/4/0→0/4/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=0/4/0；活动组≤7、Ready容量≥3；最终0个受保护A+3个B，活动数回3 |
| RUN-552 | SG | N=3；顶层U/S/P=1/0/4→1/0/4；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 有效U/S/P=1/0/4；全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-553 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=1/0/0；活动f实例≤1、Ready容量≥0；最终0个受保护A+1个B，活动数回1；b的UID保持 |
| RUN-554 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=0/1/0；活动f实例≤2、Ready容量≥1；最终0个受保护A+1个B，活动数回1；b的UID保持 |
| RUN-555 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=1,W=0,U/S/P=∅/0/"20%"→1/0/1；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=1/0/1；全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-556 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P="20%"/"20%"/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=0/1/0；活动f实例≤4、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-557 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=5,W=0,U/S/P="20%"/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=1/0/0；活动f实例≤5、Ready容量≥4；最终0个受保护A+5个B，活动数回5；b的UID保持 |
| RUN-558 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=6,W=0,U/S/P="20%"/"20%"/"20%"→1/2/2；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=1/2/2；活动f实例≤8、Ready容量≥5；最终2个受保护A+4个B，活动数回6；b的UID保持 |
| RUN-559 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P="0%"/"100%"/0→0/3/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=0/3/0；活动f实例≤6、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-560 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P="100%"/0/0→3/0/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=3/0/0；活动f实例≤3、Ready容量≥0；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-561 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/"100%"/0→0/3/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=0/3/0；活动f实例≤6、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-562 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/"100%"→1/0/3；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=1/0/3；全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-563 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/4/0→0/4/0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=0/4/0；活动f实例≤7、Ready容量≥3；最终0个受保护A+3个B，活动数回3；b的UID保持 |
| RUN-564 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/4→1/0/4；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 仅将f模板A→B；b模板保持A | 有效U/S/P=1/0/4；全部旧UID保留，不启动surge；目标停在P保护边界 |
| RUN-565 | SG | N=3；顶层U/S/P=4/0/0→4/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 连续ordinal；A全Ready | 将f模板A→B | 接受U>N，允许全部3组暂不可用；最终3组B |
| RUN-566 | SG | N=0；顶层U/S/P=0/0/0→0/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | N=0，无SG/Pod，历史A可读 | 将f模板A→B | 不创建surge或伪造缺额；零副本f保持0，目标B历史可读 |
| RUN-567 | SG | N=0；顶层U/S/P=0/0/0→0/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | N=0，无SG/Pod，模板A | 先置模板B，再将零副本维度扩到3并同时改预算为1/0/0 | 只创建目标B的3个新实例，其他实例UID保持；无幽灵Deleting |
| RUN-568 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=0,W=0,U/S/P=0/0/0→0/0/0；协调∅→关；恢复=∅→RoleRecreate | f=0且b=3 Ready，历史A可读 | 仅将f模板A→B；b模板保持A | 不创建surge或伪造缺额；零副本f保持0，目标B历史可读 |
| RUN-569 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=0,W=0,U/S/P=0/0/0→0/0/0；协调∅→关；恢复=∅→RoleRecreate | f=0且b=3 Ready，模板A | 先置模板B，再将零副本维度扩到3并同时改预算为1/0/0 | 只创建目标B的3个新实例，其他实例UID保持；无幽灵Deleting |
| RUN-570 | Role | N=0；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；协调∅→关；恢复=∅→RoleRecreate | N=0，无SG/Pod | 更新f模板为B | 无Pod；目标B历史可读取；不能因为Role默认budget而创建SG |
| RUN-571 | SG | N=0；顶层U/S/P="20%"/"20%"/"20%"→0/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 对象不存在 | 创建A后将f模板A→B | D=0时三个百分比均为0；允许预置模板，目标CR可读，无该维度实例或surge；Role模式其他b容量仍正常 |
| RUN-572 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=0,W=0,U/S/P="20%"/"20%"/"20%"→0/0/0；协调∅→关；恢复=∅→RoleRecreate | 对象不存在 | 创建A后将f模板A→B | D=0时三个百分比均为0；允许预置模板，目标CR可读，无该维度实例或surge；Role模式其他b容量仍正常 |

### 3.2 稀疏ordinal、partition与完成性边界：37个

高ordinal与稳定槽判断、稀疏保护、合法但不可推进的0/0陷阱及高ordinal终态收敛；不将RUN误改为DENY。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-573 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 更新f、b为B后：b稳定ordinal0/1/2仍A，额外ordinal3为B且Ready；f仍A | 仅触发reconcile观察f首次启动门控；随后允许b稳定槽替换并Ready | 高ordinal surge Ready本身不能作为stable target Ready放行f；待b稳定目标Ready后f才能首次启动；最终自动完成 |
| RUN-574 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数=D=3 | 将f模板A→B | 严格保护ordinal<P（P=0时保护无），不能按排序后前P个保护；3个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/0预算 |
| RUN-575 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数=D=3 | 将f模板A→B | 严格保护ordinal<P（P=1时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/0预算 |
| RUN-576 | SG | N=3；顶层U/S/P=1/0/3→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数=D=3 | 将f模板A→B | 严格保护ordinal<P（P=3时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/0预算 |
| RUN-577 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数=D=3 | 将f模板A→B | 严格保护ordinal<P（P=0时保护无），不能按排序后前P个保护；3个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足0/1预算 |
| RUN-578 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数=D=3 | 将f模板A→B | 严格保护ordinal<P（P=1时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足0/1预算 |
| RUN-579 | SG | N=3；顶层U/S/P=0/1/3→0/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数=D=3 | 将f模板A→B | 严格保护ordinal<P（P=3时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足0/1预算 |
| RUN-580 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数=D=3 | 将f模板A→B | 严格保护ordinal<P（P=0时保护无），不能按排序后前P个保护；3个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/1预算 |
| RUN-581 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数=D=3 | 将f模板A→B | 严格保护ordinal<P（P=1时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/1预算 |
| RUN-582 | SG | N=3；顶层U/S/P=1/1/3→1/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数=D=3 | 将f模板A→B | 严格保护ordinal<P（P=3时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/1预算 |
| RUN-583 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数=D=3 | 仅将f模板A→B；b模板保持A | 严格保护ordinal<P（P=0时保护无），不能按排序后前P个保护；3个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/0预算 |
| RUN-584 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数=D=3 | 仅将f模板A→B；b模板保持A | 严格保护ordinal<P（P=1时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/0预算 |
| RUN-585 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/3→1/0/3；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数=D=3 | 仅将f模板A→B；b模板保持A | 严格保护ordinal<P（P=3时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/0预算 |
| RUN-586 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数=D=3 | 仅将f模板A→B；b模板保持A | 严格保护ordinal<P（P=0时保护无），不能按排序后前P个保护；3个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足0/1预算 |
| RUN-587 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数=D=3 | 仅将f模板A→B；b模板保持A | 严格保护ordinal<P（P=1时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足0/1预算 |
| RUN-588 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数=D=3 | 仅将f模板A→B；b模板保持A | 严格保护ordinal<P（P=3时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足0/1预算 |
| RUN-589 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数=D=3 | 仅将f模板A→B；b模板保持A | 严格保护ordinal<P（P=0时保护无），不能按排序后前P个保护；3个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/1预算 |
| RUN-590 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数=D=3 | 仅将f模板A→B；b模板保持A | 严格保护ordinal<P（P=1时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/1预算 |
| RUN-591 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数=D=3 | 仅将f模板A→B；b模板保持A | 严格保护ordinal<P（P=3时保护{0}），不能按排序后前P个保护；2个eligible旧实例必须完成替换；保持受保护UID、不强制归一化名字；最终实际数3，满足1/1预算 |
| RUN-592 | SG | N=3；顶层U/S/P=0/0/3→0/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数3 | 将f模板A→B | admission按D≤P接收；runtime仍有ordinal3、4待更新但0/0无法推进：应稳定等待、不得删健康实例或误报全量完成；这是配置陷阱，不能算滚动成功；备注：合法但不可推进；验收是安全停住并记录风险 |
| RUN-593 | SG | N=3；顶层U/S/P=0/1/5→0/1/5；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数3 | 将f模板A→B | 所有现存ordinal<5，UID全保持；不启动surge；与P=3的稀疏场景区分 |
| RUN-594 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数3 | 仅重复提交完全相同A spec | 数量、模板均满足期望；不因ordinal不连续而重建/改名，不启动surge，UID全部保持 |
| RUN-595 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数3 | 仅重复提交完全相同A spec | 数量、模板均满足期望；不因ordinal不连续而重建/改名，不启动surge，UID全部保持 |
| RUN-596 | SG | N=3；顶层U/S/P=1/1/3→1/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready；实际数3 | 仅重复提交完全相同A spec | 数量、模板均满足期望；不因ordinal不连续而重建/改名，不启动surge，UID全部保持 |
| RUN-597 | SG | N=3；顶层U/S/P=1/1/"100%"→1/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 组O={0,3,4}；A全Ready | 将f模板A→B | 100%换算P=3，仍只保护ordinal0；ordinal3、4应更新，不把100%误解为保护所有现存实例 |
| RUN-598 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/0/3→0/0/3；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数3 | 仅将f模板A→B；b模板保持A | admission按D≤P接收；runtime仍有ordinal3、4待更新但0/0无法推进：应稳定等待、不得删健康实例或误报全量完成；这是配置陷阱，不能算滚动成功；备注：合法但不可推进；验收是安全停住并记录风险 |
| RUN-599 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/5→0/1/5；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数3 | 仅将f模板A→B；b模板保持A | 所有现存ordinal<5，UID全保持；不启动surge；与P=3的稀疏场景区分 |
| RUN-600 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数3 | 仅重复提交完全相同A spec | 数量、模板均满足期望；不因ordinal不连续而重建/改名，不启动surge，UID全部保持 |
| RUN-601 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数3 | 仅重复提交完全相同A spec | 数量、模板均满足期望；不因ordinal不连续而重建/改名，不启动surge，UID全部保持 |
| RUN-602 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready；实际数3 | 仅重复提交完全相同A spec | 数量、模板均满足期望；不因ordinal不连续而重建/改名，不启动surge，UID全部保持 |
| RUN-603 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/"100%"→1/1/3；协调∅→关；恢复=∅→RoleRecreate | f实例O={0,3,4}；A全Ready | 仅将f模板A→B；b模板保持A | 100%换算P=3，仍只保护ordinal0；ordinal3、4应更新，不把100%误解为保护所有现存实例 |
| RUN-604 | Role | N=1；b:R=2,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=2,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={1,2}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-605 | Role | N=1；b:R=2,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=2,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={1,2}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-606 | Role | N=1；b:R=2,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=2,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={1,2}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-607 | Role | N=1；b:R=2,W=0,U/S/P=1/0/0→1/0/0；f:R=2,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={1,2}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-608 | Role | N=1；b:R=2,W=0,U/S/P=0/1/0→0/1/0；f:R=2,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={1,2}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |
| RUN-609 | Role | N=1；b:R=2,W=0,U/S/P=1/1/0→1/1/0；f:R=2,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | f、b目标B均Ready且各2个，O={1,2}；旧A及额外容量已清理；保留滚动前的current状态 | 不重启controller，等待正常reconcile/live audit；完成后才重启做等价终态核对 | 必须自动晋升允许目标的完成状态，不因高ordinal不在[0,R)而永远UpdateInProgress；重启前后完成性一致。若当前实现不满足，记回归失败，不改成预期等待；备注：质量验收oracle；新production基线尚未Kind验证 |

### 3.3 历史命名与对象身份边界：2个

等价目标CR的AlreadyExists复用、同名ModelServing新旧owner UID隔离；不涉及controller二进制跨版本升级。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-610 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；crCollision="等价Data且本对象owner" | A全Ready；预置将与目标B命名冲突的CR | 提交f模板B，并让CR Create返回AlreadyExists | 读取并安全复用等价目标CR；没有创建竞态导致的重复Pod，最终B |
| RUN-611 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | 旧同名ModelServing已删除；其Pod/CR带旧owner UID仍残留 | 创建同名新ModelServing（新UID）及模板B | 不得采用旧UID的Pod/CR当作新对象容量、Ready或历史证据；新实例归属新UID，旧残留不诱发新对象误滚 |

### 3.4 预期拒绝：97个

所有97个DENY只在这里列一次；保存请求、错误字段、被保留的spec和UID。拒绝后原已接收滚动可能继续，不能据此认定被拒请求已生效。

#### 3.4.1 预算不可推进拒绝：13个

核心12个0/0拒绝组合及Role小百分比U归零的拒绝边界。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| DENY-001 | SG | N=3；顶层U/S/P=0/∅/∅→0/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-002 | SG | N=3；顶层U/S/P=0/∅/0→0/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-003 | SG | N=3；顶层U/S/P=0/∅/1→0/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-004 | SG | N=3；顶层U/S/P=0/0/∅→0/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-005 | SG | N=3；顶层U/S/P=0/0/0→0/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-006 | SG | N=3；顶层U/S/P=0/0/1→0/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-007 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/∅/∅→0/0/0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-008 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/∅/0→0/0/0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-009 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/∅/1→0/0/1；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-010 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/0/∅→0/0/0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-011 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/0/0→0/0/0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-012 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/0/1→0/0/1；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中预算并将f模板A→B | 拒绝0/0预算；旧Spec和Pod UID保持 |
| DENY-013 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P="20%"/0/0→0/0/0；协调∅→关；恢复=∅→RoleRecreate | 已有A全Ready，当前预算为1/0/0 | 提交表中百分比预算并将f模板A→B | 拒绝：Role正百分比向下到0，且无surge；旧Spec/UID保持 |

#### 3.4.2 数值与字段类型拒绝：30个

负值、非法百分比及字段类型等schema/webhook拒绝。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| DENY-014 | SG | N=3；顶层U/S/P=-1/1/0→非法/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U=-1不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-015 | SG | N=3；顶层U/S/P="101%"/1/0→非法/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U="101%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-016 | SG | N=3；顶层U/S/P="1.5%"/1/0→非法/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U="1.5%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-017 | SG | N=3；顶层U/S/P="25"/1/0→非法/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U="25"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-018 | SG | N=3；顶层U/S/P=0.5/1/0→非法/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U=0.5不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-019 | SG | N=3；顶层U/S/P=1/-1/0→1/非法/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S=-1不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-020 | SG | N=3；顶层U/S/P=1/"101%"/0→1/非法/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S="101%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-021 | SG | N=3；顶层U/S/P=1/"1.5%"/0→1/非法/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S="1.5%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-022 | SG | N=3；顶层U/S/P=1/"25"/0→1/非法/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S="25"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-023 | SG | N=3；顶层U/S/P=1/0.5/0→1/非法/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S=0.5不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-024 | SG | N=3；顶层U/S/P=1/1/-1→1/1/非法；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P=-1不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-025 | SG | N=3；顶层U/S/P=1/1/"101%"→1/1/非法；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P="101%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-026 | SG | N=3；顶层U/S/P=1/1/"1.5%"→1/1/非法；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P="1.5%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-027 | SG | N=3；顶层U/S/P=1/1/"25"→1/1/非法；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P="25"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-028 | SG | N=3；顶层U/S/P=1/1/0.5→1/1/非法；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P=0.5不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-029 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=-1/1/0→非法/1/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U=-1不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-030 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P="101%"/1/0→非法/1/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U="101%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-031 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P="1.5%"/1/0→非法/1/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U="1.5%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-032 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P="25"/1/0→非法/1/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U="25"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-033 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0.5/1/0→非法/1/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | U=0.5不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-034 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/-1/0→1/非法/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S=-1不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-035 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/"101%"/0→1/非法/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S="101%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-036 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/"1.5%"/0→1/非法/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S="1.5%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-037 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/"25"/0→1/非法/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S="25"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-038 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0.5/0→1/非法/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | S=0.5不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-039 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/-1→1/1/非法；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P=-1不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-040 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/"101%"→1/1/非法；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P="101%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-041 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/"1.5%"→1/1/非法；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P="1.5%"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-042 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/"25"→1/1/非法；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P="25"不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |
| DENY-043 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0.5→1/1/非法；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready，原预算1/1/0 | 提交表中非法字段，不修改业务模板 | P=0.5不符合非负整数/0%…100%整数百分比的有效字段约束；schema或webhook拒绝，存储spec与UID不变 |

#### 3.4.3 字段归属与兼容性拒绝：12个

SG/Role模式字段归属与互不兼容配置。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| DENY-044 | Role | N=1；顶层U/S/P=∅/∅/∅→1/0/0（字段归属非法）；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交Role顶层空预算配置 | Role mode禁止顶层rollingUpdateConfiguration，即使{}；拒绝且旧spec/UID保持 |
| DENY-045 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0,内联U/S/P=∅/0/∅（U忽略；S/P若存在则非法）；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交SG内联S=0配置 | SG mode禁止Role maxSurge，即使0；拒绝且旧spec/UID保持 |
| DENY-046 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0,内联U/S/P=∅/∅/0（U忽略；S/P若存在则非法）；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交SG内联P=0配置 | SG mode禁止Role partition，即使0；拒绝且旧spec/UID保持 |
| DENY-047 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调roles=∅→全部,K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交SG显式coordination配置 | coordination只可用于Role mode；拒绝且旧spec/UID保持 |
| DENY-048 | SG | N=3；strategy形态=省略type；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调roles=∅→全部,K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交省略type并写coordination配置 | type默认SG，不因coordination自动切Role；拒绝且旧spec/UID保持 |
| DENY-049 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=ServingGroupRecreate | 已有合法A全Ready | 提交Role+SG恢复配置 | Role mode与ServingGroupRecreate不兼容；拒绝且旧spec/UID保持 |
| DENY-050 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=4/1/0→4/1/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交Role U>R配置 | Role U=4超过R=3；拒绝且旧spec/UID保持 |
| DENY-051 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=0,W=0,U/S/P=∅/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交Role R=0且U省略配置 | API默认U=1超过R=0；需显式合法U=0；拒绝且旧spec/UID保持 |
| DENY-052 | SG | N=3；type=""；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交显式type空串配置 | type空串不是省略，不满足枚举；拒绝且旧spec/UID保持 |
| DENY-053 | SG | N=3；type="UnknownRollingUpdate"；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交未知type配置 | 未知枚举；拒绝且旧spec/UID保持 |
| DENY-054 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate | A/B部分更新进行中 | 仅删除rolloutStrategy.type，保留Role S/P | type默认回SG，遗留Role S/P不兼容；整次提交拒绝，不部分应用 |
| DENY-055 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate | A/B滚动中，仍有eligible旧A | 仅把S从1改0，保持U=0/P=0 | 0/0不可推进，拒绝且保留原0/1/0策略 |

#### 3.4.4 Coordination拒绝：24个

非法协调图、集合、比例和更新容量；DENY-078虽涉及在途缩容，仍只在拒绝类计数。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| DENY-056 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K=∅,依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready | 提交不含maxSkew的coordination对象 | maxSkew必填；空对象不代表默认启用；拒绝且旧spec/UID保持 |
| DENY-057 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K=∅,依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready | 提交不含maxSkew的coordination对象 | maxSkew必填；空对象不代表默认启用；拒绝且旧spec/UID保持 |
| DENY-058 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=["f","b"],K=∅,依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready | 提交不含maxSkew的coordination对象 | maxSkew必填；空对象不代表默认启用；拒绝且旧spec/UID保持 |
| DENY-059 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=["f","b"],K=∅,依赖=f→[b]；恢复=∅→RoleRecreate | A全Ready | 提交不含maxSkew的coordination对象 | maxSkew必填；空对象不代表默认启用；拒绝且旧spec/UID保持 |
| DENY-060 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K=1,依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready | 提交表中maxSkew | 必须为(0%,100%]整数百分比字符串；拒绝且旧spec/UID保持 |
| DENY-061 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="0%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready | 提交表中maxSkew | 必须为(0%,100%]整数百分比字符串；拒绝且旧spec/UID保持 |
| DENY-062 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="101%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready | 提交表中maxSkew | 必须为(0%,100%]整数百分比字符串；拒绝且旧spec/UID保持 |
| DENY-063 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="1.5%",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready | 提交表中maxSkew | 必须为(0%,100%]整数百分比字符串；拒绝且旧spec/UID保持 |
| DENY-064 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="25",依赖=∅→无边；恢复=∅→RoleRecreate | A全Ready | 提交表中maxSkew | 必须为(0%,100%]整数百分比字符串；拒绝且旧spec/UID保持 |
| DENY-065 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=["f"],K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交参与集合只有1个Role图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-066 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=["f","f","b"],K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交重复参与Role图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-067 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=["f","ghost"],K="50%",依赖=∅→无边；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交未知参与Role图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-068 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[f]；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交自环图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-069 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=b→[f],f→[b]；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交两节点环图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-070 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；c:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=b→[c],c→[f],f→[b]；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交三节点环图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-071 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[ghost]；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交未知依赖端点图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-072 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；x:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；协调roles=["f","b"],K="50%",依赖=f→[x]；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交集合外依赖端点图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-073 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；x:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；协调roles=["f","b"],K="50%",依赖=x→[b]；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交集合外依赖发起者图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-074 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b,b]；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交重复dependsOn图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-075 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=[{"dependsOn":["b"],"role":"f"},{"dependsOn":["b"],"role":"f"}]；恢复=∅→RoleRecreate | 已有合法A全Ready | 提交重复dependency owner图配置 | 静态图校验拒绝，不进入滚动；旧spec/UID保持 |
| DENY-076 | Role | N=1；b:R=1,W=0,U/S/P=0/1/0→0/1/0；f:R=1,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 单副本依赖A已经创建并Ready | 仅将f、b模板A→B，replicas仍1 | update需要同时保留旧依赖及稳定目标槽；R=1不足，即使S=1也拒绝；不能用create成功证明update可行 |
| DENY-077 | Role | N=1；b:R=3,W=0,U/S/P=0/1/3→0/1/3；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | 连续A全Ready | 提交b P=3并同时将f、b模板A→B | 必要dependency完全保护、没有目标容量，拒绝且不删旧Pod |
| DENY-078 | Role | N=1；b:R=2,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | f、b A→B进行中，旧f仍存在 | 把b R从2缩至1且维持模板B | active update移除必要稳定目标槽，拒绝；已有动作仍按上次合法spec继续 |
| DENY-079 | Role | N=1；b:R=2,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate | f、b A→B进行中，旧f仍存在 | 把b P提高至2且维持模板B | active update消除dependency目标范围，拒绝；不能只校验模板发生变化的请求 |

#### 3.4.5 结构及不可变配置拒绝：14个

结构、Gang及networkTopology不可变校验；只验收拒绝及原对象不变，不增加拓扑调度测试。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| DENY-080 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=∅；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready | 以完整Update请求省略f.workerReplicas（非保留旧字段的apply） | workerReplicas为必填，无默认0；schema拒绝且原spec不变 |
| DENY-081 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate；omitWorkerTemplateFor=["f"] | 已有合法A全Ready | 提交f W=1但不含workerTemplate的完整spec | worker数>0必须提供workerTemplate；拒绝且原spec不变 |
| DENY-082 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"SG"} | 已有合法A全Ready | 提交evictionStrategy={protectionLevel:ServingGroup}，省略minAvailable | eviction结构/数值校验拒绝；不能用rollout预算补全其缺失字段 |
| DENY-083 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"SG","min":4} | 已有合法A全Ready | 提交ServingGroup保护minAvailable=4，N保持3 | eviction结构/数值校验拒绝；不能用rollout预算补全其缺失字段 |
| DENY-084 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；gangPolicy={"minRoleReplicas":{"f":1}} | A全Ready；gangPolicy.minRoleReplicas={f:1} | 仅将已存在的gangPolicy.minRoleReplicas.f从1改0 | immutable校验拒绝，不借滚动升级绕过；原spec与健康UID保持 |
| DENY-085 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；gangPolicy={"minRoleReplicas":{"f":1}} | A全Ready；gangPolicy.minRoleReplicas={f:1} | 用merge patch将已有gangPolicy设为null | immutable校验拒绝，不借滚动升级绕过；原spec与健康UID保持 |
| DENY-086 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；networkTopology={} | A全Ready；创建时显式networkTopology:{} | 用merge patch将已有networkTopology从{}设为null | immutable校验拒绝，不借滚动升级绕过；原spec与健康UID保持 |
| DENY-087 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=∅,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate | 已有合法A全Ready | 以完整Update请求省略f.workerReplicas（非保留旧字段的apply） | workerReplicas为必填，无默认0；schema拒绝且原spec不变 |
| DENY-088 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；omitWorkerTemplateFor=["f"] | 已有合法A全Ready | 提交f W=1但不含workerTemplate的完整spec | worker数>0必须提供workerTemplate；拒绝且原spec不变 |
| DENY-089 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"SG"} | 已有合法A全Ready | 提交evictionStrategy={protectionLevel:ServingGroup}，省略minAvailable | eviction结构/数值校验拒绝；不能用rollout预算补全其缺失字段 |
| DENY-090 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；eviction={"level":"SG","min":2} | 已有合法A全Ready | 提交ServingGroup保护minAvailable=2，N保持1 | eviction结构/数值校验拒绝；不能用rollout预算补全其缺失字段 |
| DENY-091 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；gangPolicy={"minRoleReplicas":{"f":1}} | A全Ready；gangPolicy.minRoleReplicas={f:1} | 仅将已存在的gangPolicy.minRoleReplicas.f从1改0 | immutable校验拒绝，不借滚动升级绕过；原spec与健康UID保持 |
| DENY-092 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；gangPolicy={"minRoleReplicas":{"f":1}} | A全Ready；gangPolicy.minRoleReplicas={f:1} | 用merge patch将已有gangPolicy设为null | immutable校验拒绝，不借滚动升级绕过；原spec与健康UID保持 |
| DENY-093 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；networkTopology={} | A全Ready；创建时显式networkTopology:{} | 用merge patch将已有networkTopology从{}设为null | immutable校验拒绝，不借滚动升级绕过；原spec与健康UID保持 |

#### 3.4.6 扩缩容使配置失效：4个

保留已接收的初态与滚动，拒绝导致0/0无法推进、Role U超过新R或百分比U归零的副本请求。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| DENY-094 | SG | N=3；顶层U/S/P=0/0/3→0/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate | A全Ready；提交B后全保护停驻，稳定30秒 | 仅改N:3→4，U/S/P仍0/0/3 | 扩容使D>P且U=S=0，拒绝此次Update；存储副本数仍3，原3个A UID保持且不创建第4个实例；目标B仍合法停驻，稳定30秒 |
| DENY-095 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/0/3→0/0/3；协调∅→关；恢复=∅→RoleRecreate | A全Ready；提交B后全保护停驻，稳定30秒 | 仅改f.R:3→4，U/S/P仍0/0/3 | 扩容使D>P且U=S=0，拒绝此次Update；存储副本数仍3，原3个A UID保持且不创建第4个实例；目标B仍合法停驻，稳定30秒 |
| DENY-096 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=2/0/1→2/0/1；协调∅→关；恢复=∅→RoleRecreate | A全Ready；提交f模板B后，新f2/f1完整但NotReady，b仍Ready A | 仅改f.R:3→1，f.U仍为整数2 | 拒绝U=2>新R=1；存储f.R仍3，原在途替换不能因被拒请求清空或停止恢复；按原配置放行后应A{0}+B{1,2}，b UID保持 |
| DENY-097 | Role | N=1；b:R=1,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=5,W=0,U/S/P="20%"/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate | A全Ready；提交f模板B后，新f4完整但NotReady，f0/1/2/3及b仍Ready A | 仅改f.R:5→3，U/S/P仍为"20%"/0/1 | 新R下U向下取整为0且S=0、R>P，拒绝Update；存储R仍5，按原U=1持续记账；放行后达到A{0}+B{1,2,3,4}，b UID保持；不能套用SG的正百分比clamp |

## 4 Controller升级场景：60个

只收纳明确由旧controller/CRD切换到目标production版本的用例；旧环境准备与镜像版本证据遵循附录A。

### 4.1 健康旧实例的跨版本升级：54个

旧controller/CRD创建A，替换为目标production版本后健康Pod不误滚；随后真正更新业务模板。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-612 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-613 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-614 | SG | N=3；顶层U/S/P=1/0/3→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-615 | SG | N=3；顶层U/S/P=1/0/0→1/0/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-616 | SG | N=3；顶层U/S/P=1/0/1→1/0/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-617 | SG | N=3；顶层U/S/P=1/0/3→1/0/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-618 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-619 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-620 | SG | N=3；顶层U/S/P=0/1/3→0/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-621 | SG | N=3；顶层U/S/P=0/1/0→0/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-622 | SG | N=3；顶层U/S/P=0/1/1→0/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-623 | SG | N=3；顶层U/S/P=0/1/3→0/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-624 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-625 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-626 | SG | N=3；顶层U/S/P=1/1/3→1/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-627 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-628 | SG | N=3；顶层U/S/P=1/1/1→1/1/1；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-629 | SG | N=3；顶层U/S/P=1/1/3→1/1/3；f:R=1,W=0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；SG O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-630 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-631 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-632 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/3→1/0/3；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-633 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-634 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-635 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/0/3→1/0/3；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-636 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-637 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-638 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-639 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-640 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-641 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-642 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-643 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-644 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-645 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/0和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-646 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/1和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-647 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/3和实际ordinal执行，不能永久豁免旧Pod；保护区保留A，eligible区自动完成B |
| RUN-648 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/0和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-649 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/1和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-650 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/3→1/0/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/3和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-651 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/0→1/0/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/0和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-652 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/1→1/0/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/1和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-653 | Role | N=1；b:R=3,W=0,U/S/P=1/0/0→1/0/0；f:R=3,W=0,U/S/P=1/0/3→1/0/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/0/3和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-654 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/0和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-655 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/1和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-656 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/3和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-657 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/0→0/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/0和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-658 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/1→0/1/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/1和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-659 | Role | N=1；b:R=3,W=0,U/S/P=0/1/0→0/1/0；f:R=3,W=0,U/S/P=0/1/3→0/1/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按0/1/3和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-660 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/0和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-661 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/1和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-662 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,1,2}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/3和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-663 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/0→1/1/0；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/0和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-664 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/1→1/1/1；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/1和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |
| RUN-665 | Role | N=1；b:R=3,W=0,U/S/P=1/1/0→1/1/0；f:R=3,W=0,U/S/P=1/1/3→1/1/3；协调roles=∅→全部,K="50%",依赖=f→[b]；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧controller创建A；f Role O={0,3,4}；A全Ready；旧CR/标签保留 | 停止旧controller；升级CRD并提交表中策略（业务模板仍A）；启动目标controller，完成后同镜像重启一次；最后真实修改f模板B | 升级和同镜像重启阶段保留原名称、UID、IP、启动时间及revision/hash标签，不因策略/hash表示差异误滚；真实B阶段按1/1/3和实际ordinal执行，不能永久豁免旧Pod；未变b语义等价且保留UID；保护区保留A，eligible区自动完成B |

### 4.2 异常旧实例的跨版本升级：6个

升级时已有NotReady、缺worker或Deleting的旧实例；主目标仍是跨版本接管与恢复，而非普通同版本故障。

| ID | 粒度 | 原始配置 → 生效值 / 提交配置 | 初始状态 | 操作 | 预期行为 / 拒绝理由 |
| --- | --- | --- | --- | --- | --- |
| RUN-666 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧A；稀疏O={0,3,4}；ordinal0状态=旧实例NotReady，其他A健康 | 仅升级CRD/controller及表中策略，保持业务A；解除探针或终止阻塞使恢复条件具备；恢复后再真实改f为B | 模板语义判等不依赖Running；只执行原故障/缺失/删除对应恢复，不扩大到其他健康实例；随后真实B可正常滚动，避免永久Running豁免 |
| RUN-667 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧A；稀疏O={0,3,4}；ordinal0状态=旧实例缺worker，其他A健康 | 仅升级CRD/controller及表中策略，保持业务A；解除探针或终止阻塞使恢复条件具备；恢复后再真实改f为B | 模板语义判等不依赖Running；只执行原故障/缺失/删除对应恢复，不扩大到其他健康实例；随后真实B可正常滚动，避免永久Running豁免 |
| RUN-668 | SG | N=3；顶层U/S/P=1/1/0→1/1/0；f:R=1,W=1；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧A；稀疏O={0,3,4}；ordinal0状态=旧实例已Deleting，其他A健康 | 仅升级CRD/controller及表中策略，保持业务A；解除探针或终止阻塞使恢复条件具备；恢复后再真实改f为B | 模板语义判等不依赖Running；只执行原故障/缺失/删除对应恢复，不扩大到其他健康实例；随后真实B可正常滚动，避免永久Running豁免 |
| RUN-669 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧A；稀疏O={0,3,4}；ordinal0状态=旧实例NotReady，其他A健康 | 仅升级CRD/controller及表中策略，保持业务A；解除探针或终止阻塞使恢复条件具备；恢复后再真实改f为B | 模板语义判等不依赖Running；只执行原故障/缺失/删除对应恢复，不扩大到其他健康实例；随后真实B可正常滚动，避免永久Running豁免 |
| RUN-670 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧A；稀疏O={0,3,4}；ordinal0状态=旧实例缺worker，其他A健康 | 仅升级CRD/controller及表中策略，保持业务A；解除探针或终止阻塞使恢复条件具备；恢复后再真实改f为B | 模板语义判等不依赖Running；只执行原故障/缺失/删除对应恢复，不扩大到其他健康实例；随后真实B可正常滚动，避免永久Running豁免 |
| RUN-671 | Role | N=1；b:R=3,W=0,U/S/P=∅/∅/∅→1/0/0；f:R=3,W=1,U/S/P=1/1/0→1/1/0；协调∅→关；恢复=∅→RoleRecreate；upgradeFrom="70d5f82a"；upgradeTo="e2578d01" | 旧A；稀疏O={0,3,4}；ordinal0状态=旧实例已Deleting，其他A健康 | 仅升级CRD/controller及表中策略，保持业务A；解除探针或终止阻塞使恢复条件具备；恢复后再真实改f为B | 模板语义判等不依赖Running；只执行原故障/缺失/删除对应恢复，不扩大到其他健康实例；随后真实B可正常滚动，避免永久Running豁免 |

## 附录A：基线、默认行为与共同执行契约

### A.1 基线、原文保留与验证状态

- 本表固定本地 `production/release-1.0@e2578d01859bb98d9a85846bafbfb2c771a6f117`，源码核对工作树为 `kthena-resync-010/`。不是随分支移动而隐式切换基线。
- 该快照已包含revision语义判等、ControllerRevision生命周期及周期live audit相关加固。上一版分析使用更早的`4373b29e`，已有Kind代表验证使用`6fea34e0`；三者不得混用。
- [上一版原始分析](./ROLLING_UPDATE_SCENARIOS.md)保持原文，SHA-256为`c4319b80d5fe22698a2a303c0cb56fcd1d59838add0d4f5f57e07660d8950cf7`。原文中的旧基线结论和旧路径按历史材料阅读。
- [上一轮验证记录](./PROPOSAL_COMMIT.md)及[kind目录](./kind/README.md)保持不变，保留原有28个YAML；它们是旧代表场景的材料，不等于本表768行已实现或已通过。
- **上一轮新增84行均为`NOT_RUN_ON_BASELINE`，没有运行这些新增用例；本次重排也不新增验证结果。** 原684行及其verification字段保留初版登记快照，不在此次重排中批量回填。初版的“0/684”不再代表当前实际进展：`RUN-001～RUN-060`已有后续[独立runner的60项Kind记录](../../../kthena-rollout-runner/docs/KIND_RESULTS.md)，其固定副本证据不能覆盖本次扩缩交错。JSON的`counts.verifiedOnBaseline`仍按其行内登记状态统计，不作为所有外部验证记录的实时总数。
- 同步保存[机器可读清单](./ROLLING_UPDATE_CASES.json)，含同样768个ID、原始配置、初态、动作、预期、来源标签和登记状态。新增行显式记录executionProfile及subgroup。JSON是用例描述，不是可直接kubectl apply的YAML或已实现的测试驱动。预期是验收标准，不声称当前实现必然满足。

### A.2 如何读配置与公共fixture

#### A.2.1 字段和默认值

| 表中记号 | production字段/含义 | 省略时行为 |
| --- | --- | --- |
| SG / Role | `spec.rolloutStrategy.type`两种粒度 | 整个strategy省略时有效SG |
| N | `spec.replicas` | 1 |
| f / b / c / d / x | Role名称分别为frontend/backend/role-c/role-d/extra | 没列出的Role不存在 |
| R / W | Role.replicas / workerReplicas | R默认1；W必填，无默认 |
| 顶层U/S/P | `spec.rolloutStrategy.rollingUpdateConfiguration` | SG有效1/0/0；Role模式禁止该对象 |
| Role内联U/S/P | `spec.template.roles[].maxUnavailable/maxSurge/partition` | Role有效1/0/0；SG忽略Role U，拒绝Role S/P |
| 协调roles / K / 依赖 | `roleCoordination.roles/maxSkew/dependencies` | 对象省略为关闭；roles省略或[]选全部；dependencies省略或[]无边；K必填 |
| 恢复 | `spec.recoveryPolicy` | RoleRecreate；不是自动跟随rollout粒度 |
| eviction={level,min,role} | `rolloutStrategy.evictionStrategy` | 省略无eviction budget；SG写protectionLevel=ServingGroup/minAvailable，Role写protectionLevel=Role/roleMinAvailable[role] |
| gangPolicy / networkTopology | `spec.template`下对应字段 | 省略；默认gang全量要求，不等于coordination |
| restartGracePeriodSeconds | `spec.template.restartGracePeriodSeconds` | 0；故障恢复宽限期，不是发布批次间隔 |
| revisionHistoryLimit | `spec.revisionHistoryLimit` | 10；live引用优先，不计入非live保留上限 |

`∅`是本次完整输入未配置；`0`是显式整数零；`"0%"`是字符串零百分比；`null`单独保留为API默认化测试输入。`∅/1/∅→1/1/0`表示原始U/S/P与有效整数预算。对不合法输入的“→”仅辅助解释，不能代替整体验证。

SG的D=N，Role的D=对应R。U百分比向下取整，SG在D>0且正百分比取整为0时最小为1；Role不作此clamp。S/P百分比向上取整。百分比按最新D计算。Role U不得超过R；SG整数U可超过N；S整数可以超过D。P是ordinal阈值，不是“现存实例排序后前P个”。

策略形态：`∅`省略整个对象；`{}`仅空strategy；“仅type”不含预算对象；“省略type”由API默认SG；“仅eviction”除eviction外省略type/预算；`null`按服务端默认化/删除后状态验收。没标特殊形态时显式写表中mode，是否含顶层预算以该列为准。协调图`f→[b]`展开为`dependencies: [{role: frontend, dependsOn: [backend]}]`。JSON保留数组以测试重复dependency owner，不先转map去重。

**配置列一般是初始配置；若初态/操作明确说“已有合法旧配置，再提交表中非法/新配置”，则表中配置是此次提交目标。运行中调整以操作列的明确新值覆盖初始值。** Admission测试采用完整Create/Update或明确删除字段的patch；不能用遗漏字段但实际保留旧值的SSA请求冒充默认值测试。

#### A.2.2 公共production形态fixture（不是API默认）

除行内明确覆盖外，沿用保留YAML的生产形态：`schedulerName: volcano`，显式启用headless-service和ranktable，Ranktable引用`production020-ranktable-template`及保留的bootstrap配置；工作负载用`busybox:1.36`作确定性Kind替身，CPU/memory requests为5m/4Mi，entry及每个worker均用`touch /tmp/ready; sleep 3600`、1秒`test -f /tmp/ready`探针。这验证控制器滚动语义，不验证真实模型/GPU/RPC兼容性。

每行列出的Role均有entryTemplate；W>0自动具备同版本workerTemplate，除非拒绝用例显式要求缺失。A/B/C默认只改变对应entry/worker的`ROLLOUT_VERSION`环境变量，模板其余字段保持；镜像、探针、W、成员等变更按行覆盖。完全最小对象用例另行省略scheduler/plugins以验证API默认，不把fixture的插件选择误记为产品默认。

无特别说明时A全Ready、N/R满足、ordinal从0连续、CR.Data及owner正确、没有外部故障；Role模式未点名变更的Role保持A。稀疏O只应用于该行指明的粒度及Role，不隐式扩展到其他Role。coordination按组独立，K检查稳定槽计算，不使用“全部B Pod占比”替代。

**拓扑与调度的范围边界：本表验证滚动行为，不验证集群拓扑调度能力。** 保留`DENY-086`（SG）和`DENY-093`（Role）的`networkTopology`不可变校验：初始为空对象`{}`，尝试改为`null`，仅断言请求被拒绝、原spec和健康Pod UID不变。不配置实际拓扑约束，也不检查Pod是否落在满足约束的节点或拓扑域。

本表不要求建设HyperNode、拓扑树或专用节点标签，不纳入拓扑亲和/反亲和、跨拓扑域放置等调度正确性验收。gang、recoveryPolicy及其与滚动的组合继续保留。Pending/NotReady用例只验证新容量不可用时的滚动预算和恢复推进；schedulerName变更用例只验证是否引发无意滚动，均不扩展为调度算法或拓扑放置测试。拓扑范围说明本身不增删用例；上一轮增加的84行来自独立的扩缩交错类别。

### A.3 统一验收与证据（不另计场景）

无故障、固定D、普通模板替换时检查活动容量≤D+S、Ready容量≥max(D−U,0)；活动、terminating、实际Pod总数分开记录。期望扩缩、外部故障、用户删Role、S收缩及已发出的删除不能生硬套用这个瞬时不变量。Role/SG预算单位不是单个Pod。

全保护和旧依赖保留允许合法混合停点；合法但无法推进的稀疏0/0场景要求安全等待并记录配置风险，不能填“滚动成功”。需要完成的场景，在资源、API、探针、finalizer等前置条件恢复后必须自动收敛；不能靠额外重启解锁。完成性与实际可用容量分别验收，不仅看条件名或revision标签字符串。

每个ID应保留`before.yaml`、`after.yaml`及中间patch、server-defaulted spec、镜像commit/架构、UID/IP/ordinal时间线、Ready/删除原因与日志、CR.Data/owner/live引用和最终status。故障行必须明确记录注入目标UID、触发点及恢复点；如果观察窗口已错过，应重建该独立用例，不能悄悄替换注入对象。

测试先准备隔离Kind和Volcano、按节点架构构建固定commit镜像，再逐行执行。旧版本升级行先由`70d5f82a`及其兼容CRD创建A，再停止旧controller、升级CRD后提交新策略；不能把旧版本不支持的maxSurge字段直接塞进旧CRD。暂停/重启controller、删除Pod、修改历史、注入API/hook/事件故障都只限隔离测试环境。

其中API错误、丢watch、历史损坏、高ordinal中间态等用例需要明确的注入/预置机制；本次未新增或运行这些机制。后续落地时先保存完整YAML和驱动，再回填真实结果。原有28份YAML不应被覆盖成新矩阵的失败证据。

### A.4 扩缩容交错用例的共同阶段契约

本节保留上一轮84项扩展集的执行契约，不重复展示用例行，也不再作为一个与四大类并列的类别。80个运行用例现位于1.2.1，4个拒绝位于3.4.6；1.2.1另外包含原表35个相关用例。本节的执行方式和断言适用范围不因重排而扩大到所有768行。

上一轮扩展集的运行用例为 `RUN-195～RUN-274`；相关4个拒绝用例见§3.4.6的 `DENY-094～DENY-097`。**扩展集共84个独立用例，正文中的每行均已展开，没有隐含“SG/Role再各测一次”。** 只补充规划，不表示runner已经支持这些动作或已完成Kind验证。

| 子类 | 运行数 | 编号 |
| --- | ---: | --- |
| 在途替换与整数预算 | 12 | RUN-195～RUN-206 |
| 停驻灰度与partition边界 | 12 | RUN-207～RUN-218 |
| 在途partition与百分比预算重算 | 8 | RUN-219～RUN-226 |
| 跨粒度扩缩容与历史成员布局 | 8 | RUN-227～RUN-234 |
| surge重分类与连续阶段 | 8 | RUN-235～RUN-242 |
| 正常自动Ready下的真实交错 | 20 | RUN-243～RUN-262 |
| 单请求同时修改副本数与PodSpec | 12 | RUN-263～RUN-274 |
| 扩缩容使配置失效 | 0运行+4拒绝 | DENY-094～DENY-097 |
| **扩展集合计** | **80运行+4拒绝=84** | 不重复累计 |

Ready门控只是其中一种观测方式，不是扩缩容交错的业务前提。执行方式独立列出，不用门控结果代替正常运行结果：

| 执行方式 | 运行数 | 编号 | Ready与触发方式 |
| --- | ---: | --- | --- |
| `HOLD_READY` | 48 | RUN-195～RUN-242 | 手动控制探针条件，命中确定的在途或灰度停点 |
| `AUTO_READY_INTERLEAVE` | 20 | RUN-243～RUN-262 | 正常自动Ready，滚动未完成时再发独立副本变更请求 |
| `ATOMIC_SPEC_SCALE` | 12 | RUN-263～RUN-274 | 正常自动Ready，从A基线用同一个请求同时修改副本数与PodSpec |

这三行只拆分上述80个运行用例，不再增加总数。4个拒绝用例使用门控/稳定灰度初态单独验证admission。

覆盖四种作用域：SG滚动时调整N、SG滚动时调整f.R、Role滚动时调整f.R、Role滚动时调整N。百分比预算按实际滚动粒度重算：SG的D=N，Role的D=f.R；不能把Role模式的N变化当成f的预算分母变化。

既有对照用例 `RUN-163～RUN-165`、`RUN-179～RUN-181` 等仅更新编号，保留原内容：先扩缩后滚动、模板/副本同时提交，本类将正常在途、门控在途和同请求变更分别展开；新增同请求用例明确细化partition、只改一个副本维度及历史worker布局，不重复搬运旧表的N/R同时+1行。已有 `DENY-078` 覆盖协调依赖在途缩容的容量拒绝，本类引用但不复制计数。这里不宣称穷尽coordination、gang、recoveryPolicy与每种扩缩时机的全交叉；这些维度在原类别继续保留，不增加拓扑放置要求。

#### 共同判定口径

以下是上述84行的组成部分，不是额外场景，也不回写原684行的执行契约：

1. **区分三个时序。** 先List/Watch ModelServing、Pod、PodGroup、ControllerRevision，再建立Ready A基线。HOLD_READY用行内实际Pod状态触发，不用sleep猜测；AUTO_READY_INTERLEAVE在首个指定旧Pod开始删除、原滚动仍在途时立即发第二次副本请求，不要求NotReady被长时间阻塞；ATOMIC_SPEC_SCALE从A基线只发一个含副本数与B PodSpec的请求，不能拆成先扩缩后改模板的两次请求。未到达指定前提不能跳过动作后PASS。
2. **Ready观测相同，控制方式不同。** 仅HOLD_READY覆盖§A.2.2的自动Ready命令：A/B及entry/worker启动时不创建ready文件，runner先放行基线A，后续新UID（包括受保护历史A）默认NotReady；逐个放行逻辑单位全部成员，等实际Ready后记一次信用，再观察10秒。未放行就Ready才是该模式控制失效。另两种模式沿用启动即创建ready文件、kubelet真实探针的普通fixture，runner不干预Ready，不通过额外启动延迟、finalizer或暂停controller把它们伪装成门控场景。三种模式都不写Pod.status，都逐事件记录实际Pod Ready。
3. **时序与在途继承。** 一个用例多次patch共用连续UID账本；记录每次请求的输入、返回对象/generation、原始事件与完整成员。patch成功或observedGeneration不能作为跨资源Watch的全序屏障，不能清空先前删除承诺。已发出的删除不因扩容或新partition而撤销；首次成员删除即记录该SG/Role的破坏动作，不能等所有Pod都消失才记账。
4. **三种账目分开但不能漏算。** 分别记录期望扩容缺口、合法缩容删除和保留实例的滚动替换，并同时展示全部实际Pod/逻辑单位的可用性。新增NotReady不是旧实例开始滚动，也不是Ready信用。缩容按新目标数量、已存在在途删除及保护/NotReady/删除成本/ordinal规则限定允许集合和次数；不得把扩缩容期间的任意删除都归为合法缩容。不能从Pod删除事件单独确定原因时，保留破坏事实，结合预先声明的允许动作集合校验；仍歧义则报告证据不足，不猜原因后PASS。
5. **动态预算逐次约束后续破坏。** 百分比U/S/P按该阶段最新D和各自舍入规则解析，继承旧动作。扩容刚提交时Ready低于新D−U、缩容刚提交时活动数高于新D+S，不单凭这个瞬时差值判滚动失败；但不得关闭该阶段预算检查。合法缩容之外的每次新滚动破坏，扣除已观察到且尚未反映为容量损失的整单位删除承诺后，剩余完整Ready容量须满足新下限；不能重复扣除已计入不可用的同一单位。超额旧容量只能有界回收，不能因阶段切换重复创建新surge。阶段收敛后重新严格检查目标容量/布局、Ready下限与清理结果。
6. **一个可写死的容量序列。** `RUN-197` 中初始A0/A1 Ready、B2 NotReady，再N:3→5，新增B3/B4均NotReady。保持不放行时，不允许开始替换A1；依次放行B2、B3后，Ready分别为3、4，仍不允许删除A1；放行B4后Ready=5，才有一次删除A1后保持Ready≥4的信用。A0始终禁止滚动。这是本例固定阶段预期，不推广为所有扩缩场景的通用固定D公式。
7. **partition按ordinal和历史解释。** 保护的是尚未开始的滚动替换，不是缩容最小副本数。必要的缩容可以删除保护实例；整数P不会随D变化，百分比P会重算；进入保护范围的既有B不能回退A。动态场景不以“最终A=P”作为通用公式。R变化引起revision标识变化时，按Pod实际模板语义、版本标记及历史成员布局识别A/B，不把原始hash字符串变化直接算成模板更新。
8. **顺序分开验收。** 普通替换对当阶段允许的旧ordinal按降序开始；缩容先选非保护，再按NotReady优先、删除成本低优先、ordinal高优先。本类没有额外设置deletion cost；写死序号的缩容行要求触发时对应健康等级与成本相同。跨SG/Role的独立动作只要求各自合法偏序，不人为拼成一个全局先后序列；行内没有指定精确B ordinal的surge终态，按保留A集合、合法创建/删除集合、B数量与UID历史校验，不强制补齐高ordinal洞。
9. **停点与终态不是瞬间快照。** 全部目标成员真实Ready、允许更新已完成、应缩容/旧surge资源已清理、受保护保留集及派生资源一致后，才启动连续30秒稳定观察。期间任何不允许的开始删除、重建或继续滚动都失败；不能把收敛前的等待时间算进窗口。每个行内灰度停点和最终停点都执行此契约；有限观察窗口不等价于永久不会继续滚动。
10. **材料与执行状态。** 每个新增ID后续落地时保留独立case YAML、before/after及所有中间patch YAML、server-defaulted spec、触发点/每次放行/每次阶段终态快照、Pod/PodGroup原始Watch和UID/Ready/容量账本；正常Ready模式同样必须保存全过程，不能只保存终态。当前JSON只是规划，未生成这些新增可运行YAML，也未执行新测试；原有28份Kind YAML及runner核心60份case YAML不动。`RUN-237/RUN-241` 的Pod finalizer只加到本用例拥有的确切UID；未支持该动作时显式标`MANUAL_REQUIRED`，不算通过，并保存清理手段。

11. **正常Ready必须证明真实重叠。** 记录旧版本更新启动事件、副本请求发送/成功时间及返回spec、请求后仍存在的原滚动未完成事实（例如live Pod读取仍有原先应更新的A，或对应替换尚未完成），结合连续Watch校验。仅“先收到一个删除事件再发请求”不足以证明请求发生在滚动结束前，事件可能滞后。窗口未命中记`TRIGGER_MISSED`，证据不足或Watch断档记`INCONCLUSIVE`，均非PASS；最多重建独立基线重试3次，保留每次证据，不偷偷改成门控、增加副本数或延迟Ready来通过。自然Ready下事件可快速交错，按各阶段允许动作的偏序验收，不把门控用例唯一序列强加给它。
12. **同请求与先灰度后扩容的终态可能不同。** SG的N=3、P=50%：先完成灰度得到A{0,1}+B{2}再扩到5（RUN-211），B2必须保留，最终A{0,1}+B{2,3,4}；从全A直接一次提交N=5和B（RUN-265），新P=3，原0/1/2全部保护，最终A{0,1,2}+B{3,4}。两者不是重复用例，不能只按最终spec生成同一份版本数量断言。

### A.5 原分析、源码与历史验证材料

原分析是规则解释层，本表是固定用例层；机器清单的`refs`保存来源标签，不把同一用例的多个标签累计为多个场景。引用某个标签表示该行验证其中所写的性质，不声称该标签与所有其他维度都已全交叉。旧内存对象绕过API的nil helper等单元级防御分支，应由对应单元测试补充，不能虚构为API Server可提交的新YAML。

| 核对内容 | 本地来源 |
| --- | --- |
| API字段/默认/CRD标记 | [model_serving_types.go](../../../kthena-resync-010/pkg/apis/workload/v1alpha1/model_serving_types.go)、[servinggroup_types.go](../../../kthena-resync-010/pkg/apis/workload/v1alpha1/servinggroup_types.go) |
| 预算、字段归属、恢复、不可变校验 | [validator.go](../../../kthena-resync-010/pkg/model-serving-controller/webhook/validator.go) |
| coordination静态图与update容量检查 | [role_coordination_validator.go](../../../kthena-resync-010/pkg/model-serving-controller/webhook/role_coordination_validator.go) |
| 百分比及模板判等 | [utils.go](../../../kthena-resync-010/pkg/model-serving-controller/utils/utils.go)、[revision_util.go](../../../kthena-resync-010/pkg/model-serving-controller/utils/revision_util.go) |
| 运行预算、历史、故障恢复、live audit | [model_serving_controller.go](../../../kthena-resync-010/pkg/model-serving-controller/controller/model_serving_controller.go) |
| 比例、依赖和稳定ordinal计数 | [role_rolling_update_coordinator.go](../../../kthena-resync-010/pkg/model-serving-controller/controller/role_rolling_update_coordinator.go) |
| 升级误滚动 | issues提交`27e52d3`中的`bugs/016-upgrade-sparse-ordinals-pod-recreation-DONE/PROPOSAL_COMMIT.md` |
| 历史生命周期 | issues提交`832c684`中的`bugs/021-role-rollingupdate-controllerrevision-DONE/PROPOSAL_COMMIT.md` |
| 删除恢复、事件丢失 | issues提交`fc10c26`中的`bugs/010-role-deleting-recovery-hardening-DONE/PROPOSAL_COMMIT.md` |

这些源码链接是工作树导航，证据基线仍以上方完整commit为准；工作树后续变化时应使用`git show e2578d01:<path>`还原。

最终核对时issues工作树位于`example-yaml`分支，未检出上述三个DONE记录的正文；因此历史记录固定到已验证存在的issues提交，用`git -C issues show <提交>:<路径>`读取，不切换用户分支，也不留下指向不存在正文的链接。

后续若实现驱动或加固controller，仍按工作区proposal-first流程评审；本表本身不批准修改实现，也没有把尚未执行的Kind测试记为完成。

上一轮（2026-09-07）补表一致性校验记录：Markdown与JSON均为768个唯一ID（671运行/97拒绝）；新增84行的粒度、初态、动作、预期逐字段一致，执行方式数量为48/20/12运行加4拒绝。与本次修改前的快照逐项比对，原684条JSON记录和684行Markdown用例内容均未变；原始分析SHA-256仍为上方记录值，原28份Kind YAML校验和均未变。该校验只验证文档/清单一致性，不是新增场景的Kind通过记录。

## 附录B：原分类迁移对照（不另计场景）

旧分类在JSON的legacyGroup/legacyStats中保留；下表只说明其768个ID分别迁入四大类的数量，不增加用例。涉及多个验证目标的行按正文归类规则选择唯一主归属。

| 原分类 | 原总数 | 1 正常 | 2 故障恢复 | 3 边界/拒绝 | 4 Controller升级 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 基础配置组合 | 60 | 60 | 0 | 0 | 0 |
| 默认与提交形态 | 12 | 12 | 0 | 0 | 0 |
| 数值与副本边界 | 33 | 0 | 0 | 33 | 0 |
| Role协调组合 | 33 | 32 | 0 | 1 | 0 |
| 稀疏ordinal与partition | 32 | 1 | 1 | 30 | 0 |
| 恢复策略全交叉 | 96 | 0 | 96 | 0 | 0 |
| 运行故障与自动恢复 | 52 | 0 | 52 | 0 | 0 |
| 变更触发与运行中改配置 | 93 | 93 | 0 | 0 | 0 |
| 历史升级防误滚动 | 60 | 0 | 0 | 0 | 60 |
| ControllerRevision与混合历史 | 76 | 1 | 73 | 2 | 0 |
| 完成性与重启恢复 | 20 | 6 | 8 | 6 | 0 |
| Eviction、调度与插件 | 24 | 18 | 6 | 0 | 0 |
| 滚动中扩缩容与partition灰度 | 84 | 80 | 0 | 4 | 0 |
| 预算不可推进 | 13 | 0 | 0 | 13 | 0 |
| 数值与字段类型拒绝 | 30 | 0 | 0 | 30 | 0 |
| 字段归属与兼容性拒绝 | 12 | 0 | 0 | 12 | 0 |
| Coordination拒绝 | 24 | 0 | 0 | 24 | 0 |
| 结构及不可变配置拒绝 | 14 | 0 | 0 | 14 | 0 |
| **合计** | **768** | **303** | **236** | **169** | **60** |

上一轮分类重排一致性校验已通过：768个ID各出现一次；Markdown/JSON同序、每行归属与标题一致、分类统计合计768；配置、初态、动作、预期、来源、执行方式和验证状态逐项保留，仅替换已移动章节的导航引用。原始分析、原28份Kind YAML及runner核心60份case YAML共89个文件的校验和与重排前完全一致。本轮不修改runner或controller实现，也不将未运行的Kind用例改为通过。

本轮RUN重编号校验：671个运行ID按正文顺序连续，97个DENY编号和行内容不变；Markdown与JSON同序；配置、初态、动作、预期、来源、分类、执行方式与验证状态保持不变。正文和附录的编号区间、JSON中的扩缩扩展集及交叉引用已同步；索引新增当前编号范围，清单仅保留当前编号。原始分析、旧Runner V1设计、保留的28份Kind YAML、4份设计示例YAML及runner核心60份YAML保持不变。本次只验证文档和编号一致性，不新增Kind通过记录。
