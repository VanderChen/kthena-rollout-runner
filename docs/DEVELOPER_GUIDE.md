# Runner 开发指南

## 仓库入口和执行链

从 `main` 开发 runner；`legacy` 用于低版本兼容验证，正常完成后的 ordinal 契约不同。工作树中的 `bin/` 和 `artifacts/` 被 Git 忽略，前者是构建输出，后者是不可覆盖的尝试证据。

```text
cmd/rollout-runner/main.go       CLI 参数
internal/runner/case.go          按目录加载、严格解析并验证 case
internal/runner/run.go           预检查、串行执行、结果及 namespace 清理
internal/runner/normal_*.go      v2/v3/v4 阶段、预算、终态与动作
internal/runner/observer.go      List/Watch 原始观察
cmd/fault-proxy/                 独立的 API 故障代理进程
internal/faultproxy/             代理规则、重放、遗漏与 trace
cases/                           自包含 YAML 用例与来源索引
scripts/generate-*.py            从已审阅矩阵生成用例
scripts/audit-*.py               独立检查历史 Kind 原始证据
scripts/summarize-*.py           分类/全量报告完整性检查
deploy/                          已运行的 Job、fixture 和代理部署记录
```

`LoadCases` 只读取传入目录中的 `RUN-*.yaml` 与 `DENY-*.yaml`，按文件名排序、拒绝重复 ID 或不支持的输入。`Run` 先建立唯一结果目录，再检查 controller 镜像、Ready、CRD、Volcano 与 fixture；v3/v4 还检查故障代理。每例先记录基线与原始观察，然后执行请求、持续断言并按 UID 清理自己的 namespace。过程违规会锁存；最终健康状态不能抹去违例。`summary.json` 和 JUnit 随执行更新，清理失败阻止后续执行。

## 镜像构建入口与历史配方

根目录只保留两个 Dockerfile。`Makefile image` 调用默认 `Dockerfile`；故障代理从当前源码单独构建后使用 `Dockerfile.fault-proxy`。

| 文件 | 用途 |
| --- | --- |
| `Dockerfile` | 当前通用 runner：`bin/rollout-runner` + 整个 `cases/`；新执行优先使用 |
| `Dockerfile.fault-proxy` | 当前源码构建出的 `bin/fault-proxy`；供 v3/v4 故障注入另行部署 |

此前根目录的 41 个 `Dockerfile.*` 是 022 扩展测试期间的专项或迭代镜像配方，各自 `COPY` 当时命名的 `bin/rollout-runner-*` 或 `bin/fault-proxy-r*`，有的只带一个场景目录。它们未被当前构建脚本调用，且 `bin/` 不纳入 Git，不能从干净克隆直接重建对应旧镜像。清理后仍可用 `git show 376a38a:Dockerfile.history-fixed` 这类命令读取原文；精确文件清单见 issues 任务 035 的 `PROPOSAL_COMMIT.md`。历史 `deploy/*.yaml`、镜像身份、二进制 SHA256 和原始结果仍按原样保存。新实验使用当前入口和新的 Job、镜像 tag 与 run ID；复核旧候选时需同时核对当时源码、二进制、镜像与用例输入。

## 构建、测试和架构

从仓库根目录运行。Go 最低版本由 `go.mod` 指定；使用主机 Go 工具链与缓存。

```sh
make test
make build GOARCH=arm64
docker build --platform linux/arm64 -f Dockerfile -t kthena-rollout-runner:my-build .
```

`make test` 执行 `go test ./...`、`go test -race ./...` 和 `go vet ./...`。`GOARCH` 应取实际 Kind 节点架构：

```sh
kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}'
```

