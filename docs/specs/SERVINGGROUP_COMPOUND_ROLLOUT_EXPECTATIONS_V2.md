# ServingGroupRollingUpdate 组合场景预期行为表 v2.0

本文是**设计预期**，不是 production 已实现行为或 Kind 验证记录；只讨论
`ServingGroupRollingUpdate`。下面把全部 35 个组合场景直接写成过程表，
每一步都能看到 sg-0、sg-1 等组所处的版本和状态。

这里用 **v1 → v2 → v3** 表示先后提交的配置版本。“v2 Ready”表示整组
已经就绪；“v2 NotReady”表示还未就绪；“v1 删除中”表示旧组正在退出；
“临时 surge”表示滚动期间多建的组；“受保护”表示当前 partition 不允许
主动更新该序号。“无”表示该序号当前没有组。每张表的“组数”同时列出
活动组与 Ready 组；删除中的组在彻底消失前仍占活动名额。
表中 `N` 是期望组数，`U` 是 maxUnavailable，`S` 是 maxSurge，
`P` 是 partition；例如“活动 4→2；Ready 2～4”表示缩容删除过程中
活动组逐步从 4 变为 2，Ready 组在 2 到 4 之间变化。

阅读所有场景时，按下面几条原则判断：

- 所有**新发起的滚动替换**都从最高的可更新旧组向低序号进行；
  扩容需要建新组时，先补最低的空序号。若选中的组已 NotReady，
  换成新版本不会使 Ready 组数再减少；即使故障已超预算，
  也仅在下述清理额度仍有余额时允许继续；
  不能为了修复低位故障而跳过更高的旧组。
- 同时扩缩容和更新时，先安排新旧副本数，再考虑删除健康旧组。
  每一次删除都要重新看真实 Ready 组数。新扩出来的组不必全部 Ready；
  正式扩容组的未 Ready 缺口不能当成额外滚动信用，临时 surge
  即使未 Ready，也要在组数和新版未就绪数两边**同时入账**。
