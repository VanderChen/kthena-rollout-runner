# 完整用例集及证据复核

本轮以原始矩阵 `issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json` 为语义契约，SHA256 为 `757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5`。共 708 个唯一 ID；`cases/core` 的旧 60 项以及同 ID 的修正源准备目录不重复计数。

| 类别 | 唯一范围 | 当前证据状态 |
| --- | --- | --- |
| 正常流程 | RUN-001～303 | Kind 266 PASS、37 产品 FAIL |
| 故障恢复 | RUN-304～539 | Kind 128 PASS、108 产品 FAIL |
| 边界与拒绝 | RUN-540～611、DENY-001～097 | Kind 158 PASS、11 产品 FAIL |

三类 708 项均已完成实现及真实 Kind 独立复核，合计 552 PASS、156 项产品 FAIL。总报告位于 `artifacts/expanded-suite-final-verification/`；完成表示每项都有有效结论，产品失败仍保留。

当前控制器基线为 production `538b2825c06bc1e8c5392d18f18f84faee9fca95`，镜像 `kthena-controller-manager:normal-022-538b2825`，实际摘要 `sha256:7c6ed6c78b37d7afaf104381351554ad75aed56c64d0aca2ec941ce652756265`。测试集群为 arm64 Kind `kthena-resync-010`，工作负载使用 Volcano。源码未修改。

## 选择执行集

`cases/normal` 是完整第一类。第二类按真实故障方式拆为 `recovery`、`container-restart`、`midrollout-faults`、`pending-faults`、`controller-restart`、`grace-restart`、`eviction`、`leader-switch`、`lost-deletion`、`initial-sync`、`deletion-replay`、`api-retry`、`plugin-retry` 及 `history-*` 目录。`history-source` 保存五项 Role 历史用例的有效源准备补充，不能重复计入类别总数。

| 第三类目录 | 用例 |
| --- | --- |
| boundary-numeric | RUN-540～565 |
| boundary-zero | RUN-566～572 |
| boundary-dependency | RUN-573 |
| boundary-sparse | RUN-574～603 |
| boundary-completion | RUN-604～609 |
| history-equal | RUN-610 |
| boundary-identity | RUN-611 |
| rejection | DENY-001～097 |

通过 `--cases` 选择一个目录，`--select` 明确选中的 ID。`deploy/` 保存实际批次的 Job 请求及对应不可变镜像；这些是已有 attempt 的证据，不应原名再次提交。新执行需要独立 Job 名、run-id、输出目录和构建记录。阶段超时、观察窗口、受控 Ready 和注入方式均属于执行契约，修改后必须保留原配置并说明差异。

## 本轮 Kind 编排

`scripts/run-history-kind.py MANIFEST BUILD_JSON BINARY` 是本轮环境专用的串行批次编排器，固定了 kubeconfig、原控制器 spec 和 9 个历史 ModelServing UID；不是任意新集群的初始化工具。默认使用已验证的 R6 代理，可通过 `RUNNER_PROXY_POD`、`RUNNER_PROXY_CONTROL_URL`、`RUNNER_PROXY_CA_FILE` 指定经过组件验证的独立代理实例；控制器连接地址取自 manifest 的 `--fault-proxy-api`。专用 Dockerfile、二进制 SHA256、镜像 ID 和构建记录位于 `artifacts/environment-022/`。

编排器先核对原环境，建立每批独立且不可变的代理 ConfigMap，验证控制器实际挂载内容和代理原生启动请求，再创建唯一用例 Job。结束后收集原始输出和请求 trace，恢复原完整控制器 spec，核对全部历史 UID 和代理状态。只有前一批完成收集及恢复后才能开始下一批；不能并发修改共享控制器。

代理规则限定用例 namespace、资源、操作和实际对象身份。HTTP 503、Watch 重放、丢弃或暂停分别记录实际命中。原生 API 409 与代理合成错误须明确区分；没有命中的注入不算故障覆盖。源状态准备可对有限已知对象做带 UID 前置条件的操作；明确测试边界之后，不得使用准备清理替代被测控制器恢复。

等价历史冲突使用 R7 的 `omit-list-object`：在有界期间只从指定 namespace、selector、无分页 ControllerRevision List 中隐藏精确 name/UID/controller-owner 的对象，保留其他对象和集合元数据。它不修改存储对象、按名 GET 或 runner 的直接 Watch。随后一次 GET404 打开实际创建竞态，暂停的 POST 必须转发给真实 API 并得到原生 AlreadyExists；缺少任一命中均无冲突覆盖。`scripts/check-list-omission-kind.py` 验证代理本身，不计目录用例。

## 判定与报告

语义决定预期：需要清理的旧资源未清理就是行为问题，不能因为 `OnPodDelete` 是空操作或清理位于 `OnRoleDelete` 而豁免。实际调用路径用来证明注入是否有效，不能代替资源生命周期的正确性要求。

原始 `result.json`、连续 List/Watch、真实请求与响应、Pod/CR UID、版本及时间窗口由 `scripts/audit-*.py` 独立复核。超预算、错误删除次序或实际不收敛都保留为产品失败；runner 注入或判定问题允许修正后在新的 attempt 补充验证。已确认的有效产品失败不通过重跑覆盖。复合用例在早期失败时，未执行的后续重启、恢复或稳定阶段没有覆盖信用。

三个分类报告位于 `artifacts/category1-final-verification/`、`artifacts/category2-final-verification/` 和 `artifacts/category3-final-verification/`。第二类汇总由 `cases/category2-verification-manifest.json` 固定独立审计 SHA256，`scripts/summarize-category2.py` 检查全部唯一 ID，保留早期 runner 与源准备发现，拒绝缺项、待复核结果及重复有效结论。各分类报告中的早期阶段状态按原生成时点保留，以总报告为最终验收状态。

第三类使用 `cases/category3-verification-manifest.json` 与 `scripts/summarize-category3.py`，额外核对不可变构建、实际镜像/二进制和完整环境恢复。三个分类报告均完成后，`scripts/summarize-expanded-suite.py` 才可输出 `artifacts/expanded-suite-final-verification/`；它要求 708 个唯一结论、统一 production 基线和最终环境核对，不把未命中或 runner 中止算成 PASS/产品 FAIL。

失败独立保存于 issues 任务 022 的 `FAILURES_CATEGORY_1.md`、`FAILURES_CATEGORY_2.md`、`FAILURES_CATEGORY_3.md`。各项报告中的 limitation 说明实际覆盖范围；有界超时证明观测窗口内未收敛，不自动证明永久泄漏或唯一内部根因。

## 本地门禁和覆盖限制

Go 修改使用主机工具链与默认缓存，运行 `go test ./...`、`go vet ./...` 及相关 race 检查；本地门禁不能代替真实 Kind。汇总完整性测试可运行 `python3 -m unittest discover -s scripts -p 'test_summarize_*.py'`。

工作负载为受控 Ready 的轻量测试替身，不代表真实 GPU、模型、吞吐或节点拓扑验证。目录中的请求参数、长 Terminating、worker 和故障场景各按具体用例覆盖，不把旧 60 项的 W=0、1 秒宽限期限制套用到全部扩展场景，也不由单元测试推断 Kind 覆盖。
