# Runner 使用指南

本指南面向在专用 Kind 开发集群中验证 Kthena `ModelServing` 滚动更新的使用者。runner 会创建、更新和删除测试用的 namespace 与工作负载；`deploy/runner.yaml` 还会给 Job 的 ServiceAccount 绑定 `cluster-admin`。先确认 `kubectl` 指向可破坏的测试集群。

## 选择什么来运行

`main` 保存完整用例与正常流程起止 ordinal 检查；`legacy` 保留低版本兼容契约。一次 runner 进程只读取一个 `--cases` 目录，并按 ID 串行执行；默认值 `cases/core` 只是最早的 60 项，不代表全部 708 项。

| 目的 | 用例目录 | 例子与附加条件 |
| --- | --- | --- |
| 基础预算回归 | `cases/core` | 原 RUN-001～060；`cases/normal` 已包含同样 ID |
| 正常流程 | `cases/normal` | RUN-001～303；选 RUN-001 入门，选 RUN-061 看 v2 多阶段输入 |
| 故障恢复 | `cases/recovery` 及其他按故障命名的目录 | RUN-304～539；v3/v4 需要故障代理、控制器路由和独立审计 |
| 边界与拒绝 | `cases/boundary-*`、`cases/history-equal`、`cases/rejection` | RUN-540～611、DENY-001～097；拒绝类检查实际 API 拒绝及旧状态保持 |

`cases/` 中的 YAML 总数高于 708，因为 `core` 与 `normal` 重复前 60 项，且某些 ID 有准备方式修正目录。不要把文件数、某一目录的 `suite.json` 或旧 Job 的 PASS 相加当成当前有效覆盖；完整目录和证据口径见 [完整用例集](EXPANDED_SUITE.md)。历史 708 项结果发生在新增正常起止 ordinal 检查之前，当前契约见 [序号验证](ORDINAL_VERIFICATION.md)。

## 从源码到一个 Kind Job

以下以 `main` 的 `cases/normal/RUN-001.yaml` 为例。需要 Go（版本见 `go.mod`）、Docker、Kind、kubectl，以及已经部署并 Ready 的 Kthena controller、ModelServing CRD 和 Volcano。runner 仓库不负责安装后两者。示例沿用原验证集群 `kthena-resync-010`；使用自己的集群时先替换集群名和被测控制器 commit。

```sh
cd /path/to/kthena-rollout-runner
export KIND_CLUSTER=kthena-resync-010
kind get clusters
kind export kubeconfig --name "$KIND_CLUSTER" --kubeconfig /tmp/rollout-runner-guide.kubeconfig
export KUBECONFIG=/tmp/rollout-runner-guide.kubeconfig

kubectl config current-context
kubectl -n kthena-system rollout status deployment/kthena-controller-manager
kubectl -n volcano-system get deployments
kubectl get crd modelservings.workload.serving.volcano.sh
kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}'
```

记录实际被测控制器镜像及其源码 commit。`--controller-image` 会与 Deployment 的容器镜像精确比较；`--controller-commit` 则写入证据，必须来自所部署镜像的构建记录，不能随意取本地 `kthena` 工作树的 HEAD。以下 commit 是仓库历史 production 验证基线，仅当集群确实运行此版本时使用。

```sh
export CONTROLLER_IMAGE="$(kubectl -n kthena-system get deployment kthena-controller-manager -o jsonpath='{.spec.template.spec.containers[0].image}')"
export CONTROLLER_COMMIT=538b2825c06bc1e8c5392d18f18f84faee9fca95
export NODE_ARCH="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')"
export RUNNER_IMAGE=kthena-rollout-runner:guide-normal-001

make test
make build GOARCH="$NODE_ARCH"
docker build --platform "linux/$NODE_ARCH" -f Dockerfile -t "$RUNNER_IMAGE" .
kind load docker-image "$RUNNER_IMAGE" --name "$KIND_CLUSTER"
```

`make build` 产生 Linux 二进制 `bin/rollout-runner`。默认 `Dockerfile` 将它和整个 `cases/` 复制进镜像。`make image GOARCH=... IMAGE=...` 是本机与 Kind 节点同架构时的捷径；跨架构时使用上面的显式 `docker build --platform`。构建上下文应为仓库根目录；故障代理需要时再单独使用 `Dockerfile.fault-proxy`。

安装 runner 的测试权限、结果 PVC/reader 和 ranktable fixture：

```sh
kubectl apply -f deploy/production-fixture.yaml
kubectl apply -f deploy/runner.yaml
kubectl -n rollout-runner get pvc rollout-results
kubectl -n rollout-runner wait --for=condition=Ready pod/rollout-results-reader --timeout=120s
```

为这次执行创建唯一 Job 与 `--run-id`。先在实际环境核对上面的镜像和 commit，再执行：

```sh
export RUN_ID="guide-$(date -u +%Y%m%d%H%M%S)"
export JOB_NAME="rollout-$RUN_ID"
cat > "/tmp/$JOB_NAME.yaml" <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: $JOB_NAME
  namespace: rollout-runner
spec:
  backoffLimit: 0
  activeDeadlineSeconds: 1800
  template:
    spec:
      serviceAccountName: rollout-runner
      restartPolicy: Never
      containers:
        - name: runner
          image: $RUNNER_IMAGE
          imagePullPolicy: IfNotPresent
          args:
            - --cases=/cases/normal
            - --select=RUN-001
            - --artifacts=/artifacts
            - --run-id=$RUN_ID
            - --controller-image=$CONTROLLER_IMAGE
            - --controller-commit=$CONTROLLER_COMMIT
          resources:
            requests: {cpu: 100m, memory: 128Mi}
            limits: {memory: 1Gi}
          volumeMounts:
            - {name: results, mountPath: /artifacts}
      volumes:
        - name: results
          persistentVolumeClaim:
            claimName: rollout-results
EOF
kubectl create -f "/tmp/$JOB_NAME.yaml"
kubectl -n rollout-runner logs -f "job/$JOB_NAME" --pod-running-timeout=2m
```

