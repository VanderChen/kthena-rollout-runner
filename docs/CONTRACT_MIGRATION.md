# Runner contract 2.1 migration

2026-10-06：本轮仅修改 rollout runner；具体批准、原始快照及 Kind 证据见 workspace issue 052。当前参考为 [API 中文](specs/modelserving-api-reference.zh-CN.md) / [English](specs/modelserving-api-reference.en.md) 及 [SG 中文](specs/servinggroup-compound-rollout.zh-CN.md) / [English](specs/servinggroup-compound-rollout.en.md)。

## 当前可执行合约

以下从 workspace 根目录执行，使用项目 `.venv`，不会安装全局依赖。输出目录必须不存在，namespace 必须是本次新建的专用名称。显式传入 kubeconfig 和实际 controller 镜像/commit。执行器保留逐请求和 Pod 状态证据，失败返回非零。

```sh
.venv/bin/python kthena-rollout-runner/scripts/run-api-contract.py \
  --kubeconfig "$KUBECONFIG_PATH" --artifacts "$OUT/api" \
  --controller-image "$CONTROLLER_IMAGE" --controller-commit "$CONTROLLER_COMMIT"
.venv/bin/python kthena-rollout-runner/scripts/run-budget-contract.py \
  --kubeconfig "$KUBECONFIG_PATH" --artifacts "$OUT/budgets" \
  --controller-image "$CONTROLLER_IMAGE" --controller-commit "$CONTROLLER_COMMIT"
.venv/bin/python kthena-rollout-runner/scripts/run-recovery-contract.py \
  --kubeconfig "$KUBECONFIG_PATH" --artifacts "$OUT/recovery" \
  --controller-image "$CONTROLLER_IMAGE" --controller-commit "$CONTROLLER_COMMIT"
```

恢复执行器会重启被测 controller deployment，以验证任务取消和 -1/None 在重启后保持语义；应在独立 Kind 集群执行，不对共享生产环境执行。脚本会保留验证 namespace 供检查，清理时使用显式 kubeconfig。

| 执行器 | 数据与判定 |
| --- | --- |
| API | `cases/api-contract/cases.json` 的 188 项。创建请求使用 server dry-run；更新创建真实前置对象并实际 PUT，验证拒绝后 spec/generation/UID 不变。只把真实 admission/schema 拒绝算作 REJECT；网络、权限、冲突耗尽及 setup 失败为 INCONCLUSIVE。前置 Pod 配置不可满足的 nodeSelector，避免部署大量实际负载。 |
| Budget | SG/Role 均为 N=3/U=25%/S=1，同时非生效层 U/S/P=100。新 surge 未 Ready 时保留三个旧 Ready UID；释放 Ready 后必须完成三个目标 Pod，验证非生效层不控制滚动。 |
| Recovery | `cases/recovery-contract/suite.json` 声明作用域：真实 kubelet 重启、Same UID 恢复、有限宽限、Failed 保留、旧任务取消、controller 重启，以及真实 Pod 删除后的精确 SG/Role/缺失 Pod UID 范围。不可用对象不能被报告为可用。 |

API fixture 来源为 043 最终 revision-3 契约探针中与本轮范围相关的独立请求，增加 041 grace 创建/更新边界与零副本 Role 身份检查；不导入被测产品 validator 作为 oracle。恢复与预算执行器复用已有实测脚本的故障机制，并参数化环境、增加镜像/源码和脚本 hash 记录。

## 历史用例冲突

[contract-migration.json](contract-migration.json) 记录 53 个历史 suite/ID 的逐步骤冲突清单，可从 runner 目录重新生成：

```sh
go run ./cmd/contract-audit > docs/contract-migration.json
```

历史 YAML 和原始矩阵保留作追溯。正常 runner 选择到已知冲突时，在连接集群前返回 `CATALOGUE_CONTRACT_CONFLICT`，保存 execution-plan/run-error；这不是产品 FAIL，也不会当作 PASS 或默默跳过。选择未冲突的 ID 不受同目录其他旧 case 影响。该检查仅涵盖本次批准的契约差异，不能替代完整 admission 校验。

| 旧预期 / 输入 | 当前判定与替代覆盖 |
| --- | --- |
| 小百分比 SG U 补 1；全保护/零副本双零可接受 | SG/Role floor；188 项 API 中 `*-percentage-floor-zero`、`*-full-partition-rounded-zero`、`*-zero-omitted-surge` 等要求拒绝；合法 S=1 的实际滚动由 budget 执行器覆盖 |
| 生效 U/P 大于 replicas | API `*-unavailable-exceeds-replicas`、`*-partition-exceeds-replicas`、缩容重验拒绝；零副本默认/显式 U=1 允许 |
| Role 增删改名；运行中修改 gang/coordination | API `role-set-*`、`gang-*`、`coordination-*`；重排/合法内容变更允许，零副本不能绕过身份不可变 |
| DENY-020/035：S 百分比 >100% 必须拒绝 | 对应 SG/Role surge-250-percent 允许，sum int32 溢出拒绝 |
| DENY-044/045/046：存在非生效层预算必须拒绝 | 对应 inactive budget 允许；负值/非法格式仍拒绝；实际忽略由 budget 执行器验证 |
| DENY-094/095 的全保护双零前置对象可创建 | 当前该前置本身已非法；改由 API 初始 create 拒绝覆盖，不伪造一次不存在的在途滚动 |

这些替代覆盖不声称旧无效前提下的全部滚动轨迹仍可执行。保留旧源数据、不把无效配置改成别的值后冒充同一历史 case 已通过。

## SG 用例同步

- SG-S03 的准备步骤缩到 N=2 时同步 P=2，保持高位健康 B 的场景来源合法。
- SG-P02 初始 N=3/P=3，扩到 N=6 时同请求 P=5，保留低位历史模板与跨保护边界扩容。
- SG-P03 缩到 N=2 时同请求 P=2，保留移除旧保护区成员和 NotReady 优先产生稀疏的覆盖。
- SG-P11 按先前 042 明确批准修订：高位旧 3 替换到受保护低位 1，历史 A 暂停，再降低 P 更新 1；健康目标高位规则不变。

组合目录 baseline 现固定 `bd0d650f6ab3fd3d2b473d765d26a1a9d1be4a2b`（本次集群实际候选），它不表示 35 项均已通过。原 baseline 在 Git/052 before 快照及来源记录保留。
