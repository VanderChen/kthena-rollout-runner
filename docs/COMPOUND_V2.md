# ServingGroup 组合滚动 v2 用例

设计基准是本仓保存的 [SG v2.6 预期过程表](specs/servinggroup-compound-rollout.zh-CN.md) （原始来源为 workspace issue 033）。它描述期望行为，不代表当前控制器已通过。`cases/servinggroup-compound-v2/` 的 35 个 case 分别保留 `designID`：S01～S07 对应 RUN-618～624，C01～C16 对应 RUN-625～640，P01～P11 对应 RUN-641～651，R01 对应 RUN-652。用例输入由 `scripts/generate-servinggroup-compound-v2.py` 生成，`TestCompoundCatalogueIsExecutable` 验证目录、ID 和设计映射。

**2026-10-07 执行器迁移：** 规范 2.2 已落实到 RUN-622/632 的生成器、YAML 和通用顺序判定：SG/独立 Role 默认旧 NotReady 优先、同类降序；有 coordination 的稳定 Role 不跳过，标记明确且未转正的临时 surge 单独回收。Q/B/I 与 protected 全量 Ready 断言保留。RUN-624 最终只放行新版本，旧错误版本持续 NotReady。16 个查表行仍不是新增 executable case；实际运行结果见 workspace [051 验证记录](../../issues/features/051-production-baseline-realignment-DONE/runner-production-20261007/README.md)，不因执行器迁移而宣称产品全套通过。

**2026-10-07 时点契约 2.3：** 已批准逐完整单元 Ready 重新结算并继续推进，无额外整批屏障，其他约束保持。SG 附录 B.5 给出仅放行同批一个单位的可区分例子；本轮文档同步没有新增 case/generator/verdict 修改或 Kind 结果，既有 release-all 的组合轨迹不能冒充该专项覆盖。

字段语义同时对照 [API reference](specs/modelserving-api-reference.en.md)；口径差异和人工批准要求见[规范入口](specs/README.md)，不能为修复或通过用例直接改写预期。

本套件独立于历史 708 项与 RUN-612～617。`baseline` 固定被测候选源码 `cad19d0fce097e2a5f481938727e9f4e4c42175c`；运行时仍需传入与真实 Deployment 镜像相符的 `--controller-image`，并显式传入同一 `--controller-commit`。不能将旧套件的 PASS 数量用于推断 v2 覆盖。

组合用例也通过通用 Job 入口运行；YAML 在构建镜像时复制到 `/cases/servinggroup-compound-v2`，无需另行挂载。先按 [使用指南](USER_GUIDE.md)核对集群、controller 镜像及架构、安装 `deploy/runner.yaml` 与 `deploy/production-fixture.yaml`，然后选择一个匹配上述 baseline 的 case：

```sh
RUNNER_IMAGE=kthena-rollout-runner:compound-v2-001
NODE_ARCH="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')"
make image GOARCH="$NODE_ARCH" IMAGE="$RUNNER_IMAGE"
# Kind 使用 kind load docker-image "$RUNNER_IMAGE" --name "$KIND_CLUSTER"；
# 其他集群先把该镜像推到节点可访问的 registry。
CONTROLLER_IMAGE="$(kubectl -n kthena-system get deployment kthena-controller-manager -o jsonpath='{.spec.template.spec.containers[0].image}')"
RUN_ID=compound-v2-001
../.venv/bin/python scripts/render_job.py \
  --runner-image "$RUNNER_IMAGE" \
  --controller-image "$CONTROLLER_IMAGE" \
  --controller-commit cad19d0fce097e2a5f481938727e9f4e4c42175c \
  --case-dir servinggroup-compound-v2 --select RUN-625 \
  --run-id "$RUN_ID" --phase-timeout-seconds 70 \
  --out "/tmp/rollout-$RUN_ID.json"
kubectl create -f "/tmp/rollout-$RUN_ID.json"
```

这里的 controller commit 对应 `kthena-controller-manager:rollout-051-cad19d0f`，构建与合入记录位于 issue 051；运行前仍需核对当前 Deployment 的实际镜像。Job 结束后按[使用指南第 3 部分](USER_GUIDE.md#3-导出看结果定位失败) 导出 attempt、Job/Pod JSON，并执行 `../.venv/bin/python scripts/report_attempt.py --attempt artifacts/$RUN_ID`。`failure-report.md` 会逐例列出 PASS/FAIL/证据不足、违规代码和原始结果链接。每个 case 创建自己的 namespace，按 UID 清理，保存请求/响应、连续 Watch、阶段 checkpoint 与账本；清理失败会阻止后续 case。

复合执行器使用最新期望 N 计算整数或百分比 U/S/P。账本将删除中的组计入 C；在每次旧组替换前检查真实 Ready 底线、按模式选择合法旧候选、目标组保护和 `Q=max(0,C-max(0,N-U)-V-I)`，健康删除另受 `B=max(0,R-max(0,N-U))` 限制。Role 增员按组内已应用的成员目标重新计算 SG Ready。坏版本源需实际 Running/NotReady；稳定阻塞停点观察至少 30 秒。`conditions` 源状态未命中时单次 attempt 记为 `TRIGGER_MISSED`，三次均未命中则最终报告 `INCONCLUSIVE`；连续观察中断也报告 `INCONCLUSIVE`，有效轨迹违背设计报告 `FAIL`。`PASS` 只证明该候选上的该例，不推断其他候选。

编辑生成脚本后重新运行它，并执行 `go test ./...`；修改 runner 代码后还要运行 `go test -race ./...`、`go vet ./...` 和真实 Kind 验证。

本轮使用宿主 runner 和已有 kubeconfig 在现有 Kind 执行，未创建 Job，也未新增 cluster-admin 绑定。宿主结果按 summary/result/observations 判定；不得伪造 Job 终态或把只适用于 Job 的 report_attempt 缺失 Job 提示解释为产品失败。

**2.4 恢复交叉边界：** SG 内仅恢复一个 Role/Pod 时保留所属 SG 已应用模板，完整 SG 重建才按当前 partition 选择版本；先恢复旧版后正常滚动可以发生。该规则及 Role 对应关系见 API §6.1；当前 recovery 首次版本/重复创建判定尚待专项迁移，本页既有执行结果不是 2.4 完整覆盖证明。

**2.6 重启收敛边界：** controller 持续运行时，RoleRecreate/ServingGroupRecreate 仍以启动操作时捕获的有限旧 UID 集合完成所选范围；同名新 UID 不属于该操作。controller 重启会丢弃该进程内计划，不重放重启前的整批删除事务。随后按现存 Pod、最新配置、已应用模板、partition、预算和协调规则收敛：可补齐中断留下的缺失成员并保留健康幸存成员，后续真实故障再按当前 recoveryPolicy/grace 判断。Terminating 和其他实际不可用容量继续占用预算，重启不提供额外删除额度。此规则取代 2.5 的旧记录暂停边界；RUN-436/RUN-439 与 058 专项证据分别覆盖 runner 重启流程和局部 SG 恢复中断。
