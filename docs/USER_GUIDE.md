# Runner 使用指南

本指南只处理一次测试的三件事：构建镜像、在**已部署且 Ready 的 `kthena-controller-manager`** 集群下发 Job、离线查看结果。runner 会创建和删除测试 namespace；`deploy/runner.yaml` 给测试 ServiceAccount 绑定 `cluster-admin`，请使用专用测试集群。集群还需要 ModelServing CRD 和 Volcano。

## 1. 构建镜像

在 runner 仓库根目录执行。`Dockerfile` 把 `bin/rollout-runner` 和整个 `cases/` 拷入镜像，Job 中的 YAML 路径为 `/cases/<目录>/RUN-*.yaml` 或 `DENY-*.yaml`；**运行 Job 不需要再挂载 YAML**。修改 case 后重新构建镜像并使用新 tag。

```sh
NODE_ARCH="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')"
RUNNER_IMAGE=registry.example.com/kthena-rollout-runner:my-run-001
make image GOARCH="$NODE_ARCH" IMAGE="$RUNNER_IMAGE"
docker push "$RUNNER_IMAGE"
```

如果是 Kind 且只使用本机镜像，先设置 `KIND_CLUSTER` 为实际 Kind 集群名，再用 `kind load docker-image "$RUNNER_IMAGE" --name "$KIND_CLUSTER"` 代替 `docker push`，下发 Job 时保持 `IfNotPresent`。镜像架构必须与节点一致。可先用 `python3 scripts/render_job.py --help` 查看全部可调参数。

故障类 case 还使用**单独**的 fault-proxy 镜像；它不包含在 runner Job 镜像里。镜像地址在生成代理清单时显式指定：

```sh
FAULT_PROXY_IMAGE=registry.example.com/kthena-fault-proxy:my-run-001
make fault-proxy-image GOARCH="$NODE_ARCH" FAULT_PROXY_IMAGE="$FAULT_PROXY_IMAGE"
docker push "$FAULT_PROXY_IMAGE"
```

## 2. 在已有 controller 的集群下发 Job

先核对当前 kubectl 集群、controller 镜像，并从该镜像的构建记录取得**真实源码 commit**。case 的 `baseline` 也必须与被测 commit 一致；runner 会拒绝不匹配的输入。

```sh
kubectl config current-context
kubectl -n kthena-system rollout status deployment/kthena-controller-manager
CONTROLLER_IMAGE="$(kubectl -n kthena-system get deployment kthena-controller-manager -o jsonpath='{.spec.template.spec.containers[0].image}')"
CONTROLLER_COMMIT="替换为这个镜像对应的源码commit"
kubectl apply -f deploy/runner.yaml
kubectl apply -f deploy/production-fixture.yaml
kubectl -n rollout-runner wait --for=condition=Ready pod/rollout-results-reader --timeout=120s
```

以下以镜像内 `cases/normal/RUN-001.yaml` 为例；它的 `baseline` 必须与上面的 `CONTROLLER_COMMIT` 一致，否则请换成匹配当前 controller 的 case。`--case-dir` 只能选择一个镜像内目录；`--select` 可写多个逗号分隔 ID，省略则执行该目录全部 case。每次使用新的 run ID 和 Job 名，避免覆盖旧结果。按实际用例数量调整 Job deadline 和阶段 timeout。

```sh
RUN_ID="test-$(date -u +%Y%m%d%H%M%S)"
JOB_NAME="rollout-$RUN_ID"
python3 scripts/render_job.py \
  --runner-image "$RUNNER_IMAGE" \
  --controller-image "$CONTROLLER_IMAGE" \
  --controller-commit "$CONTROLLER_COMMIT" \
  --case-dir normal --select RUN-001 \
  --run-id "$RUN_ID" --job-name "$JOB_NAME" \
  --active-deadline-seconds 3600 \
  --out "/tmp/$JOB_NAME.json"
kubectl create -f "/tmp/$JOB_NAME.json"
kubectl -n rollout-runner logs -f "job/$JOB_NAME" --pod-running-timeout=2m
kubectl -n rollout-runner get "job/$JOB_NAME"
```

日志结束不代表 Job 成功：case 为 FAIL 时 runner 应退出 1，Job 会是 Failed；这仍是一份有效失败证据。`backoffLimit: 0` 不会自动重试。一次只执行一个会修改 controller 或代理环境的故障 Job。

**故障类 case 的可选准备。** 提前在 `rollout-runner` namespace 创建 key 为 `token` 的控制 Secret，以及 `tls.crt`/`tls.key` TLS Secret，证书应覆盖代理 Service DNS。指定镜像生成并部署代理；`FAULT_PROXY_NAME` 必须是本轮唯一名称：

