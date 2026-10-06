# ServingGroupRollingUpdate 组合场景预期行为规范 v2.2


版本 2.2 · 2026-10-07 · [English](servinggroup-compound-rollout.en.md)

本文是**设计预期**，不是 production 已实现行为或 Kind 验证记录。主体保留 `ServingGroupRollingUpdate` 的全部 35 个组合场景；共同预算和默认选择规则也适用于 `RoleRollingUpdate`，计数单位改为每个 SG 内的完整 Role 实例。[附录 B 的 SG/Role 共用速查表](#budget-lookup)对照无协调/有协调、全旧版故障、保护区故障、连续滚动和在途动作；Role 模式的未变更 Role 保留 UID。这不表示现有 35 个 SG 执行用例已经覆盖 Role 或已经迁移到本版。

这里用 **v1 → v2 → v3** 表示先后提交的配置版本。“v2 Ready”表示整组已经就绪；“v2 NotReady”表示还未就绪；“v1 删除中”表示旧组正在退出；“临时 surge”表示滚动期间多建的组；“受保护”表示当前 partition 不允许主动更新该序号。“无”表示该序号当前没有组。每张表的“组数”同时列出活动组与 Ready 组；删除中的组在彻底消失前仍占活动名额。表中 `N` 是期望组数，`U` 是 maxUnavailable，`S` 是 maxSurge，`P` 是 partition；例如“活动 4→2；Ready 2～4”表示缩容删除过程中活动组逐步从 4 变为 2，Ready 组在 2 到 4 之间变化。

阅读所有场景时，按下面几条原则判断：

- SG 和未配置 roleCoordination 的 Role **默认允许跳过**：合法旧 NotReady 优先，同类按 ordinal 高到低，再处理旧 Ready，无新增开关。配置 coordination 的 Role 模式稳定旧实例必须按高到低，最高候选受阻就等待；maxSkew 仍是百分比进度约束，不是 index 配对承诺。扩容仍优先补最低空洞，滚动仍保留原位补建和身份规则。
- 同时扩缩容和更新时，先安排新旧副本数，再考虑删除健康旧组。每一次删除都要重新看真实 Ready 组数。新扩出来的组不必全部 Ready；正式扩容组的未 Ready 缺口不能当成额外滚动信用，临时 surge 即使未 Ready，也要在组数和新版未就绪数两边**同时入账**。
- `maxUnavailable` 和 `maxSurge` 的百分比按**最新期望组数**重算：前者向下取整，后者向上取整。活动组不得超过“期望组数 + maxSurge”；删除健康组后，Ready 组不得低于“期望组数 − maxUnavailable”。如果故障已使 Ready 低于这条底线，仍可清理**过期且已 NotReady**的合法旧组，数量受 `M=max(0,N-U)`、`Q=max(0,C-M-V-I)` 限制；健康删除另受 `B=max(0,R-M)` 限制。C 含实际已创建 surge，V 含最新目标 NotReady surge；R 覆盖 protected/旧版/可用 surge，排除已承诺删除容量。I 是尚未由 C 减少或 V 增加反映的在途额度，不能重复扣减；稳定检查点 I=0。完整定义和两阶段选择见 [API §2.3](modelserving-api-reference.zh-CN.md#23-两层共用的预算与默认选择顺序)。Q 不是任意健康删除许可，不能预支尚未创建的 maxSurge。同一额度不能在替代组尚未 Ready 时重复领取。同一目标版本已 NotReady 时不反复重建。缩容刚发生时，原有组可能短暂超过新上限，此时只能清理，不能再创建。
- 单独缩容可以留下稀疏序号。后续扩容和滚动优先利用真正需要创建或替换的机会补洞，**不会为了排齐序号而重建健康且已是目标版本的组**。一次动作不要求把此前的空洞全部补完。
- `partition` 按固定序号判断，不是保护“当前排在前面的若干组”。被保护序号若要补建，使用本轮灰度前的历史版本；已经存在的新版组后来进入保护区时，不会为了回旧版而主动重建。
- 旧的临时 surge 若因扩容进入正式序号范围，可以保留原组并转为正式组；没进入范围的临时组不会仅因总数凑齐就自动转正。组内 Role 副本数增加若影响整组 Ready，也要分批进行以守住可用性。

下面的阶段是**一条允许的执行过程**，异步事件不必严格逐行发生。预算、历史版本与完成条件的完整约束见 [附录中的完整行为规范](#behavior-rules)。

<a id="scenario-tables"></a>

## 1. 缩容稀疏、机会性补洞与顺序取舍

### SG-S01：单独缩容留洞；一次扩容不强制排齐

`N=4`，sg-0～sg-3 均为 v1 Ready。自定义删除优先级（deletion cost）使 sg-1/sg-2 成为缩容对象，保留健康 sg-3；随后分别扩到 3 和 4。

| 阶段 | N | sg-0 | sg-1 | sg-2 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 4；Ready 4 | 连续 |
| 缩容 | 2 | v1 Ready | v1 删除中 | v1 删除中 | v1 Ready | 活动 4→2；Ready 2～4 | 缩容可选择低 cost；不能当成滚动 U 消耗 |
| 缩容终态 | 2 | v1 Ready | 无 | 无 | v1 Ready | 活动 2；Ready 2 | `{0,3}` 是允许的稀疏保留集 |
| 扩容 | 3 | v1 Ready | v1 NotReady | 无 | v1 Ready | 活动 3；Ready 2 | 只有一个新增容量槽，先补最低洞 1 |
| 扩容终态 | 3 | v1 Ready | v1 Ready | 无 | v1 Ready | 活动 3；Ready 3 | `{0,1,3}`；不为补 2 重建健康 sg-3 |
| 再扩容 | 4 | v1 Ready | v1 Ready | v1 NotReady→v1 Ready | v1 Ready | 活动 4；Ready 3→4 | 补 2；sg-3 直接进入新范围且 UID 不变 |

这就是“一次动作不保证恢复连续”的正例。若只有第一次扩容，不再发生新动作，旧空洞 `{2}` 可以稳定存在；控制器日常处理不主动消灭它。

### SG-S02：滚动可以用“本来就过期”的高位组偿还旧空洞

从 `N=2,{sg-0 v1,sg-3 v1},P=0,U=1,S=0` 提交目标 v2；sg-3 也过期，且在 `[0,2)` 外。

| 阶段 | sg-0 | sg-1 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- |
| 初始 | v1 Ready | 无 | v1 Ready | 活动 2；Ready 2 | 空洞 1 来自以前的缩容 |
| 1 | v1 Ready | 无 | v1 删除中 | 活动 2→1；Ready 1 | 最高旧版 sg-3 本就需要替换 |
| 2 | v1 Ready | v2 NotReady→v2 Ready | 无 | 活动 2；Ready 1→2 | 其替代容量落到最低洞 1，不重建健康目标版组 |
| 3 | v1 删除中 | v2 Ready | 无 | 活动 2→1；Ready 1 | 再处理旧版 sg-0，不能把替代容量放到高位 |
| 终态 | v2 Ready | v2 Ready | 无 | 活动 2；Ready 2 | sg-0 原位重建；旧空洞顺带清零 |

### SG-S03：不能为了排齐而删除健康、目标版本的高位组

`N=2,P=0,U=1,S=0`，之前缩容留下 `{sg-0 v1,sg-3 v2}`，当前目标仍为 v2。

| 阶段 | sg-0 | sg-1 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- |
| 初始 | v1 Ready | 无 | v2 Ready | 活动 2；Ready 2 | sg-3 v2 已健康且无需更新 |
| 1 | v1 删除中 | 无 | v2 Ready | 活动 2→1；Ready 1 | 只更新旧版 sg-0 v1 |
| 终态 | v2 Ready | 无 | v2 Ready | 活动 2；Ready 2 | 旧洞 1 尚在；不删除 sg-3 v2 换 sg-1 v2 |

即使配置 `S=1`，也不能用“先造 sg-1 v2 再删除 sg-3 v2”绕过少重建原则；sg-3 v2 是保留组，不是待清理的临时 surge。需要准确标记两类身份。

### SG-S04：扩容后旧 surge 序号仍不在正式范围内，不能按总数误复用

缩容曾留下保留组 `{sg-0 v1,sg-1 v1,sg-3 v2}`，`N=3,U=0,S=1`；sg-3 v2 已是目标版本，为更新 sg-1 v1 和 sg-0 v1 创建了 sg-4 v2 临时 surge。此时 N:3→4，目标仍是 v2。新的正式序号范围是 0～3：它包含保留组 sg-3，却**不包含** sg-4；虽然活动数恰好为新 N=4，正式保留组仍只有三个，sg-2 必须补建。

| 阶段 | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3 | v1 Ready | v1 Ready | 无 | v2 Ready | v2 Ready（临时 surge） | 活动 4；Ready 4 | sg-3 v2 是健康保留组，sg-4 v2 是临时组 |
| 扩容 | 4 | v1 Ready | v1 Ready | v2 NotReady | v2 Ready | v2 Ready（临时 surge） | 活动 5；Ready 4 | 新上限 5；在最低空洞 2 建正式 sg-2 v2 |
| 扩容 Ready | 4 | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5；Ready 5 | sg-4 v2 未因“总数=N”被错误转正 |
| 滚动 | 4 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 4～5；Ready 4→5 | 借 sg-4 v2 的服务容量滚 sg-1 |
| 终态 | 4 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 4；Ready 4 | 再滚 sg-0，最后清理 sg-4 v2；sg-3 v2 UID 不变 |

与 SG-C03/P10 不同，本例的旧 surge 序号**仍在新的正式范围外**；“复用”取决于身份是否进入新范围，不能只看当前组数。

### SG-S05：稀疏扩容形成低位故障，默认优先修复

先从 3 组缩到 2 组，删除代价较低的 sg-1 被移除，保留 sg-0/2:v1。扩回 3 组并提交 v2，在低洞 1 创建的 v2 持续 NotReady；再提交 v3。预算 N=3/U=1/S=0/P=0。SG 与无 coordination 的 Role 默认旧 NotReady 优先，可以跳过健康旧 2 修复旧坏 1，无需额外开关。

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| v1 稳态 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 活动 3；Ready 3 | 初始健康 |
| 缩容 | 2/v1 | v1 Ready | v1 删除中 | v1 Ready | 活动 3→2；Ready 2～3 | 按删除代价移除 1 |
| 留洞 | 2/v1 | v1 Ready | 无 | v1 Ready | 活动 2；Ready 2 | 保留高位正式组 2 |
| 扩容并提交 v2 | 3/v2 | v1 Ready | v2 NotReady | v1 Ready | 活动 3；Ready 2 | 正式新增槽补低洞 |
| 提交 v3 | 3/v3 | v1 Ready | v2 NotReady | v1 Ready | 活动 3；Ready 2 | M=2、Q=1、B=0，不能删健康 2 |
| 优先修复 1 | 3/v3 | v1 Ready | v2 删除中→v3 NotReady | v1 Ready | 活动 2～3；Ready 2 | 只删除旧坏 1，原位补 v3 |
| 修复完成 | 3/v3 | v1 Ready | v3 Ready | v1 Ready | 活动 3；Ready 3 | 恢复健康删除余量 |
| 滚高位 2 | 3/v3 | v1 Ready | v3 Ready | v1 删除中→v3 Ready | 活动 2～3；Ready 2→3 | 健康旧候选按高到低 |
| 滚 0 | 3/v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | 活动 2～3；Ready 2→3 | 最后一个旧组 |
| 终态 | 3/v3 | v3 Ready | v3 Ready | v3 Ready | 活动 3；Ready 3 | 无额外健康损失 |

在相同状态的 **Role + coordination 对照**中，最高旧候选 2 健康且 B=0，仍必须等待，不能跳过它先修 1；Q=1 不覆盖顺序、maxSkew 或依赖约束。若新建的 v3-1 也不 Ready，V=1/Q=0，应等待而非反复重建同版。本版替代此前 SG-S05 的严格顺序阻塞预期；执行用例迁移状态见附录 B。

### SG-S06：两组滚到错误 v2 后，用 v3 修复

期望 5 组，maxUnavailable=2、maxSurge=0。v1 全部 Ready；同一批次选最高的 sg-4、sg-3 更新为 v2，但错误配置使两组都无法 Ready。此时已恰好达到“至少 3 组 Ready”的底线。沿用 Kubernetes Deployment `rolling.go` 的额度演算：v2 为当前目标时，`Q=5-3-2=0`，不得继续清理；提交 v3 后，坏 v2 变成旧版，空的 v3 暂无不可用组，`Q=5-3-0=2`，可以同批清理最高的两组。

| 阶段 | 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 5；Ready 5 | 尚未更新 |
| 同批启动 | v2 | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | v1 删除中 | 活动 3～5；Ready 3 | U=2 同时选择最高的 sg-4、sg-3 |
| 两组 v2 未就绪 | v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 错误版本占满 U=2 |
| v2 阻塞 | v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | Q=0；不能删健康 sg-2，也不重建同版本 v2 |
| 提交修正版本 | v3 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 坏 v2 变旧版，Q=2 |
| 同批清理坏 v2 | v3 | v1 Ready | v1 Ready | v1 Ready | v2 删除中 | v2 删除中 | 活动 3～5；Ready 3 | 选择顺序 4→3；两组原本都不可用 |
| 创建 v3 | v3 | v1 Ready | v1 Ready | v1 Ready | v3 NotReady | v3 NotReady | 活动 5；Ready 3 | 此时 Q 又为 0，不能继续删 sg-2 |
| v3 就绪 | v3 | v1 Ready | v1 Ready | v1 Ready | v3 Ready | v3 Ready | 活动 5；Ready 5 | U=2，可同批选择剩余最高旧组 sg-2、sg-1 |
| 同批删除 sg-2、sg-1 | v3 | v1 Ready | v1 删除中 | v1 删除中 | v3 Ready | v3 Ready | 活动 3～5；Ready 3 | 按 2→1 选组；Ready 恰到最低要求 3 |
| 同批创建 v3 | v3 | v1 Ready | v3 NotReady | v3 NotReady | v3 Ready | v3 Ready | 活动 5；Ready 3 | V=2、Q=0；不得再删 sg-0 |
| 两组 v3 就绪 | v3 | v1 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | 活动 5；Ready 5 | 等本批完成，再处理 sg-0 |
| 滚 sg-0 | v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | 活动 4～5；Ready 4→5 | 最后更新 |
| 终态 | v3 | v3 Ready | v3 Ready | v3 Ready | v3 Ready | v3 Ready | 活动 5；Ready 5 | 全部完成 |

如果用户没有提交新版本，sg-4/sg-3 已经是目标 v2，“仍 NotReady”本身不构成再次滚动的理由。

### SG-S07：初始版本五组全 NotReady，允许不降级修复

期望 5 组，maxUnavailable=2、maxSurge=0。已创建的 v1 五组全都 NotReady，Ready 数为 0，故障在滚动前就已超出预算。用户提交修正版本 v2；虽然无法立即达到“至少 3 组 Ready”，旧版五组都已不可用，初始 `Q=5-3-0=2`：可按 4→3、2→1、0 分批替换，前批 v2 Ready 后才重新获得清理额度。

| 阶段 | 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始故障 | v1 | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | 活动 5；Ready 0 | 已有故障，不是本轮滚动造成 |
| 提交修正版本 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | v1 NotReady | 活动 5；Ready 0 | Q=2；先选最高的 4、3 |
| 第一批清理 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v1 删除中 | v1 删除中 | 活动 3～5；Ready 0 | 删除两组不再降低 Ready |
| 第一批创建 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v2 NotReady | v2 NotReady | 活动 5；Ready 0 | Q=0；若 v2 也坏，在此阻塞 |
| 第一批就绪 | v2 | v1 NotReady | v1 NotReady | v1 NotReady | v2 Ready | v2 Ready | 活动 5；Ready 2 | Q 恢复为 2 |
| 第二批清理 | v2 | v1 NotReady | v1 删除中 | v1 删除中 | v2 Ready | v2 Ready | 活动 3～5；Ready 2 | 继续按 2→1 选择 |
| 第二批创建 | v2 | v1 NotReady | v2 NotReady | v2 NotReady | v2 Ready | v2 Ready | 活动 5；Ready 2 | Q=0；等待本批就绪 |
| 第二批就绪 | v2 | v1 NotReady | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 4 | 已超过底线 3 |
| 修复 sg-0 | v2 | v1 删除中→v2 NotReady→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 最后一个旧版组 |
| 终态 | v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 全部恢复 |

若首批 v2 也持续 NotReady，`V=2,Q=0`，停止后续清理并报告阻塞；不会因为五个 v1 都坏而一次重建五组。后续操作等待版本再次修正或明确的故障恢复策略。

## 2. 滚动过程中扩缩容

### SG-C01：滚动中扩容，S=0

`N:3→5,U=1`，v1→v2；sg-2 的删除已发出。

| 阶段 | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | v1 稳态 |
| 1 | 3 | v1 Ready | v1 Ready | v1 删除中→v2 NotReady | 无 | 无 | 活动 2～3；Ready 2 | 高位先滚 |
| 2 | 5 | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | v2 NotReady | 活动 5；Ready 2 | 扩容槽 3、4 直接用 v2；不撤销旧在途动作 |
| 3 | 5 | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 规模阶段全部 Ready 后再删健康旧组 |
| 4 | 5 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 继续 sg-1 |
| 5 | 5 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 4～5；Ready 4→5 | 最后更新 sg-0 |
| 终态 | 5 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 全部 v2 |

阶段 2 的 Ready 低于新 `N-U=4`，是扩容缺口/既有在途的事实，**不是**可以再删一组 v1 的许可。

### SG-C02：滚动中缩容

`N:3→2,U=1`；v1→v2 时 sg-2 已在创建 v2，但新 N 不再需要它。

| 阶段 | N | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3 | v1 Ready | v1 Ready | v2 NotReady | 活动 3；Ready 2 | 在途 sg-2 v2 不会 Ready |
| 1 | 2 | v1 Ready | v1 Ready | v2 删除中 | 活动 3→2；Ready 2 | 缩容先选 NotReady sg-2 v2，不继续滚健康组 |
| 2 | 2 | v1 Ready | v1 Ready | 无 | 活动 2；Ready 2 | 缩容完成 |
| 3 | 2 | v1 Ready | v1 删除中→v2 Ready | 无 | 活动 1～2；Ready 1→2 | 最高保留旧组 sg-1 |
| 4 | 2 | v1 删除中→v2 Ready | v2 Ready | 无 | 活动 1～2；Ready 1→2 | 最后 sg-0 |
| 终态 | 2 | v2 Ready | v2 Ready | 无 | 活动 2；Ready 2 | 不复活 sg-2 |

### SG-C03：滚动中扩容复用旧 surge

`N:3→5,U=0,S=1,P=0`，原 sg-0～sg-2 均为 v1 Ready，临时组 sg-3 为 v2 Ready。新 N 使 sg-3 v2 转为保留组；sg-4 v2 是新保留组，sg-5 v2 才占新 surge 槽。

| 阶段 | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
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

`N:4→3,U=0,S=1`；sg-0～sg-3 均为 v1 Ready，sg-4 是 v2 Ready 的临时 surge。新物理上限从 5 变 4，但 sg-4 v2 的**序号**超出 `[0,N+S)=[0,4)` 不等于必须先删它。缩容本来就要移除最高旧版保留组 sg-3 v1；等 sg-3 v1 完全消失，sg-4 v2 仍可占唯一临时槽，为更新 sg-2 v1/sg-1 v1/sg-0 v1 提供 Ready 信用。这样无需重建 sg-4 v2，也不把要缩掉的 sg-3 v1 先滚成 sg-3 v2。

| 阶段 | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready（临时 surge） | 活动 5；Ready 5 | 旧上限 5 |
| 缩容 | 3 | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | v2 Ready（临时 surge） | 活动 5→4；Ready 4～5 | sg-3 v1 是本来就要缩掉的旧版保留组 |
| 缩容完成 | 3 | v1 Ready | v1 Ready | v1 Ready | 无 | v2 Ready（临时 surge） | 活动 4；Ready 4 | 活动数满足新上限；sg-4 v2 仍是临时组 |
| 滚 2 | 3 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | 无 | v2 Ready（临时 surge） | 活动 3～4；Ready 3→4 | 借 sg-4 v2 守住 Ready≥3 |
| 滚 1 | 3 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | 无 | v2 Ready（临时 surge） | 活动 3～4；Ready 3→4 | sg-2 Ready 后继续 |
| 滚 0 | 3 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | 无 | v2 Ready（临时 surge） | 活动 3～4；Ready 3→4 | 最后 sg-0 |
| 终态 | 3 | v2 Ready | v2 Ready | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 清理旧 sg-4 v2 surge；未重建它 |

### SG-C15：百分比 U 随扩容重算，坏 v2 阻塞

maxUnavailable 设为 25%：期望 5 组时向下取整，可容忍 1 组不可用，至少要有 4 组 Ready；扩到 9 组后可容忍 2 组不可用，至少要有 7 组 Ready。此例没有 surge；新增的不可用额度不是扩容之外的“免费删除额度”。

| 阶段 | 期望组数 / U / Ready 底线 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | sg-6 | sg-7 | sg-8 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/1/4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | 无 | 无 | 无 | 无 | 活动 5；Ready 5 | 高位 sg-4 已完成 |
| 扩容 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | v2 NotReady | v2 NotReady | v2 NotReady | 活动 9；Ready 5 | 新组先用 v2；不能删 sg-3 v1 |
| 部分 Ready | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | v2 NotReady | 活动 9；Ready 7 | 恰在底线，仍不能删 sg-3 v1 |
| 获得信用 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | 活动 9；Ready 8 | 现在可删一组健康 v1 |
| 滚 3 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 删除中→v2 NotReady | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | 活动 8～9；Ready 7 | sg-3 与扩容中 sg-8 共占 U=2 |
| 恢复 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 9；Ready 9 | sg-3 v2 与 sg-8 v2 均 Ready |
| 滚 2 | 9/2/7 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 8～9；Ready 8→9 | 继续高到低 |
| 滚 1 | 9/2/7 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 8～9；Ready 8→9 | sg-2 Ready 后继续 |
| 滚 0 | 9/2/7 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 8～9；Ready 8→9 | 最后 sg-0 |
| 终态 | 9/2/7 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 9；Ready 9 | 全部 v2 |
| 坏 v2 分支 | 9/2/7 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | v2 NotReady | v2 NotReady | v2 NotReady | 活动 9；Ready 4 | v2 永不 Ready；不再删 sg-3 v1 |

“坏 v2 分支”是另一次运行：初始 sg-4 v2 也未 Ready，不是从上面的 sg-4 v2 Ready 状态倒退。即使 `U=2`，该分支的真实 Ready=4 已低于 7，唯一合法进展是修正目标或恢复组，不能继续破坏健康 v1。

### SG-C16：已创建的 surge 与新版 NotReady 必须同时入账

`N=3,U=1,S=1,P=0`，最少需要 2 个 Ready 组。旧版 sg-2 原本就 NotReady；目标改为 v2，并在 sg-3 建立临时 surge。表中 `C/R/V/Q` 依次是活动组数、Ready 组数、目标 v2 的 NotReady 组数、`Q=max(0,C-(N-U)-V)`。只在删除彻底完成后才重建同名组；表中的“无”是更新在途的短暂缺位。

| 阶段 | sg-0 | sg-1 | sg-2 | sg-3（临时 surge） | C / R / V / Q | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| v1 故障，提交 v2 | v1 Ready | v1 Ready | v1 NotReady | 无 | 3 / 2 / 0 / 1 | 最高旧组 sg-2 已不可用 |
| 创建 surge | v1 Ready | v1 Ready | v1 NotReady | v2 NotReady | 4 / 2 / 1 / 1 | 实际多出 1 组，且它未 Ready；两项抵消 |
| 清理 sg-2 | v1 Ready | v1 Ready | 无 | v2 NotReady | 3 / 2 / 1 / 0 | Q=1 允许清理旧版 unhealthy；Ready 不降 |
| 重建 sg-2 | v1 Ready | v1 Ready | v2 NotReady | v2 NotReady | 4 / 2 / 2 / 0 | Q 耗尽；若两组 v2 都坏，就停在这里 |
| sg-2 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | 4 / 3 / 1 / 1 | 可以更新下一个健康旧组 sg-1 |
| 删除 sg-1 | v1 Ready | 无 | v2 Ready | v2 NotReady | 3 / 2 / 1 / 0 | 健康组删除后仍有 2 组 Ready |
| 重建 sg-1 | v1 Ready | v2 NotReady | v2 Ready | v2 NotReady | 4 / 2 / 2 / 0 | 等新 sg-1 Ready |
| sg-1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 NotReady | 4 / 3 / 1 / 1 | 下一个旧组是 sg-0 |
| 删除 sg-0 | 无 | v2 Ready | v2 Ready | v2 NotReady | 3 / 2 / 1 / 0 | 保持高到低顺序 |
| 重建 sg-0 | v2 NotReady | v2 Ready | v2 Ready | v2 NotReady | 4 / 2 / 2 / 0 | 仍不重建同版本的坏 surge |
| sg-0 Ready | v2 Ready | v2 Ready | v2 Ready | v2 NotReady | 4 / 3 / 1 / 1 | 正式组已全部更新 |
| 清理 surge | v2 Ready | v2 Ready | v2 Ready | 无 | 3 / 3 / 0 / 1 | 临时组未 Ready，清理不减少 Ready |

创建 sg-3 前后，`Q` 都是 1：`C` 增 1，`V` 也增 1。直接写 `U-V=1-1=0` 会错误阻塞 sg-2 的无损清理。反之，重建 sg-2 后 `V=2,Q=0`，即使剩余旧组也有故障，仍要等至少一个新版组 Ready、提交新目标，或通过其他合法方式取得额度。这里借鉴的是社区的**额度结算**，不是 Deployment 按旧 RS 健康状况自由选择删除对象：本例旧坏组恰为最高位 2，其余健康旧组仍按 1→0 处理。

## 3. 扩缩容途中开始滚动，以及同一请求的原子变更

### SG-C04：扩容途中开始 v2

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | 稳态 |
| 扩容途中 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | 无 | 活动 4；Ready 3 | sg-3 v1 创建请求已接受 |
| v2 到来 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | v2 NotReady | 活动 5；Ready 3 | sg-3 v1 不原地变 v2；缺位 4 用 v2；V=1、Q=0 |
| sg-4 就绪 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | v2 Ready | 活动 5；Ready 4 | V=0、Q=1；不用等待旧 sg-3 v1 Ready |
| 清理 sg-3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 删除中 | v2 Ready | 活动 5→4；Ready 4 | 最高旧组已 NotReady，删除不降低 Ready |
| 重建 sg-3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | v2 Ready | 活动 5；Ready 4 | V=1、Q=0；等 sg-3 v2 Ready |
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
| 滚动 | 3/v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 新 N 下 U=1，先 sg-2 |
| 继续 1 | 3/v2 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | sg-2 Ready 后更新 sg-1 |
| 继续 0 | 3/v2 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 最后 sg-0 |
| 终态 | 3/v2 | v2 Ready | v2 Ready | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 全部 v2 |

如果缩容按 NotReady/cost 选择 sg-1 而留下高位健康组，终态可以稀疏；随后滚动只以必要的旧版替换机会补洞，不为排齐删除目标版组（见 SG-S01～S03）。

### SG-C13：扩容中开始滚动，按新 N 申请 surge

`N:3→5,U=0,S=1`，sg-3 v1 已由先前扩容创建并 Ready，sg-4 v1 尚未创建；此时 v2 到来。`sg-4` 是正式扩容槽，`sg-5` 才是新 N 的 surge。

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 4；Ready 4 | 扩容未完 |
| v2 到来 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | 无 | 活动 5；Ready 4 | 先补正式缺位；不删 sg-3 v1 |
| 规模完成 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady（临时 surge）→v2 Ready（临时 surge） | 活动 6；Ready 5→6 | 额外 sg-5 v2 占 S=1 |
| 滚 3 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 最高旧组 sg-3 |
| 滚 2 | 5/v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | sg-3 Ready 后更新 sg-2 |
| 滚 1 | 5/v2 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 再 sg-1 |
| 滚 0 | 5/v2 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 最后 sg-0 |
| 终态 | 5/v2 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 5；Ready 5 | sg-5 v2 清理，S 归还 |

## 4. v1→v2→v3、坏版本与二次滚动

### SG-C08：固定 N，v2 尚未完成即直接提交 v3

`N=3,U=1,S=0,P=0`；sg-2 已更新为 v2 Ready，sg-1 的旧版 v1 删除已发出，但新版 sg-1 的创建**尚未**发出。v3 到来后不可撤销已发删除，重建直接使用 v3。

| 阶段 | 目标 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | v1 | v1 Ready | v1 Ready | v1 Ready | 活动 3；Ready 3 | 基线 |
| v2 进行中 | v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | 活动 2～3；Ready 2→3 | 先滚最高位 2 |
| v2 下一步 | v2 | v1 Ready | v1 删除中 | v2 Ready | 活动 3→2；Ready 2 | sg-1 删除已接受 |
| v3 接管 | v3 | v1 Ready | v3 NotReady→v3 Ready | v2 Ready | 活动 3；Ready 2→3 | 不能复活旧 sg-1 v1；直接创建 sg-1 v3 |
| 继续 | v3 | v1 Ready | v3 Ready | v2 删除中→v3 Ready | 活动 2～3；Ready 2→3 | sg-1 v3 Ready 后回到最高健康旧组 2 |
| 最后 | v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | 活动 2～3；Ready 2→3 | 最后 sg-0 v1→v3 |
| 终态 | v3 | v3 Ready | v3 Ready | v3 Ready | 活动 3；Ready 3 | 无需全量 v2 |

若 sg-1 v2 的创建请求已经发出，不能偷偷把在建 sg-1 v2 改成 v3。它后来若 NotReady 且版本过期，SG/独立 Role 默认可以先修 sg-1：此时 `N=3,U=1,C=3,R=2,V=0,I=0,Q=1,B=0`，只删除旧坏 sg-1，原位补 v3，Ready 保持 2；待其 Ready 后再滚健康 sg-2，最后 sg-0。若 sg-1 v2 Ready，则两者同为健康旧实例，先 sg-2 再 sg-1。相同状态的协调 Role 不可跳过健康高位，仍在 B=0 时等待；这不改变百分比 maxSkew。已发事件决定允许的中间态，最新意图决定未来动作。

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

原 `N=3`，扩到 5 时 sg-3 v1 已 Ready、sg-4 尚未创建；这时提交错误配置 v2，最后的扩容槽 sg-4 直接创建 v2 却永不 Ready。`N=5,U=1,S=0`，目标回到 v1 后，sg-4 v2 已过期且不贡献 R，自动以 v1 修复。

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | 原基线 |
| 扩容中 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 无 | 活动 4；Ready 4 | sg-3 v1 已 Ready，sg-4 尚未申请 |
| v2 阻塞 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | 活动 5；Ready 4 | U=1 已占满；不碰健康 sg-3 v1 |
| 回滚 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 删除中 | 活动 5→4；Ready 4 | sg-4 v2 过期且不可用，可自动清理 |
| 恢复 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | 活动 5；Ready 4 | 按最新 v1 在原位重建 |
| 终态 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 5；Ready 5 | 其余健康 v1 组 UID 不变 |

这是有意采用最新意图与自动修复的 ModelServing 语义；[StatefulSet 文档](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) 说明其 OrderedReady 坏模板回滚可能仍需用户手工删除坏 Pod。

### SG-C14：v1→v2→v3 时，旧 v2 surge 仍承担服务容量

`N=3,U=0,S=1,P=0`；sg-3 v2 临时 surge Ready，sg-2 v1 删除已经发出；在 v3 到来时不可先清理 sg-3 v2，否则 Ready 低于 3。

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

### 基础过程：P=1，高位先滚

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/1/v1 | v1 Ready | v1 Ready | v1 Ready | 活动 3；Ready 3 | 保护 sg-0 |
| 1 | 3/1/v2 | v1 Ready（受保护） | v1 Ready | v1 删除中→v2 Ready | 活动 2～3；Ready 2→3 | 从序号最高的可更新组 sg-2 开始 |
| 2 | 3/1/v2 | v1 Ready（受保护） | v1 删除中→v2 Ready | v2 Ready | 活动 2～3；Ready 2→3 | sg-2 Ready 后更新 sg-1 |
| 终态 | 3/1/v2 | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 3；Ready 3 | 不动 sg-0 |

### SG-P01：P=1 灰度后扩容

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/1 | v1 Ready（受保护） | v2 Ready | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 灰度稳态 |
| 扩容 | 5/1 | v1 Ready（受保护） | v2 Ready | v2 Ready | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 从低位缺号 3、4 补 v2 |
| 终态 | 5/1 | v1 Ready（受保护） | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 活动 5；Ready 5 | 旧 0～2 UID 不变 |

### SG-P02：扩容时原子调整整数 P，跨过保护边界

初始 N=3/P=3，提交 N=6/P=5 的同一请求。旧版示例 N=3/P=5 现在必须被 admission 拒绝；不能为了执行流程绕过上界。

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/3/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | 无 | 无 | 无 | 活动 3；Ready 3 | partition=3，暂时没有可更新的组 |
| 扩容 | 6/5/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v1 NotReady（受保护） | v1 NotReady（受保护） | v2 NotReady | 活动 6；Ready 3 | 新增 sg-3/4 用历史 v1，sg-5 用 v2 |
| 终态 | 6/5/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 活动 6；Ready 6 | 5 组 v1、1 组 v2 |

### SG-P03：缩容可以穿越保护区并留下稀疏

在组都健康、删除优先级相同时，期望组数从 5 缩至 2，同一请求将 partition 从 3 改为 2：从最高序号缩容，先删 sg-4 v2 和 sg-3 v2，再删 sg-2 v1，留下 `{sg-0 v1,sg-1 v1}`。P 不是最小副本数。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 5；Ready 5 | 灰度稳态 |
| 缩容 | 2/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 删除中 | v2 删除中 | v2 删除中 | 活动 5→2；Ready 2～5 | 同优先级按高位先删 |
| 终态 | 2/2 | v1 Ready（受保护） | v1 Ready（受保护） | 无 | 无 | 无 | 活动 2；Ready 2 | 默认路径连续 |

若另有 `N=3,P=2,{sg-0 v1 Ready,sg-1 v1 NotReady,sg-2 v2 Ready}`，缩到 N=2 时 NotReady sg-1 优先于健康 sg-2 被删除，允许 `{sg-0 v1,sg-2 v2}`：

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 缩容前 | 3/2 | v1 Ready（受保护） | v1 NotReady（受保护） | v2 Ready | 活动 3；Ready 2 | sg-1 故障 |
| 缩容后 | 2/2 | v1 Ready（受保护） | 无 | v2 Ready | 活动 2；Ready 2 | 缩容合法留洞，不删除健康 sg-2 v2 |
| 再扩容 | 3/2 | v1 Ready（受保护） | v1 NotReady（受保护）→v1 Ready（受保护） | v2 Ready | 活动 3；Ready 2→3 | 最低洞 1 取历史 v1；sg-2 v2 保 UID |

### SG-P04：百分比 P 因扩容提高

`P="50%"`：N=3 时 P=2；N=5 时 P=3。旧 sg-2 v2 被新 P 保护，但现存 sg-2 v2 不主动回滚 v1。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 灰度 |
| 扩容 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready（受保护） | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 存量 sg-2 v2 仍 v2；3/4 用 v2 |
| 终态 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready（受保护） | v2 Ready | v2 Ready | 活动 5；Ready 5 | 不要求“恰好 P 个 v1” |

若 sg-2 v2 后来失效/被删除，恢复缺位 2 依新 P 使用历史 v1；这与不主动替换健康 sg-2 v2 并不矛盾。

### SG-P05：百分比 P 因缩容降低

`P="50%"`：N=5 时 P=3；N=3 时 P=2。缩容先删除高位，其后 sg-2 v1 因保护边界下降而可以更新。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 5；Ready 5 | 原灰度 |
| 缩容 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready | v2 删除中 | v2 删除中 | 活动 5→3；Ready 3～5 | 缩容先完成 |
| 滚动 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 删除中→v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 只更新新解锁的 sg-2 |
| 终态 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 2 组 v1、1 组 v2 |

### SG-P06：在途时提高 P

`N=3`，sg-2 v2 Ready，sg-1 v1 的删除已发出；P:0→3。后续新建的 sg-1 按历史 v1，现存 sg-2 v2 不主动回退。

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 在途 | 3/0/v2 | v1 Ready | v1 删除中 | v2 Ready | 活动 3→2；Ready 2 | 删除不可撤销 |
| 改 P | 3/3/v2 | v1 Ready（受保护） | v1 删除中（受保护） | v2 Ready（受保护） | 活动 3→2；Ready 2 | 所有位置被新 P 保护 |
| 停点 | 3/3/v2 | v1 Ready（受保护） | v1 NotReady（受保护）→v1 Ready（受保护） | v2 Ready（受保护） | 活动 3；Ready 2→3 | 不更新 sg-0，不回滚现存 sg-2 v2 |

### SG-P07：降低 P 的同时提交 v3

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/2/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 活动 3；Ready 3 | 原灰度 |
| 新目标 | 3/1/v3 | v1 Ready（受保护） | v1 Ready | v2 删除中→v3 Ready | 活动 2～3；Ready 2→3 | 最高旧组 2 先转 v3 |
| 继续 | 3/1/v3 | v1 Ready（受保护） | v1 删除中→v3 Ready | v3 Ready | 活动 2～3；Ready 2→3 | sg-2 Ready 后，sg-1 直接 v1→v3 |
| 终态 | 3/1/v3 | v1 Ready（受保护） | v3 Ready | v3 Ready | 活动 3；Ready 3 | sg-1 不必先到 v2 |

### SG-P08：百分比 P + surge + 扩容重分类

`N=3,P=50%→2,U=0,S=1`，sg-3 v2 surge 尚未 Ready；扩到 N=5 使 P=3，sg-2 重新受保护，sg-3 原地转正式组。没有需要更新的旧版组，不再额外申请 sg-5。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready | v2 NotReady（临时 surge） | 无 | 无 | 活动 4；Ready 3 | U=0，不删 sg-2 v1 |
| 扩容 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 NotReady | v2 NotReady | 无 | 活动 5；Ready 3 | sg-3 v2 保 UID，sg-4 v2 补正式缺位 |
| 终态 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | v2 Ready | 无 | 活动 5；Ready 5 | 没有新的 surge 需求 |

### SG-P09：受保护组的故障也占用 maxUnavailable

`N=3,P=1,U=1,S=0`；受保护的 sg-0 是 v1 NotReady，sg-1 和 sg-2 是 v1 Ready。

| 阶段 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- |
| 故障 | v1 NotReady（受保护） | v1 Ready | v1 Ready | 活动 3；Ready 2 | 已在 `N-U=2` 底线；不能删健康 sg-2 v1 |
| 恢复 | v1 Ready（受保护） | v1 Ready | v1 Ready | 活动 3；Ready 3 | sg-0 v1 依历史模板恢复 |
| 滚动 | v1 Ready（受保护） | v1 Ready | v1 删除中→v2 Ready | 活动 2～3；Ready 2→3 | 再更新序号最高的可更新组 sg-2 |
| 终态 | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 3；Ready 3 | sg-1 最后到 v2 |

### SG-P10：surge 被扩容复用后继续 partition 滚动

`N:3→4,P=1,U=0,S=1`；sg-3 v2 已 Ready，扩容使 sg-3 v2 成正式组；旧组 sg-1 和 sg-2 仍可更新，需要临时组 sg-4 v2 Ready 后才可删除健康组。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/1 | v1 Ready（受保护） | v1 Ready | v1 Ready | v2 Ready（临时 surge） | 无 | 活动 4；Ready 4 | sg-3 v2 是临时容量 |
| 扩容 | 4/1 | v1 Ready（受保护） | v1 Ready | v1 Ready | v2 Ready | v2 NotReady（临时 surge）→v2 Ready（临时 surge） | 活动 5；Ready 4→5 | sg-3 v2 保 UID，sg-4 v2 占新 surge |
| 滚动 | 4/1 | v1 Ready（受保护） | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 4～5；Ready 4→5 | 先 sg-2 |
| 继续 | 4/1 | v1 Ready（受保护） | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 4～5；Ready 4→5 | 再 sg-1 |
| 终态 | 4/1 | v1 Ready（受保护） | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 4；Ready 4 | 最后清理临时 sg-4 v2 |

### SG-P11：旧高位替换补低位，partition 按新槽位的绝对序号决定版本

先前缩容留下 `N=2,{sg-0 v1,sg-3 v1}`；设置 `P=2,U=1,S=0`，提交 v2。sg-3 在保护范围之外且仍是旧版本，本来就需要替换；替代容量优先补最低空位 sg-1。sg-1 的绝对序号小于 P，因此使用历史 v1。不是因为它替代了高位 sg-3 就使用 v2。

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 2/2/v2 | v1 Ready（受保护） | 无 | v1 Ready | 活动 2；Ready 2 | 高位旧版可替换；空洞本身不会触发滚动 |
| 滚动 | 2/2/v2 | v1 Ready（受保护） | 无 | v1 删除中 | 活动 2→1；Ready 1 | 受 U=1 限制 |
| 补位 | 2/2/v2 | v1 Ready（受保护） | v1 NotReady→v1 Ready（受保护） | 无 | 活动 2；Ready 1→2 | 最低空位 1 用历史 v1 |
| 灰度停点 | 2/2/v2 | v1 Ready（受保护） | v1 Ready（受保护） | 无 | 活动 2；Ready 2 | 两个 v1；没有 eligible 实例，不宣称全量 v2 完成 |
| 降低 partition | 2/1/v2 | v1 Ready（受保护） | v1 删除中→v2 Ready | 无 | 活动 1～2；Ready 1～2 | 释放绝对序号 1，继续使用同一目标 revision |

灰度停点 `currentRevision` 保持 v1、`updateRevision` 为 v2、`updatedReplicas=0`。本条于 2026-10-06 经用户明确澄清，替代此前必须在 sg-3 原位更新的描述。SG-S03 不变：如果 sg-3 已经是健康目标 v2，则不能为了补洞把它换成 sg-1 v2。SG 和 Role 的 partition 都按绝对序号解释，不按现存列表位置解释。

## 6. ServingGroupRollingUpdate 中的 Role 副本交错

### SG-R01：partition 灰度中只扩 Role 成员数

`N=3,P=1`，sg-0 使用历史 v1 的 worker 模板 W1，sg-1 和 sg-2 使用 v2 的 worker 模板 W2。仅将 `roles[].replicas` 从 1 扩到 2，不产生新的 SG 版本，也不触发序号补洞。默认 `U=1,S=0`；按每组已应用的成员目标判断完整 Ready。未轮到的组仍以旧成员数提供服务，不会因全局 spec 刚改变就同时失去 Ready 信用。

| 阶段 | 每组 Role 成员数 | sg-0 | sg-1 | sg-2 | 整组 Ready | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 1 | v1/W1×1 Ready（受保护） | v2/W2×1 Ready | v2/W2×1 Ready | 3 | 灰度稳态 |
| 扩 sg-2 | 2 | v1/W1×1 Ready（受保护） | v2/W2×1 Ready | v2/W2×1 Ready+v2/W2×1 NotReady | 2→3 | 只给一组应用新成员目标，等 W2 Ready |
| 扩 sg-1 | 2 | v1/W1×1 Ready（受保护） | v2/W2×1 Ready+v2/W2×1 NotReady | v2/W2×2 Ready | 2→3 | 仍守住 N-U=2 |
| 扩 sg-0 | 2 | v1/W1×1 Ready（受保护）+v1/W1×1 NotReady | v2/W2×2 Ready | v2/W2×2 Ready | 2→3 | 受保护组仍使用历史 W1 模板 |
| 终态 | 2 | v1/W1×2 Ready（受保护） | v2/W2×2 Ready | v2/W2×2 Ready | 3 | 不重滚 SG 模板，不把 W2 套给 sg-0 |

## 7. 验收与反例清单

验证每个场景时，要记录当时的期望组数、partition、maxUnavailable、maxSurge，以及每个 SG 的序号、版本、UID、Ready/删除状态。还要区分正式保留组与临时 surge，并保留已发动作和历史版本记录。“过程中又来了新请求”的测试，应确保前一步确实还在执行。

不能只检查最后“组数等于期望值”。还要检查：非缩容动作没有留下新的稳定空洞；健康且已是目标版本的组没有仅为排齐序号而被删除；没有超出 surge 上限创建新组，也没有通过滚动删除健康组把 Ready 数降到 maxUnavailable 所允许的底线以下。如果故障本已使 Ready 低于底线，过期 NotReady 组的替换只能在 Q 有余额时保持或增加 Ready，同类候选高到低；只有配置 coordination 的 Role 对照禁止越过健康高位旧实例。缩容造成的短暂超额只允许逐步消退。

三个必须保留的反例：

1. SG-S01：`{0,3}` 的 N2→N3 只补 sg-1，留下洞 2；若强行归位就要重建健康 sg-3。
2. SG-S05：低位 sg-1 v2 NotReady、高位 sg-2 v1 Ready，目标 v3；默认先修 1 并保持 Ready=2，不能把 Q=1 用于删除健康 2。相同状态的协调 Role 应等待，不能为了修故障破坏高到低顺序。
3. SG-C14：旧 v2 surge 虽过期却仍在提供服务；若按“最新版本优先”立即删除它，就会越过可用性底线。

SG-S06/S07 是可收敛的正例：Q 仍有余额时，已不可用且版本过期的最高序号组可成批被新版本替换；即使当前故障已经超过底线，替换也不会再减少 Ready。SG-C16 要求同时核对 `C` 和 `V`：一个尚未 Ready 的实际 surge 不会凭空增加清理信用，但也不能只扣 `V` 而忽略它已使 `C` 增 1。

本文是设计推导，**没有**把任何一行表格视作当前实现或 Kind 验证结果。

<a id="behavior-rules"></a>

## 附录：SG 行为规范

原设计日期：2026-09-20；本版含 2026-10-07 明确批准的默认跳过和共享预算修订。本文不以当前 controller 实现作为规则来源；[35 个 SG 过程表](#scenario-tables) 是 SG 规则的实例化。共同预算、身份/历史及选择原则适用于完整 Role 实例；Role 协调额外限制见 API §5 和附录 B。SG 模式下 roles[].replicas 增员（SG-R01）不是 Role 模式模板滚动，不能机械替换其计数轴。

### A.1. 决策与社区参照

ServingGroup 是多 Pod 的完整服务单位，替换健康组可能重建模型、占用大量 GPU 并造成长时间预热。取舍次序为：**不扩大既有可用性缺口、遵守容量上限 > 不重建健康且无需更新的组 > 规模收敛与已有空洞的机会性修复**。SG/独立 Role 的新版本替换先选合法旧 NotReady，再选旧 Ready，同类高到低；有 coordination 的 Role 稳定实例不跳过。候选选择不能突破 Q、健康删除上限 B、partition、依赖或物理容量。

| 主题 | 可借鉴的社区语义 | ModelServing 决策 |
| --- | --- | --- |
| 身份与 partition | StatefulSet 稳定 ordinal、`ordinal<P` 保留旧模板、高到低替换 | 保留绝对 ordinal 阈值；但允许单独缩容因健康/删除代价而留下稀疏集合 |
| 预算 | Deployment 的百分比 U 向下取整、S 向上取整；LWS 以整组为单位提供 U/S | U/S 按**最新 N**计算；整组 Ready 才算可用；不能把扩容缺口当成删除信用 |
| 最新意图 | Deployment rollover 接管最新模板；StatefulSet 坏版本可能需要人工干预 | A→B→C 可以跳过中间 B；合法旧 NotReady B 可优先换成 C，同类高到低；协调 Role 仍按稳定旧序号 |
| 推理代价 | LWS 以组为滚动单位；RBG 区分角色与协调策略 | 不为单纯序号整理重建健康、目标版本的保留组；Role 副本轴与 SG 轴分开 |

Deployment [§rolling.go](https://github.com/kubernetes/kubernetes/blob/master/pkg/controller/deployment/rolling.go#L803-L918) 给出的关键计算是：

`maxScaledDown := allPodsCount - minAvailable - newReplicaSetPodsUnavailable`

同一段代码随后先执行 `cleanupUnhealthyReplicas`，其注释明确说“Clean up unhealthy replicas first”。意思是：旧版已有不健康副本时，可以先清理它们而不增加不可用数；新版尚未可用的副本须从**已创建的总副本数** 带来的额度中扣除，不能把配置的 surge 上限当成已经创建且可用的副本。若扣除后额度为零，不能继续清理旧副本。rollover 到 C 时，坏 B 变为旧 RS，空的 C RS 暂无不可用副本，于是可以先清掉坏 B，再创建 C。Deployment 的旧 RS 按创建时间处理，并没有 SG ordinal 或 partition。本版在保留这些身份约束的前提下，对 SG/独立 Role 采用合法旧 NotReady 优先、同类高到低；SG-S05 因而可以先修低位。有 coordination 的 Role 稳定实例仍不得跳过高位。

原文的 `N=10,U=2,S=3` 例子可直接验算：坏新版有 5 个不可用副本、全部 RS 合计 13 个时，最少可用 8 个，清理额度是 `13-8-5=0`；回滚到旧好版后，新 RS 尚有 1 个不可用，额度变为 `13-8-1=4`；若改为提交全新模板，空的新 RS 暂无不可用副本，额度是 `13-8-0=5`。后两者都可以先清理**已成为旧版**的坏副本，为新目标腾位置；不是把这些故障当作额外的 maxUnavailable。

SG/Role 共用 `M=max(0,N-U)`、`Q=max(0,C-M-V-I)`、`B=max(0,R-M)`。稳定检查点 I=0，Q 退化为 `max(0,C-M-V)`；当 U≤N 且 C=N 时才可简写为 `max(0,U-V)`。C 含实际已创建 surge，V 含最新目标 NotReady surge，不能把配置 S 当实际容量。I 是尚未体现在 C/V 中的在途额度，只记一次；R 含保护区及旧版 Ready，排除已承诺删除容量。完整定义见 API §2.3。默认两阶段选择 `dBad=min(|Ebad|,Q)`、`dHealthy=min(|Ehealthy|,Q-dBad,B)`；候选还要满足 partition、身份、依赖、在途占位和容量限制。Q 不是任意健康删除许可；健康删除必须按实时 R 再核验底线。SG-C16 逐阶段展示：N=3、U=1 时创建一个 NotReady 的 v2 surge，`C` 与 `V` 同时加 1，清理旧版 NotReady 组的 `Q` 不变。

参考：[Kubernetes StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)、[Kubernetes Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)、[LeaderWorkerSet Rollout Strategy](https://lws.sigs.k8s.io/docs/concepts/leaderworkerset/rollout-strategy/)、[LeaderWorkerSet API](https://lws.sigs.k8s.io/docs/reference/leaderworkerset.v1/)、[RoleBasedGroup API](https://github.com/sgl-project/rbg/blob/main/doc/reference/api.md)。这些都是参照；没有单一原生 controller 同时给出本规范的稀疏保留、partition、surge 和嵌套 Role 副本行为。尤其 LWS 文档中的“百分比基于更新开始时副本数”不照搬：这里以最新期望 N 重新结算，是扩缩与滚动交错时明确选择的产品规则。LWS 在 partition 与 surge 同时配置时会保留 burst 副本直至 partition 归零；这里的灰度停点若 eligible 组已更新且可用性达标，就清理不再需要的临时 surge，避免长期占用推理 GPU。

### A.2. 状态、预算与身份

- `N`：最新 `spec.replicas`；`D=[0,N)` 是希望优先覆盖的 ordinal 范围，不是“每轮必须强制排齐”的完成条件。`L` 是尚未完全消失的 SG 集合；Deleting 仍占物理容量，但一旦停止服务就不计 Ready。`R` 是其中整组所有必需成员 Ready 的 SG 数。
- 将 SG 身份区分为**保留组 K**与**临时 surge T**，二者均进入 `L` 和真实 Ready/活动数账本；缩容留下的高 ordinal 仍是 K，不因 ordinal≥N 变成 T。旧 T 只有 ordinal 进入新 `D` 且可承接正式容量时才转为 K，不能因为 `|L|=N` 就自动改身份。`H=D\ord(K)` 是正式范围内空洞；缩容后可有 `|K|=N` 且 `H≠∅`。`S` 限制的是总活动数超过 N 的**额度**，不是“名字大于等于 N 的组”的分类。
- `P` 是绝对 ordinal 阈值：`ordinal<P` 不因目标模板改变而主动更新；`ordinal≥P` 可向最新目标收敛。百分比 P 按最新 N 向上取整。P 不保证最少保留 P 组，也不禁止显式缩容删除 protected 组。
- `U=maxUnavailable`：整数原值或 `floor(百分比×N)`；`S=maxSurge`：整数原值或 `ceil(百分比×N)`。默认 `U=1,S=0`；允许 U 为 0，但有效 SG 滚动配置不得 `U=S=0`。百分比 U 不人为补成 1；例如 N=3、U=25%、S=0 解析成 0/0，必须拒绝配置，不能悄悄取 U=1；零副本或全 partition 也不豁免实际双零。生效整数 U/P 不得超过 N，零副本允许默认/显式整数 U=1；S 可超过 100%，但 N+S 不得超 int32。两者都按最新 N 重算，而不是在第一次更新时冻结。
- 容量上限是 `|L|≤N+S`；如果 N 刚缩小导致现存组暂时超过新上限，控制器只能清理/等待，不能再创建加重超额。滚动删除健康组必须保证删除后 `R≥max(0,N-U)`；滚动删除**已经 NotReady 且版本过期**的组不降低 R，即使故障已使 R 低于底线，也可在清理额度 Q 尚有余额时作为恢复动作继续。这不是“不降 R 就能无限删除”的豁免；它不改变容量上限、partition、本模式的候选顺序或在途控制；同一目标版本仍 NotReady 不反复重建。protected 故障和未 Ready 的新组都计入同一真实可用性账本。
- 显式缩容的目标是最新 N，不把被授权删除的组另记一次“滚动不可用”；但缩容结束后健康组的模板替换仍须遵守最新 `N-U` 下限。surge 是临时物理容量上限，不是必须创建的副本数，也不能被扩容和滚动重复领取。

### A.3. 保序与最小扰动

**只有缩容可以新增稳定的 ordinal 空洞。** 滚动的删除/重建可以短暂产生同一 ordinal 的在途缺位；故障也可造成临时缺位，但控制器不得把这两者当成新的稳定稀疏结果。在动作前后都已稳定的检查点，扩容、滚动应满足 `|H_after|≤|H_before|`，且不得使此前存在的保留 ordinal 永久消失；N 扩大时，新 `D` 揭示的缺位由新增目标槽优先补偿。已有空洞可在多次动作中逐步偿还，不要求一次扩容或一次滚动必定使 `H=∅`。在途创建/删除不拿来做这个稳定检查点的比较。

1. 扩容真正增加了多少组的目标容量，就最多新建多少个保留组；优先选 `D` 内**最低** 的缺失 ordinal。新 N 使旧 surge/高 ordinal 保留组进入 `D` 时，直接复用其 UID，不能先删再建。旧 surge 若仍在 `D` 外，即使总数恰好等于新 N，仍是 T，可以在 U/S 允许时为新增的正式组让出容量；不能误把它当成健康保留组。没有空余容量槽时，不为“排齐序号”删除健康目标版本的**保留组**。
2. 健康且无需更新的保留组不主动重建：“无需更新”包括**现存 protected 组** （即使其实际版本是后来被保护的 B）以及已是目标版本的 eligible 组。即使它位于 `D` 外，也不为了连续编号或强行回到旧版而动它。
3. 替换 `D` 内的旧模板组时，优先在**原 ordinal**重建；不能删掉 sg-0 却把替代容量放到 sg-4，制造新洞。替换 `D` 外、且本来就需要滚动的旧模板保留组时，可将替代容量放在 `D` 中最低空洞，从而利用本就需要的替换修复身份；若最低空洞受 P 保护，按新槽位的绝对 ordinal 使用历史模板补建。即使回补后所有组都为旧版，也可形成合法灰度停点（SG-P11）；不能宣称全量目标版完成。
4. 为零停机而创建的临时 surge，若能够与一名需要删除的范围外旧版保留组配对，可直接建在范围内最低空洞并转为保留组，按该槽位的 partition 选历史/目标模板；否则使用最低空闲范围外 ordinal，滚动结束后清理它。不能先在空洞建一个无法保留的新组，最后为了归位删除健康目标版组。
5. 缩容先确定 **K 中**需要移除的数量；在 K 内先选 NotReady，后选较低 deletion cost，最后选较高 ordinal，P 不改变此选择次序。T 另按新 N/S 与 Ready 底线决定保留或清理，不把“sg 编号最高”误当成先删有用 surge 的理由。缩容可因此删除 protected 旧版组而保留较高的新版组。已发删除不能撤销；新 spec 只控制尚未发出的动作。

这里的“保序”不是强制每次得到 `[0,N)`，而是**不制造稳定新洞、存在合法容量机会时优先补最低旧洞、保留已经有用的身份**。例如 N:2→3 时从 `{0,3}` 建 sg-1 得到 `{0,1,3}`，sg-2 仍缺失；sg-3 若健康且无需更新，不得仅为了把 3 换成 2 而重建。下次扩容到 N=4 可以再建 sg-2，原 sg-3 原地成为范围内组。

### A.4. 操作偏序与版本选择

每次以**最新 spec**重新推导 N、P、U、S 和目标 revision，不把早先推导的未来动作当成承诺；已接受的 API 删除/创建按事实继续完成。为已发删除补建同名组是完成在途动作，不算在新目标下跳过高位组另选低位滚动；完成它以后，新发起的滚动按本模式规则选择：SG/独立 Role 旧 NotReady 优先，同类高到低；协调 Role 稳定候选不可跳过。

1. 先识别 Deleting、在建和已有 surge，按新 N/P 对所有存量组重分类。删除中的组在完全消失前不得复用同名 ordinal 或释放容量槽。
2. 若 N 缩小，先执行缩容选择并等待本轮计划删除完成；若 N 增大，先用新增目标槽补低序号空洞。已开始创建的旧模板组不原地改模板；尚未创建的组直接使用最新适用版本。扩容可批量申请不同 ordinal。先完成正式扩容槽的**安置**，再考虑健康旧组的破坏性滚动；不要求所有新组先 Ready，但每次删除**健康旧组**前必须用实时 R 核验 `R-1≥max(0,N-U)`。扩容缺口消耗同一可用性预算，不能借“新 N 的 U 变大”把缺口当成额外删除信用。若正式槽尚无法安置，也不能靠滚动先腾它的位置。
3. SG/独立 Role 每次先选合法的旧 NotReady，同类按 ordinal 高到低；没有可选旧 NotReady 后再选择健康旧实例。可跳过健康高位修复低位故障，但旧坏实例数量不超过剩余 Q，不能突破 partition/身份约束。每组须等删除彻底完成后才能重用名称和物理容量；已发动作占位，替代组未 Ready 时不能重复领取同一额度。新目标也坏则等待目标修正或明确恢复路径，不反复重建同版。协调 Role 对照改为最高旧序号前缀，还须满足 remainingStart 和依赖保留；受阻就等待，不跳到低位。
4. 健康候选按高到低，必要时先申请合法 surge；每次删除后必须有 `R≥max(0,N-U)`，所选健康数量不超过 B；不满足就等待健康删除余量。U>1 可选择健康候选批次，并等待整批 Ready 后进入下一批（既有 SG 批次条款；与普通执行器逐 Ready 信用的口径差异见附录 B，不在本次顺序修订中更改）。protected 缺位/故障按历史修复，不借此突破 P。
5. 仍承担 `N-U` 可用性底线的健康临时 surge 必须保留，等替代容量就绪再清理；新 N 吸收旧 surge 时保留 UID。目标改变后，废弃旧 NotReady surge 可在 Q 内优先合法回收，不能把它的清理额度用于健康稳定实例。此路径须核验真实临时身份、依赖和在途动作；协调 Role 中不计作稳定序号跳过，也不返还稳定启动额度。

例如 `N=5,U=2,S=0`，同一批次选最高的 sg-4/3 更新，但错误版本 B 使两组都 NotReady，此时 `C=5,V=2,Q=5-3-2=0`，健康的 sg-2 不能再删除。目标修正为 C 后，坏 B 转为旧版，`V=0,Q=2`，可按 4→3 同批清理并创建 C；两组 C Ready 前不继续删除健康的 sg-2。它们 Ready 后，可同批滚动最高剩余旧组 2/1（R 从 5 降到 3），待这批 C Ready 再滚 sg-0。若起初五组 A 都 NotReady，R=0 已低于预算底线，提交 B 后仍有 `C=5,V=0,Q=2`，可先按 4→3 替换；待 B Ready，再按 2→1、0 继续。不能因“故障已超 U”拒绝所有恢复动作，也不能一次重建五组。这是**不扩大既有可用性缺口**的规则，不是放宽 U，也不是低序号优先。稀疏扩容后的 sg-2=v1 Ready、sg-1=v2 NotReady、目标 v3，若 Q=1/B=0，默认可先原位修 sg-1，Ready 不下降；它 Ready 后再滚健康 sg-2。相同协调 Role 状态仍受最高候选顺序限制而等待。Q 耗尽、新目标仍坏、protected 故障、依赖或调度容量不足也可能阻塞，不能保证自动解除所有故障。

新建/重建版本由**动作当时**的最新 P 决定：`ordinal<P` 用该次灰度固定且仍可追溯的受保护历史模板，`ordinal≥P` 用最新目标模板。现存 B 因 P 增大落入保护区时保留 B；之后它若被删除，重建可回到历史 A，这并不构成主动回滚。受保护基线取进入这轮灰度前最近一次全量完成的模板；A→B→C 的中途提交不自动把基线推进到 B。解除保护并完成全量目标后，才可把新目标作为以后灰度的基线。历史模板不可确认时安全等待，不得猜测为最新模板。Role 副本数变动只改变每组内部数量，受保护组继续使用历史 Pod/worker 模板；它本身不产生 SG 模板 revision。若增加必需 Role 成员会使整组暂时 NotReady，应按实时 `N-U` 底线分批给各组应用新成员数，不能一次令所有旧组失去 Ready 信用。未轮到的组仍以自己**已应用** 的旧成员目标判断 Ready，而非在全局 spec 改变瞬间全部计为缺员；已轮到的组必须等新增成员 Ready 才恢复整组 Ready。若其他故障已耗尽 U，则等待修复或使用合法 surge，不放宽预算。

### A.5. 完成、阻塞和可实现性

- **规模完成**：不再有计划中的缩容删除，`|K|=N`，所有新建保留组按其版本达到完整 Ready。缩容完成可带身份债 `H≠∅`；后续扩容/滚动也允许保留无法无扰动偿还的旧债。
- **滚动完成**：所有现存、eligible 的保留组达到最新目标并 Ready，已发替换完成，临时 surge 已清理，`|L|=N`；不要求 `H=∅`。即使 `P=N`，稀疏缩容留下的高 ordinal 保留组也可能满足 `ordinal≥P`；只在**确实没有 eligible 保留组**时才是 partition 暂停/灰度停点，不主动重建健康组。
- **身份债**：单独记录 `H` 与范围外保留组；不能把它误报成版本未完成，也不能在下次普通 reconcile 中无缘无故把缩容保留下来的健康组删除。故障造成的缺位仍应走修复逻辑，不与“缩容允许的稳定债”混淆。
- **阻塞**：坏目标、历史 revision 丢失、Q 耗尽、预算/容量不足或协调限制挡住候选时停止相应新动作并报告原因；SG 不再仅因健康高位存在就阻塞合法低位旧坏修复。不隐式放宽 U/S，不把已发删除回滚，不为表面连续而增加额外重建。

实现上需要持久区分保留组、临时 surge 和缩容产生的身份债（仅靠 count/ordinal 不足以在重启后判断），保存/恢复 live 历史版本，并以真实 Ready、Deleting、最新 N、实际活动组数 C 和目标版未就绪数 V 结算预算；在途删除/创建须占位，不能按短暂对象数反复领取 Q。这是有限的控制器状态与选择逻辑，不依赖不可实现的 Pod 重命名。本文不宣称现有 production 已符合，也不把静态推导当成 Kind 通过记录。所有复合场景的可观察过程和取舍见 [场景过程表](#scenario-tables)。

<a id="budget-lookup"></a>

## 附录 B：SG / Role 共用预算与行为速查

本表使用 API §2.3 的 `M=max(0,N-U)`、`Q=max(0,C-M-V-I)`、`B=max(0,R-M)`。SG 的一个单位是完整 SG；Role 的一个单位是同一 SG 内同一 Role 的完整实例。“默认”列适用于 SG 和无 coordination 的 Role；“协调”列仅适用于配置 coordination 的 Role 模式。除特别说明外，候选历史已知、无冲突在途动作，协调尚有启动额度且依赖满足。版本 v1/v2/v3 与预算 B 无关，H 在附录 A 仍表示空洞集合。

### B.1 数值与选择对照

`N/U/S/P` 是最新有效配置，`C/R/V/I` 是动作前的实际账本。表中 RU-Bxx 是查表 ID，不是新增 executable case ID；不能将本表行数计为测试通过数量。

| 查表 ID / 场景 | N/U/S/P | C/R/V/I | Q/B | 默认：SG / 独立 Role | Role + coordination |
| --- | --- | --- | --- | --- | --- |
| RU-B01 全部旧版健康，提交 v2 | 3/1/0/0 | 3/3/0/0 | 1/1 | 先替换最高旧 2，Ready 不低于 2 | 相同，另受 remainingStart/依赖限制 |
| RU-B02 低位旧 1 坏，高位旧 2 健康，目标 v3 | 3/1/0/0 | 3/2/0/0 | 1/0 | 先原位修 1，Ready 保持 2；Ready 后再滚 2 | 最高旧 2 健康但 B=0，等待，不能跳过 |
| RU-B03 全旧 v1 坏，提交 v2 | 3/1/0/0 | 3/0/0/0 | 1/0 | 先修最高旧 2，只修一个 | 最高旧 2 可在进度/依赖允许时修复，不因 R<M 一律拒绝 |
| RU-B04 RU-B03 已建一个 v2，仍坏 | 3/1/0/0 | 3/0/1/0 | 0/0 | 等待，不继续清旧，也不重建当前 v2 | 相同 |
| RU-B05 两个旧 v2 坏，提交 v3 | 5/2/0/0 | 5/3/0/0 | 2/0 | 最多修两个旧坏，同类高到低，不删健康旧版 | 只取合法有序前缀；不能绕过前方健康阻塞者 |
| RU-B06 025：只有 protected 0 坏 | 3/1/0/1 | 3/2/0/0 | 1/0 | 无合法旧坏候选，健康删除为零，Ready 保持 2 | 相同，保护故障仍计入 R 缺口 |
| RU-B07 旧版健康，最新目标 surge 未 Ready | 3/0/1/0 | 4/3/1/0 | 0/0 | 等 surge Ready；C/V 同增，不产生健康删除信用 | 相同 |
| RU-B08 049：健康 v1 + 旧坏 v2 surge，目标 v3 | 1/0/1/0 | 2/1/0/0 | 1/0 | 只回收合法旧坏 surge，保留健康 v1 | 同样可独立回收旧坏 surge，不算稳定序号跳过 |
| RU-B09 RU-B08 的 v3 surge 已 Ready | 1/0/1/0 | 2/2/0/0 | 1/1 | 可以开始替换稳定旧 v1 | 有序且 remainingStart/依赖允许时才开始 |
| RU-B10 健康旧 surge 承担底线，新稳定 v3 未 Ready | 3/0/1/0 | 4/3/1/0 | 0/0 | 保留健康旧 surge，不因其过期而清理 | 相同，并检查必要旧依赖 |
| RU-B11 旧删除已发仍占物理槽，尚未反映到 V | 3/1/0/0 | 3/2/0/1 | 0/0 | 在途占 I，不重复领取 U | 相同，已启动还占协调额度 |
| RU-B12 T=4，skew=25%，最慢 Ready=0，已启动 1 个 | 4/2/0/0 | 4/3/1/0 | 1/1 | 无协调时预算可再滚一个旧健康实例 | allowedStarted=1、remainingStart=0，稳定替换等待 |
| RU-B13 目标依赖未就绪，或最后必要旧依赖须保留 | 2/1/0/0 | 2/2/0/0 | 1/1 | 无协调时按默认候选规则 | 数字有额度也不得越过相应依赖约束 |
| RU-B14 全旧版坏，U=0，surge 因资源不足 Pending | 3/0/1/0 | 4/0/1/0 | 0/0 | 仍阻塞；允许跳过不等于保证所有故障自愈 | 相同，不能绕过预算或依赖 |
| RU-B15 旧 2 坏，同时最新 surge 未 Ready | 3/1/1/0 | 4/2/1/0 | 1/0 | 可修一个旧坏 2；替代目标未 Ready 后 V=2/Q=0 | 最高候选及协调约束允许时相同 |
| RU-B16 零期望、零实际更新量 | 0/1/0/0 | 0/0/0/0 | 0/0 | 不启动模板替换，不产生永久 UpdateInProgress | 不把零更新量作为进度分母或永久阻塞者 |

单独缩容不是上述模板替换公式的任意删除许可：先遵守最新 N 的规模操作规则。全部账本按最新 N 重算百分比，U floor、S/P ceil；旧版本判断按最新目标重算。故障已造成 R<M 时，本规则只允许不扩大 Ready 缺口，不承诺立即恢复到 M。

### B.2 全旧版本不可用：U=1 逐个修复

N=3/U=1/S=0/P=0，v1 全坏，目标 v2 正常；相同过程适用于 SG 和独立 Role，协调 Role 还须满足合法的高到低启动、依赖和百分比额度。

| 检查点 | 实例 0 | 实例 1 | 实例 2 | R/V/I | Q/B | 下一步 |
| --- | --- | --- | --- | --- | --- | --- |
| 提交 v2 | v1 NotReady | v1 NotReady | v1 NotReady | 0/0/0 | 1/0 | 修 2 |
| 2 已承诺删除但仍存在 | v1 NotReady | v1 NotReady | v1 Deleting | 0/0/1 | 0/0 | 等删除和补建，不重复领取 |
| 2 已补 v2 | v1 NotReady | v1 NotReady | v2 NotReady | 0/1/0 | 0/0 | 等 v2 Ready |
| 2 Ready | v1 NotReady | v1 NotReady | v2 Ready | 1/0/0 | 1/0 | 修 1 |
| 1 已补 v2 | v1 NotReady | v2 NotReady | v2 Ready | 1/1/0 | 0/0 | 等待 |
| 1 Ready | v1 NotReady | v2 Ready | v2 Ready | 2/0/0 | 1/0 | 修 0 |
| 0 已补 v2 | v2 NotReady | v2 Ready | v2 Ready | 2/1/0 | 0/0 | 等待 |
| 全部 Ready | v2 Ready | v2 Ready | v2 Ready | 3/0/0 | 1/1 | 无旧候选，不再删除 |

该表 C=3；实际旧对象消失到替代对象出现之间 C=2，对应 I 不再重复扣除，Q 仍为 0。Q>0 也必须存在合法旧候选，不能为花完额度而删除目标版本。

### B.3 连续滚动的旧 surge：049 对照

每个 Role N=1/U=0/S=1，稳定实例 0:v1 Ready，临时实例 1:v2 NotReady，目标改为 v3。两个 Role 的协调用例应始终保留至少 2 个 Ready Pod （此例 workerReplicas=0）；各 Role N=3 的同构例应保留至少 6 个。

| 阶段 | 稳定 0 | 临时 surge 1 | 每 Role 的 C/R/V/I | Q/B | 预期 |
| --- | --- | --- | --- | --- | --- |
| 提交 v3 | v1 Ready | v2 NotReady | 2/1/0/0 | 1/0 | 只回收旧坏 surge，不删健康 0 |
| 旧 surge 删除已发 | v1 Ready | v2 Deleting | 2/1/0/1 | 0/0 | 占 I，不能另删 0 |
| 创建 v3 surge | v1 Ready | v3 NotReady | 2/1/1/0 | 0/0 | 等新容量 Ready |
| surge Ready | v1 Ready | v3 Ready | 2/2/0/0 | 1/1 | 协调/依赖允许才开始稳定替换 |
| 稳定槽补建 | v3 NotReady | v3 Ready | 2/1/1/0 | 0/0 | 保留仍在服务的 surge |
| 稳定槽 Ready | v3 Ready | v3 Ready | 2/2/0/0 | 1/1 | 可清理不再需要的健康临时容量 |
| 终态 | v3 Ready | 无 | 1/1/0/0 | 0/0 | 每 Role 保留一个完整 Ready 实例 |

旧 surge 消失后的短暂检查点 C=1/I=0/Q=0；随后新建仍受 N+S 与依赖约束。仅 ordinal≥N 不足以证明它是 surge；范围外正式保留实例和已被扩容吸收的旧 surge 必须按真实身份处理。若旧 surge 健康且承担 R=M，按 RU-B10 保留，不能机械套用坏 surge 清理。

### B.4 覆盖与仍需区分的执行口径

- 35 个既有设计 ID 仍为 SG-S01～S07、SG-C01～C16、SG-P01～P11、SG-R01（合计 **35**）；本附录新增 **16 个查表场景**，不是新增 executable case 或 Kind 通过记录。
- 明确改变的旧预期为 `servinggroup-compound-v2/RUN-622`（SG-S05）和 `RUN-632`（SG-C08 的旧低位 NotReady 分支）。case、generator 和通用顺序 verdict 已按 2.2 迁移；RUN-624 只放行新版本以验证全旧版持续故障。实际运行与未通过项见 [051 验证记录](../../../issues/features/051-production-baseline-realignment-DONE/runner-production-20261007/README.md)，旧结果不能直接算作新候选的 PASS/FAIL。
- Role 需要同构故障/在途场景及有协调对照；现有 35 个 SG case 不能充当 Role 覆盖。025/049 的既有复现是缺陷证据，不是新预期通过证据。
- SG A.4 仍有整批 Ready 后继续的条款，普通 `CORE_EXPECTATIONS.md` 第 6 条按逐 Ready 信用推进。此版只修预算与候选顺序，不以 Q>0 自动裁定这项独立批次差异；部分批次 Ready 的时点断言须单独统一。
- admission、恢复策略、worker 完整性、真实 Ready 更新、在途去重和重启身份恢复仍须各自验证；允许跳过不授权放宽它们。
