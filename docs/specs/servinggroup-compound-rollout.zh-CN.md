# ServingGroupRollingUpdate 组合场景预期行为规范 v2.2

版本 2.7 · 2026-10-08 · [English](servinggroup-compound-rollout.en.md)

本文描述**设计预期**；实现符合程度与实际 Kind 验证结果另行记录。正文保留 `ServingGroupRollingUpdate` 的全部 35 个组合场景；共用原则也说明 `RoleRollingUpdate` 的适用范围。[附录 B](#budget-lookup)提供两种模式的预算与行为对照，不代表新增了相同数量的 Role 执行用例。

<a id="basic-principles"></a>

## 0. 阅读指南与基础原则

**核心原则：**`spec.replicas` 和 Role 的 `replicas` 变更是扩缩容；`workerReplicas` 或 Pod 模板变更是滚动更新。按配置替换完整 ServingGroup 或完整 Role 实例，全部必需成员 Ready 才计为可用。每次动作都按最新配置和实际状态核对 `maxUnavailable`、`maxSurge` 与 `partition`；默认先处理合法旧 NotReady，同类按序号从高到低，有 Role 协调时不跳过稳定候选。保留健康且无需更新的实例身份。

下文按变更、单位、预算、顺序、版本和身份展开；具体过程见[场景表](#scenario-tables)，完整约束见[附录 A](#behavior-rules)。

### 0.1 变更分类：数量扩缩与模板滚动

ModelServing 有三个不同的副本数层次：ServingGroup 数量、每组内的 Role 实例数量、每个 Role 实例内部的 worker 数量。**`workerReplicas` 增加或减少都属于模板滚动更新**，按配置的滚动策略替换相应单元。

| 变更字段 | 变更含义 | 基础行为 |
| --- | --- | --- |
| `spec.replicas` | ServingGroup 数量，例如 2 → 3 | 扩缩 SG 数量；单独修改不产生模板 revision |
| `spec.template.roles[].replicas` | 每个 SG 内该 Role 的实例数量，例如 1 → 2 | 扩缩 Role 实例数量；单独修改不产生模板 revision，已有实例继续使用各自适用的模板 |
| `spec.template.roles[].workerReplicas` | 一个 Role 实例内部的 worker 数，例如 0 → 1、2 → 1 | 改变 entry + workers 的实例布局，产生模板更新，遵守生效层的滚动预算与 partition |
| `spec.template.roles[].entryTemplate` / `workerTemplate` | entry / worker Pod 模板，例如镜像更新 | 按配置的 SG 或 Role 滚动策略更新 |
| 生效层的 `maxUnavailable` / `maxSurge` / `partition` | 滚动预算或保护范围 | 重新约束后续动作；单独修改不产生模板 revision |

worker 数变化时，尚未轮到更新的旧实例仍按自己的历史 worker 布局判断完整性和 Ready；不能把新增 worker 要求立刻套到所有旧实例上，误判为缺 Pod 并绕过滚动预算重建。替换时按槽位使用适用的模板布局：非保护槽位使用新布局，受保护槽位按 §0.5 使用历史布局；所有必需成员 Ready 后才恢复该单元的可用性。

例如一个 SG 内有 3 个 Role 实例，每实例从 1 个 worker 改为 2 个 worker：Role 实例数仍是 3，改变的是三个实例各自的布局。使用 Role 模式时，以完整 Role 实例滚动；使用 SG 模式时，以完整 SG 滚动。字段范围与校验见 [API §1.4](modelserving-api-reference.zh-CN.md#14-副本数与-role-模板)。

### 0.2 滚动单位、预算范围与 Ready

| 滚动策略 | 更新单位 | 生效预算与作用范围 | 未变更 Role 的身份 |
| --- | --- | --- | --- |
| `ServingGroupRollingUpdate` | 完整 SG，包含组内全部 Role 实例及其 Pod | 顶层 `rollingUpdateConfiguration`，以 SG 数量计数 | 随 SG 一起替换，即使该 Role 模板没有变化 |
| `RoleRollingUpdate` | 一个完整 Role 实例，即 1 个 entry + 该实例所需全部 worker | 对应 Role 的 `maxUnavailable` / `maxSurge` / `partition`，在每个 SG 内独立计算 | 未变更模板的 Role 保留原 Pod UID |

只有更新单元的**全部必需成员 Ready**，才计一个 Ready。Role 模式不能把 entry Ready 当成整个实例 Ready，也不能把 worker Pod 数直接当成可用 Role 数。不同 Role、不同 SG 不互借 Role 预算；多个 SG 可以各自推进 Role 滚动。

非生效层预算允许配置但忽略。`roleCoordination` 仅适用于 Role 模式；依赖与进度约束叠加在本地预算之上。模式限制见 [API §2](modelserving-api-reference.zh-CN.md#2-rolloutstrategytype)。

### 0.3 预算：先核算实际容量，再批准动作

| 约束 | 判断原则 |
| --- | --- |
| 预算基数 | 使用最新期望副本数；`maxUnavailable` 百分比向下取整，`maxSurge` 和 `partition` 百分比向上取整 |
| 活动容量 | 实际活动单元数不超过“期望副本数 + maxSurge”；删除中的单元彻底消失前仍占容量，创建预约也不能漏记 |
| 健康删除 | 每次删除前重新核对真实 Ready；删除后不得低于 `max(0, 期望副本数 − maxUnavailable)` |
| 已有故障 | protected、旧版本及可用 surge 均参与真实可用性统计，已承诺删除的容量不再计 Ready；若故障已使 Ready 低于底线，仍可在总清理额度内替换合法的过期 NotReady 单元，但不能进一步减少 Ready |
| 新容量与在途动作 | 只计实际已创建的 surge；最新目标 NotReady surge 同时计入活动数与目标未就绪数。同一删除/补建额度不能重复领取，在途账本与已反映的状态变化只记一次 |

正式扩容槽尚未 Ready 的缺口不能当成额外滚动信用；仅配置了 `maxSurge` 也不等于已经获得可用容量。缩容使已有活动数短暂超过新上限时，只能清理或等待，不能继续创建加重超额。

完整公式、总清理额度与健康删除上限的区别见 [API §2.3](modelserving-api-reference.zh-CN.md#23-两层共用的预算与默认选择顺序)；数值例子见[附录 B](#budget-lookup)。预算给出动作上界；每个完整单元 Ready 后重新结算并按合法剩余额度继续，不额外等待整批。

### 0.4 候选选择：先看资格，再看顺序

候选必须先满足版本、partition、身份、依赖和在途约束；顺序规则不能突破预算或容量上限。下面的顺序用于模板滚动；显式缩容的删除选择另见 §0.6。

| 模式 | 旧实例选择顺序 | 最高健康旧实例受阻时 |
| --- | --- | --- |
| SG 模式 | 合法旧 NotReady 优先，同类按 ordinal 高到低，再处理旧 Ready | 可先处理合法的低位旧 NotReady |
| Role 模式，未配置 `roleCoordination` | 同 SG 模式 | 可先处理合法的低位旧 NotReady |
| Role 模式，配置 `roleCoordination` | 稳定旧实例按 ordinal 高到低，叠加依赖与进度约束 | 等待，不跳过最高稳定候选 |

以上为默认行为，无新增开关。`maxSkew` 表达百分比进度约束，不承诺同 index 配对更新。合法回收废弃的旧 NotReady **临时 surge** 不属于跳过稳定候选，但仍须检查预算、依赖和在途动作，也不返还稳定启动额度。

已是最新目标但仍 NotReady 的单元，不因模板滚动而反复重建；明确 `recoveryPolicy` 的故障恢复另行适用。连续提交 v1 → v2 → v3 时，以最新目标重新判断旧版本和候选，不要求先完成 v2；已发出的删除不能撤销。

### 0.5 partition 与版本：按绝对序号确定保护

| 状态或动作 | 版本与身份原则 |
| --- | --- |
| 现存实例的 ordinal 小于 `partition` | 不因目标模板变化而主动更新；保护的是固定序号，不是当前排序后的前若干实例 |
| 新建或补建受保护槽位 | 按动作当时的 partition 使用本轮灰度前固定、可追溯的历史模板；历史无法确认时安全等待 |
| 新建或补建非保护槽位 | 使用最新目标模板；范围外旧组替换到低位空洞时，由**新槽位**的绝对序号决定版本 |
| 已存在的新版实例因 partition 提高进入保护区 | 保留现有实例，不为主动回到旧版而重建；后续真实删除后的补建仍按适用历史规则处理 |
| v1 → v2 → v3 尚未全量完成 | 中间版本不会自动成为新的受保护基线；全量完成后才可推进该基线 |

partition 限制模板更新，不禁止显式缩容删除受保护实例。受保护区的故障仍消耗可用性预算；Role 实例数扩容时，受保护 SG 使用历史 Pod/worker 模板。具体过程见 SG-P01～P11 和 SG-R01。

局部故障修复沿用所属滚动单元已经应用的模板和 worker 布局，不借修复提前应用最新目标。ServingGroupRollingUpdate 的滚动单元是完整 SG，RoleRollingUpdate 的滚动单元是一个 Role 实例；完整滚动单元重建才按动作当时的 partition 选择受保护基线或最新目标。已有 v2 单元因 partition 提高而被保护时，仅修其中一个 Pod/Role 仍保持该单元已应用的 v2；只有整个单元重建才重新按保护区基线选择，可能回到 v1。历史无法可靠确定时等待并报告，不猜测最新版本。 这里的“槽位新建/重建”指完整滚动单元；局部故障补员按本段处理，恢复范围见 API §6.1。

### 0.6 扩缩容、稀疏序号与 surge 身份

同时扩缩容和更新时，先安排最新规模下的正式容量，再考虑健康旧单元的破坏性滚动。正式扩容槽须先安置，但新实例不必全部 Ready；每次健康删除仍须重新检查预算。SG 模式下，Role 实例数增加若会使整组暂时 NotReady，应分批应用，未轮到的组按已应用的成员目标判断 Ready，守住同一可用性底线。

| 场景 | 身份与顺序原则 |
| --- | --- |
| 单独缩容 | 在正式保留组中先选 NotReady，再选较低 deletion cost，最后选较高 ordinal；可留下稀疏序号，缩容删除不再重复计作滚动不可用 |
| 后续扩容 | 用实际新增的容量槽优先补最低空洞；一次动作不要求补齐全部旧空洞 |
| 必须替换的旧模板组 | 正式范围内优先原位补建；范围外旧组可借替换机会补最低旧洞，按新槽位选择历史/目标模板 |
| 健康且无需更新的保留组 | 即使位于期望序号范围外，也不为排齐序号而删除重建 |
| 临时 surge 被扩容吸收 | 只有 ordinal 进入新的正式范围且能承接正式容量时才转正，保留 UID；仅总数等于期望值不足以判定转正 |
| 临时 surge 仍提供必要 Ready 容量 | 等替代容量 Ready 后再清理；高 ordinal 的正式保留组不因此变成临时 surge |

只有显式缩容可以新增稳定空洞；滚动或故障造成的在途缺位应继续补建，不能当成新的稳定稀疏结果。详细反例见 SG-S01～S04、SG-C14 和 SG-P11。

### 0.7 完成、阻塞与场景表读法

规模完成、partition 灰度停点和全量目标版本完成要分别判断。滚动完成须满足：所有符合更新条件的保留单元达到最新目标并 Ready、已发替换完成、临时 surge 已清理、规模符合期望；已有且无法无扰动补齐的稀疏序号可以保留。灰度停点可能仍包含历史版本，不能宣称全量采用了目标版本。

坏目标、历史 revision 缺失、预算耗尽、容量或协调限制都可能阻塞。此时等待并说明原因，不能隐式放宽预算或为表面连续而重建健康实例。每个完整单元 Ready 后，按最新状态重新计算 maxScaleDown、maxHealthyScaleDown 和 inFlightReservations；存在合法候选且预算、partition、身份、物理容量及协调条件允许时继续推进，不等待原批次其他单元全部 Ready。Ready 回落或其他故障会重新消耗额度，不能把一次 Ready 事件当作永久或重复信用。协调 Role 仍遵守稳定候选顺序、dependencies 和 maxSkew。 部分就绪例子见附录 B.5。

场景中的 v1 / v2 / v3 表示先后提交的模板版本；“Ready”表示完整单元就绪；“NotReady”表示尚未完整就绪；“删除中”仍占活动容量；“无”表示该 ordinal 没有实例。配置使用完整字段名；计数与预算使用 [API §2.3](modelserving-api-reference.zh-CN.md#23-两层共用的预算与默认选择顺序) 的完整名称，含义见[附录 B](#budget-lookup)。

每张过程表是一条**允许的执行过程**，异步事件不必严格逐行发生；“活动 4→2；Ready 2～4”表示删除期间活动数逐步减少，Ready 数可在该区间变化。阅读时同时核对数量、版本、UID、Ready 与临时/保留身份，不能只看最终数量。

<a id="scenario-tables"></a>

## 1. 缩容稀疏、机会性补洞与顺序取舍

### SG-S01：单独缩容留洞；一次扩容不强制排齐

`desiredReplicas=4`，sg-0～sg-3 均为 v1 Ready。自定义删除优先级（deletion cost）使 sg-1/sg-2 成为缩容对象，保留健康 sg-3；随后分别扩到 3 和 4。

| 阶段 | 期望副本数 | sg-0 | sg-1 | sg-2 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 4；Ready 4 | 连续 |
| 缩容 | 2 | v1 Ready | v1 删除中 | v1 删除中 | v1 Ready | 活动 4→2；Ready 2～4 | 缩容可选择低 cost；不能当成滚动 maxUnavailable 消耗 |
| 缩容终态 | 2 | v1 Ready | 无 | 无 | v1 Ready | 活动 2；Ready 2 | `{0,3}` 是允许的稀疏保留集 |
| 扩容 | 3 | v1 Ready | v1 NotReady | 无 | v1 Ready | 活动 3；Ready 2 | 只有一个新增容量槽，先补最低洞 1 |
| 扩容终态 | 3 | v1 Ready | v1 Ready | 无 | v1 Ready | 活动 3；Ready 3 | `{0,1,3}`；不为补 2 重建健康 sg-3 |
| 再扩容 | 4 | v1 Ready | v1 Ready | v1 NotReady→v1 Ready | v1 Ready | 活动 4；Ready 3→4 | 补 2；sg-3 直接进入新范围且 UID 不变 |

这就是“一次动作不保证恢复连续”的正例。若只有第一次扩容，不再发生新动作，旧空洞 `{2}` 可以稳定存在；控制器日常处理不主动消灭它。

### SG-S02：滚动可以用“本来就过期”的高位组偿还旧空洞

从 `desiredReplicas=2,{sg-0 v1,sg-3 v1},partition=0,maxUnavailable=1,maxSurge=0` 提交目标 v2；sg-3 也过期，且在 `[0,2)` 外。

| 阶段 | sg-0 | sg-1 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- |
| 初始 | v1 Ready | 无 | v1 Ready | 活动 2；Ready 2 | 空洞 1 来自以前的缩容 |
| 1 | v1 Ready | 无 | v1 删除中 | 活动 2→1；Ready 1 | 最高旧版 sg-3 本就需要替换 |
| 2 | v1 Ready | v2 NotReady→v2 Ready | 无 | 活动 2；Ready 1→2 | 其替代容量落到最低洞 1，不重建健康目标版组 |
| 3 | v1 删除中 | v2 Ready | 无 | 活动 2→1；Ready 1 | 再处理旧版 sg-0，不能把替代容量放到高位 |
| 终态 | v2 Ready | v2 Ready | 无 | 活动 2；Ready 2 | sg-0 原位重建；旧空洞顺带清零 |

### SG-S03：不能为了排齐而删除健康、目标版本的高位组

`desiredReplicas=2,partition=0,maxUnavailable=1,maxSurge=0`，之前缩容留下 `{sg-0 v1,sg-3 v2}`，当前目标仍为 v2。

| 阶段 | sg-0 | sg-1 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- |
| 初始 | v1 Ready | 无 | v2 Ready | 活动 2；Ready 2 | sg-3 v2 已健康且无需更新 |
| 1 | v1 删除中 | 无 | v2 Ready | 活动 2→1；Ready 1 | 只更新旧版 sg-0 v1 |
| 终态 | v2 Ready | 无 | v2 Ready | 活动 2；Ready 2 | 旧洞 1 尚在；不删除 sg-3 v2 换 sg-1 v2 |

即使配置 `maxSurge=1`，也不能用“先造 sg-1 v2 再删除 sg-3 v2”绕过少重建原则；sg-3 v2 是保留组，不是待清理的临时 surge。需要准确标记两类身份。

### SG-S04：扩容后旧 surge 序号仍不在正式范围内，不能按总数误复用

缩容曾留下保留组 `{sg-0 v1,sg-1 v1,sg-3 v2}`，`desiredReplicas=3,maxUnavailable=0,maxSurge=1`；sg-3 v2 已是目标版本，为更新 sg-1 v1 和 sg-0 v1 创建了 sg-4 v2 临时 surge。此时 desiredReplicas:3→4，目标仍是 v2。新的正式序号范围是 0～3：它包含保留组 sg-3，却**不包含** sg-4；虽然活动数恰好为新 desiredReplicas=4，正式保留组仍只有三个，sg-2 必须补建。

| 阶段 | 期望副本数 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3 | v1 Ready | v1 Ready | 无 | v2 Ready | v2 Ready（临时 surge） | 活动 4；Ready 4 | sg-3 v2 是健康保留组，sg-4 v2 是临时组 |
| 扩容 | 4 | v1 Ready | v1 Ready | v2 NotReady | v2 Ready | v2 Ready（临时 surge） | 活动 5；Ready 4 | 新上限 5；在最低空洞 2 建正式 sg-2 v2 |
| 扩容 Ready | 4 | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5；Ready 5 | sg-4 v2 未因“总数=desiredReplicas”被错误转正 |
| 滚动 | 4 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 4～5；Ready 4→5 | 借 sg-4 v2 的服务容量滚 sg-1 |
| 终态 | 4 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 4；Ready 4 | 再滚 sg-0，最后清理 sg-4 v2；sg-3 v2 UID 不变 |

与 SG-C03/P10 不同，本例的旧 surge 序号**仍在新的正式范围外**；“复用”取决于身份是否进入新范围，不能只看当前组数。

### SG-S05：稀疏扩容形成低位故障，默认优先修复

先从 3 组缩到 2 组，删除代价较低的 sg-1 被移除，保留 sg-0/2:v1。扩回 3 组并提交 v2，在低洞 1 创建的 v2 持续 NotReady；再提交 v3。预算 desiredReplicas=3/maxUnavailable=1/maxSurge=0/partition=0。SG 与无 coordination 的 Role 默认旧 NotReady 优先，可以跳过健康旧 2 修复旧坏 1，无需额外开关。

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| v1 稳态 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 活动 3；Ready 3 | 初始健康 |
| 缩容 | 2/v1 | v1 Ready | v1 删除中 | v1 Ready | 活动 3→2；Ready 2～3 | 按删除代价移除 1 |
| 留洞 | 2/v1 | v1 Ready | 无 | v1 Ready | 活动 2；Ready 2 | 保留高位正式组 2 |
| 扩容并提交 v2 | 3/v2 | v1 Ready | v2 NotReady | v1 Ready | 活动 3；Ready 2 | 正式新增槽补低洞 |
| 提交 v3 | 3/v3 | v1 Ready | v2 NotReady | v1 Ready | 活动 3；Ready 2 | minAvailable=2、maxScaleDown=1、maxHealthyScaleDown=0，不能删健康 2 |
| 优先修复 1 | 3/v3 | v1 Ready | v2 删除中→v3 NotReady | v1 Ready | 活动 2～3；Ready 2 | 只删除旧坏 1，原位补 v3 |
| 修复完成 | 3/v3 | v1 Ready | v3 Ready | v1 Ready | 活动 3；Ready 3 | 恢复健康删除余量 |
| 滚高位 2 | 3/v3 | v1 Ready | v3 Ready | v1 删除中→v3 Ready | 活动 2～3；Ready 2→3 | 健康旧候选按高到低 |
| 滚 0 | 3/v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | 活动 2～3；Ready 2→3 | 最后一个旧组 |
| 终态 | 3/v3 | v3 Ready | v3 Ready | v3 Ready | 活动 3；Ready 3 | 无额外健康损失 |

在相同状态的 **Role + coordination 对照**中，最高旧候选 2 健康且 maxHealthyScaleDown=0，仍必须等待，不能跳过它先修 1；maxScaleDown=1 不覆盖顺序、maxSkew 或依赖约束。若新建的 v3-1 也不 Ready，unavailableTargetReplicas=1/maxScaleDown=0，应等待而非反复重建同版。本版替代此前 SG-S05 的严格顺序阻塞预期；执行用例迁移状态见附录 B。

### SG-S06：两组滚到错误 v2 后，用 v3 修复

表中的同批全部 Ready 轨迹表示这些完成事件在下一次协调前均已发生，不是必须等待整批的屏障；部分 Ready 的可区分轨迹见附录 B.5。

期望 5 组，maxUnavailable=2、maxSurge=0。v1 全部 Ready；同一批次选最高的 sg-4、sg-3 更新为 v2，但错误配置使两组都无法 Ready。此时已恰好达到“至少 3 组 Ready”的底线。沿用 Kubernetes Deployment `rolling.go` 的额度演算：v2 为当前目标时，`maxScaleDown=5-3-2=0`，不得继续清理；提交 v3 后，坏 v2 变成旧版，空的 v3 暂无不可用组，`maxScaleDown=5-3-0=2`，可以同批清理最高的两组。

| 阶段 | 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 5；Ready 5 | 尚未更新 |
| 同批启动 | v2 | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | v1 删除中 | 活动 3～5；Ready 3 | maxUnavailable=2 同时选择最高的 sg-4、sg-3 |
| 两组 v2 未就绪 | v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 错误版本占满 maxUnavailable=2 |
| v2 阻塞 | v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | maxScaleDown=0；不能删健康 sg-2，也不重建同版本 v2 |
| 提交修正版本 | v3 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 坏 v2 变旧版，maxScaleDown=2 |
| 同批清理坏 v2 | v3 | v1 Ready | v1 Ready | v1 Ready | v2 删除中 | v2 删除中 | 活动 3～5；Ready 3 | 选择顺序 4→3；两组原本都不可用 |
| 创建 v3 | v3 | v1 Ready | v1 Ready | v1 Ready | v3 NotReady | v3 NotReady | 活动 5；Ready 3 | 此时 maxScaleDown 又为 0，不能继续删 sg-2 |
| v3 就绪 | v3 | v1 Ready | v1 Ready | v1 Ready | v3 Ready | v3 Ready | 活动 5；Ready 5 | maxUnavailable=2，可同批选择剩余最高旧组 sg-2、sg-1 |
| 同批删除 sg-2、sg-1 | v3 | v1 Ready | v1 删除中 | v1 删除中 | v3 Ready | v3 Ready | 活动 3～5；Ready 3 | 按 2→1 选组；Ready 恰到最低要求 3 |
| 同批创建 v3 | v3 | v1 Ready | v3 NotReady | v3 NotReady | v3 Ready | v3 Ready | 活动 5；Ready 3 | unavailableTargetReplicas=2、maxScaleDown=0；不得再删 sg-0 |
| 两组 v3 就绪 | v3 | v1 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | 活动 5；Ready 5 | 本轨迹两组均已 Ready；若仅一组先 Ready，也可按剩余额度处理 sg-0 |
| 滚 sg-0 | v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | 活动 4～5；Ready 4→5 | 最后更新 |
| 终态 | v3 | v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | 活动 5；Ready 5 | 全部完成 |

如果用户没有提交新版本，sg-4/sg-3 已经是目标 v2，“仍 NotReady”本身不构成再次滚动的理由。

### SG-S07：初始版本五组全 NotReady，允许不降级修复

表中的同批全部 Ready 轨迹表示这些完成事件在下一次协调前均已发生，不是必须等待整批的屏障；部分 Ready 的可区分轨迹见附录 B.5。

期望 5 组，maxUnavailable=2、maxSurge=0。已创建的 v1 五组全都 NotReady，Ready 数为 0，故障在滚动前就已超出预算。用户提交修正版本 v2；虽然无法立即达到“至少 3 组 Ready”，旧版五组都已不可用，初始 `maxScaleDown=5-3-0=2`：可按 4→3、2→1、0 分批替换，每个完整 v2 单元 Ready 后重新计算清理额度；不必等前批全部 Ready。

| 阶段 | 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始故障 | v1 | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | 活动 5；Ready 0 | 已有故障，不是本轮滚动造成 |
| 提交修正版本 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | 活动 5；Ready 0 | maxScaleDown=2；先选最高的 4、3 |
| 第一批清理 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v1 删除中 | v1 删除中 | 活动 3～5；Ready 0 | 删除两组不再降低 Ready |
| 第一批创建 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v2 NotReady | v2 NotReady | 活动 5；Ready 0 | maxScaleDown=0；若 v2 也坏，在此阻塞 |
| 第一批就绪 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v2 Ready | v2 Ready | 活动 5；Ready 2 | maxScaleDown 恢复为 2 |
| 第二批清理 | v2 | v1 NotReady | v1 删除中 | v1 删除中 | v2 Ready | v2 Ready | 活动 3～5；Ready 2 | 继续按 2→1 选择 |
| 第二批创建 | v2 | v1 NotReady | v2 NotReady | v2 NotReady | v2 Ready | v2 Ready | 活动 5；Ready 2 | maxScaleDown=0；任一完整目标组 Ready 后重算 |
| 第二批就绪 | v2 | v1 NotReady | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 4 | 已超过底线 3 |
| 修复 sg-0 | v2 | v1 删除中→v2 NotReady→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 最后一个旧版组 |
| 终态 | v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 全部恢复 |

若首批 v2 也持续 NotReady，`unavailableTargetReplicas=2,maxScaleDown=0`，停止后续清理并报告阻塞；不会因为五个 v1 都坏而一次重建五组。后续操作等待版本再次修正或明确的故障恢复策略。

## 2. 滚动过程中扩缩容

<a id="sg-c01滚动中扩容s0"></a>

### SG-C01：滚动中扩容，maxSurge=0

`desiredReplicas:3→5,maxUnavailable=1`，v1→v2；sg-2 的删除已发出。

| 阶段 | 期望副本数 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | v1 稳态 |
| 1 | 3 | v1 Ready | v1 Ready | v1 删除中→v2 NotReady | 无 | 无 | 活动 2～3；Ready 2 | 高位先滚 |
| 2 | 5 | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | v2 NotReady | 活动 5；Ready 2 | 扩容槽 3、4 直接用 v2；不撤销旧在途动作 |
| 3 | 5 | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 规模阶段全部 Ready 后再删健康旧组 |
| 4 | 5 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 继续 sg-1 |
| 5 | 5 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 最后更新 sg-0 |
| 终态 | 5 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 全部 v2 |

阶段 2 的 Ready 低于新 `desiredReplicas-maxUnavailable=4`，是扩容缺口/既有在途的事实，**不是**可以再删一组 v1 的许可。

### SG-C02：滚动中缩容

`desiredReplicas:3→2,maxUnavailable=1`；v1→v2 时 sg-2 已在创建 v2，但新 desiredReplicas 不再需要它。

| 阶段 | 期望副本数 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3 | v1 Ready | v1 Ready | v2 NotReady | 活动 3；Ready 2 | 在途 sg-2 v2 不会 Ready |
| 1 | 2 | v1 Ready | v1 Ready | v2 删除中 | 活动 3→2；Ready 2 | 缩容先选 NotReady sg-2 v2，不继续滚健康组 |
| 2 | 2 | v1 Ready | v1 Ready | 无 | 活动 2；Ready 2 | 缩容完成 |
| 3 | 2 | v1 Ready | v1 删除中→v2 Ready | 无 | 活动 1～2；Ready 1→2 | 最高保留旧组 sg-1 |
| 4 | 2 | v1 删除中→v2 Ready | v2 Ready | 无 | 活动 1～2；Ready 1→2 | 最后 sg-0 |
| 终态 | 2 | v2 Ready | v2 Ready | 无 | 活动 2；Ready 2 | 不复活 sg-2 |

### SG-C03：滚动中扩容复用旧 surge

`desiredReplicas:3→5,maxUnavailable=0,maxSurge=1,partition=0`，原 sg-0～sg-2 均为 v1 Ready，临时组 sg-3 为 v2 Ready。新 desiredReplicas 使 sg-3 v2 转为保留组；sg-4 v2 是新保留组，sg-5 v2 才占新 surge 槽。

| 阶段 | 期望副本数 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3 | v1 Ready | v1 Ready | v1 Ready | v2 Ready（临时 surge） | 无 | 无 | 活动 4；Ready 4 | 上限 4 |
| 1 | 5 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | v2 NotReady（临时 surge） | 活动 6；Ready 4 | sg-3 v2 保 UID；新上限 6 |
| 2 | 5 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 6；Ready 6 | 取得健康删除信用 |
| 3 | 5 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 先滚最高旧组 sg-2 |
| 4 | 5 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 再滚 sg-1 |
| 5 | 5 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 最后滚 sg-0 |
| 终态 | 5 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 5；Ready 5 | 临时 sg-5 v2 清理；不产生新洞 |

Kubernetes Deployment 的 surge 仅是聚合容量，不承诺复用某个 Pod UID；“sg-3 v2 原地转为保留组”是 ModelServing 利用稳定序号自行定义的规则。

### SG-C12：滚动中缩容，旧 surge 序号超出新范围

`desiredReplicas:4→3,maxUnavailable=0,maxSurge=1`；sg-0～sg-3 均为 v1 Ready，sg-4 是 v2 Ready 的临时 surge。新物理上限从 5 变 4，但 sg-4 v2 的**序号**超出 `[0,desiredReplicas+maxSurge)=[0,4)` 不等于必须先删它。缩容本来就要移除最高旧版保留组 sg-3 v1；等 sg-3 v1 完全消失，sg-4 v2 仍可占唯一临时槽，为更新 sg-2 v1/sg-1 v1/sg-0 v1 提供 Ready 信用。这样无需重建 sg-4 v2，也不把要缩掉的 sg-3 v1 先滚成 sg-3 v2。

| 阶段 | 期望副本数 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready（临时 surge） | 活动 5；Ready 5 | 旧上限 5 |
| 缩容 | 3 | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | v2 Ready（临时 surge） | 活动 5→4；Ready 4～5 | sg-3 v1 是本来就要缩掉的旧版保留组 |
| 缩容完成 | 3 | v1 Ready | v1 Ready | v1 Ready | 无 | v2 Ready（临时 surge） | 活动 4；Ready 4 | 活动数满足新上限；sg-4 v2 仍是临时组 |
| 滚 2 | 3 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | 无 | v2 Ready（临时 surge） | 活动 3～4；Ready 3→4 | 借 sg-4 v2 守住 Ready≥3 |
| 滚 1 | 3 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | 无 | v2 Ready（临时 surge） | 活动 3～4；Ready 3→4 | sg-2 Ready 后继续 |
| 滚 0 | 3 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | 无 | v2 Ready（临时 surge） | 活动 3～4；Ready 3→4 | 最后 sg-0 |
| 终态 | 3 | v2 Ready | v2 Ready | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 清理旧 sg-4 v2 surge；未重建它 |

<a id="sg-c15百分比-u-随扩容重算坏-v2-阻塞"></a>

### SG-C15：百分比 maxUnavailable 随扩容重算，坏 v2 阻塞

maxUnavailable 设为 25%：期望 5 组时向下取整，可容忍 1 组不可用，至少要有 4 组 Ready；扩到 9 组后可容忍 2 组不可用，至少要有 7 组 Ready。此例没有 surge；新增的不可用额度不是扩容之外的“免费删除额度”。

| 阶段 | 期望组数 / maxUnavailable / Ready 底线 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | sg-6 | sg-7 | sg-8 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/1/4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | 无 | 无 | 无 | 无 | 活动 5；Ready 5 | 高位 sg-4 已完成 |
| 扩容 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | v2 NotReady | v2 NotReady | v2 NotReady | 活动 9；Ready 5 | 新组先用 v2；不能删 sg-3 v1 |
| 部分 Ready | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | v2 NotReady | 活动 9；Ready 7 | 恰在底线，仍不能删 sg-3 v1 |
| 获得信用 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | 活动 9；Ready 8 | 现在可删一组健康 v1 |
| 滚 3 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 删除中→v2 NotReady | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | 活动 8～9；Ready 7 | sg-3 与扩容中 sg-8 共占 maxUnavailable=2 |
| 恢复 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 9；Ready 9 | sg-3 v2 与 sg-8 v2 均 Ready |
| 滚 2 | 9/2/7 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 8～9；Ready 8→9 | 继续高到低 |
| 滚 1 | 9/2/7 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 8～9；Ready 8→9 | sg-2 Ready 后继续 |
| 滚 0 | 9/2/7 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 8～9；Ready 8→9 | 最后 sg-0 |
| 终态 | 9/2/7 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 9；Ready 9 | 全部 v2 |
| 坏 v2 分支 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | v2 NotReady | v2 NotReady | v2 NotReady | 活动 9；Ready 4 | v2 永不 Ready；不再删 sg-3 v1 |

“坏 v2 分支”是另一次运行：初始 sg-4 v2 也未 Ready，不是从上面的 sg-4 v2 Ready 状态倒退。即使 `maxUnavailable=2`，该分支的真实 Ready=4 已低于 7，唯一合法进展是修正目标或恢复组，不能继续破坏健康 v1。

### SG-C16：已创建的 surge 与新版 NotReady 必须同时入账

`desiredReplicas=3,maxUnavailable=1,maxSurge=1,partition=0`，最少需要 2 个 Ready 组。旧版 sg-2 原本就 NotReady；目标改为 v2，并在 sg-3 建立临时 surge。表中 `activeReplicas/readyReplicas/unavailableTargetReplicas/maxScaleDown` 依次是活动组数、Ready 组数、目标 v2 的 NotReady 组数、`maxScaleDown=max(0,activeReplicas-(desiredReplicas-maxUnavailable)-unavailableTargetReplicas)`。只在删除彻底完成后才重建同名组；表中的“无”是更新在途的短暂缺位。

| 阶段 | sg-0 | sg-1 | sg-2 | sg-3（临时 surge） | 动作前计数 | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| v1 故障，提交 v2 | v1 Ready | v1 Ready | v1 NotReady | 无 | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>maxScaleDown=1 | 最高旧组 sg-2 已不可用 |
| 创建 surge | v1 Ready | v1 Ready | v1 NotReady | v2 NotReady | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>maxScaleDown=1 | 实际多出 1 组，且它未 Ready；两项抵消 |
| 清理 sg-2 | v1 Ready | v1 Ready | 无 | v2 NotReady | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>maxScaleDown=0 | maxScaleDown=1 允许清理旧版 unhealthy；Ready 不降 |
| 重建 sg-2 | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=2<br>maxScaleDown=0 | maxScaleDown 耗尽；若两组 v2 都坏，就停在这里 |
| sg-2 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>maxScaleDown=1 | 可以更新下一个健康旧组 sg-1 |
| 删除 sg-1 | v1 Ready | 无 | v2 Ready | v2 NotReady | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>maxScaleDown=0 | 健康组删除后仍有 2 组 Ready |
| 重建 sg-1 | v1 Ready | v2 NotReady | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=2<br>maxScaleDown=0 | 等新 sg-1 Ready |
| sg-1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>maxScaleDown=1 | 下一个旧组是 sg-0 |
| 删除 sg-0 | 无 | v2 Ready | v2 Ready | v2 NotReady | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>maxScaleDown=0 | 保持高到低顺序 |
| 重建 sg-0 | v2 NotReady | v2 Ready | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=2<br>maxScaleDown=0 | 仍不重建同版本的坏 surge |
| sg-0 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>maxScaleDown=1 | 正式组已全部更新 |
| 清理 surge | v2 Ready | v2 Ready | v2 Ready | 无 | activeReplicas=3<br>readyReplicas=3<br>unavailableTargetReplicas=0<br>maxScaleDown=1 | 临时组未 Ready，清理不减少 Ready |

创建 sg-3 前后，`maxScaleDown` 都是 1：`activeReplicas` 增 1，`unavailableTargetReplicas` 也增 1。直接写 `maxUnavailable-unavailableTargetReplicas=1-1=0` 会错误阻塞 sg-2 的无损清理。反之，重建 sg-2 后 `unavailableTargetReplicas=2,maxScaleDown=0`，即使剩余旧组也有故障，仍要等至少一个新版组 Ready、提交新目标，或通过其他合法方式取得额度。这里借鉴的是社区的**额度结算**，不是 Deployment 按旧 RS 健康状况自由选择删除对象：本例旧坏组恰为最高位 2，其余健康旧组仍按 1→0 处理。

## 3. 扩缩容途中开始滚动，以及同一请求的原子变更

### SG-C04：扩容途中开始 v2

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | 稳态 |
| 扩容途中 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | 无 | 活动 4；Ready 3 | sg-3 v1 创建请求已接受 |
| v2 到来 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | v2 NotReady | 活动 5；Ready 3 | sg-3 v1 不原地变 v2；缺位 4 用 v2；unavailableTargetReplicas=1、maxScaleDown=0 |
| sg-4 就绪 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | v2 Ready | 活动 5；Ready 4 | unavailableTargetReplicas=0、maxScaleDown=1；不用等待旧 sg-3 v1 Ready |
| 清理 sg-3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | v2 Ready | 活动 5→4；Ready 4 | 最高旧组已 NotReady，删除不降低 Ready |
| 重建 sg-3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 Ready | 活动 5；Ready 4 | unavailableTargetReplicas=1、maxScaleDown=0；等 sg-3 v2 Ready |
| sg-3 就绪 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 才能继续破坏健康 sg-2 |
| 继续 2 | 5/v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | sg-3 Ready 后更新 sg-2 |
| 继续 1 | 5/v2 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 再更新 sg-1 |
| 继续 0 | 5/v2 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 最后 sg-0 |
| 终态 | 5/v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 全部 v2 |

### SG-C05：缩容途中开始 v2

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 5；Ready 5 | 稳态 |
| 缩容途中 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | 活动 5→4；Ready 4～5 | sg-4 删除已发 |
| v2 到来 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | 无 | 活动 4→3；Ready 3～4 | 缩容先继续；不滚将删除的 sg-3 |
| 规模完成 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | 等删除彻底完成 |
| 滚 2 | 3/v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 从最高保留组 sg-2 开始 |
| 滚 1 | 3/v2 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | sg-2 Ready 后更新 sg-1 |
| 滚 0 | 3/v2 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 最后 sg-0 |
| 终态 | 3/v2 | v2 Ready | v2 Ready | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 不复活 sg-3/4 |

### SG-C06：同一请求扩容并更新

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | 旧 spec |
| 原子提交 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 先扩容，3/4 直接 v2 |
| 等 Ready | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 扩容不换成滚动信用 |
| 滚动 | 5/v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 从保留旧组最高位开始 |
| 继续 1 | 5/v2 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | sg-2 Ready 后更新 sg-1 |
| 继续 0 | 5/v2 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 最后 sg-0 |
| 终态 | 5/v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 全部 v2 |

### SG-C07：同一请求缩容并更新

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 5；Ready 5 | 旧 spec |
| 原子提交 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | v1 删除中 | 活动 5→3；Ready 3～5 | 缩容先选受害组；可并行删除 |
| 规模完成 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | 不重建 3/4 |
| 滚动 | 3/v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 新 desiredReplicas 下 maxUnavailable=1，先 sg-2 |
| 继续 1 | 3/v2 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | sg-2 Ready 后更新 sg-1 |
| 继续 0 | 3/v2 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 最后 sg-0 |
| 终态 | 3/v2 | v2 Ready | v2 Ready | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 全部 v2 |

如果缩容按 NotReady/cost 选择 sg-1 而留下高位健康组，终态可以稀疏；随后滚动只以必要的旧版替换机会补洞，不为排齐删除目标版组（见 SG-S01～S03）。

<a id="sg-c13扩容中开始滚动按新-n-申请-surge"></a>

### SG-C13：扩容中开始滚动，按新 desiredReplicas 申请 surge

`desiredReplicas:3→5,maxUnavailable=0,maxSurge=1`，sg-3 v1 已由先前扩容创建并 Ready，sg-4 v1 尚未创建；此时 v2 到来。`sg-4` 是正式扩容槽，`sg-5` 才是新 desiredReplicas 的 surge。

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 4；Ready 4 | 扩容未完 |
| v2 到来 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | 无 | 活动 5；Ready 4 | 先补正式缺位；不删 sg-3 v1 |
| 规模完成 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady（临时 surge）→v2 Ready（临时 surge） | 活动 6；Ready 5→6 | 额外 sg-5 v2 占 maxSurge=1 |
| 滚 3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 最高旧组 sg-3 |
| 滚 2 | 5/v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | sg-3 Ready 后更新 sg-2 |
| 滚 1 | 5/v2 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 再 sg-1 |
| 滚 0 | 5/v2 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 最后 sg-0 |
| 终态 | 5/v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 5；Ready 5 | sg-5 v2 清理，maxSurge 归还 |

## 4. v1→v2→v3、坏版本与二次滚动

<a id="sg-c08固定-nv2-尚未完成即直接提交-v3"></a>

### SG-C08：固定 desiredReplicas，v2 尚未完成即直接提交 v3

`desiredReplicas=3,maxUnavailable=1,maxSurge=0,partition=0`；sg-2 已更新为 v2 Ready，sg-1 的旧版 v1 删除已发出，但新版 sg-1 的创建**尚未**发出。v3 到来后不可撤销已发删除，重建直接使用 v3。

| 阶段 | 目标 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | v1 | v1 Ready | v1 Ready | v1 Ready | 活动 3；Ready 3 | 基线 |
| v2 进行中 | v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | 活动 2～3；Ready 2→3 | 先滚最高位 2 |
| v2 下一步 | v2 | v1 Ready | v1 删除中 | v2 Ready | 活动 3→2；Ready 2 | sg-1 删除已接受 |
| v3 接管 | v3 | v1 Ready | v3 NotReady→v3 Ready | v2 Ready | 活动 3；Ready 2→3 | 不能复活旧 sg-1 v1；直接创建 sg-1 v3 |
| 继续 | v3 | v1 Ready | v3 Ready | v2 删除中→v3 Ready | 活动 2～3；Ready 2→3 | sg-1 v3 Ready 后回到最高健康旧组 2 |
| 最后 | v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | 活动 2～3；Ready 2→3 | 最后 sg-0 v1→v3 |
| 终态 | v3 | v3 Ready | v3 Ready | v3 Ready | 活动 3；Ready 3 | 无需全量 v2 |

若 sg-1 v2 的创建请求已经发出，不能偷偷把在建 sg-1 v2 改成 v3。它后来若 NotReady 且版本过期，SG/独立 Role 默认可以先修 sg-1：此时 `desiredReplicas=3,maxUnavailable=1,activeReplicas=3,readyReplicas=2,unavailableTargetReplicas=0,inFlightReservations=0,maxScaleDown=1,maxHealthyScaleDown=0`，只删除旧坏 sg-1，原位补 v3，Ready 保持 2；待其 Ready 后再滚健康 sg-2，最后 sg-0。若 sg-1 v2 Ready，则两者同为健康旧实例，先 sg-2 再 sg-1。相同状态的协调 Role 不可跳过健康高位，仍在 maxHealthyScaleDown=0 时等待；这不改变百分比 maxSkew。已发事件决定允许的中间态，最新意图决定未来动作。

### SG-C09：扩容中 v1→v2→v3

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | 基线 |
| 扩容途中 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 无 | 活动 4；Ready 4 | sg-3 v1 已创建 |
| v2 到来 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady→v2 Ready | 活动 5；Ready 4→5 | 最后一个扩容槽直接 v2 |
| v3 到来 | 5/v3 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | 活动 5；Ready 5 | sg-4 v2 已存在且过期 |
| 滚 4 | 5/v3 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 删除中→v3 Ready | 活动 4～5；Ready 4→5 | 最高旧组 4 先转 v3 |
| 滚 3 | 5/v3 | v1 Ready | v1 Ready | v1 Ready | v1 删除中→v3 Ready | v3 Ready | 活动 4～5；Ready 4→5 | sg-3 v1 直接到 v3，不先到 v2 |
| 滚 2 | 5/v3 | v1 Ready | v1 Ready | v1 删除中→v3 Ready | v3 Ready | v3 Ready | 活动 4～5；Ready 4→5 | 再 sg-2 |
| 滚 1 | 5/v3 | v1 Ready | v1 删除中→v3 Ready | v3 Ready | v3 Ready | v3 Ready | 活动 4～5；Ready 4→5 | 再 sg-1 |
| 滚 0 | 5/v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | 活动 4～5；Ready 4→5 | 最后 sg-0 |
| 终态 | 5/v3 | v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | 活动 5；Ready 5 | 全部 v3 |

### SG-C10：缩容中 v1→v2→v3

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 5；Ready 5 | 基线 |
| 缩容中 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | 活动 5→4；Ready 4～5 | 已发删除保留事实 |
| v2 到来 | 3/v2 | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | 无 | 活动 4→3；Ready 3～4 | 先完成缩容；不建 sg-3 v2 |
| 开始 v2 | 3/v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 最高保留组到 v2 |
| v3 到来 | 3/v3 | v1 Ready | v1 Ready | v2 Ready | 无 | 无 | 活动 3；Ready 3 | sg-2 v2 已健康但过期 |
| 滚 2 | 3/v3 | v1 Ready | v1 Ready | v2 删除中→v3 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | sg-2 v2→v3 |
| 滚 1 | 3/v3 | v1 Ready | v1 删除中→v3 Ready | v3 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | sg-1 v1 直接到 v3 |
| 滚 0 | 3/v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | sg-0 v1 直接到 v3 |
| 终态 | 3/v3 | v3 Ready | v3 Ready | v3 Ready | 无 | 无 | 活动 3；Ready 3 | 只更新最终保留集 |

缩容若按健康/cost 留下高位稀疏集合，则仍按 SG-S02/S03：只借需要更新的组偿还空洞，不因 v3 到来就移动健康 v3 组。

### SG-C11：扩容中 v2 无法 Ready，用户把目标回滚 v1

原 `desiredReplicas=3`，扩到 5 时 sg-3 v1 已 Ready、sg-4 尚未创建；这时提交错误配置 v2，最后的扩容槽 sg-4 直接创建 v2 却永不 Ready。`desiredReplicas=5,maxUnavailable=1,maxSurge=0`，目标回到 v1 后，sg-4 v2 已过期且不贡献 readyReplicas，自动以 v1 修复。

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | 原基线 |
| 扩容中 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 无 | 活动 4；Ready 4 | sg-3 v1 已 Ready，sg-4 尚未申请 |
| v2 阻塞 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | 活动 5；Ready 4 | maxUnavailable=1 已占满；不碰健康 sg-3 v1 |
| 回滚 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 删除中 | 活动 5→4；Ready 4 | sg-4 v2 过期且不可用，可自动清理 |
| 恢复 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | 活动 5；Ready 4 | 按最新 v1 在原位重建 |
| 终态 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 5；Ready 5 | 其余健康 v1 组 UID 不变 |

这是有意采用最新意图与自动修复的 ModelServing 语义；[StatefulSet 文档](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) 说明其 OrderedReady 坏模板回滚可能仍需用户手工删除坏 Pod。

### SG-C14：v1→v2→v3 时，旧 v2 surge 仍承担服务容量

`desiredReplicas=3,maxUnavailable=0,maxSurge=1,partition=0`；sg-3 v2 临时 surge Ready，sg-2 v1 删除已经发出；在 v3 到来时不可先清理 sg-3 v2，否则 Ready 低于 3。

| 阶段 | 目标 | sg-0 | sg-1 | sg-2 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | v1 | v1 Ready | v1 Ready | v1 Ready | 无 | 活动 3；Ready 3 | 基线 |
| v2 surge | v2 | v1 Ready | v1 Ready | v1 Ready | v2 Ready（临时 surge） | 活动 4；Ready 4 | surge 取得服务信用 |
| sg-2 v1 删除 | v2 | v1 Ready | v1 Ready | v1 删除中 | v2 Ready（临时 surge） | 活动 4→3；Ready 3 | 旧删除不可撤销 |
| v3 接管 | v3 | v1 Ready | v1 Ready | v3 NotReady | v2 Ready（临时 surge） | 活动 4；Ready 3 | 缺位 2 直接 v3；sg-3 v2 暂留 |
| sg-2 v3 Ready | v3 | v1 Ready | v1 Ready | v3 Ready | v2 Ready（临时 surge） | 活动 4；Ready 4 | 此时才可回收过期 sg-3 v2 |
| 换 surge | v3 | v1 Ready | v1 Ready | v3 Ready | v2 删除中（临时 surge）→v3 NotReady（临时 surge）→v3 Ready（临时 surge） | 活动 3～4；Ready 3→4 | 同一槽顺序复用，不并存 sg-3 v2/sg-3 v3 |
| 滚 1 | v3 | v1 Ready | v1 删除中→v3 Ready | v3 Ready | v3 Ready（临时 surge） | 活动 3～4；Ready 3→4 | sg-3 v3 Ready 后更新 sg-1 |
| 滚 0 | v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | v3 Ready（临时 surge） | 活动 3～4；Ready 3→4 | 再更新 sg-0 |
| 终态 | v3 | v3 Ready | v3 Ready | v3 Ready | 无 | 活动 3；Ready 3 | 清理 sg-3 v3，无全量 v2 稳态 |

若 sg-2 v3 也坏到无法 Ready，保留健康 sg-3 v2，停止对 sg-1 v1/sg-0 v1 的破坏，而不是为“清理过期 surge”牺牲服务底线。

## 5. partition、百分比边界和 surge 重分类

所有表中的 partition 都是**固定序号边界**，不是现存组排序后的前几组。例如 partition=2 时，sg-0 和 sg-1 受保护，新增这两个序号时使用本轮灰度前的历史 v1；sg-2 及以上使用最新版本。已经运行 v2 的组后来被纳入保护区，不会主动回滚 v1；但它若消失，可能按历史 v1 重建。

<a id="基础过程p1高位先滚"></a>

### 基础过程：partition=1，高位先滚

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/1/v1 | v1 Ready | v1 Ready | v1 Ready | 活动 3；Ready 3 | 保护 sg-0 |
| 1 | 3/1/v2 | v1 Ready（受保护） | v1 Ready | v1 删除中→v2 Ready | 活动 2～3；Ready 2→3 | 从序号最高的可更新组 sg-2 开始 |
| 2 | 3/1/v2 | v1 Ready（受保护） | v1 删除中→v2 Ready | v2 Ready | 活动 2～3；Ready 2→3 | sg-2 Ready 后更新 sg-1 |
| 终态 | 3/1/v2 | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 3；Ready 3 | 不动 sg-0 |

<a id="sg-p01p1-灰度后扩容"></a>

### SG-P01：partition=1 灰度后扩容

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/1 | v1 Ready（受保护） | v2 Ready | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 灰度稳态 |
| 扩容 | 5/1 | v1 Ready（受保护） | v2 Ready | v2 Ready | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 从低位缺号 3、4 补 v2 |
| 终态 | 5/1 | v1 Ready（受保护） | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 旧 0～2 UID 不变 |

<a id="sg-p02扩容时原子调整整数-p跨过保护边界"></a>

### SG-P02：扩容时原子调整整数 partition，跨过保护边界

初始 desiredReplicas=3/partition=3，提交 desiredReplicas=6/partition=5 的同一请求。旧版示例 desiredReplicas=3/partition=5 现在必须被 admission 拒绝；不能为了执行流程绕过上界。

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/3/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | 无 | 无 | 无 | 活动 3；Ready 3 | partition=3，暂时没有可更新的组 |
| 扩容 | 6/5/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v1 NotReady（受保护） | v1 NotReady（受保护） | v2 NotReady | 活动 6；Ready 3 | 新增 sg-3/4 用历史 v1，sg-5 用 v2 |
| 终态 | 6/5/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 活动 6；Ready 6 | 5 组 v1、1 组 v2 |

### SG-P03：缩容可以穿越保护区并留下稀疏

在组都健康、删除优先级相同时，期望组数从 5 缩至 2，同一请求将 partition 从 3 改为 2：从最高序号缩容，先删 sg-4 v2 和 sg-3 v2，再删 sg-2 v1，留下 `{sg-0 v1,sg-1 v1}`。partition 不是最小副本数。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 5；Ready 5 | 灰度稳态 |
| 缩容 | 2/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 删除中 | v2 删除中 | v2 删除中 | 活动 5→2；Ready 2～5 | 同优先级按高位先删 |
| 终态 | 2/2 | v1 Ready（受保护） | v1 Ready（受保护） | 无 | 无 | 无 | 活动 2；Ready 2 | 默认路径连续 |

若另有 `desiredReplicas=3,partition=2,{sg-0 v1 Ready,sg-1 v1 NotReady,sg-2 v2 Ready}`，缩到 desiredReplicas=2 时 NotReady sg-1 优先于健康 sg-2 被删除，允许 `{sg-0 v1,sg-2 v2}`：

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 缩容前 | 3/2 | v1 Ready（受保护） | v1 NotReady（受保护） | v2 Ready | 活动 3；Ready 2 | sg-1 故障 |
| 缩容后 | 2/2 | v1 Ready（受保护） | 无 | v2 Ready | 活动 2；Ready 2 | 缩容合法留洞，不删除健康 sg-2 v2 |
| 再扩容 | 3/2 | v1 Ready（受保护） | v1 NotReady（受保护）→v1 Ready（受保护） | v2 Ready | 活动 3；Ready 2→3 | 最低洞 1 取历史 v1；sg-2 v2 保 UID |

<a id="sg-p04百分比-p-因扩容提高"></a>

### SG-P04：百分比 partition 因扩容提高

`partition="50%"`：desiredReplicas=3 时 partition=2；desiredReplicas=5 时 partition=3。旧 sg-2 v2 被新 partition 保护，但现存 sg-2 v2 不主动回滚 v1。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 灰度 |
| 扩容 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready（受保护） | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 存量 sg-2 v2 仍 v2；3/4 用 v2 |
| 终态 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready（受保护） | v2 Ready | v2 Ready | 活动 5；Ready 5 | 不要求“恰好 partition 个 v1” |

若 sg-2 v2 后来失效/被删除，恢复缺位 2 依新 partition 使用历史 v1；这与不主动替换健康 sg-2 v2 并不矛盾。

<a id="sg-p05百分比-p-因缩容降低"></a>

### SG-P05：百分比 partition 因缩容降低

`partition="50%"`：desiredReplicas=5 时 partition=3；desiredReplicas=3 时 partition=2。缩容先删除高位，其后 sg-2 v1 因保护边界下降而可以更新。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 5；Ready 5 | 原灰度 |
| 缩容 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready | v2 删除中 | v2 删除中 | 活动 5→3；Ready 3～5 | 缩容先完成 |
| 滚动 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 删除中→v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 只更新新解锁的 sg-2 |
| 终态 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 2 组 v1、1 组 v2 |

<a id="sg-p06在途时提高-p"></a>

### SG-P06：在途时提高 partition

`desiredReplicas=3`，sg-2 v2 Ready，sg-1 v1 的删除已发出；partition:0→3。后续新建的 sg-1 按历史 v1，现存 sg-2 v2 不主动回退。

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 在途 | 3/0/v2 | v1 Ready | v1 删除中 | v2 Ready | 活动 3→2；Ready 2 | 删除不可撤销 |
| 改 partition | 3/3/v2 | v1 Ready（受保护） | v1 删除中（受保护） | v2 Ready（受保护） | 活动 3→2；Ready 2 | 所有位置被新 partition 保护 |
| 停点 | 3/3/v2 | v1 Ready（受保护） | v1 NotReady（受保护）→v1 Ready（受保护） | v2 Ready（受保护） | 活动 3；Ready 2→3 | 不更新 sg-0，不回滚现存 sg-2 v2 |

<a id="sg-p07降低-p-的同时提交-v3"></a>

### SG-P07：降低 partition 的同时提交 v3

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/2/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 活动 3；Ready 3 | 原灰度 |
| 新目标 | 3/1/v3 | v1 Ready（受保护） | v1 Ready | v2 删除中→v3 Ready | 活动 2～3；Ready 2→3 | 最高旧组 2 先转 v3 |
| 继续 | 3/1/v3 | v1 Ready（受保护） | v1 删除中→v3 Ready | v3 Ready | 活动 2～3；Ready 2→3 | sg-2 Ready 后，sg-1 直接 v1→v3 |
| 终态 | 3/1/v3 | v1 Ready（受保护） | v3 Ready | v3 Ready | 活动 3；Ready 3 | sg-1 不必先到 v2 |

<a id="sg-p08百分比-p--surge--扩容重分类"></a>

### SG-P08：百分比 partition + surge + 扩容重分类

`desiredReplicas=3,partition=50%→2,maxUnavailable=0,maxSurge=1`，sg-3 v2 surge 尚未 Ready；扩到 desiredReplicas=5 使 partition=3，sg-2 重新受保护，sg-3 原地转正式组。没有需要更新的旧版组，不再额外申请 sg-5。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready | v2 NotReady（临时 surge） | 无 | 无 | 活动 4；Ready 3 | maxUnavailable=0，不删 sg-2 v1 |
| 扩容 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 NotReady | v2 NotReady | 无 | 活动 5；Ready 3 | sg-3 v2 保 UID，sg-4 v2 补正式缺位 |
| 终态 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | v2 Ready | 无 | 活动 5；Ready 5 | 没有新的 surge 需求 |

### SG-P09：受保护组的故障也占用 maxUnavailable

`desiredReplicas=3,partition=1,maxUnavailable=1,maxSurge=0`；受保护的 sg-0 是 v1 NotReady，sg-1 和 sg-2 是 v1 Ready。

| 阶段 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- |
| 故障 | v1 NotReady（受保护） | v1 Ready | v1 Ready | 活动 3；Ready 2 | 已在 `desiredReplicas-maxUnavailable=2` 底线；不能删健康 sg-2 v1 |
| 恢复 | v1 Ready（受保护） | v1 Ready | v1 Ready | 活动 3；Ready 3 | sg-0 v1 依历史模板恢复 |
| 滚动 | v1 Ready（受保护） | v1 Ready | v1 删除中→v2 Ready | 活动 2～3；Ready 2→3 | 再更新序号最高的可更新组 sg-2 |
| 终态 | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 3；Ready 3 | sg-1 最后到 v2 |

### SG-P10：surge 被扩容复用后继续 partition 滚动

`desiredReplicas:3→4,partition=1,maxUnavailable=0,maxSurge=1`；sg-3 v2 已 Ready，扩容使 sg-3 v2 成正式组；旧组 sg-1 和 sg-2 仍可更新，需要临时组 sg-4 v2 Ready 后才可删除健康组。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/1 | v1 Ready（受保护） | v1 Ready | v1 Ready | v2 Ready（临时 surge） | 无 | 活动 4；Ready 4 | sg-3 v2 是临时容量 |
| 扩容 | 4/1 | v1 Ready（受保护） | v1 Ready | v1 Ready | v2 Ready | v2 NotReady（临时 surge）→v2 Ready（临时 surge） | 活动 5；Ready 4→5 | sg-3 v2 保 UID，sg-4 v2 占新 surge |
| 滚动 | 4/1 | v1 Ready（受保护） | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 4～5；Ready 4→5 | 先 sg-2 |
| 继续 | 4/1 | v1 Ready（受保护） | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 4～5；Ready 4→5 | 再 sg-1 |
| 终态 | 4/1 | v1 Ready（受保护） | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 4；Ready 4 | 最后清理临时 sg-4 v2 |

### SG-P11：旧高位替换补低位，partition 按新槽位的绝对序号决定版本

先前缩容留下 `desiredReplicas=2,{sg-0 v1,sg-3 v1}`；设置 `partition=2,maxUnavailable=1,maxSurge=0`，提交 v2。sg-3 在保护范围之外且仍是旧版本，本来就需要替换；替代容量优先补最低空位 sg-1。sg-1 的绝对序号小于 partition，因此使用历史 v1。不是因为它替代了高位 sg-3 就使用 v2。

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 2/2/v2 | v1 Ready（受保护） | 无 | v1 Ready | 活动 2；Ready 2 | 高位旧版可替换；空洞本身不会触发滚动 |
| 滚动 | 2/2/v2 | v1 Ready（受保护） | 无 | v1 删除中 | 活动 2→1；Ready 1 | 受 maxUnavailable=1 限制 |
| 补位 | 2/2/v2 | v1 Ready（受保护） | v1 NotReady→v1 Ready（受保护） | 无 | 活动 2；Ready 1→2 | 最低空位 1 用历史 v1 |
| 灰度停点 | 2/2/v2 | v1 Ready（受保护） | v1 Ready（受保护） | 无 | 活动 2；Ready 2 | 两个 v1；没有 eligible 实例，不宣称全量 v2 完成 |
| 降低 partition | 2/1/v2 | v1 Ready（受保护） | v1 删除中→v2 Ready | 无 | 活动 1～2；Ready 1～2 | 释放绝对序号 1，继续使用同一目标 revision |

灰度停点 `currentRevision` 保持 v1、`updateRevision` 为 v2、`updatedReplicas=0`。本条于 2026-10-06 经用户明确澄清，替代此前必须在 sg-3 原位更新的描述。SG-S03 不变：如果 sg-3 已经是健康目标 v2，则不能为了补洞把它换成 sg-1 v2。SG 和 Role 的 partition 都按绝对序号解释，不按现存列表位置解释。

## 6. ServingGroupRollingUpdate 中的 Role 副本交错

### SG-R01：partition 灰度中只扩 Role 成员数

`desiredReplicas=3,partition=1`，sg-0 使用历史 v1 的 worker 模板 W1，sg-1 和 sg-2 使用 v2 的 worker 模板 W2。仅将 `roles[].replicas` 从 1 扩到 2，不产生新的 SG 版本，也不触发序号补洞。默认 `maxUnavailable=1,maxSurge=0`；按每组已应用的成员目标判断完整 Ready。未轮到的组仍以旧成员数提供服务，不会因全局 spec 刚改变就同时失去 Ready 信用。

| 阶段 | 每组 Role 成员数 | sg-0 | sg-1 | sg-2 | 整组 Ready | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 1 | v1/W1×1 Ready（受保护） | v2/W2×1 Ready | v2/W2×1 Ready | 3 | 灰度稳态 |
| 扩 sg-2 | 2 | v1/W1×1 Ready（受保护） | v2/W2×1 Ready | v2/W2×1 Ready+v2/W2×1 NotReady | 2→3 | 只给一组应用新成员目标，等 W2 Ready |
| 扩 sg-1 | 2 | v1/W1×1 Ready（受保护） | v2/W2×1 Ready+v2/W2×1 NotReady | v2/W2×2 Ready | 2→3 | 仍守住 desiredReplicas-maxUnavailable=2 |
| 扩 sg-0 | 2 | v1/W1×1 Ready（受保护）+v1/W1×1 NotReady | v2/W2×2 Ready | v2/W2×2 Ready | 2→3 | 受保护组仍使用历史 W1 模板 |
| 终态 | 2 | v1/W1×2 Ready（受保护） | v2/W2×2 Ready | v2/W2×2 Ready | 3 | 不重滚 SG 模板，不把 W2 套给 sg-0 |

## 7. 验收与反例清单

验证每个场景时，要记录当时的期望组数、partition、maxUnavailable、maxSurge，以及每个 SG 的序号、版本、UID、Ready/删除状态。还要区分正式保留组与临时 surge，并保留已发动作和历史版本记录。“过程中又来了新请求”的测试，应确保前一步确实还在执行。

不能只检查最后“组数等于期望值”。还要检查：非缩容动作没有留下新的稳定空洞；健康且已是目标版本的组没有仅为排齐序号而被删除；没有超出 surge 上限创建新组，也没有通过滚动删除健康组把 Ready 数降到 maxUnavailable 所允许的底线以下。如果故障本已使 Ready 低于底线，过期 NotReady 组的替换只能在 maxScaleDown 有余额时保持或增加 Ready，同类候选高到低；只有配置 coordination 的 Role 对照禁止越过健康高位旧实例。缩容造成的短暂超额只允许逐步消退。

三个必须保留的反例：

1. SG-S01：`{0,3}` 的 N2→N3 只补 sg-1，留下洞 2；若强行归位就要重建健康 sg-3。
2. SG-S05：低位 sg-1 v2 NotReady、高位 sg-2 v1 Ready，目标 v3；默认先修 1 并保持 Ready=2，不能把 maxScaleDown=1 用于删除健康 2。相同状态的协调 Role 应等待，不能为了修故障破坏高到低顺序。
3. SG-C14：旧 v2 surge 虽过期却仍在提供服务；若按“最新版本优先”立即删除它，就会越过可用性底线。

SG-S06/S07 是可收敛的正例：maxScaleDown 仍有余额时，已不可用且版本过期的最高序号组可成批被新版本替换；即使当前故障已经超过底线，替换也不会再减少 Ready。SG-C16 要求同时核对 `activeReplicas` 和 `unavailableTargetReplicas`：一个尚未 Ready 的实际 surge 不会凭空增加清理信用，但也不能只扣 `unavailableTargetReplicas` 而忽略它已使 `activeReplicas` 增 1。

本文是设计推导，**没有**把任何一行表格视作当前实现或 Kind 验证结果。

<a id="behavior-rules"></a>

## 附录：SG 行为规范

原设计日期：2026-09-20；本版含 2026-10-07 明确批准的默认跳过和共享预算修订。本文不以当前 controller 实现作为规则来源；[35 个 SG 过程表](#scenario-tables) 是 SG 规则的实例化。共同预算、身份/历史及选择原则适用于完整 Role 实例；Role 协调额外限制见 API §5 和附录 B。SG 模式下 roles[].replicas 增员（SG-R01）不是 Role 模式模板滚动，不能机械替换其计数轴。

### A.1. 决策与社区参照

ServingGroup 是多 Pod 的完整服务单位，替换健康组可能重建模型、占用大量 GPU 并造成长时间预热。取舍次序为：**不扩大既有可用性缺口、遵守容量上限 > 不重建健康且无需更新的组 > 规模收敛与已有空洞的机会性修复**。SG/独立 Role 的新版本替换先选合法旧 NotReady，再选旧 Ready，同类高到低；有 coordination 的 Role 稳定实例不跳过。候选选择不能突破 maxScaleDown、健康删除上限 maxHealthyScaleDown、partition、依赖或物理容量。

| 主题 | 可借鉴的社区语义 | ModelServing 决策 |
| --- | --- | --- |
| 身份与 partition | StatefulSet 稳定 ordinal、`ordinal<partition` 保留旧模板、高到低替换 | 保留绝对 ordinal 阈值；但允许单独缩容因健康/删除代价而留下稀疏集合 |
| 预算 | Deployment 的百分比 maxUnavailable 向下取整、maxSurge 向上取整；LWS 以整组为单位提供 maxUnavailable/maxSurge | maxUnavailable/maxSurge 按**最新 desiredReplicas**计算；整组 Ready 才算可用；不能把扩容缺口当成删除信用 |
| 最新意图 | Deployment rollover 接管最新模板；StatefulSet 坏版本可能需要人工干预 | A→B→C 可以跳过中间 B；合法旧 NotReady B 可优先换成 C，同类高到低；协调 Role 仍按稳定旧序号 |
| 推理代价 | LWS 以组为滚动单位；RBG 区分角色与协调策略 | 不为单纯序号整理重建健康、目标版本的保留组；Role 副本轴与 SG 轴分开 |

Deployment [§rolling.go](https://github.com/kubernetes/kubernetes/blob/master/pkg/controller/deployment/rolling.go#L803-L918) 给出的关键计算是：

`maxScaledDown := allPodsCount - minAvailable - newReplicaSetPodsUnavailable`

同一段代码随后先执行 `cleanupUnhealthyReplicas`，其注释明确说“Clean up unhealthy replicas first”。意思是：旧版已有不健康副本时，可以先清理它们而不增加不可用数；新版尚未可用的副本须从**已创建的总副本数** 带来的额度中扣除，不能把配置的 surge 上限当成已经创建且可用的副本。若扣除后额度为零，不能继续清理旧副本。rollover 到 C 时，坏 B 变为旧 RS，空的 C RS 暂无不可用副本，于是可以先清掉坏 B，再创建 C。Deployment 的旧 RS 按创建时间处理，并没有 SG ordinal 或 partition。本版在保留这些身份约束的前提下，对 SG/独立 Role 采用合法旧 NotReady 优先、同类高到低；SG-S05 因而可以先修低位。有 coordination 的 Role 稳定实例仍不得跳过高位。

原文的 `desiredReplicas=10,maxUnavailable=2,maxSurge=3` 例子可直接验算：坏新版有 5 个不可用副本、全部 RS 合计 13 个时，最少可用 8 个，清理额度是 `13-8-5=0`；回滚到旧好版后，新 RS 尚有 1 个不可用，额度变为 `13-8-1=4`；若改为提交全新模板，空的新 RS 暂无不可用副本，额度是 `13-8-0=5`。后两者都可以先清理**已成为旧版**的坏副本，为新目标腾位置；不是把这些故障当作额外的 maxUnavailable。

SG/Role 共用 `minAvailable=max(0,desiredReplicas-maxUnavailable)`、`maxScaleDown=max(0,activeReplicas-minAvailable-unavailableTargetReplicas-inFlightReservations)`、`maxHealthyScaleDown=max(0,readyReplicas-minAvailable)`。稳定检查点 inFlightReservations=0，maxScaleDown 退化为 `max(0,activeReplicas-minAvailable-unavailableTargetReplicas)`；当 maxUnavailable≤desiredReplicas 且 activeReplicas=desiredReplicas 时才可简写为 `max(0,maxUnavailable-unavailableTargetReplicas)`。activeReplicas 含实际已创建 surge，unavailableTargetReplicas 含最新目标 NotReady surge，不能把配置 maxSurge 当实际容量。inFlightReservations 是尚未体现在 activeReplicas/unavailableTargetReplicas 中的在途额度，只记一次；readyReplicas 含保护区及旧版 Ready，排除已承诺删除容量。完整定义见 API §2.3。默认两阶段选择 `oldNotReadyToReplace=min(|eligibleOldNotReady|,maxScaleDown)`、`oldReadyToReplace=min(|eligibleOldReady|,maxScaleDown-oldNotReadyToReplace,maxHealthyScaleDown)`；候选还要满足 partition、身份、依赖、在途占位和容量限制。maxScaleDown 不是任意健康删除许可；健康删除必须按实时 readyReplicas 再核验底线。SG-C16 逐阶段展示：desiredReplicas=3、maxUnavailable=1 时创建一个 NotReady 的 v2 surge，`activeReplicas` 与 `unavailableTargetReplicas` 同时加 1，清理旧版 NotReady 组的 `maxScaleDown` 不变。

参考：[Kubernetes StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)、[Kubernetes Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)、[LeaderWorkerSet Rollout Strategy](https://lws.sigs.k8s.io/docs/concepts/leaderworkerset/rollout-strategy/)、[LeaderWorkerSet API](https://lws.sigs.k8s.io/docs/reference/leaderworkerset.v1/)、[RoleBasedGroup API](https://github.com/sgl-project/rbg/blob/main/doc/reference/api.md)。这些都是参照；没有单一原生 controller 同时给出本规范的稀疏保留、partition、surge 和嵌套 Role 副本行为。尤其 LWS 文档中的“百分比基于更新开始时副本数”不照搬：这里以最新期望 desiredReplicas 重新结算，是扩缩与滚动交错时明确选择的产品规则。LWS 在 partition 与 surge 同时配置时会保留 burst 副本直至 partition 归零；这里的灰度停点若 eligible 组已更新且可用性达标，就清理不再需要的临时 surge，避免长期占用推理 GPU。

### A.2. 状态、预算与身份

- `desiredReplicas`：最新 `spec.replicas`；`desiredOrdinalRange=[0,desiredReplicas)` 是希望优先覆盖的 ordinal 范围，不是“每轮必须强制排齐”的完成条件。`activeGroups` 是尚未完全消失的 SG 集合；Deleting 仍占物理容量，但一旦停止服务就不计 Ready。`readyReplicas` 是其中整组所有必需成员 Ready 的 SG 数。
- 将 SG 身份区分为**保留组 retainedGroups**与**临时 surge temporarySurgeGroups**，二者均进入 `activeGroups` 和真实 Ready/活动数账本；缩容留下的高 ordinal 仍是 retainedGroups，不因 ordinal≥desiredReplicas 变成 temporarySurgeGroups。旧 temporarySurgeGroups 只有 ordinal 进入新 `desiredOrdinalRange` 且可承接正式容量时才转为 retainedGroups，不能因为 `|activeGroups|=desiredReplicas` 就自动改身份。`missingOrdinals=desiredOrdinalRange\ord(retainedGroups)` 是正式范围内空洞；缩容后可有 `|retainedGroups|=desiredReplicas` 且 `missingOrdinals≠∅`。`maxSurge` 限制的是总活动数超过 desiredReplicas 的**额度**，不是“名字大于等于 desiredReplicas 的组”的分类。
- `partition` 是绝对 ordinal 阈值：`ordinal<partition` 不因目标模板改变而主动更新；`ordinal≥partition` 可向最新目标收敛。百分比 partition 按最新 desiredReplicas 向上取整。partition 不保证最少保留 partition 组，也不禁止显式缩容删除 protected 组。
- `maxUnavailable`：整数原值或 `floor(百分比×desiredReplicas)`；`maxSurge`：整数原值或 `ceil(百分比×desiredReplicas)`。默认 `maxUnavailable=1,maxSurge=0`；允许 maxUnavailable 为 0，但有效 SG 滚动配置不得 `maxUnavailable=maxSurge=0`。百分比 maxUnavailable 不人为补成 1；例如 desiredReplicas=3、maxUnavailable=25%、maxSurge=0 解析成 0/0，必须拒绝配置，不能悄悄取 maxUnavailable=1；零副本或全 partition 也不豁免实际双零。生效整数 maxUnavailable/partition 不得超过 desiredReplicas，零副本允许默认/显式整数 maxUnavailable=1；maxSurge 可超过 100%，但 desiredReplicas+maxSurge 不得超 int32。两者都按最新 desiredReplicas 重算，而不是在第一次更新时冻结。
- 容量上限是 `|activeGroups|≤desiredReplicas+maxSurge`；如果 desiredReplicas 刚缩小导致现存组暂时超过新上限，控制器只能清理/等待，不能再创建加重超额。滚动删除健康组必须保证删除后 `readyReplicas≥max(0,desiredReplicas-maxUnavailable)`；滚动删除**已经 NotReady 且版本过期**的组不降低 readyReplicas，即使故障已使 readyReplicas 低于底线，也可在清理额度 maxScaleDown 尚有余额时作为恢复动作继续。这不是“不降 readyReplicas 就能无限删除”的豁免；它不改变容量上限、partition、本模式的候选顺序或在途控制；同一目标版本仍 NotReady 不反复重建。protected 故障和未 Ready 的新组都计入同一真实可用性账本。
- 显式缩容的目标是最新 desiredReplicas，不把被授权删除的组另记一次“滚动不可用”；但缩容结束后健康组的模板替换仍须遵守最新 `desiredReplicas-maxUnavailable` 下限。surge 是临时物理容量上限，不是必须创建的副本数，也不能被扩容和滚动重复领取。

### A.3. 保序与最小扰动

**只有缩容可以新增稳定的 ordinal 空洞。** 滚动的删除/重建可以短暂产生同一 ordinal 的在途缺位；故障也可造成临时缺位，但控制器不得把这两者当成新的稳定稀疏结果。在动作前后都已稳定的检查点，扩容、滚动应满足 `|missingOrdinalsAfter|≤|missingOrdinalsBefore|`，且不得使此前存在的保留 ordinal 永久消失；desiredReplicas 扩大时，新 `desiredOrdinalRange` 揭示的缺位由新增目标槽优先补偿。已有空洞可在多次动作中逐步偿还，不要求一次扩容或一次滚动必定使 `missingOrdinals=∅`。在途创建/删除不拿来做这个稳定检查点的比较。

1. 扩容真正增加了多少组的目标容量，就最多新建多少个保留组；优先选 `desiredOrdinalRange` 内**最低** 的缺失 ordinal。新 desiredReplicas 使旧 surge/高 ordinal 保留组进入 `desiredOrdinalRange` 时，直接复用其 UID，不能先删再建。旧 surge 若仍在 `desiredOrdinalRange` 外，即使总数恰好等于新 desiredReplicas，仍是 temporarySurgeGroups，可以在 maxUnavailable/maxSurge 允许时为新增的正式组让出容量；不能误把它当成健康保留组。没有空余容量槽时，不为“排齐序号”删除健康目标版本的**保留组**。
2. 健康且无需更新的保留组不主动重建：“无需更新”包括**现存 protected 组** （即使其实际版本是后来被保护的 B）以及已是目标版本的 eligible 组。即使它位于 `desiredOrdinalRange` 外，也不为了连续编号或强行回到旧版而动它。
3. 替换 `desiredOrdinalRange` 内的旧模板组时，优先在**原 ordinal**重建；不能删掉 sg-0 却把替代容量放到 sg-4，制造新洞。替换 `desiredOrdinalRange` 外、且本来就需要滚动的旧模板保留组时，可将替代容量放在 `desiredOrdinalRange` 中最低空洞，从而利用本就需要的替换修复身份；若最低空洞受 partition 保护，按新槽位的绝对 ordinal 使用历史模板补建。即使回补后所有组都为旧版，也可形成合法灰度停点（SG-P11）；不能宣称全量目标版完成。
4. 为零停机而创建的临时 surge，若能够与一名需要删除的范围外旧版保留组配对，可直接建在范围内最低空洞并转为保留组，按该槽位的 partition 选历史/目标模板；否则使用最低空闲范围外 ordinal，滚动结束后清理它。不能先在空洞建一个无法保留的新组，最后为了归位删除健康目标版组。
5. 缩容先确定 **retainedGroups 中**需要移除的数量；在 retainedGroups 内先选 NotReady，后选较低 deletion cost，最后选较高 ordinal，partition 不改变此选择次序。temporarySurgeGroups 另按新 desiredReplicas/maxSurge 与 Ready 底线决定保留或清理，不把“sg 编号最高”误当成先删有用 surge 的理由。缩容可因此删除 protected 旧版组而保留较高的新版组。已发删除不能撤销；新 spec 只控制尚未发出的动作。

这里的“保序”不是强制每次得到 `[0,desiredReplicas)`，而是**不制造稳定新洞、存在合法容量机会时优先补最低旧洞、保留已经有用的身份**。例如 desiredReplicas:2→3 时从 `{0,3}` 建 sg-1 得到 `{0,1,3}`，sg-2 仍缺失；sg-3 若健康且无需更新，不得仅为了把 3 换成 2 而重建。下次扩容到 desiredReplicas=4 可以再建 sg-2，原 sg-3 原地成为范围内组。

### A.4. 操作偏序与版本选择

每次以**最新 spec**重新推导 desiredReplicas、partition、maxUnavailable、maxSurge 和目标 revision，不把早先推导的未来动作当成承诺；已接受的 API 删除/创建按事实继续完成。为已发删除补建同名组是完成在途动作，不算在新目标下跳过高位组另选低位滚动；完成它以后，新发起的滚动按本模式规则选择：SG/独立 Role 旧 NotReady 优先，同类高到低；协调 Role 稳定候选不可跳过。

1. 先识别 Deleting、在建和已有 surge，按新 desiredReplicas/partition 对所有存量组重分类。删除中的组在完全消失前不得复用同名 ordinal 或释放容量槽。
2. 若 desiredReplicas 缩小，先执行缩容选择并等待本轮计划删除完成；若 desiredReplicas 增大，先用新增目标槽补低序号空洞。已开始创建的旧模板组不原地改模板；尚未创建的组直接使用最新适用版本。扩容可批量申请不同 ordinal。先完成正式扩容槽的**安置**，再考虑健康旧组的破坏性滚动；不要求所有新组先 Ready，但每次删除**健康旧组**前必须用实时 readyReplicas 核验 `readyReplicas-1≥max(0,desiredReplicas-maxUnavailable)`。扩容缺口消耗同一可用性预算，不能借“新 desiredReplicas 的 maxUnavailable 变大”把缺口当成额外删除信用。若正式槽尚无法安置，也不能靠滚动先腾它的位置。
3. SG/独立 Role 每次先选合法的旧 NotReady，同类按 ordinal 高到低；没有可选旧 NotReady 后再选择健康旧实例。可跳过健康高位修复低位故障，但旧坏实例数量不超过剩余 maxScaleDown，不能突破 partition/身份约束。每组须等删除彻底完成后才能重用名称和物理容量；已发动作占位，替代组未 Ready 时不能重复领取同一额度。新目标也坏则等待目标修正或明确恢复路径，不反复重建同版。协调 Role 对照改为最高旧序号前缀，还须满足 remainingStart 和依赖保留；受阻就等待，不跳到低位。
4. 健康候选按高到低，必要时先申请合法 surge；每次删除后必须有 `readyReplicas≥max(0,desiredReplicas-maxUnavailable)`，所选健康数量不超过 maxHealthyScaleDown；不满足就等待健康删除余量。maxUnavailable>1 可同时选择多个合法候选。每个完整单元 Ready 后，按最新状态重新计算 maxScaleDown、maxHealthyScaleDown 和 inFlightReservations；存在合法候选且预算、partition、身份、物理容量及协调条件允许时继续推进，不等待原批次其他单元全部 Ready。Ready 回落或其他故障会重新消耗额度，不能把一次 Ready 事件当作永久或重复信用。协调 Role 仍遵守稳定候选顺序、dependencies 和 maxSkew。 protected 缺位/故障按历史修复，不借此突破 partition。
5. 仍承担 `desiredReplicas-maxUnavailable` 可用性底线的健康临时 surge 必须保留，等替代容量就绪再清理；新 desiredReplicas 吸收旧 surge 时保留 UID。目标改变后，废弃旧 NotReady surge 可在 maxScaleDown 内优先合法回收，不能把它的清理额度用于健康稳定实例。此路径须核验真实临时身份、依赖和在途动作；协调 Role 中不计作稳定序号跳过，也不返还稳定启动额度。

例如 `desiredReplicas=5,maxUnavailable=2,maxSurge=0`，同一批次选最高的 sg-4/3 更新，但错误版本 B 使两组都 NotReady，此时 `activeReplicas=5,unavailableTargetReplicas=2,maxScaleDown=5-3-2=0`，健康的 sg-2 不能再删除。目标修正为 C 后，坏 B 转为旧版，`unavailableTargetReplicas=0,maxScaleDown=2`，可按 4→3 同批清理并创建 C；两组 C 均未 Ready 时不能继续删除健康的 sg-2；其中一组完整 Ready 后，readyReplicas=4、unavailableTargetReplicas=1、maxScaleDown=1、maxHealthyScaleDown=1，即可滚 sg-2，不必等另一组。若两组都在下次协调前 Ready，则可同批滚动 2/1（readyReplicas 从 5 降到 3）；此后仍逐 Ready 结算，再滚 sg-0。若起初五组 A 都 NotReady，readyReplicas=0 已低于预算底线，提交 B 后仍有 `activeReplicas=5,unavailableTargetReplicas=0,maxScaleDown=2`，可先按 4→3 替换；每个完整 B Ready 后按新额度继续选择 2→1、0，不等待固定批次全部 Ready。不能因“故障已超 maxUnavailable”拒绝所有恢复动作，也不能一次重建五组。这是**不扩大既有可用性缺口**的规则，不是放宽 maxUnavailable，也不是低序号优先。稀疏扩容后的 sg-2=v1 Ready、sg-1=v2 NotReady、目标 v3，若 maxScaleDown=1/maxHealthyScaleDown=0，默认可先原位修 sg-1，Ready 不下降；它 Ready 后再滚健康 sg-2。相同协调 Role 状态仍受最高候选顺序限制而等待。maxScaleDown 耗尽、新目标仍坏、protected 故障、依赖或调度容量不足也可能阻塞，不能保证自动解除所有故障。

完整滚动单元的新建/重建版本由**动作当时**的最新 partition 决定：`ordinal<partition` 用该次灰度固定且仍可追溯的受保护历史模板，`ordinal≥partition` 用最新目标模板。现存 B 因 partition 增大落入保护区时保留 B；之后它若被删除，重建可回到历史 A，这并不构成主动回滚。受保护基线取进入这轮灰度前最近一次全量完成的模板；A→B→C 的中途提交不自动把基线推进到 B。解除保护并完成全量目标后，才可把新目标作为以后灰度的基线。历史模板不可确认时安全等待，不得猜测为最新模板。Role 副本数变动只改变每组内部数量，受保护组继续使用历史 Pod/worker 模板；它本身不产生 SG 模板 revision。若增加必需 Role 成员会使整组暂时 NotReady，应按实时 `desiredReplicas-maxUnavailable` 底线分批给各组应用新成员数，不能一次令所有旧组失去 Ready 信用。未轮到的组仍以自己**已应用** 的旧成员目标判断 Ready，而非在全局 spec 改变瞬间全部计为缺员；已轮到的组必须等新增成员 Ready 才恢复整组 Ready。若其他故障已耗尽 maxUnavailable，则等待修复或使用合法 surge，不放宽预算。

局部故障修复沿用所属滚动单元已经应用的模板和 worker 布局，不借修复提前应用最新目标。ServingGroupRollingUpdate 的滚动单元是完整 SG，RoleRollingUpdate 的滚动单元是一个 Role 实例；完整滚动单元重建才按动作当时的 partition 选择受保护基线或最新目标。已有 v2 单元因 partition 提高而被保护时，仅修其中一个 Pod/Role 仍保持该单元已应用的 v2；只有整个单元重建才重新按保护区基线选择，可能回到 v1。历史无法可靠确定时等待并报告，不猜测最新版本。 先局部恢复旧版再进行合法模板滚动是允许路径；旧恢复动作不能因此删除新 UID。

<a id="legacy-deletion-records"></a>

**controller 重启后的删除收敛（2.6）**

相关删除标记和事务版本尚未上线，因此没有旧记录兼容负担。controller 不持久化 Role/SG 删除批次的 Pod 集合、操作阶段或跨重启续删许可；Pod annotation 也不承担该权限。controller 存活期间仍按配置完成 RoleRecreate/ServingGroupRecreate 的范围，并用原始 UID 与 owner 校验保证重试不吸纳同名新实例。

controller 重启后，放弃精确延续旧批次，按当前事实重新收敛：

| 重启时事实 | 预期处理 | 例子 |
| --- | --- | --- |
| 原批次尚未发出 DELETE | 按最新配置、partition、候选和预算重新判断；旧计划不产生任何许可 | 提高 partition 后原 Pod 仍在，保护规则直接生效 |
| 整组/Role 删除只完成一部分，另有健康成员幸存 | 按所属滚动单元已应用的历史模板补齐缺失成员；补齐 Ready 后保留健康幸存者，不为完成旧批次继续删除它 | SG 删除了 prefill、decode 尚存；重启后补回同版 prefill，decode UID 保留 |
| 旧 Pod 仍 Terminating 或当前单元不可用 | 把实际占用/不可用计入当前容量与预算；不得因进程内计划消失重新取得删除额度 | 原 UID 未物理消失时不复用名称或容量槽，也不额外删除另一健康单元 |
| 补齐后仍满足当前故障条件 | 重新按当前 recoveryPolicy、grace 和完整健康判断开始新的恢复 episode | None/grace=-1 不主动删除；有限 grace 可多等待一次检测周期 |

局部补齐必须使用可追溯的历史模板、已应用成员布局及 partition 规则；后续仍需更新的实例按当前预算、顺序和 roleCoordination 正常滚动。UID/owner 前置条件、旧事件隔离、完整 Ready、Terminating 容量占用和同名新 UID 保护均保留。真正的离线完整 Role 丢失若有可靠完成事实，仍按当前 recoveryPolicy 的恢复范围处理；只有 controller 自己启动但未完成的旧删除批次在重启后不再重放。

<a id="restart-convergence-cases"></a>

**恢复中断后重启：过程与用例速查（2.6）**

以下四例使用 `desiredReplicas=2`、`maxUnavailable=1`、`maxSurge=0`。SG 模式的单位是完整组；Role 模式是 prefill 的一个完整 entry + worker 实例，decode 保持原版本。先完整运行 A，再提交 B 且 `partition=2`；用真实 Failed Pod 触发恢复。仅在确认故障 UID 已消失、健康成员 DELETE 被拒绝、该恢复范围的完成事实已清除后重启，避免把“离线外部删除完整 Role”误当成“controller 已启动的部分恢复”。

| 用例 | 恢复中断点 | 重启后必须保留的健康成员 | 补建与后续更新 |
| --- | --- | --- | --- |
| [RC-01](../../cases/restart-convergence/suite.json) SG + ServingGroupRecreate | 组 0 的 decode 已删，prefill 尚存 | prefill 原 UID；预算被缺失 decode 占用时，组 1 全部原 UID | 先补历史 A decode；组 0 完整 Ready 后组 1 可滚 B |
| RC-02：SG + RoleRecreate | 组 0 的 prefill worker 已删，entry 尚存 | prefill entry 与同组 decode 原 UID；组 1 同样受预算约束 | 按所属 SG 的 A 模板和 worker 布局补齐，不顺带重建健康 entry |
| RC-03：独立 Role + RoleRecreate | prefill 实例 0 的 worker 已删，entry 尚存 | 实例 0 entry、另一 Role 的全部原 UID；缺员时不得删健康 prefill 实例 1 | 按实例 0 的 A 模板补 worker；完整实例 Ready 后实例 1 可滚 B |
| RC-04：协调 Role + RoleRecreate | 同 RC-03，配置 roleCoordination、maxSkew=50% | 同 RC-03；重启不提供跳过、额外预算或解除协调约束的许可 | 按当前 partition、预算与协调规则收敛；本例不等于所有 maxSkew/依赖/顺序组合覆盖 |

四例共用的可观察过程：

| 阶段 | 当前事实与动作 | 允许结果 | 必须判错的现象 |
| --- | --- | --- | --- |
| 部分恢复后停机，partition 2→1 | 低位缺一个成员；高位旧 A 健康且刚获得更新资格 | controller 换 UID、同镜像启动并完成 initialSync | 未形成真实部分恢复就把用例判 PASS |
| 重启后暂时阻断缺员 POST | 完整 Ready 单位为 1；健康删除余量为 0 | 保留健康幸存者及高位全部 UID，等待补齐 | 为收尾旧恢复批次删幸存者；额外滚高位导致 Ready 1→0 |
| 解除 POST 阻断 | 低位仍受 partition=1 保护 | 首次新 UID 使用历史 A，entry/worker 完整 Ready；原健康成员 UID 保持 | 用 B 补受保护局部缺员；仅 entry Ready 就释放整单元预算；再次删刚补建的新 UID |
| 补齐后继续当前滚动 | 低位 A 完整 Ready，预算恢复 | 高位按正常规则滚 B，Ready 始终不少于 1 | 以“版本曾属于旧恢复”为由重删健康成员或突破预算 |
| partition 1→0 | 低位 A 现在应更新 | 允许正常滚动替换先前保留的 A UID，最终目标 B Ready | 将“保留幸存者”误解成永久禁止后续合法滚动 |
| 不再重启，注入一次新的真实故障 | 上一轮已稳定；仍配置主动恢复 | 正常完成新的 Role/SG 恢复范围，仅替换该范围 UID | 将重启简化推广为 controller 连续运行时也只补单 Pod |

补齐后如果仍不健康，只能依据**当前**故障条件、recoveryPolicy 与 grace 再次判断：纯 NotReady 不自动触发主动恢复，None 或 grace=-1 不主动删除；已有故障起点跨重启计时规则仍然有效。这些边界沿用 API §6 与 `recovery-contract` / `grace-restart`，并非 RC-01～04 新增的故障计时覆盖。旧 Pod 仍 Terminating 的正常滚动重启另见 `controller-restart/RUN-436、RUN-439`；它们不能代替本表的部分故障恢复用例。

执行入口与证据要求见 [重启收敛用例说明](../RESTART_CONVERGENCE.md)。用例定义、判定器通过和真实 Kind 结果分别记录；不能以最终 B Ready 单独判定上述中途行为正确。

### A.5. 完成、阻塞和可实现性

- **规模完成**：不再有计划中的缩容删除，`|retainedGroups|=desiredReplicas`，所有新建保留组按其版本达到完整 Ready。缩容完成可带身份债 `missingOrdinals≠∅`；后续扩容/滚动也允许保留无法无扰动偿还的旧债。
- **滚动完成**：所有现存、eligible 的保留组达到最新目标并 Ready，已发替换完成，临时 surge 已清理，`|activeGroups|=desiredReplicas`；不要求 `missingOrdinals=∅`。即使 `partition=desiredReplicas`，稀疏缩容留下的高 ordinal 保留组也可能满足 `ordinal≥partition`；只在**确实没有 eligible 保留组**时才是 partition 暂停/灰度停点，不主动重建健康组。
- **身份债**：单独记录 `missingOrdinals` 与范围外保留组；不能把它误报成版本未完成，也不能在下次普通 reconcile 中无缘无故把缩容保留下来的健康组删除。故障造成的缺位仍应走修复逻辑，不与“缩容允许的稳定债”混淆。
- **阻塞**：坏目标、历史 revision 丢失、maxScaleDown 耗尽、预算/容量不足或协调限制挡住候选时停止相应新动作并报告原因；SG 不再仅因健康高位存在就阻塞合法低位旧坏修复。不隐式放宽 maxUnavailable/maxSurge，不把已发删除回滚，不为表面连续而增加额外重建。

实现上需要持久区分保留组、临时 surge 和缩容产生的身份债（仅靠 count/ordinal 不足以在重启后判断），保存/恢复 live 历史版本，并以真实 Ready、Deleting、最新 desiredReplicas、实际活动组数 activeReplicas 和目标版未就绪数 unavailableTargetReplicas 结算预算；在途删除/创建须占位，不能按短暂对象数反复领取 maxScaleDown。这是有限的控制器状态与选择逻辑，不依赖不可实现的 Pod 重命名。本文不宣称现有 production 已符合，也不把静态推导当成 Kind 通过记录。所有复合场景的可观察过程和取舍见 [场景过程表](#scenario-tables)。

<a id="budget-lookup"></a>

## 附录 B：SG / Role 共用预算与行为速查

本表使用 API §2.3 的 `minAvailable=max(0,desiredReplicas-maxUnavailable)`、`maxScaleDown=max(0,activeReplicas-minAvailable-unavailableTargetReplicas-inFlightReservations)`、`maxHealthyScaleDown=max(0,readyReplicas-minAvailable)`。SG 的一个单位是完整 SG；Role 的一个单位是同一 SG 内同一 Role 的完整实例。“默认”列适用于 SG 和无 coordination 的 Role；“协调”列仅适用于配置 coordination 的 Role 模式。除特别说明外，候选历史已知、无冲突在途动作，协调尚有启动额度且依赖满足。版本使用 v1/v2/v3；空洞集合使用 missingOrdinals。

| 完整名称 | 含义 |
| --- | --- |
| `desiredReplicas` | 最新期望副本数：ServingGroup 模式取 `spec.replicas`，Role 模式取每组内该 Role 的 `replicas` |
| `maxUnavailable` / `maxSurge` / `partition` | 生效层配置解析后的整数值，百分比按最新期望副本数取整 |
| `activeReplicas` | 实际活动实例数，包含实际 surge；删除中的实例彻底消失前仍占容量 |
| `readyReplicas` | 全部必需成员 Ready 的实例数；包含保护区、旧版和可用 surge，排除已承诺删除的容量 |
| `unavailableTargetReplicas` | 最新目标版本的不可用实例数，包含目标 surge |
| `inFlightReservations` | 尚未由活动数减少或目标不可用数增加反映的在途额度，只扣一次 |
| `minAvailable` | 最低 Ready 数 |
| `maxScaleDown` | 旧实例总清理额度 |
| `maxHealthyScaleDown` | 健康旧实例删除上限 |

### B.1 数值与选择对照

`desiredReplicas/maxUnavailable/maxSurge/partition` 是最新有效配置，`activeReplicas/readyReplicas/unavailableTargetReplicas/inFlightReservations` 是动作前的实际账本。表中 RU-Bxx 是查表 ID，不是新增 executable case ID；不能将本表行数计为测试通过数量。

| 查表 ID / 场景 | 生效配置 | 动作前计数 | 删除预算 | 默认：SG / 独立 Role | Role + coordination |
| --- | --- | --- | --- | --- | --- |
| RU-B01 全部旧版健康，提交 v2 | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=3<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | 先替换最高旧 2，Ready 不低于 2 | 相同，另受 remainingStart/依赖限制 |
| RU-B02 低位旧 1 坏，高位旧 2 健康，目标 v3 | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | 先原位修 1，Ready 保持 2；Ready 后再滚 2 | 最高旧 2 健康但 maxHealthyScaleDown=0，等待，不能跳过 |
| RU-B03 全旧 v1 坏，提交 v2 | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=0<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | 先修最高旧 2，只修一个 | 最高旧 2 可在进度/依赖允许时修复，不因 readyReplicas<minAvailable 一律拒绝 |
| RU-B04 RU-B03 已建一个 v2，仍坏 | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=0<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 等待，不继续清旧，也不重建当前 v2 | 相同 |
| RU-B05 两个旧 v2 坏，提交 v3 | desiredReplicas=5<br>maxUnavailable=2<br>maxSurge=0<br>partition=0 | activeReplicas=5<br>readyReplicas=3<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=2<br>maxHealthyScaleDown=0 | 最多修两个旧坏，同类高到低，不删健康旧版 | 只取合法有序前缀；不能绕过前方健康阻塞者 |
| RU-B06 025：只有 protected 0 坏 | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=1 | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | 无合法旧坏候选，健康删除为零，Ready 保持 2 | 相同，保护故障仍计入 readyReplicas 缺口 |
| RU-B07 旧版健康，最新目标 surge 未 Ready | desiredReplicas=3<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 等 surge Ready；activeReplicas/unavailableTargetReplicas 同增，不产生健康删除信用 | 相同 |
| RU-B08 049：健康 v1 + 旧坏 v2 surge，目标 v3 | desiredReplicas=1<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | 只回收合法旧坏 surge，保留健康 v1 | 同样可独立回收旧坏 surge，不算稳定序号跳过 |
| RU-B09 RU-B08 的 v3 surge 已 Ready | desiredReplicas=1<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=2<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | 可以开始替换稳定旧 v1 | 有序且 remainingStart/依赖允许时才开始 |
| RU-B10 健康旧 surge 承担底线，新稳定 v3 未 Ready | desiredReplicas=3<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 保留健康旧 surge，不因其过期而清理 | 相同，并检查必要旧依赖 |
| RU-B11 旧删除已发仍占物理槽，尚未反映到 unavailableTargetReplicas | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=3<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=1 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 在途占 inFlightReservations，不重复领取 maxUnavailable | 相同，已启动还占协调额度 |
| RU-B12 N=4，maxSkew=25%，最慢 Ready=0，已启动 1 个 | desiredReplicas=4<br>maxUnavailable=2<br>maxSurge=0<br>partition=0 | activeReplicas=4<br>readyReplicas=3<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | 无协调时预算可再滚一个旧健康实例 | allowedStarted=1、remainingStart=0，稳定替换等待 |
| RU-B13 目标依赖未就绪，或最后必要旧依赖须保留 | desiredReplicas=2<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=2<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | 无协调时按默认候选规则 | 数字有额度也不得越过相应依赖约束；与 skew 门控形成收尾循环的例子见 [B.6](#terminal-maxskew-example) |
| RU-B14 全旧版坏，maxUnavailable=0，surge 因资源不足 Pending | desiredReplicas=3<br>maxUnavailable=0<br>maxSurge=1<br>partition=0 | activeReplicas=4<br>readyReplicas=0<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 仍阻塞；允许跳过不等于保证所有故障自愈 | 相同，不能绕过预算或依赖 |
| RU-B15 旧 2 坏，同时最新 surge 未 Ready | desiredReplicas=3<br>maxUnavailable=1<br>maxSurge=1<br>partition=0 | activeReplicas=4<br>readyReplicas=2<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | 可修一个旧坏 2；替代目标未 Ready 后 unavailableTargetReplicas=2/maxScaleDown=0 | 最高候选及协调约束允许时相同 |
| RU-B16 零期望、零实际更新量 | desiredReplicas=0<br>maxUnavailable=1<br>maxSurge=0<br>partition=0 | activeReplicas=0<br>readyReplicas=0<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 不启动模板替换，不产生永久 UpdateInProgress | N=0 不作进度分母或永久阻塞者；非零容量的 partition 停点仍参与 |

**协调边界澄清（2.7；RU-B11/12/13/16）**：实际版本比例使用完整正式容量 V/N，包含 partition 保护实例；局部完成不能将模板变化的非零 Role 排除。配置阶段校验共同名义 partition 比例，运行时不传播各 Role 的 partition。RU-B12 的 partition=0，原数值不变；RU-B13 仍保留必要旧依赖。API §5.1 仅在全量末步、各 Role 至多一个正式旧实例、全部 Ready、无在途及额外旧容量时许可调用方最后一步；此明确量化例外可短暂超过 maxSkew，不用于灰度停点。共用预算与候选顺序保持。任务 059 的 Go/Kind 验证单独记录，本速查说明不增加 runner 可执行覆盖。

单独缩容不是上述模板替换公式的任意删除许可：先遵守最新 desiredReplicas 的规模操作规则。全部账本按最新 desiredReplicas 重算百分比，maxUnavailable floor、maxSurge/partition ceil；旧版本判断按最新目标重算。故障已造成 readyReplicas<minAvailable 时，本规则只允许不扩大 Ready 缺口，不承诺立即恢复到 minAvailable。

<a id="b2-全旧版本不可用u1-逐个修复"></a>

### B.2 全旧版本不可用：maxUnavailable=1 逐个修复

desiredReplicas=3/maxUnavailable=1/maxSurge=0/partition=0，v1 全坏，目标 v2 正常；相同过程适用于 SG 和独立 Role，协调 Role 还须满足合法的高到低启动、依赖和百分比额度。

| 检查点 | 实例 0 | 实例 1 | 实例 2 | 动作前计数 | 删除预算 | 下一步 |
| --- | --- | --- | --- | --- | --- | --- |
| 提交 v2 | v1 NotReady | v1 NotReady | v1 NotReady | readyReplicas=0<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | 修 2 |
| 2 已承诺删除但仍存在 | v1 NotReady | v1 NotReady | v1 Deleting | readyReplicas=0<br>unavailableTargetReplicas=0<br>inFlightReservations=1 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 等删除和补建，不重复领取 |
| 2 已补 v2 | v1 NotReady | v1 NotReady | v2 NotReady | readyReplicas=0<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 等 v2 Ready |
| 2 Ready | v1 NotReady | v1 NotReady | v2 Ready | readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | 修 1 |
| 1 已补 v2 | v1 NotReady | v2 NotReady | v2 Ready | readyReplicas=1<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 等待 |
| 1 Ready | v1 NotReady | v2 Ready | v2 Ready | readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | 修 0 |
| 0 已补 v2 | v2 NotReady | v2 Ready | v2 Ready | readyReplicas=2<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 等待 |
| 全部 Ready | v2 Ready | v2 Ready | v2 Ready | readyReplicas=3<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | 无旧候选，不再删除 |

该表 activeReplicas=3；实际旧对象消失到替代对象出现之间 activeReplicas=2，对应 inFlightReservations 不再重复扣除，maxScaleDown 仍为 0。maxScaleDown>0 也必须存在合法旧候选，不能为花完额度而删除目标版本。

### B.3 连续滚动的旧 surge：049 对照

每个 Role desiredReplicas=1/maxUnavailable=0/maxSurge=1，稳定实例 0:v1 Ready，临时实例 1:v2 NotReady，目标改为 v3。两个 Role 的协调用例应始终保留至少 2 个 Ready Pod （此例 workerReplicas=0）；各 Role desiredReplicas=3 的同构例应保留至少 6 个。

| 阶段 | 稳定 0 | 临时 surge 1 | 动作前计数 | 删除预算 | 预期 |
| --- | --- | --- | --- | --- | --- |
| 提交 v3 | v1 Ready | v2 NotReady | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=0 | 只回收旧坏 surge，不删健康 0 |
| 旧 surge 删除已发 | v1 Ready | v2 Deleting | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=1 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 占 inFlightReservations，不能另删 0 |
| 创建 v3 surge | v1 Ready | v3 NotReady | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 等新容量 Ready |
| surge Ready | v1 Ready | v3 Ready | activeReplicas=2<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | 协调/依赖允许才开始稳定替换 |
| 稳定槽补建 | v3 NotReady | v3 Ready | activeReplicas=2<br>readyReplicas=1<br>unavailableTargetReplicas=1<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 保留仍在服务的 surge |
| 稳定槽 Ready | v3 Ready | v3 Ready | activeReplicas=2<br>readyReplicas=2<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=1<br>maxHealthyScaleDown=1 | 可清理不再需要的健康临时容量 |
| 终态 | v3 Ready | 无 | activeReplicas=1<br>readyReplicas=1<br>unavailableTargetReplicas=0<br>inFlightReservations=0 | maxScaleDown=0<br>maxHealthyScaleDown=0 | 每 Role 保留一个完整 Ready 实例 |

旧 surge 消失后的短暂检查点 activeReplicas=1/inFlightReservations=0/maxScaleDown=0；随后新建仍受 desiredReplicas+maxSurge 与依赖约束。仅 ordinal≥desiredReplicas 不足以证明它是 surge；范围外正式保留实例和已被扩容吸收的旧 surge 必须按真实身份处理。若旧 surge 健康且承担 readyReplicas=minAvailable，按 RU-B10 保留，不能机械套用坏 surge 清理。

### B.4 覆盖与仍需区分的执行口径

- 35 个既有设计 ID 仍为 SG-S01～S07、SG-C01～C16、SG-P01～P11、SG-R01（合计 **35**）；本附录新增 **16 个查表场景**，不是新增 executable case 或 Kind 通过记录。
- 明确改变的旧预期为 `servinggroup-compound-v2/RUN-622`（SG-S05）和 `RUN-632`（SG-C08 的旧低位 NotReady 分支）。case、generator 和通用顺序 verdict 已按 2.2 迁移；RUN-624 只放行新版本以验证全旧版持续故障。实际运行与未通过项见 [051 验证记录](../../../issues/features/051-production-baseline-realignment-DONE/runner-production-20261007/README.md)，旧结果不能直接算作新候选的 PASS/FAIL。
- Role 需要同构故障/在途场景及有协调对照；现有 35 个 SG case 不能充当 Role 覆盖。025/049 的既有复现是缺陷证据，不是新预期通过证据。
- 2.3 已按 2026-10-07 用户批准统一逐 Ready 推进，替代 A.4 的整批等待要求。普通 `CORE_EXPECTATIONS.md` 第 6 条沿用该语义。现有整批放行/终态用例不证明部分 Ready 的推进时点；须以 B.5 的控制方式单独补齐执行覆盖和 Kind 验证。本轮仅同步文档。
- admission、恢复策略、worker 完整性、真实 Ready 更新、在途去重和重启身份恢复仍须各自验证；允许跳过不授权放宽它们。

### B.5 同批部分 Ready 后继续推进

desiredReplicas=5、maxUnavailable=2、maxSurge=0、partition=0；旧 sg-2/1/0 健康，无其他故障或协调限制。以下 activeReplicas 始终为 5，minAvailable=3；删除中的旧对象尚未物理消失时仍计入活动数。

| 检查点 | readyReplicas | unavailableTargetReplicas | inFlightReservations | maxScaleDown | maxHealthyScaleDown | 行为 |
| --- | --- | --- | --- | --- | --- | --- |
| 新 sg-4/3 都 NotReady | 3 | 2 | 0 | 0 | 0 | 等待 |
| 只放行 sg-4，完整 Ready；sg-3 仍 NotReady | 4 | 1 | 0 | 1 | 1 | 可以启动旧 sg-2，不等 sg-3 |
| sg-2 删除已承诺，旧对象仍存在 | 3 | 1 | 1 | 0 | 0 | 不得继续删除 sg-1 |
| sg-2 已补新目标但尚未 Ready；sg-3 仍 NotReady | 3 | 2 | 0 | 0 | 0 | 等待下一份真实 Ready 容量 |

该例的时点回归须只放行 sg-4 的完整成员、持续保持 sg-3 NotReady，并观察 sg-2 在期限内启动；仅等待全部 Ready 不能区分旧规则。SG 以完整 SG、Role 以同一 SG 内同一 Role 的完整实例计数。协调 Role 还须满足原有有序候选、依赖和 maxSkew。Ready 回落、在途预约、protected 故障或其他限制可使额度再次为零；这不构成整批屏障。此表是批准后的设计预期，不是新增 executable case 或 Kind 通过记录。

2.4 补充的局部恢复版本规则见 API §6.1；现有 recovery 用例的首次版本和重复创建判定须专项迁移。35 个 SG 场景、B.5 时点例子与旧 Kind 结果不证明该交叉规则已被执行验证。

<a id="terminal-maxskew-example"></a>

### B.6 最后一步为何会超过 maxSkew：8/4/10 副本

本例展开 API §5.1 已批准的 2.7 全量末步规则，解释 RU-B12 的比例额度与 RU-B13 的旧依赖保留如何相互阻塞。配置为 prefill→decode→fff（箭头表示 dependsOn），正式副本数分别为 8/4/10；三个 Role 均改为 v2，partition=0、maxUnavailable=1、maxSurge=0、maxSkew=10%。开始检查时各剩一个 v1，所有正式实例完整 Ready、历史已知，无缺员、在途或额外旧容量。

进度 `p=目标版本 Ready 数/正式副本数`。此时最慢的是 decode：`3/4=75%`；普通累计启动上限为 `min(N,ceil((75%+10%)*N))`。已启动数在该检查点等于目标 Ready 数。

| Role | 目标 Ready / 正式副本 | 真实目标比例 | 普通累计启动上限 | 剩余启动额度 | 为什么不能再滚一个 |
| --- | --- | --- | --- | --- | --- |
| prefill | 7/8 | 87.5% | ceil(0.85×8)=ceil(6.8)=7 | 7−7=0 | 普通 skew 门控挡住最后一个 v1 |
| decode | 3/4 | 75% | ceil(0.85×4)=ceil(3.4)=4 | 4−3=1 | 旧 prefill 尚在，须保留最后一个旧 decode |
| fff | 9/10 | 90% | ceil(0.85×10)=ceil(8.5)=9 | 9−9=0 | 普通 skew 门控阻塞，同时旧 decode 仍需旧 fff |

即使每个 Role 都有一个健康删除预算，也无法打破这个循环。按已批准的全量末步条件，prefill 没有要求保留其旧容量的旧调用方，且依赖仍保留的旧 decode，因此可额外启动自己的最后一个实例：累计启动数从 7 到 8，超出普通上限 7 **恰好一个**。decode/fff 的旧依赖保护继续生效。

| 一种合法的 Ready 检查点轨迹 | prefill 目标 Ready | decode 目标 Ready | fff 目标 Ready | 最大比例差 Δ=max(p)−min(p) | 许可与后续动作 |
| --- | --- | --- | --- | --- | --- |
| 各剩一个旧实例 | 7/8=87.5% | 3/4=75% | 9/10=90% | 15 个百分点 | prefill 的最后一步需要末步许可；其余 Role 不能先清空必要旧依赖 |
| prefill 最后一个替代实例已 Ready，decode 尚未完成 | 8/8=100% | 3/4=75% | 9/10=90% | **25 个百分点** | prefill 的一个实例增加 12.5 个百分点；旧 prefill 已消失，decode 可按普通上限 4 完成 |
| decode 最后一个替代实例已 Ready | 8/8=100% | 4/4=100% | 9/10=90% | 10 个百分点 | fff 的普通上限为 ceil((90%+10%)×10)=10，旧 decode 已消失，可完成最后一步 |
| 全部完成 | 8/8=100% | 4/4=100% | 10/10=100% | 0 个百分点 | 无旧实例，结束滚动 |

这里有两个不同来源的离散误差：**普通向上取整**已经允许尾部 fff 达到 90%，高于最慢 75% 共 15 个百分点；**额外末步许可**才让原本被挡住的 prefill 达到 100%，相对 75% 暂时领先 25 个百分点。不能把后者解释成普通公式已放行第 8 个，也不是浮点精度误差。25 是本例可能出现的差值，不是新的全局阈值，配置的 maxSkew 仍为 10%。

表中只示意一种 Ready 检查点顺序，不要求下游等待上游替代实例 Ready：旧调用方彻底消失后，下游即可按当时普通额度、预算与其他约束判断；实际过程可交错。尚未 Ready 的替代实例只占启动额度，不提前增加 p。上述末步许可只适用于 API §5.1 的完整条件，不能用于 partition>0 灰度停点；无预算、缺员、目标不可用、未知历史、Terminating/在途或额外旧容量均不能凭此表放行。各 Role 的最低 Ready 数仍为 7/3/9。

本节是既有规则的数值说明，未新增查表 ID 或 runner executable case，也不新增 Kind 通过数量；实现和已有真实验证仍见 [059 记录](../../../issues/bugs/059-role-coordination-skew-boundaries-DONE/PROPOSAL_COMMIT.md)。
