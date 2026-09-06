# 验证结论

2026-09-06，最终验收 attempt **core-60-r4b：RUN-001～RUN-060 全部通过**。
SG 30/30、Role 30/30；无跳过、无观察缺口。Job 已 Complete，runner 退出码0、重启0次。

- [60行实测结果表](docs/KIND_RESULTS.md)：逐例有效预算、启动顺序、最低Ready、最大活动数量。
- [60项计数与预期](docs/CORE_EXPECTATIONS.md)：省略字段、显式配置与共同过程规则。
- [机器可读过程审计](docs/evidence/r4b-audit.json)及[Job/环境验收](docs/evidence/r4b-acceptance.json)。
- 完整原始证据：本地 `artifacts/core-60-r4b/`，同时保留于集群结果PVC的同名目录。
  60份独立源配置在 `cases/core/`；每例before/after.yaml、server默认化结果和原始Watch均保留。

## 本轮验证了什么

独立重放3,911条原始事件、172个10秒停点、112次逐单位Ready放行。
重新计算的容量预算、活动数量、旧实例启动顺序、partition/backend旧UID保护均通过；
没有PG/Pod接收顺序造成的预算判定歧义。还核对了输入字段存在性、服务端默认值、
放行Pod UID、放行后到下一停点的实际时间、最终版本/副本/PG资源/revision收敛。

不是只看最终Ready：B从出生NotReady；旧Pod开始删除即记账，消失不释放额度；
只有完整且实际Ready的新SG/Role提供容量信用。未经runner放行的B Pod提前Ready会失败。
已发现的过程违例不能被健康终态覆盖，Watch无法连续恢复不能判PASS。

## 版本与真实Job

| 项目 | 验收值 |
| --- | --- |
| Kthena生产源码 | production/release-1.0 @ e2578d01859bb98d9a85846bafbfb2c771a6f117 |
| 源码工作树 | kthena-resync-010，最终检查仍干净；未修改controller |
| 集群 | Kind kthena-resync-010，Kubernetes v1.34.0，linux/arm64 |
| Volcano | v1.14.1 |
| Controller镜像 | kthena-controller-manager:resync-010-e2578d01 |
| Controller实际imageID | sha256:739584e01a39de03b52edae09c870ecfe32725dc59590c7071277f6c600d34d2 |
| Runner构建源码 | e2ca051f3cf5db4f212e80589a99db8f0fe62a2e，Signed-off-by，vcs.modified=false |
| Runner镜像 | kthena-rollout-runner:dev-021-r4 |
| Runner实际imageID | sha256:b9528d5a3144e1230201252f96194d80894172c5160c49042d896ecbb4051e00 |
| Job | rollout-runner/rollout-core-60-r4b |
| Job UID | e7737027-fc53-4b43-a449-9f6ea96aaf1d |
| 时间（UTC） | 10:14:11开始，10:57:25 Complete，约43分14秒 |

r4b复用同一个r4镜像，未修改Go实现或60份用例。后续提交只包含运行示例、文档和验收证据。
结束后核对：controller/Volcano Deployment与ModelServing CRD的UID和spec未变，
controller Pod UID、imageID、重启次数未变，节点UID/架构/版本未变。证据记录了spec哈希。
所有本轮测试namespace已清理；已有bug-010等namespace保留。结果PVC、reader及Job记录保留。
本轮临时防空闲休眠断言已释放，未修改永久电源设置。

## 验证门禁

- runner：`go test ./...`、`go test -race ./...`、`go vet ./...`，全部通过。
- 18个检查器测试覆盖超预算删除、旧槽位消失、Ready信用撤销、合法surge信用、
  worker完整性、保护范围、PG提前删除、Watch不合并/410缺口、revision映射、
  结束前违例锁存，以及未经放行的Ready（包含不完整Role的entry）。
- production工作树：`go test $(go list ./... | rg -v '/e2e')`已通过；没有controller代码变更。
- 独立Ruby审计没有调用controller内部候选选择/预算算法；与原feature020目录逐字段核对。

从新仓根目录复核现有证据：

```sh
ruby ../issues/features/021-modelserving-rollout-runner-DONE/audit-core.rb \
  artifacts/core-60-r4b --write-report
ruby ../issues/features/021-modelserving-rollout-runner-DONE/verify-final.rb \
  artifacts/core-60-r4b /private/tmp/rollout-runner-kind.kubeconfig
```

第二条需要仍可访问原集群；kubeconfig未提交。重新构建/运行说明见[README](README.md)。

## 历史attempt不隐藏

| Attempt | 用例结果 | Job/说明 |
| --- | --- | --- |
| smoke-01 | 不计通过 | 原型发现runner的revision hash/name映射错误后中断，修复并补回归 |
| smoke-02 | 7/7 PASS | r2，Job Complete；不能替代完整60项 |
| core-60-r3 | 60/60 PASS | Job Complete；独立审计3,900事件/172停点/112放行 |
| core-60-r4 | 60/60 PASS | Pod退出0，但Job Failed/DeadlineExceeded，不能称为Job成功 |
| core-60-r4b | 60/60 PASS | 最终验收，Job Complete，使用相同r4镜像 |

r4期间主机16:01:58起反复空闲休眠，17:49:52恢复；用例单调时钟时长合计约44分钟，
Job墙钟却达到2小时29分，超过7200秒上限。[原失败状态](docs/evidence/r4-infrastructure-failure.json)
和完整原始产物均保留。r4b仅临时防止空闲休眠，没有放宽Job deadline、停点窗口或滚动断言。

r4相对r3新增“未经放行不得Ready”的检查：合成反向测试先确认旧检查器会漏报，
修复后单元测试及最终Kind全60项通过。这是runner自检加固，不是发现production有该缺陷。

## 覆盖边界

本轮仅60个固定副本、健康初始态、无coordination、W=0的基础组合。
使用production形态的Volcano/headless-service/ranktable配置及busybox受控Ready替身；
A/B终止宽限均为1秒。不等价于真实模型/GPU、长Terminating/finalizer或拓扑调度验证。

其余624个目录场景尚未自动化，不计通过。原feature020分析、684行目录和28份Kind YAML保持不变。
