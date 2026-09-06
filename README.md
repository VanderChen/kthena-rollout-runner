# Kthena rolling-update runner

内部开发/CI工具：Go + client-go，一个Kubernetes Job，逐用例串行执行。

本次实现范围为 **RUN-001～RUN-060：30个SG + 30个Role基础配置组合**。
每例独立YAML，保留未配置、显式0及不同partition输入；不把同一有效预算的不同输入合并。
其余624个场景尚未实现，不能计作自动通过。源码无需依赖Kthena controller内部函数。

被测生产基线：`production/release-1.0@e2578d01859bb98d9a85846bafbfb2c771a6f117`。
实际验证使用Kind `kthena-resync-010`、Kubernetes v1.34.0、Linux/arm64、
Volcano v1.14.1、controller镜像
`kthena-controller-manager:resync-010-e2578d01`。
部署前核对实际镜像和架构；其他集群须预装相同API的Kthena及Volcano，不会由runner改换controller版本。

## 核心过程检查

A全Ready → 建立UID基线 → 提交B（从出生NotReady）→ 检查初始启动额度 →
持续hold → 每次只放行一个新SG/Role的全部所需Pod → 检查真实Ready与后续推进 →
最终容量、版本、PG和revision收敛 → 保存结果 → 清理本例namespace。

- 默认每个停点观察10秒，期间Watch和断言一直运行，不是只采样窗口末尾。
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

详见[配置与观察契约](docs/CONTRACT.md)。

## 构建和测试

需要本机Go（版本见go.mod）、Docker、Kind、kubectl。使用主机Go缓存：

```sh
make test

# 先确认实际Kind节点架构；不是所有集群都用arm64。
kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}'
make image GOARCH=arm64 IMAGE=kthena-rollout-runner:dev-021-r3
kind load docker-image kthena-rollout-runner:dev-021-r3 --name kthena-resync-010
```

`make test`执行`go test ./...`、`go test -race ./...`和`go vet ./...`。
检查器测试包含超预算删除、Ready信用撤销、surge合法信用、worker完整性、保护范围、
PG提前删除、Watch事件不合并、410观察缺口以及revision hash/name映射。

## Kind Job运行

下面的权限仅适用于可破坏的开发/CI集群。首次初始化：

```sh
kind export kubeconfig --name kthena-resync-010 --kubeconfig /tmp/rollout-runner-kind.kubeconfig
export KUBECONFIG=/tmp/rollout-runner-kind.kubeconfig

kubectl apply -f deploy/production-fixture.yaml
kubectl apply -f deploy/runner.yaml
kubectl apply -f deploy/core-job.yaml
kubectl -n rollout-runner logs -f job/rollout-core-60
kubectl -n rollout-runner get job rollout-core-60
```

`deploy/runner.yaml`创建CI ServiceAccount/ClusterRoleBinding、结果PVC和独立reader Pod；
`deploy/core-job.yaml`才启动60项Job。Job不自动重跑：`backoffLimit: 0`。
默认用例业务namespace名为`rr-<run-id>-run-NNN`，每例结束按UID删除并等待消失；
清理失败停止suite，未执行的后续行标NOT_RUN。不会清理其他已有工作负载。

重跑时在新的Job YAML中修改Job名及`--run-id`，保留旧attempt，不覆盖旧结果。
结果根目录已存在时runner直接拒绝运行。同一个开发集群不要并发运行两套suite。

导出当前attempt（先在本机创建artifacts目录）：

```sh
mkdir -p artifacts
kubectl -n rollout-runner cp rollout-results-reader:/artifacts/core-60-r3 ./artifacts/core-60-r3
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
这是`controlled-readiness`变体，不验证真实模型/GPU/RPC或节点拓扑调度能力。

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

当前是60个固定规模、健康初始态、无coordination的核心组合执行器，
不是684个场景的完整解释器。扩缩、故障恢复、依赖图、人工操作等需要新增对应
过程检查，不能只改数字后宣称已支持。Watch也不证明没有API副作用的controller内部选择；
那类检查仍需要专用trace/手工证据。

验证进度与最终验收证据见[VERIFICATION.md](VERIFICATION.md)。
