# 正常流程序号检查补充验证

本文保留保序分支的历史验证。当前分支的要求见[低版本质量契约](LEGACY_QUALITY.md)。

2026-09-11：全部 RUN-001～303 已接入公共起止检查。SG 和各 SG 内的每种 Role 在正常基线及每个完成阶段要求 `0..replicas-1`，按阶段最新副本数判断；中间允许合法 surge。RUN-101 的明确未完成依赖停点不当作全量完成。原 708 项报告保留为新增要求前的历史结果。

实现提交：`3f72e67` 和 `7ca4f04`，均签署 Signed-off-by。后者同时允许在预算内回收越界临时目标实例，避免新增完成要求与禁止无故替换的规则冲突；正常序号内目标、零预算和明确 noReplacement 仍受保护。

## 回归与 Kind

runner 全包测试、vet、相关 race 测试、独立审计的 5 项 Python 回归均通过。固定 production `538b2825c06bc1e8c5392d18f18f84faee9fca95` 的非 e2e Go 门禁通过（78 个包），源码未改。日志及 SHA256 见 `artifacts/ordinal-031/gates.json`。

在 arm64 Kind `kthena-resync-010` 串行执行两批，共 10 项，6 PASS、4 项产品 FAIL：

| 批次 | 通过 | 失败 |
| --- | --- | --- |
| ordinal-031-r1 | RUN-001、031、070、071 | RUN-016、046、073、075 |
| ordinal-031-r2 | RUN-220、224 | 无 |

四项失败都由正常 `012` 滚动为 `123`，最终实际 Ready 数量正确、currentRevision/updateRevision 已收敛，但不符合新增的序号要求。新版 runner 给出 `FINAL_ORDINAL_MISMATCH`，没有把它们改成通过。阶段超时为 180 秒，保持用例声明的 hold/stable 窗口；此处证明有界窗口内没有收敛，不证明永久不能恢复。r1 Job 因四项失败退出 1，完整执行了八项；r2 Job Complete、退出 0。两批均无重启或观察缺口。

RUN-220（SG）和 RUN-224（Role）均在 D=3 时保留 `012` 和 NotReady 的 surge 3 至少 10 秒，然后服务端接受 D=5，最终成为 `01234` 并稳定至少 30 秒。原 `012` 和 surge 3 的 UID 均保留，证明过程中的额外序号被允许，终态按新目标副本数检查。具体 UID、窗口及快照 SHA256 见 `artifacts/ordinal-031/expansion-audit.json`。

独立采集核对了镜像/二进制摘要、Job/Pod UID、921 条连续 Watch、实际起止快照和清理。原控制器完整 deployment spec 未变化，9 个历史 ModelServing UID 保持，无活跃测试 Job 和遗留测试 namespace。最终核对时间为 2026-09-11T09:30:11Z；总证据为 `artifacts/ordinal-031/kind-verification.json`，原始数据在 `artifacts/ordinal-031-r1/`、`artifacts/ordinal-031-r2/`。

这是 10 项实际 Kind 补测，不能称为新版 runner 已重新跑完全部 303 项。预算内回收临时目标的许可还有正反单元测试；不能据此声称 production 已实现序号归位。

## 全部 303 项历史起止快照复核

全部正常基线的序号符合要求。既有有效执行结果的最终快照按新增要求复核：

| 结论 | 数量 |
| --- | ---: |
| 起止序号符合 | 179 |
| 旧 PASS 的最终序号不符合 | 86 |
| 原有失败，未给完成终态信用 | 37 |
| 明确未完成的依赖停点 | 1 |

86 项包括 37 项 SG、49 项 Role 序号问题；新 Kind 的四个失败是其中四项的新版检查器验证，不重复加成 90 项。这里仅复核起止快照，不重新证明中间各完成阶段和其他全流程规则。

可复核命令（输出目录必须不存在）：

```sh
python3 scripts/audit-normal-ordinals.py \
  --summary artifacts/category1-final-verification/summary.json \
  --yaml-parser /private/tmp/runner022-yaml-json \
  --out artifacts/ordinal-031/previous-303-endpoints
```

机器报告保留每个有效 attempt、实际/期望集合及输入 SHA256。86 项逐项失败另存于 issues 任务 `031-runner-normal-final-ordinals-DONE/FAILURES.md`，原始报告未覆盖。
