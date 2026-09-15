# Kthena rolling-update runner

内部开发/CI工具：Go + client-go，一个Kubernetes Job，逐用例串行执行。

执行集已扩展为 **708 个唯一用例**：正常流程 RUN-001～303、故障恢复 RUN-304～539、边界 RUN-540～611 和拒绝 DENY-001～097。
708 项均已完成 Kind 独立复核：第一类 266 PASS/37 产品 FAIL，第二类 128 PASS/108 产品 FAIL，第三类 158 PASS/11 产品 FAIL；合计 **552 PASS、156 项产品 FAIL**。Kthena production 源码未修改，失败与无效尝试保留。完整报告见 `artifacts/expanded-suite-final-verification/` 和 issues 任务 022 的 `FINAL_VERIFICATION.md`。
当前验证统一使用 Kthena production `538b2825c06bc1e8c5392d18f18f84faee9fca95`，未修改 Kthena 源码。
参见[完整用例集及证据复核](docs/EXPANDED_SUITE.md)和[303项执行契约和运行方式](docs/NORMAL_SUITE.md)。

当前为低版本兼容分支 `legacy`（原 `fix/032-runner-legacy-quality`）：正常完成端点允许 `012 → 123` 等非连续身份集合，仍检查精确副本数、合法且唯一身份、entry/worker 完整性及全部过程质量约束。结果通过 `ordinalContract` 标识兼容契约。全量检查及连续序号要求已合入 `main`，原保序分支及 `runner-canonical-20260915` 标签继续保留；[检查点](docs/CHECKPOINT_20260915.md)记录原提交和时间，[兼容契约及验证](docs/LEGACY_QUALITY.md)说明具体范围。

上面的 708 项统计是历史验收，本分支不会重写或自动转换旧 verdict。[历史序号补充验证](docs/ORDINAL_VERIFICATION.md)中的 86 项序号发现仍保留，但不属于本分支要求的连续编号标准。

原 `cases/core/` 的60份输入及历史验收结果保留不变。以下核心过程和旧Kind结果描述
对应原60项；新集群验证必须显式传入当前production镜像及 `--controller-commit`。

## 核心过程检查

A全Ready → 建立UID基线 → 提交B（从出生NotReady）→ 检查初始启动额度 →
持续hold → 每次只放行一个新SG/Role的全部所需Pod → 检查真实Ready与后续推进 →
最终容量、版本、PG和revision收敛 → 保存结果 → 清理本例namespace。

- 默认每个停点观察10秒，期间Watch和断言一直运行，不是只采样窗口末尾。
- B的任一Pod未经runner按UID放行就Ready，立即报CONTROL_VIOLATION；
  避免readinessProbe丢失等控制失效被误判为快速通过。
- Pod首次deletionTimestamp/Deleted即记录旧实例开始替换；多Pod按SG或Role去重。
- 旧Pod消失不会清除启动记录。健康固定规模替换检查
  `D - startedOld + currentTargetReady >= max(D-U, 0)`。
- 另查实际完整Ready容量、活动单位上限D+S、2→1→0的旧ordinal启动顺序、
  partition旧UID和未变backend UID。新surge创建不算旧替换启动。
- U=2只放开一个B就要求后续推进；不会误写成“整批两个都Ready才继续”。
- Role的entry Ready但worker未Ready不提供Role容量信用；单元测试覆盖此边界。
  当前60个目录用例自身W均为0，不把该单元测试称为W>0 Kind覆盖。
- PodGroup旧UID的删除是SG早期启动证据。与Pod流的精确Ready预算分开处理，
  防止跨资源通知乱序误报；PG启动仍不能超出U加显式放行容量的绝对上界。
- 终态检查PG owner、Pod关联、minMember/minResources，及MS observedGeneration、
  replicas/available/updated/current/update revision与受控历史引用。
- 过程失败锁存；终态健康或结束前最后一条Watch事件不能抹掉已发现的违例。
- Watch从List游标开始，断线仅尝试续传，无法连续恢复记INCONCLUSIVE，
  不用relist后的健康快照冒充遗漏过程安全。

详见[60项计数与过程预期](docs/CORE_EXPECTATIONS.md)和[配置与观察契约](docs/CONTRACT.md)。

## 构建和测试

需要本机Go（版本见go.mod）、Docker、Kind、kubectl。使用主机Go缓存：

```sh
make test

# 先确认实际Kind节点架构；不是所有集群都用arm64。
kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}'
make image GOARCH=arm64 IMAGE=kthena-rollout-runner:dev-021-r4
kind load docker-image kthena-rollout-runner:dev-021-r4 --name kthena-resync-010
```

`make test`执行`go test ./...`、`go test -race ./...`和`go vet ./...`。
本次镜像实测架构为linux/arm64。若构建主机与目标节点架构不同，除了GOARCH，
还需用`docker build --platform linux/<目标架构>`保证基础镜像与二进制架构一致。
检查器测试包含超预算删除、Ready信用撤销、surge合法信用、worker完整性、保护范围、
PG提前删除、Watch事件不合并、410观察缺口以及revision hash/name映射。

## Kind Job运行

下面的权限仅适用于可破坏的开发/CI集群。首次初始化：

