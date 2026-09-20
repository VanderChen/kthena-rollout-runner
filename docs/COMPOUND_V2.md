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

从 runner 仓库根目录，先在专用 Kind 集群安装
`deploy/production-fixture.yaml`，再构建并执行：

```sh
go build -o /tmp/kthena-rollout-runner-compound ./cmd/rollout-runner
/tmp/kthena-rollout-runner-compound \
  --cases cases/servinggroup-compound-v2 \
  --artifacts artifacts/compound-v2 \
  --run-id compound-v2-001 \
  --kubeconfig "$HOME/.kube/config" \
  --controller-image kthena-controller-manager:dev-018-gapfill \
  --controller-commit 6fea34e03a179b07686fd5415b83018e159b196b
```

这里的镜像 tag 是 issue 020 记录的该候选构建；运行前仍需核对当前
Deployment、镜像 ID 和 Kind 节点架构。每个 case 创建自己的 namespace，
按 UID 清理，并保存请求/响应、Pod/PodGroup/ControllerRevision 连续 Watch、
阶段 checkpoint、账本、环境和结果。清理失败会中止后续 case。

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