```sh
FAULT_PROXY_NAME=fault-my-run-001
python3 scripts/render_fault_proxy.py \
  --proxy-image "$FAULT_PROXY_IMAGE" --name "$FAULT_PROXY_NAME" \
  --token-secret fault-control --tls-secret fault-tls \
  --out "/tmp/$FAULT_PROXY_NAME.json"
kubectl apply -f "/tmp/$FAULT_PROXY_NAME.json"
kubectl -n rollout-runner wait --for=condition=Ready "pod/$FAULT_PROXY_NAME" --timeout=120s
```

controller 必须事先通过带 CA 的 kubeconfig 访问 `https://$FAULT_PROXY_NAME.rollout-runner.svc:8080`；runner 的预检查会核对实际挂载的 kubeconfig、CA、代理地址和 tokenFile。仅指定代理镜像或启动 Pod **不会**让 controller 流量经过代理。配置好后，以故障用例目录重新生成 Job：

```sh
python3 scripts/render_job.py \
  --runner-image "$RUNNER_IMAGE" \
  --controller-image "$CONTROLLER_IMAGE" \
  --controller-commit "$CONTROLLER_COMMIT" \
  --case-dir recovery --select RUN-305 \
  --run-id "$RUN_ID" --job-name "$JOB_NAME" \
  --fault-proxy-api "https://$FAULT_PROXY_NAME.rollout-runner.svc:8080" \
  --fault-proxy-control "http://$FAULT_PROXY_NAME.rollout-runner.svc:8081" \
  --fault-proxy-token-secret fault-control \
  --out "/tmp/$JOB_NAME.json"
```

使用新的 run ID/Job 名创建此清单，随后 `kubectl create -f "/tmp/$JOB_NAME.json"`。导出结果前另存代理 `/evidence/trace.jsonl` 和 controller 原配置/恢复证据；故障测试的代理命中需按其 case 契约检查。

## 3. 导出、看结果、定位失败

Job 到达 Complete 或 Failed 后，先导出 runner 的 attempt 目录，再导出同一个 Job 和 Pod 的终态 JSON。下面命令不修改集群。PVC/reader 在导出前不要删除。

```sh
mkdir -p artifacts
kubectl -n rollout-runner cp "rollout-results-reader:/artifacts/$RUN_ID" "artifacts/$RUN_ID"
POD_NAME="$(kubectl -n rollout-runner get pods -l "batch.kubernetes.io/job-name=$JOB_NAME" -o jsonpath='{.items[0].metadata.name}')"
kubectl -n rollout-runner get "job/$JOB_NAME" -o json > "artifacts/$RUN_ID/job.json"
kubectl -n rollout-runner get "pod/$POD_NAME" -o json > "artifacts/$RUN_ID/runner-pod.json"
python3 scripts/report_attempt.py --attempt "artifacts/$RUN_ID"
```

最后一条命令只读本地 JSON，**不连接 Kubernetes，也不调用 AI**。它总会先写 `failure-report.json` 与 `failure-report.md`；全通过且 Job/Pod 证据一致时退出 0，否则退出 1。输入无法解析时退出 2。打开 `failure-report.md` 先看整体结论，再看每例的状态、原因和 `result.json` 链接。`INCONCLUSIVE`、`ERROR`、`NOT_RUN`、`MISSING_RESULT`、`EVIDENCE_CONFLICT` 都是未通过，但不会伪装成产品 FAIL。

定位时按报告中的 ID 查 `artifacts/$RUN_ID/<ID>/`：

| 文件 | 看什么 |
| --- | --- |
| `result.json` | runner 最终状态、错误、违规代码、清理错误 |
| `case.yaml` | 这次实际执行的输入与预期 |
| `checkpoint-*.json` | 每阶段真实预算、Ready 与停点 |
| `observations.jsonl` | 按顺序保存的 Pod/PodGroup/ModelServing/Revision List/Watch |
| `before*.yaml`、`after*.yaml`、请求/响应文件 | 实际提交给 API 的配置与回执 |
| 根目录 `environment.json` | controller 镜像、runner 构建、case SHA 与集群身份 |
| 根目录 `execution-plan.json` | 连接集群前写出的选中用例和输入 SHA；预检查失败时也能列出缺失结果 |

报告会核对 `summary.json` 和逐例 `result.json`；结果缺失、重复或矛盾会明确显示，不需要人工读日志才知道哪一例没有结论。不要把本次报告与其他镜像、run ID 或旧目录的结果直接相加。
