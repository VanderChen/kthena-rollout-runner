# ModelServing 规范 / Specifications

本目录是 Kthena 源码、rollout runner 和人工评审共享的最终契约。当前版本 **2.7（2026-10-08）**；文档预期、历史实现、runner 覆盖和实际测试结果分别记录，不互相替代。工作与审批规则见 workspace [AGENTS.md](../../../AGENTS.md)。

This directory holds the shared final contract for source changes, runner expectations and human review. Version **2.7 (2026-10-08)** defines expectations; it does not imply implementation compliance or a passing cluster run.

## 文档入口 / Documents

| 主题 / Topic | 中文 | English |
| --- | --- | --- |
| API、默认值、可变性和校验 / API, defaults, mutability, validation | [API 参考](modelserving-api-reference.zh-CN.md) | [API reference](modelserving-api-reference.en.md) |
| Plugin 配置、不可修改规则与效果 / Plugin configuration, immutability and effects | [Plugin 说明](modelserving-api-reference.zh-CN.md#12-plugin-配置与效果) | [Plugin reference](modelserving-api-reference.en.md#12-plugin-configuration-and-effects) |
| SG 35 个组合场景及 SG/Role 共用速查 / 35 SG scenarios and shared SG/Role lookup | [SG 规范](servinggroup-compound-rollout.zh-CN.md) | [SG expectations](servinggroup-compound-rollout.en.md) |

## 命名与同步 / Naming and synchronization

- 正文采用 `<topic>.<language>.md`，topic 使用小写 kebab-case；language 为 `en` 或 `zh-CN`。README.md 是双语入口，SOURCES.json 是机器可读来源清单。
- 版本写在正文和 SOURCES.json，不放入文件名；目录只保留每个语言的最终版本，旧内容从 Git 和原始来源追溯。
- 两种语言共享 API 节号、SG 设计 ID、数值表和语义范围。修订必须同时维护两份；叙述可以适应语言，不能产生不同的默认值、约束或例外。
- 语义修订按 workspace 审批流程，记录批准原文、前后规则、受影响用例和验证；不得从实现或失败结果自动改写规范。更新正文后同步 SHA256/字节数，保留原始导入来源。

Use `<topic>.<language>.md`, with lower-case kebab-case topics and `en` / `zh-CN` locales. Keep versions inside documents and SOURCES.json, preserving stable links. Maintain both editions in the same change, with matching clause numbers, scenario IDs, numeric tables and semantics. Record explicit semantic approval and provenance; never regenerate expectations from observed implementation. Earlier editions remain recoverable from Git or the recorded original sources.

## 本次批准的修订 / Approved changes

**2.7（2026-10-08）**：协调进度改为实际目标 Ready/完整正式容量，不扣 partition，灰度停点仍参与。配置阶段校验共同名义 partition 比例，独立取整、不传播；已有不兼容对象的无关更新兼容，并暂停继续选择稳定旧删除。全量收尾循环可有条件许可调用方最后一步，保留预算与旧依赖，明确可能暂时超过 maxSkew。具体审批、原版快照、源码差异和独立验证见 [059 proposal](../../../issues/bugs/059-role-coordination-skew-boundaries-DONE/PROPOSAL_COMMIT.md)。原工单“一个 Role 保留 v1、其余全 v2”预期已由用户纠正，不作为新验收目标。runner 的用例生成器/判定尚未迁移到本次新语义，历史结果不能视为 2.7 通过。

**2.7 (2026-10-08)**: Coordination uses target Ready/full formal capacity without subtracting partition; canary stops remain in the baseline. Admission checks a common nominal partition fraction with independent rounding and no propagation. Unrelated legacy updates remain possible while further stable old deletions are blocked. A constrained full-rollout terminal caller step may exceed maxSkew temporarily; budgets and old dependency retention remain. See task 059 for approval, prior snapshots, source changes and separate verification. The user superseded the historical expectation of retaining v1 in only one Role. Runner generators/verdicts have not been migrated to these new semantics; historical results are not 2.7 conformance evidence.

**2.6（2026-10-08）**：删除 `workload.kthena.io/deletion-scope` 及仅用于跨重启续跑旧删除事务的 operation/阶段/UID 集合持久化。controller 重启后不重放重启前的整批删除计划，而是依据当前实际 Pod、最新配置、历史已应用模板、partition、预算和协调规则重新收敛。部分恢复留下的缺失 Role/Pod 可按其所属滚动单元的正确历史模板补齐；健康幸存实例保留。后续故障按当时 recoveryPolicy、grace 与健康事实重新判断。UID/owner 校验、旧事件隔离、Terminating 占用、完整 Ready、滚动预算和持续运行期间配置的 Role/SG 恢复范围保持。该决议明确取代 2.5 的 D7 旧记录暂停/迁移方案；相关版本未上线，无兼容负担。

**2.6 (2026-10-08)**: Remove `workload.kthena.io/deletion-scope` and the operation/phase/UID persistence used only to resume a pre-restart deletion transaction. After a controller restart, converge from actual Pods and the latest configuration under applied-template history, partition, budget and coordination rules instead of replaying the old batch. A role or Pod left missing by an interrupted repair may be refilled from the correct historical template while healthy survivors remain. Any later fault is reevaluated under the then-current recoveryPolicy, grace and health facts. UID/owner fencing, stale-event isolation, Terminating occupancy, complete Ready checks, rollout budgets and configured Role/SG scope during continuous operation remain. This decision explicitly supersedes the 2.5 D7 migration/pause design; the affected version was never deployed, so no compatibility path is required.

**2.5（2026-10-07）**：旧删除记录无法安全重建事实时，暂停受影响 SG 的相关删除，明确告警并等待人工处置；其他 SG 在原有预算、顺序、容量及协调等约束允许时继续。事实可确认的未提交计划或已接受删除仍正常处理，不把全部旧记录一律冻结。分支和两个例子见 SG A.4；专项执行覆盖仍待补齐。D6 的输入兼容策略尚未选择。

**2.5 (2026-10-07)**: When legacy deletion facts cannot be safely reconstructed, pause related deletions in the affected SG, alert and await manual intervention. Other SGs continue under existing budgets, order, capacity and coordination. Established uncommitted plans or accepted deletions follow normal handling; do not freeze all old records. See SG A.4 for branches and two examples; dedicated executable coverage remains pending. D6 input compatibility remains undecided.

**2.4（2026-10-07）**：局部修复保留所属滚动单元已应用模板，完整单元重建才按 partition 选版本；故障起点跨 controller 重启保留并计入离线时间；pod-discovery 作为组网成员表保留 Running/NotReady 成员 IP。按用户纠正，被 ModelServing 使用的 ranktable 模板内容应禁止修改，用户随后确认保护 webhook 在生产分支，并要求暂时忽略此问题，故暂停定位，不宣称已验证部署。2.4 时 D6/D7 未决定；D7 后续批准见 2.5。

**2.4 (2026-10-07)**: Local repair preserves the enclosing rollout unit’s applied template; whole-unit recreation uses partition. Preserve fault start time across controller restarts, counting downtime. pod-discovery publishes Running/NotReady member IPs for initialization. The user clarified that in-use ranktable template content must not change; the user later confirmed that its webhook is on the production branch and asked to set the issue aside, so lookup is deferred without claiming deployment verification. D6/D7 were undecided at 2.4; D7 was subsequently approved in 2.5.

**2.3（2026-10-07）**：每个完整单元 Ready 后重新结算预算，有合法额度即继续，无额外整批屏障。保留预算、候选顺序、partition、身份、容量和协调限制；例子见 SG 附录 B.5。本次同步文档，专项执行覆盖和 Kind 验证另行记录。

**2.3 (2026-10-07)**: Recompute allowance after each complete Ready unit and progress when eligible, without an extra whole-batch barrier. Budgets, candidate order, partition, identity, capacity and coordination remain in force; see SG Appendix B.5. This is a documentation update; dedicated executable coverage and Kind verification remain separate.

**2.2（2026-10-07）**：SG/独立 Role 默认合法旧 NotReady 优先、同类高到低；配置 roleCoordination 的 Role 稳定实例不跳过，无新增开关。maxSkew 仍为百分比进度，不增加 index 配对语义。统一 maxScaleDown 总额度、maxHealthyScaleDown 健康删除上限及不重复扣减的 inFlightReservations 在途账本。

**2.2 (2026-10-07)**: SG/independent Role rollout defaults to eligible old NotReady first, descending within each health class. Coordinated Role stable instances cannot skip; no new switch or index-pairing guarantee. Shared accounting distinguishes total maxScaleDown, healthy bound maxHealthyScaleDown and nonduplicated in-flight inFlightReservations.

| 快速入口 / Quick lookup | 内容 / Content |
| --- | --- |
| [API §2.3 中文](modelserving-api-reference.zh-CN.md#23-两层共用的预算与默认选择顺序) / [English](modelserving-api-reference.en.md#23-shared-budgets-and-default-candidate-selection) | 两层公式、计数单位、模式规则 / Shared formulas, units and mode selection |
| [行为速查中文](servinggroup-compound-rollout.zh-CN.md#budget-lookup) / [English](servinggroup-compound-rollout.en.md#budget-lookup) | 16 个数值场景；全旧版坏、025/049、协调阻塞、surge、在途动作 / 16 numeric cases, all-old failure, 025/049, coordination, surge and reservations |
| [末步 maxSkew 示例中文](servinggroup-compound-rollout.zh-CN.md#terminal-maxskew-example) / [English](servinggroup-compound-rollout.en.md#terminal-maxskew-example) | B.6：8/4/10 的普通取整、收尾循环及 100%−75%=25 个百分点；既有规则数值说明 / Ordinary rounding, the terminal cycle and a 25-point gap; illustration of existing rules |

本轮文档批准及验证见 [052 proposal](../../../issues/features/052-runner-spec-contract-alignment-IP/PROPOSAL_COMMIT.md)；公式分析见 [049 proposal](../../../issues/bugs/049-role-coordination-rollover-DONE/PROPOSAL_COMMIT.md)。前次同步文档和来源记录；本次执行器迁移与真实测试见 [051 验证记录](../../../issues/features/051-production-baseline-realignment-DONE/runner-production-20261007/README.md)，不宣称产品全套符合。

Approval and verification are recorded in those proposals. The initial revision updated documents and provenance. Executor migration and actual results are recorded separately in issue 051; they do not imply full product conformance.

以下保留此前 2.1 的批准摘要 / Earlier approved 2.1 changes:

| 范围 / Scope | 契约 2.1 / Contract 2.1 |
| --- | --- |
| API §1–5、§8、§11 | Role 名称集合、gangPolicy/roleCoordination 整体及存在性不可变；非生效层预算允许但忽略。Immutable identities/policies; inactive budgets are allowed but ignored. |
| API §1.2、§3–4；SG A.2 | maxUnavailable 一律 floor，不补 1；实际双零拒绝，含零副本/全 partition；生效 maxUnavailable/partition 上界，零副本 maxUnavailable=1 例外；maxSurge 可 >100%，desiredReplicas+maxSurge 不超 int32。Floor maxUnavailable, reject resolved double zero, enforce active bounds and int32 capacity. |
| API §6 | grace=-1 永久容忍重启错误/Failed；None 不主动删除；真实 PodDeleted 仍按 SG/Role/单 Pod 范围恢复。Indefinite tolerance and no proactive deletion under None; real deletion still follows the selected scope. |
| SG-P02/P03 | 扩缩请求同时调整 partition，满足 partition<=desiredReplicas；保留历史模板场景，旧非法输入转由 API 拒绝用例覆盖。Adjust partition atomically with desiredReplicas; test invalid old inputs at admission. |
| SG-P11 | 保留此前 042 明确批准的“过期高位替换补低位，新 ordinal 决定历史/目标版本”；同步 runner，SG-S03 健康目标高位保留规则不变。Preserve the prior approved replacement-slot clarification and healthy target identities. |

本轮批准及证据：[052 proposal](../../../issues/features/052-runner-spec-contract-alignment-IP/PROPOSAL_COMMIT.md)。既有 SG-P11 批准：[042 proposal](../../../issues/bugs/042-maxsurge-final-ordinals-DONE/PROPOSAL_COMMIT.md)。修改前未提交的文档整理及稀疏澄清已保留在 052 的 before/；未修改 Kthena 产品代码或 production。

Approval and evidence are recorded in the linked proposals. Pre-existing uncommitted document consolidation and the sparse-partition clarification were preserved. This task changes the runner and its reference documents only.

## 覆盖与剩余边界 / Coverage and boundaries

- 当前 API 与恢复入口、执行命令、历史用例冲突见 [CONTRACT_MIGRATION.md](../CONTRACT_MIGRATION.md)。历史矩阵原始来源不改写；被替代的旧预期不计作当前通过，也不计作产品失败。
- `cases/servinggroup-compound-v2/` 的 35 个 ID 映射 SG-S01～S07、SG-C01～C16、SG-P01～P11、SG-R01 至 RUN-618～652；引用必须同时含 suite/路径和 ID，不能把重复编号直接相加。全套未运行不能宣称全套通过。
- 2.2 明确改变 SG-S05/RUN-622 与 SG-C08/RUN-632 的低位故障分支；case/generator/顺序 verdict 已迁移，真实结果单独记录。16 个共用查表场景也不是 16 个新 executable case。旧结果不能直接作为 2.2 PASS/FAIL，详见速查表 B.4 和 [COMPOUND_V2](../COMPOUND_V2.md)。
- 2.3 已统一为逐 Ready 推进；部分批次 Ready 的专项断言/Kind 验证仍需单独补齐，不能用整批放行或终态通过替代。
- 历史普通用例的连续终态与 SG 稀疏身份保留分属不同场景；不得全局放松连续检查，或把连续终态强加于 SG-S01/S03。
- API §12 已补充 046 最终批准的插件不可变规则、语义等价表达及常见插件效果；文档补齐不代表新增 runner 插件执行覆盖。047 手动 revision、048 Pod 隔离等能力仍未在本 API 参考完整描述；缺文档不授权删除这些能力。
- 2.4 的 recovery 首次版本/后续合法滚动判定尚未迁移，有限 grace 重启计时与插件边界缺专项验证；既有 PASS/FAIL 不直接视为 2.4 结论。ranktable 保护预期与本地 production 校验入口的差异记录于 052，不因未定位实现而删除用户确认的规则。
- 2.6 已取代 2.5 的旧删除记录暂停方案。RUN-436/RUN-439 保持正常滚动 Terminating 重启范围，生成器已同步批准说明；新增 [restart-convergence/RC-01～04](../RESTART_CONVERGENCE.md) 专门构造 SG/Role 恢复部分删除后重启，检查首次历史版本、健康幸存 UID、缺员预算、后续合法滚动及连续运行恢复对照。用例、单测、真实 Kind 结果独立记录，不能把文字迁移或最终 Ready 当作中间行为证明。058 原始产品证据保留。D5 实现定位按用户要求暂不继续。
- runner 执行契约另见 [DEVELOPER_GUIDE](../DEVELOPER_GUIDE.md)、[CONTRACT](../CONTRACT.md)、[CORE_EXPECTATIONS](../CORE_EXPECTATIONS.md)、[COMPOUND_V2](../COMPOUND_V2.md)。

Current executors and historical conflicts are documented in the migration guide. Suite-qualified IDs are required. Contiguous historical workflows and approved sparse layouts retain their own assertions. API section 12 now documents task 046's final approved plugin immutability, equivalent representations and common effects; this adds no runner plugin execution coverage. Remaining documentation gaps for manual-revision and Pod-isolation work do not authorize removal of those capabilities. A passing subset does not establish complete suite coverage. Cases/generators/order verdicts now implement the 2.2 ordering, including RUN-622/632; the 16 lookup rows are not executable coverage. Version 2.3 resolves the whole-batch versus per-Ready timing discrepancy in favor of per-Ready progress; dedicated partial-readiness execution coverage remains separate. Version 2.6 supersedes the 2.5 legacy-transaction pause design; RUN-436/439 retain rolling-Terminating restart coverage with reproducible approved descriptions. The separate restart-convergence/RC-01–04 suite establishes partial fault recovery, historical refill, survivor UIDs, occupied budget, later lawful rollout and uninterrupted recovery controls. Definitions and fresh Kind outcomes remain separate from earlier task 058 evidence.

[SOURCES.json](SOURCES.json) preserves original imports, prior final-edition hashes, approvals and current bilingual checksums.
