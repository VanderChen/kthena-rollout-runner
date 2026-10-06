# ModelServing 规范 / Specifications

本目录是 Kthena 源码、rollout runner 和人工评审共享的最终契约。当前版本 **2.1（2026-10-06）**；文档预期、历史实现、runner 覆盖和实际测试结果分别记录，不互相替代。工作与审批规则见 workspace [AGENTS.md](../../../AGENTS.md)。

This directory holds the shared final contract for source changes, runner expectations and human review. Version **2.1 (2026-10-06)** defines expectations; it does not imply implementation compliance or a passing cluster run.

## 文档入口 / Documents

| 主题 / Topic | 中文 | English |
| --- | --- | --- |
| API、默认值、可变性和校验 / API, defaults, mutability, validation | [API 参考](modelserving-api-reference.zh-CN.md) | [API reference](modelserving-api-reference.en.md) |
| SG 组合滚动、35 个设计场景、完整行为规则 / SG compound rollout, 35 scenarios and behavioral rules | [SG 规范](servinggroup-compound-rollout.zh-CN.md) | [SG expectations](servinggroup-compound-rollout.en.md) |

## 命名与同步 / Naming and synchronization

- 正文采用 `<topic>.<language>.md`，topic 使用小写 kebab-case；language 为 `en` 或 `zh-CN`。README.md 是双语入口，SOURCES.json 是机器可读来源清单。
- 版本写在正文和 SOURCES.json，不放入文件名；目录只保留每个语言的最终版本，旧内容从 Git 和原始来源追溯。
- 两种语言共享 API 节号、SG 设计 ID、数值表和语义范围。修订必须同时维护两份；叙述可以适应语言，不能产生不同的默认值、约束或例外。
- 语义修订按 workspace 审批流程，记录批准原文、前后规则、受影响用例和验证；不得从实现或失败结果自动改写规范。更新正文后同步 SHA256/字节数，保留原始导入来源。

Use `<topic>.<language>.md`, with lower-case kebab-case topics and `en` / `zh-CN` locales. Keep versions inside documents and SOURCES.json, preserving stable links. Maintain both editions in the same change, with matching clause numbers, scenario IDs, numeric tables and semantics. Record explicit semantic approval and provenance; never regenerate expectations from observed implementation. Earlier editions remain recoverable from Git or the recorded original sources.

## 本次批准的修订 / Approved changes

| 范围 / Scope | 契约 2.1 / Contract 2.1 |
| --- | --- |
| API §1–5、§8、§11 | Role 名称集合、gangPolicy/roleCoordination 整体及存在性不可变；非生效层预算允许但忽略。Immutable identities/policies; inactive budgets are allowed but ignored. |
| API §1.2、§3–4；SG A.2 | U 一律 floor，不补 1；实际双零拒绝，含零副本/全 partition；生效 U/P 上界，零副本 U=1 例外；S 可 >100%，N+S 不超 int32。Floor U, reject resolved double zero, enforce active bounds and int32 capacity. |
| API §6 | grace=-1 永久容忍重启错误/Failed；None 不主动删除；真实 PodDeleted 仍按 SG/Role/单 Pod 范围恢复。Indefinite tolerance and no proactive deletion under None; real deletion still follows the selected scope. |
| SG-P02/P03 | 扩缩请求同时调整 P，满足 P<=N；保留历史模板场景，旧非法输入转由 API 拒绝用例覆盖。Adjust P atomically with N; test invalid old inputs at admission. |
| SG-P11 | 保留此前 042 明确批准的“过期高位替换补低位，新 ordinal 决定历史/目标版本”；同步 runner，SG-S03 健康目标高位保留规则不变。Preserve the prior approved replacement-slot clarification and healthy target identities. |

本轮批准及证据：[052 proposal](../../../issues/features/052-runner-spec-contract-alignment-IP/PROPOSAL_COMMIT.md)。既有 SG-P11 批准：[042 proposal](../../../issues/bugs/042-maxsurge-final-ordinals-DONE/PROPOSAL_COMMIT.md)。修改前未提交的文档整理及稀疏澄清已保留在 052 的 before/；未修改 Kthena 产品代码或 production。

Approval and evidence are recorded in the linked proposals. Pre-existing uncommitted document consolidation and the sparse-partition clarification were preserved. This task changes the runner and its reference documents only.

## 覆盖与剩余边界 / Coverage and boundaries

- 当前 API 与恢复入口、执行命令、历史用例冲突见 [CONTRACT_MIGRATION.md](../CONTRACT_MIGRATION.md)。历史矩阵原始来源不改写；被替代的旧预期不计作当前通过，也不计作产品失败。
- `cases/servinggroup-compound-v2/` 的 35 个 ID 映射 SG-S01～S07、SG-C01～C16、SG-P01～P11、SG-R01 至 RUN-618～652；引用必须同时含 suite/路径和 ID，不能把重复编号直接相加。全套未运行不能宣称全套通过。
- 历史普通用例的连续终态与 SG 稀疏身份保留分属不同场景；不得全局放松连续检查，或把连续终态强加于 SG-S01/S03。
- 046 插件不可变、047 手动 revision、048 Pod 隔离等能力尚未在本 API 参考完整描述；缺文档不授权删除这些能力。
- runner 执行契约另见 [DEVELOPER_GUIDE](../DEVELOPER_GUIDE.md)、[CONTRACT](../CONTRACT.md)、[CORE_EXPECTATIONS](../CORE_EXPECTATIONS.md)、[COMPOUND_V2](../COMPOUND_V2.md)。

Current executors and historical conflicts are documented in the migration guide. Suite-qualified IDs are required. Contiguous historical workflows and approved sparse layouts retain their own assertions. Documentation gaps for later plugin, manual-revision and Pod-isolation work do not authorize removal of those capabilities. A passing subset does not establish complete suite coverage.

[SOURCES.json](SOURCES.json) preserves original imports, prior final-edition hashes, approvals and current bilingual checksums.
