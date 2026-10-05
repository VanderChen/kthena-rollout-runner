# ModelServing 滚动预期与 API 对照基线

这里保存此前梳理的文档原文，供 Kthena 主仓修复、runner 用例和人工评审共同对照。
2026-10-05 首次迁入；正文逐字节保留，来源、文件大小和 SHA256 见
[SOURCES.json](SOURCES.json)。迁入不改变原文的适用范围，也不把其中的建议、
源码推导、待实现项或历史验证自动提升为已批准、已实现或已通过。

## 从这里查

| 文档 | 用途与范围 |
| --- | --- |
| [SG 复合场景行为规范](SERVINGGROUP_COMPOUND_ROLLOUT_EXPECTATIONS.md) | SG 滚动的预算、顺序、身份、扩缩与版本选择原则；原文日期 2026-09-20 |
| [SG v2.0 全量预期过程表](SERVINGGROUP_COMPOUND_ROLLOUT_EXPECTATIONS_V2.md) | 35 个 SG 场景的逐步预期：SG-S01～S07、SG-C01～C16、SG-P01～P11、SG-R01 |
| [SG 过程表](SERVINGGROUP_COMPOUND_ROLLOUT_PROCESS_TABLES.md) | 原规范配套的过程推演，与 v2.0 并排对照；不据此覆盖 v2.0 |
| [ModelServing API reference v2.0](modelserving-api-reference.en.v2.0.md) | 字段、默认、可变性、百分比和跨字段校验；第 11 节保留与原 production 基线的区别 |
| [历史全场景分析](history/ROLLING_UPDATE_SCENARIOS.md) | 2026-09-05 基线分析，含 SG/Role、coordination、历史与故障；不能当成最新 API 规范 |
| [历史可读指南](history/MODELSERVING_ROLLOUT_READABLE_GUIDE.md) | 原 production 基线的默认、边界、版本比较和不可变字段说明 |
| [历史场景表](history/ROLLING_UPDATE_CASE_TABLE.md)、[JSON 清单](history/ROLLING_UPDATE_CASES.json) | 原 768 项设计登记快照；不是当前 runner 执行覆盖或通过数 |

本仓的[用例开发指南](../DEVELOPER_GUIDE.md)、[原 60 项执行契约](../CONTRACT.md)、
[基础预期](../CORE_EXPECTATIONS.md)和[组合用例说明](../COMPOUND_V2.md)
描述各自 runner 范围。`cases/servinggroup-compound-v2/` 保留设计 ID 到
RUN-618～652 的映射；旧 768 项清单的 RUN-612～671 则属于历史 controller 升级设计。
引用用例必须同时写 suite/路径和 ID，不能只用重复的 RUN 编号或把它们相加声称覆盖。

## 已有差异必须显式处理

这些差异在迁入前就存在。本次仅登记，不修改规范、case 或判定实现来消除它们。

| 对照点 | 现有差异和评审要求 |
| --- | --- |
| 连续序号与稀疏保留 | 既有 RUN-001～303 正常流程检查连续终态 `0..replicas-1`；SG 复合规范允许缩容留下稀疏健康组，并禁止只为排齐而重建健康目标组。先确定场景来源；禁止全局放松旧检查或把连续终态强加给 SG-S01 等场景。改变任一套语义须单独批准。 |
| API 目标与历史实现 | API reference 第 11 节明确列出 Role 名称集合、不可变对象、SG 下 Role 预算、双零预算、整数上限及 surge 百分比等区别。旧场景曾允许的输入不能直接否定目标 API，也不能无审批改写旧场景。 |
| SG 小百分比 U | API reference 明确正百分比在正副本数下最小为 1；SG 复合文档只写向下取整。涉及向下取整得到 0 的新用例时须明确所用条款；若要统一口径，先提交具体修订并获人工批准。 |
| 后续恢复语义 | 原 API reference 第 6 节保留旧恢复说明。workspace task 041 已记录 `restartGracePeriodSeconds=-1` 和 `None` 的后续语义，不能依据旧文档把该修复回退。本次未把 task 041 的实现批准解释成重写本参考的授权；如需同步，单列文档修订。 |
| 后续能力与覆盖空白 | task 046 的插件不可变、047 的手动 revision、048 的 Pod 隔离等后续工作未在这份原 API reference 中完整覆盖。文档缺项不构成删除现有能力的理由；核对相关任务的批准和验证后提出补充。 |
| 文档与实测 | 原文的设计轨迹、旧 PASS/FAIL、NOT_RUN 和基线 commit 保持原样。任何本次验收都必须固定候选源码、case、镜像、Job 和证据；历史记录不证明当前候选通过。 |

`history/` 原文中的旧工作树绝对路径和旧报告链接属于历史来源记录，可能已经不存在。
查当前文件用上方入口，追溯原件用 `SOURCES.json` 的 workspace 路径；不把失效链接改指
当前源码，避免把旧结论误绑定到新实现。本目录不承诺覆盖尚未登记的新功能。

## 修复与规范修改流程

1. Kthena 主仓的 ModelServing 修复在 proposal 阶段先核对本目录；明确适用文档版本、
   章节/设计 ID、旧行为、预期行为和回归用例。涉及 API、默认化、校验、滚动、扩缩、
   恢复、revision、状态或插件身份时，逐项说明是否符合原预期。完成前再次核对最终 diff。
2. 把实现不符合规范、runner 误判、规范冲突或覆盖缺项分别写进任务
   `PROPOSAL_COMMIT.md`。不能按当前代码、失败结果或过程讨论自动推导新的核心预期。
3. 任何核心预期变更（含文字、表格、示例、默认值、允许/禁止轨迹、case/生成器的预期和
   PASS/FAIL 判定）先提供可审阅的旧/新内容、理由、兼容影响和受影响用例，留在任务提案中。
   **人工明确批准具体规范改动后，才可改入本目录并同步对应检查。**
4. 批准实现修复、继续排查、讨论一种方案、合并分支或测试通过，都不等于批准规范变更。
   已有批准仅在明确覆盖相同规范条款、差异和范围时可沿用；不得把讨论里的试探性结论写成共识。
5. 批准后记录批准日期、可追溯的人工指令、文档/条款、前后差异、影响的 case、验证和提交，
   再更新文档及 `SOURCES.json` 的校验值与修订来源，保留首次来源追溯。禁止自动从 Kthena
   文档、controller helper、生成器或运行结果覆盖本基线。

仅修复排版或链接且完全不改变含义的维护可以直接进行，但须说明无语义影响；
范围或语义有歧义时保留原文，将具体差异交给人工确认。

## 分支与维护位置

`main` 承载完整 runner 实现及修复，含原执行套件、阻塞场景、SG 组合场景、可配置 Job
和离线报告。不再维护 `legacy` 的宽松序号契约；旧分支提交保存在
`archive/legacy-20261005` tag，仅用于追溯。Git 归档不改变任何现有预期。

此目录是后续对照和获批修订的维护位置。workspace `issues/` 中原文继续保留为来源，
Kthena 的公开 API 文档如需同步，必须通过上述规范变更流程，不采用双向自动覆盖。