日志流退出后仍要检查 Job 和 runner Pod 的终态。产品问题会使某个 case 为 FAIL，runner 退出 1，Job 为 Failed；这也是有效的失败证据。`backoffLimit: 0` 避免自动再跑。

```sh
kubectl -n rollout-runner get "job/$JOB_NAME" -o yaml
kubectl -n rollout-runner get pods -l "job-name=$JOB_NAME" -o wide
mkdir -p artifacts
kubectl -n rollout-runner cp "rollout-results-reader:/artifacts/$RUN_ID" "./artifacts/$RUN_ID"
cat "artifacts/$RUN_ID/summary.json"
cat "artifacts/$RUN_ID/RUN-001/result.json"
```

PVC 和 reader 留给结果导出；不要在导出前删除 `rollout-runner` namespace。重跑时换 Job 名、`--run-id` 和镜像 tag，不覆盖旧 attempt。一个集群中串行运行 suite，尤其不能同时运行会重启控制器或安装故障规则的用例。长 Job 需要足够的 `activeDeadlineSeconds`，Kind 宿主机保持唤醒；Job 条件、Pod 退出码和 `summary.json` 必须一致。

也可以从宿主机执行同一二进制。此方式仍会改动所连接的集群，且须显式传 `--kubeconfig`，因为空值只尝试 in-cluster ServiceAccount：

```sh
go run ./cmd/rollout-runner \
  --kubeconfig="$KUBECONFIG" \
  --cases=cases/normal \
  --select=RUN-001 \
  --controller-image="$CONTROLLER_IMAGE" \
  --controller-commit="$CONTROLLER_COMMIT" \
  --run-id="local-$(date -u +%Y%m%d%H%M%S)"
```

## 如何理解一个用例和结果

先看 `cases/normal/RUN-061.yaml`：`scenario.source` 保存矩阵来源与自然语言目标；可执行部分是 `initialSpec` 和顺序排列的 `steps`。每步的 `action` 是实际 API/故障动作，`spec` 或 `patch` 是请求，`until`、`release`、`holdSeconds`、`stableSeconds` 与 `expect` 定义停点和可观测预期。`RUN-001` 属于早期 v1：`input.spec` 是 A 基线，`update` 指明 A→B，`expect` 给出预算、partition、启动顺序与终态，`process` 控制 Ready 放行。`DENY-001` 属于拒绝类 v4：期望非法请求被 API 拒绝，原 Spec 和 Pod UID 保持。`suite.json` 是来源索引；runner 实际加载当前目录的 `RUN-*.yaml`、`DENY-*.yaml`，不会运行 `suite.json`，也不会递归扫描子目录。

一次 attempt 保存在 `artifacts/<run-id>/`：

| 文件 | 用途 |
| --- | --- |
| `environment.json` | 实际控制器、集群、CRD、runner 构建信息与用例 SHA256 |
| `summary.json`、`junit.xml` | 本次选中数、逐例状态与 CI 汇总 |
| `<ID>/case.yaml`、`before.yaml`、`after.yaml` | 实际装载输入与原始前后请求；不同动作还会保存请求/响应 |
| `<ID>/observations.jsonl`、`checkpoint-*.json` | 连续 List/Watch 与停点证据 |
| `<ID>/result.json` | PASS/FAIL/ERROR/INCONCLUSIVE、违规、时间线与清理结果 |

PASS 需要过程与终态都满足；FAIL 保留实际行为问题；INCONCLUSIVE 表示观察或故障命中不足；ERROR 是执行错误；清理失败会使后续用例为 NOT_RUN。不要把 FAIL、INCONCLUSIVE、ERROR、NOT_RUN 折算为成功。正常流程的 `ordinalContract` 说明起止序号检查是否启用；中途合法 surge 可以暂时出现额外序号。具体预算与观察语义见 [执行契约](CONTRACT.md)、[正常流程](NORMAL_SUITE.md)。

## 运行更多用例

同一目录可以在新 Job 中把 `--select` 改为逗号分隔的多个 ID；省略它会运行该目录全部用例。运行 303 项正常流程需要按 [正常流程指南](NORMAL_SUITE.md) 调整阶段超时、Job deadline，并按相同候选镜像导出每个分片。708 项是跨目录、带独立故障准备和审核的验收结果，不存在一个 `--cases` 值能直接跑完；[完整用例集](EXPANDED_SUITE.md)列出目录、历史执行清单、独立审计和最终汇总方式。

v3/v4 故障目录不能只把上面的 `--cases` 改成 `recovery`。runner 的预检查要求 `--fault-proxy-api`、`--fault-proxy-control`、`--fault-proxy-token-file`，代理无活动旧规则，且 controller 实际挂载的 kubeconfig 通过带 CA 的 HTTPS 指向该代理。代理进程由 `cmd/fault-proxy` 构建，`Dockerfile.fault-proxy` 打包；`deploy/fault-proxy-probe-r*.yaml` 和 `scripts/run-history-kind.py` 记录了历史集群的布置与审计方式，固定旧名称、证书、镜像及环境，不是可原样套用的新集群初始化器。建立新故障环境时先按 [完整用例集](EXPANDED_SUITE.md#本轮-kind-编排)核对代理、控制器路由、原生请求命中、恢复及证据，再启动唯一 Job。
