# 低版本兼容契约

2026-09-15，分支 `legacy`（原 `fix/032-runner-legacy-quality`）。全量检查及保序实现已合入 `main`；本分支保留低版本兼容契约。原实现和时间见 [检查点](CHECKPOINT_20260915.md)。

正常完成端点不再要求 SG 和 Role 的数字身份恰好等于 `0..replicas-1`。例如 D=3 时 `123`、`023` 均可通过身份集合检查，但仍必须有三个不同的合法身份，不能有缺副本、重复身份、重复 entry 或 worker 冒充额外副本。实际 Ready、模板版本、worker 数量、PodGroup、插件资源及状态历史引用还须分别通过检查。按阶段最新副本数和有效历史布局验收。

新结果标识为 `ordinalContract=normal-endpoints/unique-identities-without-contiguous-ordinals/v1`。取消强制最终 B 编号的显式预期，仍保留 A/B 数量和受保护 A 身份。RUN-154～159 的连续 B 是用于状态恢复测试的明确准备数据，并由 UID 保持规则保护；故障源明确身份、partition 边界、缩容对象和滚动下降启动顺序也保留。

核心 60 项仍从目录定义的 012 基线建立 UID/启动顺序账本；这是测试准备条件。完成后无需回到 012。普通扩展场景允许稀疏基线身份；保留的场景专用身份条件用于证明已进入所测状态。

质量约束继续生效：

- 全程真实 Ready 容量、maxUnavailable/maxSurge 预算、按旧实例下降顺序开始替换。
- partition 和未变 Role 的 UID 保护、noReplacement、目标实例无故重建检测。取消仅为回归连续编号新增的 `ordinal-cleanup` 豁免；完成容量已满足时，目标身份偏大不能作为无故删除的理由。
- UID 门控放行、entry/worker 完整性、A/B 数量和版本收敛、依赖与 Gang 约束。
- PodGroup owner/关联/资源、headless Service 和 ranktable 清理、ModelServing 状态及 ControllerRevision。
- 连续 Watch 与已锁存失败、故障注入和 API 拒绝断言、逐例资源清理。

历史 708 项及连续编号分支的执行结果不作改写。低版本不支持连续编号，只解除对应要求，其他失败仍记录为 FAIL；不修改 Kthena 源码。

实现提交：`855a0a2159d6fb8aedd48c0f7f06315ca4c92af5`（2026-09-15 01:50:47 +08:00）。保存原版本的说明提交是 `1131f3c`（2026-09-15 01:43:20 +08:00），均带 Signed-off-by。

验证完成于 2026-09-15。`go test ./...`、`go test -race ./...`、`go vet ./...` 全部通过。Python 共 89 项，81 通过、8 项因未设置历史 Kind artifact 路径按原规则跳过。独立解析核对了全部 303 份正常用例：52 份仅移除最终 B 固定编号，版本计数、A 身份、预算、布局和步骤均不变；suite 哈希有效，其他大类未改。

在 Kind `kthena-resync-010`（arm64）对固定 production `538b2825c06bc1e8c5392d18f18f84faee9fca95` 进行了 12 项串行验证，**11 PASS、1 产品 FAIL**。

| 用例 | 结果 | 验证内容 |
| --- | --- | --- |
| 001、031、070、071 | 4 PASS | 普通 SG/Role、默认值恢复及 entry/worker 放行 |
| 016、046、073、075 | 4 PASS | SG 或 frontend Role 实际 `012 → 123`，数量与 Ready 正确，旧实例仍按 `2→1→0` 开始替换 |
| 083 | PASS | 协调滚动完成；7 个预期 ranktable 全部 Completed，没有孤儿表。历史时序问题本次未重现，不表示修复 |
| 137 | FAIL | 空 labels 的无语义变更触发额外 SG（4 个、期望 3 个）及受保护 `model-2` PodGroup 删除；保留 `SURGE_VIOLATION`、`PROTECTED_PODGROUP_REPLACED` |
| 220、224 | 2 PASS | 滚动中 SG/Role 扩容，按最新 D=5 验收容量和保护对象 |

原始证据位于 `artifacts/legacy-032-r1/`，独立复核位于 `artifacts/legacy-032/`：`gates.json`、`case-diff-audit.json`、`build.json`、`kind-verification.json` 和 `product-findings.json`。复核了 1,144 条连续 Watch 及起止快照、真实镜像和二进制。Job 因唯一产品失败按设计退出 1，无重启、无 deadline 超时。

控制器 deployment spec、9 个历史 ModelServing UID 保持；本次用例 namespace 均已清理，无剩余活跃测试 Job。Kthena 固定 production 工作树保持干净。本次为 12 项代表验证，未声称重新运行全部 303/708 项，也未测试另一个未指定的旧版本提交。