```sh
kind export kubeconfig --name kthena-resync-010 --kubeconfig /tmp/rollout-runner-kind.kubeconfig
export KUBECONFIG=/tmp/rollout-runner-kind.kubeconfig

kubectl apply -f deploy/production-fixture.yaml
kubectl apply -f deploy/runner.yaml
kubectl apply -f deploy/core-job-r4b.yaml
kubectl -n rollout-runner logs -f job/rollout-core-60-r4b
kubectl -n rollout-runner get job rollout-core-60-r4b
```

`deploy/runner.yaml`创建CI ServiceAccount/ClusterRoleBinding、结果PVC和独立reader Pod；
`deploy/core-job-r4b.yaml`才启动60项Job；r3/r4 YAML保留早期attempt。
Job不自动重跑：`backoffLimit: 0`。
默认用例业务namespace名为`rr-<run-id>-run-NNN`，每例结束按UID删除并等待消失；
清理失败停止suite，未执行的后续行标NOT_RUN。不会清理其他已有工作负载。

重跑时在新的Job YAML中修改Job名及`--run-id`，保留旧attempt，不覆盖旧结果。
结果根目录已存在时runner直接拒绝运行。同一个开发集群不要并发运行两套suite。

本地Kind验证需保证宿主机/容器VM持续运行；主机休眠会计入Job的墙钟deadline，
而Go单调时钟统计可能不计休眠。一次实测就出现“用例60 PASS、Pod退出0、Job仍因
DeadlineExceeded失败”。验收必须同时检查用例结果和Job状态，不只读summary。
macOS可在验证期间临时使用`caffeinate -i`防止空闲休眠，验证后退出；不要改永久设置。

导出当前attempt（先在本机创建artifacts目录）：

```sh
mkdir -p artifacts
kubectl -n rollout-runner cp rollout-results-reader:/artifacts/core-60-r4b ./artifacts/core-60-r4b
```

PVC与reader不随Job结束删除，以便保留证据；不应在导出前删除rollout-runner namespace。
旧smoke的YAML和结果保留为实现阶段记录，不能与最终候选全量结果合并计数。

## 本地运行/选择子集

同一个二进制也支持本地client-go运行；这不替代本任务要求的Job验证：

```sh
go run ./cmd/rollout-runner \
  --kubeconfig /tmp/rollout-runner-kind.kubeconfig \
  --controller-image kthena-controller-manager:resync-010-e2578d01 \
  --select RUN-001,RUN-016,RUN-031 \
  --run-id local-check-01
```

主要参数：

| 参数 | 默认/含义 |
| --- | --- |
| --cases | cases/core；镜像里用/cases/core |
| --select | 空表示执行目录全部；否则严格匹配逗号分隔ID |
| --controller-image | 必填，预检查实际controller Deployment镜像 |
| --kubeconfig | 空使用Job ServiceAccount |
| --artifacts | artifacts，Job使用PVC上的/artifacts |
| --run-id | 默认UTC时间；指定时必须为新的独立attempt |
| --hold | 默认0，使用case声明的10秒；正数仅用于明确标记的不同观察窗口attempt |
| --phase-timeout | 每个应推进步骤180秒，不在超时后放宽预期 |

工作负载沿用production形态：Volcano、headless-service/ranktable、
busybox:1.36、每Pod请求5m CPU/4Mi memory。A/B只有frontend的ROLLOUT_VERSION改变；
Role模式backend仍为A。所有真实请求保存在before/after.yaml，可用于人工重放。
这些文件首先是原始请求证据；跨集群手工重放时需换namespace并清除resourceVersion等
旧集群metadata。通常直接复用cases/core配置交给runner重新渲染更稳妥。
这是`controlled-readiness`变体，不验证真实模型/GPU/RPC或节点拓扑调度能力。
该测试替身还将A/B的terminationGracePeriodSeconds统一设为1秒，缩短Kind中旧容器退出的等待；
这不修改滚动U/S/P，但本轮不能代替长时间Terminating/finalizer故障场景的覆盖。

## 报告与已知边界

每次attempt保存：

```text
environment.json          # 生产commit、集群架构/版本、controller实际imageID、CRD、Volcano
summary.json / junit.xml   # 选中/通过数和逐例结果
RUN-001/
  case.yaml
  before.yaml / after.yaml
  before-server.yaml / after-server.yaml
  baseline-resources.yaml / final-resources.yaml
  observations.jsonl      # 原始List/Watch，不是轮询抽样
  checkpoint-NN.json      # 每个hold后的单位、预算与启动集合
  release-NN.json         # 具体放行Pod UID与动作时间
  result.json             # 时间线、启动顺序、预算违例、检查点/放行数量、结果
```

PASS须过程和终态都通过，ERROR/INCONCLUSIVE/NOT_RUN都不折算成功。
原始journal可能含测试PodSpec及插件配置，不收集Secret对象或kubeconfig。

`cases/core` 保留60个固定规模核心组合；`cases/normal` 扩展第一类303项。
故障恢复、边界及拒绝场景另有专用执行契约和审计。不同目录可能含同一 ID 的准备方式修正，不能直接把目录行数相加作为有效覆盖。
Watch 不证明没有 API 副作用的控制器内部选择；故障场景还需按原生请求/响应、实际命中、源状态和解除后的恢复证据复核。

历史core Kind验收：RUN-001～RUN-060全部通过，Job Complete、退出码0。
详见[60行实测结果](docs/KIND_RESULTS.md)与[完整验收证据](VERIFICATION.md)。
