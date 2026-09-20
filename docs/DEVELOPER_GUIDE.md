# Runner 用例开发指南

这里解释如何**增加或调整 case**。镜像构建、Job 下发和结果读取在[使用指南](USER_GUIDE.md)。runner 的判定来自 YAML 中明确的动作与预期，以及 `internal/runner/` 的过程校验；自然语言描述本身不会被执行。

## 找到用例与执行代码

```text
cases/<suite>/RUN-NNN.yaml       一项独立场景；拒绝类为 DENY-NNN.yaml
cases/<suite>/suite.json         来源/映射索引，runner 不执行此文件
scripts/generate-*.py            部分 suite 的生成器
internal/runner/case.go          加载、格式和字段校验
internal/runner/normal_spec.go   阶段场景校验与预算解析
internal/runner/normal_run.go    阶段动作、停点与结果
internal/runner/normal_ledger.go 过程账本和违规锁存
internal/runner/observer.go      连续 List/Watch 原始记录
internal/runner/run.go           预检查、逐例执行、结果和清理
```

一个进程只加载一个 `--cases` 目录下的 `RUN-*.yaml` 与 `DENY-*.yaml`，不递归扫描。文件名和 `id` 必须一致，ID 不可重复。runner 镜像的 `Dockerfile` 把整个 `cases/` 复制到 `/cases`；调整 YAML 后需要重建并重新部署镜像，旧 Job 与旧 attempt 不能覆盖。

## 增加或修改一例

1. 在 workspace 的 `issues/` 中确认设计来源、前置条件、操作、允许轨迹、禁止轨迹和终态。先判断已有 case 是否覆盖；不要只因最终组数相同就复用另一个场景。
2. 选择合适 suite 和未使用 ID。优先复制同格式、同执行 profile 的邻近 case。`rollout-runner/v1` 使用 `input/update/expect/process`；`v2`、`v3`、`v4` 使用 `scenario.initialSpec` 与逐步 `steps[]`；拒绝类还必须校验实际 API 拒绝和旧状态保持。若 suite 有 `scripts/generate-*.py`，修改生成器/来源后重新生成，不能只手改生成结果。
3. 每个 `steps[]` 写清实际 `action`、请求 `spec` 或 `patch`、`until` 条件、`release`、稳定窗口与 `expect`。对在途/故障源，要求实际 UID、状态、请求或代理命中；未建立源应是 INCONCLUSIVE，不能算 PASS。需要新的过程不变量时，在 `internal/runner/` 添加校验，且让违例锁存到最终结果。
4. 核对 `baseline` 是被测 controller 镜像对应的源码 commit。`scenario.source`、`designID` 和 `suite.json` 记录可追溯来源，但执行依据仍是 YAML 可执行字段。保留原始请求中省略、null、空对象的区别；不要把 controller 的实现算法复制成预期 oracle。
5. 增加针对关键允许/禁止轨迹的定向测试，至少验证新增约束能拒绝错误行为。然后运行：

```sh
go test ./internal/runner/... -run '<相关测试名>'
make test
git diff --check
```

`make test` 包含 `go test ./...`、race 测试和 `go vet ./...`。修改 API `_types.go` 属于 Kthena controller 工作流，不能在 runner 中手改生成文件。新源码保持仓库现有 Apache 2.0 文件头。

## 在真实集群复核

用[使用指南](USER_GUIDE.md)构建新的 runner 镜像 tag，在已有 controller 的专用 Kind/Kubernetes 集群用新的 Job 名与 run ID 执行新增/修订的 ID。先核对节点架构、controller 镜像 ID 与源码 commit、CRD、Volcano 和 fixture。故障类还要核对代理镜像、TLS/控制 Secret、controller 的实际代理路由与命中 trace。只跑 Go 单测不能证明真实控制器行为。

Job 结束后导出 `artifacts/<run-id>/`、`job.json`、`runner-pod.json`，用 `scripts/report_attempt.py` 离线生成每例报告。对 FAIL 查看 `<ID>/result.json` 中的违规代码，再追 `checkpoint-*.json` 和 `observations.jsonl`；对 INCONCLUSIVE 查源状态、观察缺口或代理命中。过程违规不能因最后看起来健康而改成 PASS。保留失败 attempt 原样；修订 runner 后用新 run ID 复跑。`artifacts/` 被 Git 忽略，历史原始证据不随仓库跟踪文件清理而删除。

若一次变更跨多个 suite，分别执行并报告；不同目录可能含同一 ID 的不同准备方式，不能把文件数直接相加当成唯一用例数。按 workspace `AGENTS.md` 的 proposal、批准、Kind 验证、签名提交和 issue 归档流程交付。