- `maxUnavailable` 和 `maxSurge` 的百分比按**最新期望组数**重算：
  前者向下取整，后者向上取整。活动组不得超过“期望组数 + maxSurge”；
  删除健康组后，Ready 组不得低于“期望组数 − maxUnavailable”。
  如果故障已使 Ready 低于这条底线，仍可清理**过期且已 NotReady**的旧组，
  但只选最高序号端连续的一批，数量受
  [Deployment `rolling.go` 的 `maxScaledDown` 计算](https://github.com/kubernetes/kubernetes/blob/master/pkg/controller/deployment/rolling.go#L803-L918)
  启发的清理额度 `Q=max(0,C-(N-U)-V)` 限制：`C` 是稳定检查点
  的实际活动 SG 数（包含已经创建的 surge），`V` 是当前目标版本中
  NotReady 的 SG 数（也包含 surge）。即 `Q=max(0,U+(C-N)-V)`；
  只有 `C=N` 时才能简写为 `max(0,U-V)`，不能预支尚未创建的
  `maxSurge`。
  同一额度不能在替代组尚未 Ready 时重复领取。同一目标版本
  已 NotReady 时不反复重建。
  缩容刚发生时，原有组可能短暂超过新上限，此时只能清理，不能再创建。
- 单独缩容可以留下稀疏序号。后续扩容和滚动优先利用真正需要创建或替换
  的机会补洞，**不会为了排齐序号而重建健康且已是目标版本的组**。
  一次动作不要求把此前的空洞全部补完。
- `partition` 按固定序号判断，不是保护“当前排在前面的若干组”。
  被保护序号若要补建，使用本轮灰度前的历史版本；已经存在的新版组
  后来进入保护区时，不会为了回旧版而主动重建。
- 旧的临时 surge 若因扩容进入正式序号范围，可以保留原组并转为正式组；
  没进入范围的临时组不会仅因总数凑齐就自动转正。
  组内 Role 副本数增加若影响整组 Ready，也要分批进行以守住可用性。

下面的阶段是**一条允许的执行过程**，异步事件不必严格逐行发生。
预算、历史版本与完成条件的完整约束见
[行为规范](./SERVINGGROUP_COMPOUND_ROLLOUT_EXPECTATIONS.md)。

## 1. 缩容稀疏、机会性补洞与顺序取舍

### SG-S01：单独缩容留洞；一次扩容不强制排齐

`N=4`，sg-0～sg-3 均为 v1 Ready。自定义删除优先级（deletion cost）
使 sg-1/sg-2 成为缩容对象，
保留健康 sg-3；随后分别扩到 3 和 4。

| 阶段 | N | sg-0 | sg-1 | sg-2 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 4 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 4；Ready 4 | 连续 |
| 缩容 | 2 | v1 Ready | v1 删除中 | v1 删除中 | v1 Ready | 活动 4→2；Ready 2～4 | 缩容可选择低 cost；不能当成滚动 U 消耗 |
| 缩容终态 | 2 | v1 Ready | 无 | 无 | v1 Ready | 活动 2；Ready 2 | `{0,3}` 是允许的稀疏保留集 |
| 扩容 | 3 | v1 Ready | v1 NotReady | 无 | v1 Ready | 活动 3；Ready 2 | 只有一个新增容量槽，先补最低洞 1 |
| 扩容终态 | 3 | v1 Ready | v1 Ready | 无 | v1 Ready | 活动 3；Ready 3 | `{0,1,3}`；不为补 2 重建健康 sg-3 |
| 再扩容 | 4 | v1 Ready | v1 Ready | v1 NotReady→v1 Ready | v1 Ready | 活动 4；Ready 3→4 | 补 2；sg-3 直接进入新范围且 UID 不变 |

这就是“一次动作不保证恢复连续”的正例。若只有第一次扩容，不再发生新动作，
旧空洞 `{2}` 可以稳定存在；控制器日常处理不主动消灭它。

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

即使配置 `S=1`，也不能用“先造 sg-1 v2 再删除 sg-3 v2”绕过少重建原则；
sg-3 v2 是保留组，不是待清理的临时 surge。需要准确标记两类身份。

### SG-S04：扩容后旧 surge 序号仍不在正式范围内，不能按总数误复用

缩容曾留下保留组 `{sg-0 v1,sg-1 v1,sg-3 v2}`，`N=3,U=0,S=1`；sg-3 v2 已是目标版本，
为更新 sg-1 v1 和 sg-0 v1 创建了 sg-4 v2 临时 surge。此时 N:3→4，
目标仍是 v2。新的正式序号范围是 0～3：它包含保留组 sg-3，
却**不包含** sg-4；虽然活动数恰好为新 N=4，
正式保留组仍只有三个，sg-2 必须补建。

| 阶段 | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3 | v1 Ready | v1 Ready | 无 | v2 Ready | v2 Ready（临时 surge） | 活动 4；Ready 4 | sg-3 v2 是健康保留组，sg-4 v2 是临时组 |
| 扩容 | 4 | v1 Ready | v1 Ready | v2 NotReady | v2 Ready | v2 Ready（临时 surge） | 活动 5；Ready 4 | 新上限 5；在最低空洞 2 建正式 sg-2 v2 |
| 扩容 Ready | 4 | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5；Ready 5 | sg-4 v2 未因“总数=N”被错误转正 |
| 滚动 | 4 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 4～5；Ready 4→5 | 借 sg-4 v2 的服务容量滚 sg-1 |
| 终态 | 4 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 4；Ready 4 | 再滚 sg-0，最后清理 sg-4 v2；sg-3 v2 UID 不变 |

与 SG-C03/P10 不同，本例的旧 surge 序号**仍在新的正式范围外**；
“复用”取决于身份是否进入新范围，不能只看当前组数。

### SG-S05：稀疏扩容形成低位故障，严格顺序下阻塞

此状态**不是**单纯从高到低滚动产生的。先在 v1 稳态下从 3 组缩到
2 组：sg-1 因删除代价较低而被移除，保留 sg-0 和 sg-2。
随后扩回 3 组并提交 v2；扩容优先填补最低空洞 sg-1，
将它直接创建为 v2，但错误配置使它始终 NotReady。
预算为 `U=1,S=0,P=0`，此时目标再改为 v3。

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| v1 稳态 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 活动 3；Ready 3 | 还没有滚动 |
| 缩容 | 2/v1 | v1 Ready | v1 删除中 | v1 Ready | 活动 3→2；Ready 2～3 | 按删除代价选择 sg-1 |
| 留下空洞 | 2/v1 | v1 Ready | 无 | v1 Ready | 活动 2；Ready 2 | sg-2 是健康的正式保留组 |
| 扩容并提交 v2 | 3/v2 | v1 Ready | v2 NotReady | v1 Ready | 活动 3；Ready 2 | 新容量槽补 sg-1；并非跳过 sg-2 滚动 |
| 提交 v3 | 3/v3 | v1 Ready | v2 NotReady | v1 Ready | 活动 3；Ready 2 | 最高的待更新组是健康 sg-2 |
| 阻塞 | 3/v3 | v1 Ready | v2 NotReady | v1 Ready | 活动 3；Ready 2 | 删 sg-2 会使 Ready 变 1，低于底线 2；不能跳过它先换 sg-1 |

sg-1 已 NotReady，按 Deployment 式计算也有 `Q=3-2-0=1`；
单独替换它确实不会再减少 Ready。但它的序号低于仍需更新的
sg-2。若坚持**所有滚动都严格从高到低**，单靠
“不扩大缺口”无法使本例收敛。需要 sg-1 自行恢复、合法 surge
先提供 Ready 余量、调整预算，或外部干预。这是严格顺序换来的
明确阻塞边界，而不是自动修复成功的示例。

### SG-S06：两组滚到错误 v2 后，用 v3 修复

期望 5 组，maxUnavailable=2、maxSurge=0。v1 全部 Ready；
同一批次选最高的 sg-4、sg-3 更新为 v2，但错误配置使两组
都无法 Ready。此时已恰好达到“至少 3 组 Ready”的底线。
沿用 Kubernetes Deployment `rolling.go` 的额度演算：v2 为当前目标时，
`Q=5-3-2=0`，不得继续清理；提交 v3 后，坏 v2 变成旧版，
空的 v3 暂无不可用组，`Q=5-3-0=2`，可以同批清理最高的两组。

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

如果用户没有提交新版本，sg-4/sg-3 已经是目标 v2，
“仍 NotReady”本身不构成再次滚动的理由。

### SG-S07：初始版本五组全 NotReady，允许不降级修复

期望 5 组，maxUnavailable=2、maxSurge=0。已创建的 v1
五组全都 NotReady，Ready 数为 0，故障在滚动前就已超出预算。
用户提交修正版本 v2；虽然无法立即达到“至少 3 组 Ready”，
旧版五组都已不可用，初始 `Q=5-3-0=2`：可按 4→3、
2→1、0 分批替换，前批 v2 Ready 后才重新获得清理额度。

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

若首批 v2 也持续 NotReady，`V=2,Q=0`，停止后续清理并报告阻塞；
不会因为五个 v1 都坏而一次重建五组。后续操作等待版本再次修正或
明确的故障恢复策略。

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

阶段 2 的 Ready 低于新 `N-U=4`，是扩容缺口/既有在途的事实，
**不是**可以再删一组 v1 的许可。

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

`N:3→5,U=0,S=1,P=0`，原 sg-0～sg-2 均为 v1 Ready，临时组
sg-3 为 v2 Ready。
新 N 使 sg-3 v2 转为保留组；sg-4 v2 是新保留组，sg-5 v2 才占新 surge 槽。

| 阶段 | N | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3 | v1 Ready | v1 Ready | v1 Ready | v2 Ready（临时 surge） | 无 | 无 | 活动 4；Ready 4 | 上限 4 |
| 1 | 5 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 NotReady | v2 NotReady（临时 surge） | 活动 6；Ready 4 | sg-3 v2 保 UID；新上限 6 |
| 2 | 5 | v1 Ready | v1 Ready | v1 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 6；Ready 6 | 取得健康删除信用 |
| 3 | 5 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 先滚最高旧组 sg-2 |
| 4 | 5 | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 再滚 sg-1 |
| 5 | 5 | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 5～6；Ready 5→6 | 最后滚 sg-0 |
| 终态 | 5 | v2 Ready | v2 Ready | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 5；Ready 5 | 临时 sg-5 v2 清理；不产生新洞 |

Kubernetes Deployment 的 surge 仅是聚合容量，不承诺复用某个 Pod UID；
“sg-3 v2 原地转为保留组”是 ModelServing 利用稳定序号自行定义的规则。

### SG-C12：滚动中缩容，旧 surge 序号超出新范围

`N:4→3,U=0,S=1`；sg-0～sg-3 均为 v1 Ready，sg-4 是 v2 Ready 的临时 surge。
新物理上限从 5 变 4，但 sg-4 v2 的**序号**超出 `[0,N+S)=[0,4)`
不等于必须先删它。缩容本来就要移除最高旧版保留组 sg-3 v1；等 sg-3 v1 完全消失，
sg-4 v2 仍可占唯一临时槽，为更新 sg-2 v1/sg-1 v1/sg-0 v1 提供 Ready 信用。这样无需重建 sg-4 v2，
也不把要缩掉的 sg-3 v1 先滚成 sg-3 v2。

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

maxUnavailable 设为 25%：期望 5 组时向下取整，可容忍 1 组不可用，
至少要有 4 组 Ready；扩到 9 组后可容忍 2 组不可用，
至少要有 7 组 Ready。此例没有 surge；新增的不可用额度
不是扩容之外的“免费删除额度”。

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

“坏 v2 分支”是另一次运行：初始 sg-4 v2 也未 Ready，不是从上面的 sg-4 v2 Ready 状态倒退。
即使 `U=2`，该分支的真实 Ready=4 已低于 7，唯一合法进展是修正目标或恢复组，
不能继续破坏健康 v1。

### SG-C16：已创建的 surge 与新版 NotReady 必须同时入账

`N=3,U=1,S=1,P=0`，最少需要 2 个 Ready 组。旧版 sg-2 原本就
NotReady；目标改为 v2，并在 sg-3 建立临时 surge。表中 `C/R/V/Q`
依次是活动组数、Ready 组数、目标 v2 的 NotReady 组数、
`Q=max(0,C-(N-U)-V)`。只在删除彻底完成后才重建同名组；
表中的“无”是更新在途的短暂缺位。

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

创建 sg-3 前后，`Q` 都是 1：`C` 增 1，`V` 也增 1。
直接写 `U-V=1-1=0` 会错误阻塞 sg-2 的无损清理。
反之，重建 sg-2 后 `V=2,Q=0`，即使剩余旧组也有故障，
仍要等至少一个新版组 Ready、提交新目标，或通过其他合法方式取得额度。
这里借鉴的是社区的**额度结算**，不是 Deployment 按旧 RS 健康状况
自由选择删除对象：sg-2→sg-1→sg-0 的顺序仍严格成立。

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

如果缩容按 NotReady/cost 选择 sg-1 而留下高位健康组，终态可以稀疏；
随后滚动只以必要的旧版替换机会补洞，不为排齐删除目标版组（见 SG-S01～S03）。

### SG-C13：扩容中开始滚动，按新 N 申请 surge

`N:3→5,U=0,S=1`，sg-3 v1 已由先前扩容创建并 Ready，sg-4 v1 尚未创建；
此时 v2 到来。`sg-4` 是正式扩容槽，`sg-5` 才是新 N 的 surge。

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

`N=3,U=1,S=0,P=0`；sg-2 已更新为 v2 Ready，sg-1 的旧版 v1
删除已发出，但新版 sg-1 的创建**尚未**发出。v3 到来后不可撤销
已发删除，重建直接使用 v3。

| 阶段 | 目标 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | v1 | v1 Ready | v1 Ready | v1 Ready | 活动 3；Ready 3 | 基线 |
| v2 进行中 | v2 | v1 Ready | v1 Ready | v1 删除中→v2 Ready | 活动 2～3；Ready 2→3 | 先滚最高位 2 |
| v2 下一步 | v2 | v1 Ready | v1 删除中 | v2 Ready | 活动 3→2；Ready 2 | sg-1 删除已接受 |
| v3 接管 | v3 | v1 Ready | v3 NotReady→v3 Ready | v2 Ready | 活动 3；Ready 2→3 | 不能复活旧 sg-1 v1；直接创建 sg-1 v3 |
| 继续 | v3 | v1 Ready | v3 Ready | v2 删除中→v3 Ready | 活动 2～3；Ready 2→3 | sg-1 v3 Ready 后回到最高健康旧组 2 |
| 最后 | v3 | v1 删除中→v3 Ready | v3 Ready | v3 Ready | 活动 2～3；Ready 2→3 | 最后 sg-0 v1→v3 |
| 终态 | v3 | v3 Ready | v3 Ready | v3 Ready | 活动 3；Ready 3 | 无需全量 v2 |

若 sg-1 v2 的创建请求已经发出，不能偷偷把在建 sg-1 v2 改成 v3。
它后来即使 NotReady 且版本过期，也不能自动跳过更高的 sg-2 v2 Ready
先修 sg-1：此时 `N=3,U=1,C=3,V=0,Q=1`，但真实 Ready 只有 2，
删除 sg-2 会跌破底线；严格高到低规则使该分支阻塞。
若 sg-1 v2 Ready，则先更新更高的 sg-2 v2，再回到 sg-1。
已发事件决定允许的中间态，最新意图决定未来动作。

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

缩容若按健康/cost 留下高位稀疏集合，则仍按 SG-S02/S03：
只借需要更新的组偿还空洞，不因 v3 到来就移动健康 v3 组。

### SG-C11：扩容中 v2 无法 Ready，用户把目标回滚 v1

原 `N=3`，扩到 5 时 sg-3 v1 已 Ready、sg-4 尚未创建；这时提交错误配置 v2，
最后的扩容槽 sg-4 直接创建 v2 却永不 Ready。`N=5,U=1,S=0`，
目标回到 v1 后，sg-4 v2 已过期且不贡献 R，自动以 v1 修复。

| 阶段 | 期望组数 / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/v1 | v1 Ready | v1 Ready | v1 Ready | 无 | 无 | 活动 3；Ready 3 | 原基线 |
| 扩容中 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 无 | 活动 4；Ready 4 | sg-3 v1 已 Ready，sg-4 尚未申请 |
| v2 阻塞 | 5/v2 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 NotReady | 活动 5；Ready 4 | U=1 已占满；不碰健康 sg-3 v1 |
| 回滚 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v2 删除中 | 活动 5→4；Ready 4 | sg-4 v2 过期且不可用，可自动清理 |
| 恢复 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 NotReady | 活动 5；Ready 4 | 按最新 v1 在原位重建 |
| 终态 | 5/v1 | v1 Ready | v1 Ready | v1 Ready | v1 Ready | v1 Ready | 活动 5；Ready 5 | 其余健康 v1 组 UID 不变 |

这是有意采用最新意图与自动修复的 ModelServing 语义；
[StatefulSet 文档](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
说明其 OrderedReady 坏模板回滚可能仍需用户手工删除坏 Pod。

### SG-C14：v1→v2→v3 时，旧 v2 surge 仍承担服务容量

`N=3,U=0,S=1,P=0`；sg-3 v2 临时 surge Ready，sg-2 v1 删除已经发出；
在 v3 到来时不可先清理 sg-3 v2，否则 Ready 低于 3。

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

若 sg-2 v3 也坏到无法 Ready，保留健康 sg-3 v2，停止对 sg-1 v1/sg-0 v1 的破坏，
而不是为“清理过期 surge”牺牲服务底线。

## 5. partition、百分比边界和 surge 重分类

所有表中的 partition 都是**固定序号边界**，不是现存组排序后的前几组。
例如 partition=2 时，sg-0 和 sg-1 受保护，新增这两个序号时使用
本轮灰度前的历史 v1；sg-2 及以上使用最新版本。已经运行 v2 的组
后来被纳入保护区，不会主动回滚 v1；但它若消失，可能按历史 v1 重建。

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

### SG-P02：整数 P 大于原 N，扩容跨过 P

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/5/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | 无 | 无 | 无 | 活动 3；Ready 3 | partition=5，暂时没有可更新的组 |
| 扩容 | 6/5/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v1 NotReady（受保护） | v1 NotReady（受保护） | v2 NotReady | 活动 6；Ready 3 | 新增 sg-3/4 用历史 v1，sg-5 用 v2 |
| 终态 | 6/5/v2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 活动 6；Ready 6 | 5 组 v1、1 组 v2 |

### SG-P03：缩容可以穿越保护区并留下稀疏

在组都健康、删除优先级相同时，期望组数从 5 缩至 2，partition=3：
从最高序号缩容，先删 sg-4 v2 和 sg-3 v2，再删 sg-2 v1，
留下 `{sg-0 v1,sg-1 v1}`。P 不是最小副本数。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 5；Ready 5 | 灰度稳态 |
| 缩容 | 2/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 删除中 | v2 删除中 | v2 删除中 | 活动 5→2；Ready 2～5 | 同优先级按高位先删 |
| 终态 | 2/3 | v1 Ready（受保护） | v1 Ready（受保护） | 无 | 无 | 无 | 活动 2；Ready 2 | 默认路径连续 |

若另有 `N=3,P=2,{sg-0 v1 Ready,sg-1 v1 NotReady,sg-2 v2 Ready}`，缩到 N=2 时
NotReady sg-1 优先于健康 sg-2 被删除，允许 `{sg-0 v1,sg-2 v2}`：

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 缩容前 | 3/2 | v1 Ready（受保护） | v1 NotReady（受保护） | v2 Ready | 活动 3；Ready 2 | sg-1 故障 |
| 缩容后 | 2/2 | v1 Ready（受保护） | 无 | v2 Ready | 活动 2；Ready 2 | 缩容合法留洞，不删除健康 sg-2 v2 |
| 再扩容 | 3/2 | v1 Ready（受保护） | v1 NotReady（受保护）→v1 Ready（受保护） | v2 Ready | 活动 3；Ready 2→3 | 最低洞 1 取历史 v1；sg-2 v2 保 UID |

### SG-P04：百分比 P 因扩容提高

`P="50%"`：N=3 时 P=2；N=5 时 P=3。旧 sg-2 v2 被新 P 保护，
但现存 sg-2 v2 不主动回滚 v1。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 灰度 |
| 扩容 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready（受保护） | v2 NotReady | v2 NotReady | 活动 5；Ready 3 | 存量 sg-2 v2 仍 v2；3/4 用 v2 |
| 终态 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready（受保护） | v2 Ready | v2 Ready | 活动 5；Ready 5 | 不要求“恰好 P 个 v1” |

若 sg-2 v2 后来失效/被删除，恢复缺位 2 依新 P 使用历史 v1；
这与不主动替换健康 sg-2 v2 并不矛盾。

### SG-P05：百分比 P 因缩容降低

`P="50%"`：N=5 时 P=3；N=3 时 P=2。缩容先删除高位，
其后 sg-2 v1 因保护边界下降而可以更新。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 5；Ready 5 | 原灰度 |
| 缩容 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready | v2 删除中 | v2 删除中 | 活动 5→3；Ready 3～5 | 缩容先完成 |
| 滚动 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 删除中→v2 Ready | 无 | 无 | 活动 2～3；Ready 2→3 | 只更新新解锁的 sg-2 |
| 终态 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | 无 | 无 | 活动 3；Ready 3 | 2 组 v1、1 组 v2 |

### SG-P06：在途时提高 P

`N=3`，sg-2 v2 Ready，sg-1 v1 的删除已发出；P:0→3。后续新建的 sg-1
按历史 v1，现存 sg-2 v2 不主动回退。

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

`N=3,P=50%→2,U=0,S=1`，sg-3 v2 surge 尚未 Ready；扩到 N=5
使 P=3，sg-2 重新受保护，sg-3 原地转正式组。没有需要更新的旧版组，
不再额外申请 sg-5。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | sg-5 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/2 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready | v2 NotReady（临时 surge） | 无 | 无 | 活动 4；Ready 3 | U=0，不删 sg-2 v1 |
| 扩容 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 NotReady | v2 NotReady | 无 | 活动 5；Ready 3 | sg-3 v2 保 UID，sg-4 v2 补正式缺位 |
| 终态 | 5/3 | v1 Ready（受保护） | v1 Ready（受保护） | v1 Ready（受保护） | v2 Ready | v2 Ready | 无 | 活动 5；Ready 5 | 没有新的 surge 需求 |

### SG-P09：受保护组的故障也占用 maxUnavailable

`N=3,P=1,U=1,S=0`；受保护的 sg-0 是 v1 NotReady，
sg-1 和 sg-2 是 v1 Ready。

| 阶段 | sg-0 | sg-1 | sg-2 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- |
| 故障 | v1 NotReady（受保护） | v1 Ready | v1 Ready | 活动 3；Ready 2 | 已在 `N-U=2` 底线；不能删健康 sg-2 v1 |
| 恢复 | v1 Ready（受保护） | v1 Ready | v1 Ready | 活动 3；Ready 3 | sg-0 v1 依历史模板恢复 |
| 滚动 | v1 Ready（受保护） | v1 Ready | v1 删除中→v2 Ready | 活动 2～3；Ready 2→3 | 再更新序号最高的可更新组 sg-2 |
| 终态 | v1 Ready（受保护） | v2 Ready | v2 Ready | 活动 3；Ready 3 | sg-1 最后到 v2 |

### SG-P10：surge 被扩容复用后继续 partition 滚动

`N:3→4,P=1,U=0,S=1`；sg-3 v2 已 Ready，扩容使 sg-3 v2 成正式组；
旧组 sg-1 和 sg-2 仍可更新，需要临时组 sg-4 v2 Ready 后
才可删除健康组。

| 阶段 | 期望组数 / partition | sg-0 | sg-1 | sg-2 | sg-3 | sg-4 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 初始 | 3/1 | v1 Ready（受保护） | v1 Ready | v1 Ready | v2 Ready（临时 surge） | 无 | 活动 4；Ready 4 | sg-3 v2 是临时容量 |
| 扩容 | 4/1 | v1 Ready（受保护） | v1 Ready | v1 Ready | v2 Ready | v2 NotReady（临时 surge）→v2 Ready（临时 surge） | 活动 5；Ready 4→5 | sg-3 v2 保 UID，sg-4 v2 占新 surge |
| 滚动 | 4/1 | v1 Ready（受保护） | v1 Ready | v1 删除中→v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 4～5；Ready 4→5 | 先 sg-2 |
| 继续 | 4/1 | v1 Ready（受保护） | v1 删除中→v2 Ready | v2 Ready | v2 Ready | v2 Ready（临时 surge） | 活动 4～5；Ready 4→5 | 再 sg-1 |
| 终态 | 4/1 | v1 Ready（受保护） | v2 Ready | v2 Ready | v2 Ready | 无 | 活动 4；Ready 4 | 最后清理临时 sg-4 v2 |

### SG-P11：高序号组仍需更新，但低序号空洞受保护

先前缩容留下 `N=2,{sg-0 v1,sg-3 v1}`；设置 `P=2,U=1,S=0`，提交 v2。
sg-3 在保护范围之外，仍需更新；缺位的 sg-1 在保护范围之内，
只能补历史 v1。不能删除 sg-3 v1 后创建 sg-1 v1，
再声称完成了 sg-3 从 v1 到 v2 的更新。

| 阶段 | 期望组数 / partition / 目标版本 | sg-0 | sg-1 | sg-3 | 组数（活动；Ready） | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 2/2/v2 | v1 Ready（受保护） | 无 | v1 Ready | 活动 2；Ready 2 | P≥N 但高位保留组仍可更新 |
| 滚动 | 2/2/v2 | v1 Ready（受保护） | 无 | v1 删除中→v2 NotReady | 活动 1～2；Ready 1 | 必须在 sg-3 原位更新，洞 1 保持旧债 |
| 终态 | 2/2/v2 | v1 Ready（受保护） | 无 | v2 Ready | 活动 2；Ready 2 | 新版 sg-3 v2 Ready；不为连续性重建它 |
| 后续扩容 | 3/2/v2 | v1 Ready（受保护） | v1 NotReady（受保护）→v1 Ready（受保护） | v2 Ready | 活动 3；Ready 2→3 | 新增容量槽才补 sg-1，使用历史 v1；sg-3 保留 UID |

这说明 StatefulSet 常见的“partition 不小于副本数时没有 Pod 更新”
不能机械套到允许稀疏序号的 ModelServing：尽管期望只有 2 组，
sg-3 仍是现存保留组，必须按它自己的序号判断是否可更新。

## 6. ServingGroupRollingUpdate 中的 Role 副本交错

### SG-R01：partition 灰度中只扩 Role 成员数

`N=3,P=1`，sg-0 使用历史 v1 的 worker 模板 W1，
sg-1 和 sg-2 使用 v2 的 worker 模板 W2。
仅将 `roles[].replicas` 从 1 扩到 2，不产生新的 SG 版本，也不触发序号补洞。
默认 `U=1,S=0`；按每组已应用的成员目标判断完整 Ready。未轮到的组仍以
旧成员数提供服务，不会因全局 spec 刚改变就同时失去 Ready 信用。

| 阶段 | 每组 Role 成员数 | sg-0 | sg-1 | sg-2 | 整组 Ready | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 初始 | 1 | v1/W1×1 Ready（受保护） | v2/W2×1 Ready | v2/W2×1 Ready | 3 | 灰度稳态 |
| 扩 sg-2 | 2 | v1/W1×1 Ready（受保护） | v2/W2×1 Ready | v2/W2×1 Ready+v2/W2×1 NotReady | 2→3 | 只给一组应用新成员目标，等 W2 Ready |
| 扩 sg-1 | 2 | v1/W1×1 Ready（受保护） | v2/W2×1 Ready+v2/W2×1 NotReady | v2/W2×2 Ready | 2→3 | 仍守住 N-U=2 |
| 扩 sg-0 | 2 | v1/W1×1 Ready（受保护）+v1/W1×1 NotReady | v2/W2×2 Ready | v2/W2×2 Ready | 2→3 | 受保护组仍使用历史 W1 模板 |
| 终态 | 2 | v1/W1×2 Ready（受保护） | v2/W2×2 Ready | v2/W2×2 Ready | 3 | 不重滚 SG 模板，不把 W2 套给 sg-0 |

## 7. 验收与反例清单

验证每个场景时，要记录当时的期望组数、partition、maxUnavailable、
maxSurge，以及每个 SG 的序号、版本、UID、Ready/删除状态。
还要区分正式保留组与临时 surge，并保留已发动作和历史版本记录。
“过程中又来了新请求”的测试，应确保前一步确实还在执行。

不能只检查最后“组数等于期望值”。还要检查：非缩容动作没有留下新的
稳定空洞；健康且已是目标版本的组没有仅为排齐序号而被删除；
没有超出 surge 上限创建新组，也没有通过滚动删除健康组把 Ready 数
降到 maxUnavailable 所允许的底线以下。如果故障本已使 Ready 低于
底线，过期 NotReady 组的替换只能在 Q 有余额时保持或增加 Ready，
且不能跳过更高序号的旧组。
缩容造成的短暂超额只允许逐步消退。

三个必须保留的反例：

1. SG-S01：`{0,3}` 的 N2→N3 只补 sg-1，留下洞 2；若强行归位就要重建健康 sg-3。
2. SG-S05：低位 sg-1 v2 NotReady、高位 sg-2 v1 Ready；低位替换虽
   不降 Ready，但不能跳过仍需更新的高位。预算又挡住高位，须明确阻塞。
3. SG-C14：旧 v2 surge 虽过期却仍在提供服务；若按“最新版本优先”
   立即删除它，就会越过可用性底线。

SG-S06/S07 是可收敛的正例：Q 仍有余额时，已不可用且版本过期的
最高序号组可成批被新版本替换；即使当前故障已经超过底线，
替换也不会再减少 Ready。
SG-C16 要求同时核对 `C` 和 `V`：一个尚未 Ready 的实际 surge
不会凭空增加清理信用，但也不能只扣 `V` 而忽略它已使 `C` 增 1。

本文是设计推导，**没有**把任何一行表格视作当前实现或 Kind 验证结果。