`make image GOARCH=<架构> IMAGE=<tag>` 是同架构主机的简写；跨架构时先 `make build`，再显式设置 `docker build --platform linux/<架构>`。完整 Job 演示见 [使用指南](USER_GUIDE.md#从源码到一个-kind-job)。故障代理从当前源码单独构建：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/fault-proxy ./cmd/fault-proxy
docker build --platform linux/arm64 -f Dockerfile.fault-proxy -t kthena-fault-proxy:my-build .
```

构建代理镜像本身不会建立故障环境。必须另外配置 TLS 证书、私有控制 token、追加式 trace、代理 Pod/Service、controller 的代理 kubeconfig 和 runner 的三个 `--fault-proxy-*` 参数。`preflightRecovery` 会核对 controller 实际挂载配置与代理地址；注入还需从原生请求、命中和恢复证据独立审核。历史 `scripts/run-history-kind.py` 固定特定集群文件、证书、控制器 spec 和 UID，不能当作新集群的通用编排脚本。

## 读、改和增加用例

源矩阵位于相邻 workspace 的 `issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json`。目录 YAML 是提交后可自包含执行的输入，不要求运行时读取 `issues/`。`suite.json` 记录来源与旧阶段元数据，runner 不执行它；审计时以具体 YAML、最终 manifest 和真实证据为准。

| 格式 | 范围 | 主要字段 |
| --- | --- | --- |
| `rollout-runner/v1` | 原 `core` 60 项，亦在 `normal` 前 60 项 | `input.spec`、`update`、`expect`、`process` |
| `rollout-runner/v2` | 正常流程扩展及部分数值边界 | `scenario.source`、`profile`、`initialSpec`、`steps` |
| `rollout-runner/v3` | 故障恢复和部分边界 | v2 阶段结构，加故障动作与代理证据 |
| `rollout-runner/v4` | `DENY-001`～097 | 拒绝请求与旧状态保持检查 |

以 `cases/normal/RUN-061.yaml` 为例，`scenario.source` 是审阅者理解目标的来源描述，不由 runner 解释成动作；`initialSpec` 和每个 `steps[].action/spec/patch/until/release/expect` 才是执行契约。以 `RUN-001` 为例，v1 的 `expect.maxUnavailable/maxSurge/partition/startOrder` 明确预算与次序。拒绝类 `DENY-001` 必须观察真实 API 响应，而不是只看终态。修改时保留省略、`null`、空对象的区别，以及原始请求和 defaulted server 对象的区别。

新增/修订目录用例时先更新并审阅源矩阵和设计，再用对应 `scripts/generate-*.py` 显式 ID 映射生成，查看 YAML diff，核对唯一 ID、来源 SHA、期望和每个阶段。生成脚本可能依赖相邻的 `issues/` 目录；不要在没有该源时盲目重新生成。针对新的判定逻辑加入能拒绝错误轨迹的定向测试，运行 `make test`。不把 controller 的候选选择或预算函数导入 runner 作为 oracle；断言应来自目录语义和可观测资源。正常流程 `main` 的基线及每个 settled 完成阶段还必须满足 `0..replicas-1` 的 SG/Role ordinal；中途仍允许合法 surge。细节见 [正常流程契约](NORMAL_SUITE.md)和 [原 60 项契约](CONTRACT.md)。

## Kind 验证与证据维护

用例或判定代码变化需要真实 Kind 验证。运行前记录 cluster/节点架构、controller 实际镜像 ID 与源码 commit、CRD、Volcano、fixture、当前 Job/namespace。每批用新的镜像 tag、Job 名与 `--run-id`；先跑代表性单例，再按目录分片。进程一次只认一个目录，跨目录 708 项的有效结论由分类 manifest、独立 `scripts/audit-*.py` 和 `scripts/summarize-*.py` 汇总，不能用 `go test` 或文件数量替代。运行后导出结果目录、Job JSON、runner Pod JSON 与日志；核对选中 ID、全部结果、Job 条件、退出码、镜像 ID、二进制/用例 SHA、清理状态与代理命中。失败 attempt 原样保留，在新的 attempt 修复 runner 问题或复测产品行为。

独立审计的汇总测试可从仓库根目录运行：

```sh
python3 -m unittest discover -s scripts -p 'test_summarize_*.py'
```

已有 `artifacts/category*-final-verification/` 和 `artifacts/expanded-suite-final-verification/` 属于历史固定基线；[完整用例集](EXPANDED_SUITE.md)解释其审计链。2026-09-11 新增 ordinal 检查后的 10 项 Kind 补测及历史端点复核另见 [序号验证](ORDINAL_VERIFICATION.md)，不能把原 552 PASS/156 产品 FAIL 说成新版完整重跑。故障代理实验要额外保存原 controller spec、代理配置与 trace，并核对收尾恢复原 spec 和历史对象 UID。
