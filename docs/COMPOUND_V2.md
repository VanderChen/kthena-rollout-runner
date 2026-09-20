# ServingGroup 组合滚动 v2 用例

设计基准是 workspace issue 033 的
`SERVINGGROUP_COMPOUND_ROLLOUT_EXPECTATIONS_V2.md`。它描述期望行为，
不代表当前控制器已通过。`cases/servinggroup-compound-v2/` 的 35 个 case
分别保留 `designID`：S01～S07 对应 RUN-618～624，C01～C16 对应
RUN-625～640，P01～P11 对应 RUN-641～651，R01 对应 RUN-652。
用例输入由 `scripts/generate-servinggroup-compound-v2.py` 生成，
`TestCompoundCatalogueIsExecutable` 验证目录、ID 和设计映射。

本套件独立于历史 708 项与 RUN-612～617。`baseline` 固定被测候选源码
`6fea34e03a179b07686fd5415b83018e159b196b`；运行时仍需传入与真实
Deployment 镜像相符的 `--controller-image`，并显式传入同一
`--controller-commit`。不能将旧套件的 PASS 数量用于推断 v2 覆盖。

组合用例也通过通用 Job 入口运行；YAML 在构建镜像时复制到
`/cases/servinggroup-compound-v2`，无需另行挂载。先按
[使用指南](USER_GUIDE.md)核对集群、controller 镜像及架构、安装
`deploy/runner.yaml` 与 `deploy/production-fixture.yaml`，然后选择一个
匹配上述 baseline 的 case：

```sh
RUNNER_IMAGE=kthena-rollout-runner:compound-v2-001
NODE_ARCH="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')"
make image GOARCH="$NODE_ARCH" IMAGE="$RUNNER_IMAGE"
# Kind 使用 kind load docker-image "$RUNNER_IMAGE" --name "$KIND_CLUSTER"；
# 其他集群先把该镜像推到节点可访问的 registry。
CONTROLLER_IMAGE="$(kubectl -n kthena-system get deployment kthena-controller-manager -o jsonpath='{.spec.template.spec.containers[0].image}')"
RUN_ID=compound-v2-001
python3 scripts/render_job.py \
  --runner-image "$RUNNER_IMAGE" \
  --controller-image "$CONTROLLER_IMAGE" \
  --controller-commit 6fea34e03a179b07686fd5415b83018e159b196b \
  --case-dir servinggroup-compound-v2 --select RUN-625 \
  --run-id "$RUN_ID" --phase-timeout-seconds 70 \
  --out "/tmp/rollout-$RUN_ID.json"
kubectl create -f "/tmp/rollout-$RUN_ID.json"
```

这里的 controller commit 和 `kthena-controller-manager:dev-018-gapfill`
对应关系记录在 issue 020；运行前仍需核对当前 Deployment 的实际镜像。
Job 结束后按[使用指南第 3 部分](USER_GUIDE.md#3-导出看结果定位失败)
导出 attempt、Job/Pod JSON，并执行
`python3 scripts/report_attempt.py --attempt artifacts/$RUN_ID`。
`failure-report.md` 会逐例列出 PASS/FAIL/证据不足、违规代码和原始结果链接。
每个 case 创建自己的 namespace，按 UID 清理，保存请求/响应、连续 Watch、
阶段 checkpoint 与账本；清理失败会阻止后续 case。

复合契约使用最新期望 N 计算整数或百分比 U/S/P。账本将删除中的组
计入 C；在每次旧组替换前检查真实 Ready 底线、最高 eligible ordinal、
目标组保护和 `Q=max(0,C-(N-U)-V)`。Role 增员按组内已应用的成员目标
重新计算 SG Ready。坏版本源需实际 Running/NotReady；稳定阻塞停点
观察至少 30 秒。`conditions` 源状态未命中时单次 attempt 记为
`TRIGGER_MISSED`，三次均未命中则最终报告 `INCONCLUSIVE`；连续观察
中断也报告 `INCONCLUSIVE`，有效轨迹违背设计报告 `FAIL`。
`PASS` 只证明该候选上的该例，不推断其他候选。

编辑生成脚本后重新运行它，并执行 `go test ./...`；修改 runner 代码后
还要运行 `go test -race ./...`、`go vet ./...` 和真实 Kind 验证。
