# 低版本兼容契约

2026-09-15，分支 `fix/032-runner-legacy-quality`。原实现和时间见 [检查点](CHECKPOINT_20260915.md)。

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

本分支验证结果将在实际 Go 回归和 Kind 运行结束后补充。
