# Kthena rollout runner

runner 在已有 Kthena controller 的测试集群中逐例执行 ModelServing 滚动场景。它使用 Kubernetes Job 运行，按结构化断言自动给出 PASS、FAIL、INCONCLUSIVE 等结论。Job 结束后，离线命令从导出的 JSON 生成逐例故障报告。

**从这里开始：**[使用指南](docs/USER_GUIDE.md)只讲构建镜像、下发 Job、取回和定位结果；[开发指南](docs/DEVELOPER_GUIDE.md)讲如何增加或修改 case。

**行为对照：**[滚动预期与 API reference](docs/specs/README.md)保存此前梳理的原文、适用范围和已知差异。Kthena 主仓的 ModelServing 修复必须核对这些规范；核心预期的修改需要人工明确批准具体差异，不能把过程讨论自动写入规范。

**分支：**`main` 承载完整实现及修复，包括阻塞与组合滚动用例、Job 生成和离线报告。不再维护 `legacy` 兼容分支；旧提交仅通过 `archive/legacy-20261005` tag 追溯。

## 目录

| 路径 | 用途 |
| --- | --- |
| `cases/<目录>/*.yaml` | 独立用例输入和预期；构建时整个 `cases/` 被复制到镜像的 `/cases` |
| `cmd/rollout-runner/`、`internal/runner/` | 执行器、List/Watch 观察、过程及最终判定 |
| `cmd/fault-proxy/`、`internal/faultproxy/` | 故障注入代理；单独构建、单独部署 |
| `scripts/render_job.py` | 用镜像、目录、选中 ID 等参数生成一次 Job |
| `scripts/render_fault_proxy.py` | 指定代理镜像地址，生成可选的代理 Pod/Service |
| `scripts/report_attempt.py` | 无集群连接地生成逐例 JSON/Markdown 故障报告 |
| `deploy/runner.yaml`、`deploy/production-fixture.yaml` | 可复用的权限、结果 PVC/reader 和测试 fixture |
| `artifacts/<run-id>/` | 本地导出的原始结果；被 Git 忽略，不会被清理脚本删除 |

一次 Job 只读一个 `--cases=/cases/<目录>`；`--select` 选择该目录内的 ID，省略则执行整个目录。`suite.json` 是索引，runner 不把它当成用例。修改 YAML 后须重新构建 runner 镜像。

之前的 ServingGroup 组合滚动 35 项在 `cases/servinggroup-compound-v2/`（RUN-618～652），其匹配的 controller commit、Job 示例和判定边界见 [组合用例说明](docs/COMPOUND_V2.md)。

报告保留原始状态：**PASS** 为通过，**FAIL** 为用例违约，**INCONCLUSIVE** 为证据不足，**ERROR** 为执行错误，**NOT_RUN** 为未执行。缺失文件和互相矛盾的结果会显示为 `MISSING_RESULT` 或 `EVIDENCE_CONFLICT`，不会被算作 PASS。
