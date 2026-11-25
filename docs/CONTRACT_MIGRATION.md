# Runner contract 2.2 execution migration

2026-10-06：本轮仅修改 rollout runner；具体批准、原始快照及 Kind 证据见 workspace issue 052。当前参考为 [API 中文](specs/modelserving-api-reference.zh-CN.md) / [English](specs/modelserving-api-reference.en.md) 及 [SG 中文](specs/servinggroup-compound-rollout.zh-CN.md) / [English](specs/servinggroup-compound-rollout.en.md)。

## 2026-10-07 文档 2.2 与执行覆盖

前次用户要求修正 API reference 和行为预期表；本次明确要求同步执行器并以合入后的 production 运行组合与故障恢复用例。2.2 在两层统一 Q/B/I，并确认默认旧 NotReady 优先、协调 Role 不跳过、无开关。查表入口见 [中文](specs/servinggroup-compound-rollout.zh-CN.md#budget-lookup) / [English](specs/servinggroup-compound-rollout.en.md#budget-lookup)。

admission/recovery/budget 2.1 的执行语义未变。2.2 的 RUN-622/632、全旧坏 RUN-624 与通用候选顺序判定已迁移，SG 35 项和 Pod 删除 recovery 84 项绑定 production `cad19d0f`；原 recovery baseline 保存在 suite 的 priorControllerCommit 和 Git。下述 53 项历史 admission 冲突清单仍不等于顺序覆盖。Role 的 Q 及协调模式例外有正反例单元测试，35 个 SG case 不冒充 Role 运行覆盖。实际运行与失败分类见 [051 验证记录](../../issues/features/051-production-baseline-realignment-DONE/runner-production-20261007/README.md)。后续 2.3 文档已获单独批准统一逐 Ready 推进；本段的既有执行器迁移/运行结果不等于新的部分 Ready 专项验证。

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

组合目录 baseline 现固定 `cad19d0fce097e2a5f481938727e9f4e4c42175c`（本次集群实际候选），它不表示 35 项均已通过。原 baseline 在 Git/052 before 快照及来源记录保留。


## 2.4 文档决议与待迁移执行范围

用户已确认局部恢复保持所属滚动单元已应用模板、完整单元重建按 partition 选版本；有限 grace 保留跨 controller 重启故障起点；pod-discovery 发布 Running/NotReady 成员 IP；正在使用的 ranktable 模板内容应由 webhook 拒绝修改。具体批准及实现证据差异见 052；D6 仍未选择，D7 后续已按 2.5 决议。

本轮没有修改 case/generator/verdict。`cases/recovery/RUN-305、RUN-309、RUN-310` 等非保护局部恢复的首次版本规则，以及通用 WantVersions/重复创建检查，需区分“先按旧模板恢复、随后合法滚动”和“旧事件误删新 UID”；不能一律放行重复创建或覆盖旧 FAIL。30 项 recovery-contract 的既有结果不证明跨重启有限 grace 截止时刻保持；插件要求也没有新增 Kind 结果。

前述 53 项机器可读冲突清单针对原有 2.1 admission 差异，并未自动包含本次 2.4 恢复版本语义；不能据该清单未报冲突宣称用例已迁移。核查本地 production@4e5c9016 只找到 ranktable 模板存在性校验，用户随后确认 ConfigMap 更新保护 webhook 在生产分支，并要求暂时忽略此问题；因此暂停定位，不再列为当前待办，也不宣称已验证部署。

## 2.5 旧删除记录局部暂停

按 2026-10-07 的 D7 批准，SG A.4 区分确认零 DELETE 的准备、确认已接受删除且可重建有限旧 UID 集合、无法安全重建事实三条路径。前两者按最新意图和既有收尾规则处理；第三条暂停受影响 SG 的相关删除、明确告警并等待人工，其他 SG 在已有约束允许时继续。不能凭 scope 标记或成员缺失猜测提交，也不能把暂停当作完成、释放实际/未决占用或放宽顺序和预算。

专项回归仍需实现：部分准备失败后提高 partition；确认 DELETE 后崩溃；DELETE 超时且读取失败；旧记录缺阶段/UID 集合；同名新 UID 隔离；重启后暂停；另一个 SG 可继续且不重复领额度。现有 35 个 SG case 和历史 PASS/FAIL 不自动覆盖这些切点，53 项旧 admission 冲突清单也不包含本次语义。此轮仅更新文档，未修改 case/generator/verdict、产品实现或增加 Kind 验证。该方案未上线，并已被下述 2.6 明确取代，仅保留为历史决议记录。

## 2.6 重启后按当前事实重新收敛

2026-10-08 的 058 明确批准删除 `workload.kthena.io/deletion-scope` 及仅用于跨重启恢复旧删除事务的 operation、阶段和有限 UID 持久化；未上线版本不存在兼容、迁移或人工解除负担。controller 持续运行期间仍以进程内有限 UID 计划完成配置要求的 RoleRecreate/ServingGroupRecreate 范围，并保留 UID/owner fence、同名新实例隔离和开始删除前的最新意图复核。

controller 在批次中途重启后，不恢复或补完重启前的旧事务。新进程依据实际 Pod、最新配置、已应用模板、partition、Terminating/不可用容量、预算及 roleCoordination 重新判断。若 SG 恢复仅删掉 prefill，可按该 SG 正确历史模板补齐 prefill；补齐后完整 Ready 则保留原 decode。补齐后仍有真实故障时，按当前 recoveryPolicy、grace 和健康事实开始新的恢复判断；允许重新经历一轮检测/宽限，不绕过 None 或 grace=-1。

RUN-436/RUN-439 的说明和输入哈希同步迁移为上述收敛语义：检查重启不重复取得删除额度、不误删同名新 UID，并在释放实际 Terminating 对象后合法完成；不再断言重启前的所有旧 UID 必须由同一批次替换。058 的产品单测及 Kind 另外覆盖局部 SG 中断后的历史模板补齐和健康幸存实例保留。现有终态断言不能替代中途预算、版本和 UID 检查。

2026-10-08 执行覆盖补充：新增独立 `cases/restart-convergence/suite.json` 的 RC-01～04，执行与结果说明见 [RESTART_CONVERGENCE.md](RESTART_CONVERGENCE.md)。通过 controller API 代理构造真实部分故障恢复，在新进程 initialSync 后验证首次历史补建、健康幸存 UID、缺员占预算及后续合法滚动；每例带持续运行时完整恢复范围对照。RUN-436/439 仍是普通滚动重启，其生成器现使用显式 2.6 覆盖层保留原矩阵并可重生成已批准内容。本次不全局放松 84 个旧 recovery 用例的重复创建断言，也不改写旧 FAIL 原始证据。

2026-10-09，任务066迁移为契约2.9：删除私有成员ConfigMap允许特例；正常终态存在该对象即为多余资源，插件ConfigMap校验保持。RC-01～04的重启切点以故障UID消失和实际幸存者DELETE拒绝确认，不再要求created.json。grace-restart/RUN-431～432测试的是容器原地重启，并未覆盖controller新进程计时，本任务新增真实Kind新进程grace验证；历史跨进程计时结论不作为2.9通过。任务058离线整项Role旧范围预期已由用户替换为补缺，原始证据保留。

RC执行器同时补齐有界resourceVersion冲突重试，以及RC-04按既有API§5共同名义比例提交协调Role的partition；只有prefill改变模板。历史补齐、健康UID、预算和持续运行恢复范围的断言保持。实际执行基线、失败尝试分类和证据见 [066验证记录](../../issues/bugs/066-members-state-redesign-DONE/PROPOSAL_COMMIT.md#19-runner-对齐新-production-验证2026-10-10)。本次范围不代表其余历史恢复用例已完成2.9迁移。

## 2.13 controller 不在期间的丢失范围

2026-11-23，任务009按用户口径修订契约：多副本且开启选主的controller在升级、主备切换、主实例被删除或其所在节点故障的空窗不赦免，其间无标记丢失按recoveryPolicy范围恢复；完全故障期间和controller被删除之后的丢失允许只补缺，也允许按范围恢复；单副本部署下controller不在、副本数被手动调为0按完全故障处理。上文2.9记录的“任务058离线整项Role旧范围预期已由用户替换为补缺”自2.13起不再适用于不赦免的空窗；赦免的情形仍可只补缺。grace重新起算、deletion-scope续删和RC-01～04不变。条文见 [API §6.1](specs/modelserving-api-reference.zh-CN.md#controller-absence-scope)。

本轮仅更新文档，未修改case/generator/verdict和产品实现，没有新的Kind结果。按对用例、执行器和脚本的静态阅读（未执行），现有可执行用例都不在controller空窗内丢失多Pod单元的部分成员：controller-restart/RUN-435～440、leader-switch/RUN-441～442、initial-sync/RUN-450、451、453、454、normal/RUN-153～159的workerReplicas为0，runner不在空窗内从外部删除Pod，空窗内只可能有controller此前自己发起的滚动删除落地（RUN-153、157～159因既有准入冲突目前不可执行）；recovery/RUN-305～388、protected-recovery/RUN-304和deletion-replay/RUN-449、452暂停的是controller对API的访问，进程没有更换，删除事件随后补发，属于连续运行，前两组已按recoveryPolicy范围断言，后一组同时删除entry和worker；history-sparse、boundary-sparse、boundary-completion共66例在准备阶段先从外部删除整个单Pod单元并调低数量，再更换controller进程，history-create的收尾步骤、history-source的准备步骤终止controller时不删除Pod；RC-01～04停机期间没有无标记丢失；recovery-contract的Pod删除都在controller运行时进行。因此没有用例因2.13改变结论，也没有用例覆盖2.13。待补专项：升级、主备切换、多副本删主、主所在节点故障四种不赦免空窗，以及完全故障、被删除两种赦免空窗（两种结果均可接受，范围外UID变化仍判错；介于两者之间的范围契约没有单独规定，专项用例落地前要先定判法）；须构造确定的空窗，并直接观察范围内UID换代与范围外UID保留。runner现有动作的归类：terminate-leader（要求两副本并显式开启选主，强删租约持有者）是多副本删主，不赦免；terminate-controller、hold-initial-sync（要求恰好一个controller Pod）和RC执行器的副本数调为0，按完全故障处理（赦免，两种结果均可接受）；restart-controller和recovery-contract执行器的rollout restart不检查副本数和选主，在单副本夹具上按完全故障，在多副本且开启选主的夹具上是主备切换。不赦免的专项用例须使用多副本且开启选主的夹具，升级只用滚动升级（空窗里始终另有实例能够接管）；现有用例里只有terminate-leader的两例是这种夹具。

## 2.14 ServingGroup 对象：建成记录、提交形式与已声明的出入

2026-11-25，任务010按用户批准修订契约（C1、C3、C4、C12，另声明三处有界出入），条文见 [API §6.1](specs/modelserving-api-reference.zh-CN.md#deletion-scope-commit)、[API §6.2](specs/modelserving-api-reference.zh-CN.md#62-restartgraceperiodseconds) 与 [SG A.6](specs/servinggroup-compound-rollout.zh-CN.md#declared-deviations)，修订记录见 [specs/README](specs/README.md)。上文2.9记录的“删除私有成员ConfigMap……五类成员表不迁移”与2.11的Pod删除标记，自2.14起对ServingGroup为对象的实现分别改为“允许ServingGroup对象上的逐Role实例记录”和“范围记在ServingGroup对象上”；ServingGroup不是对象的实现仍按原形式。

runner 同步迁移，被测版本的对象模型在预检查时由 `servinggroups` CRD 是否存在识别并写入 `environment.json` 的 `objectModel`，不需要也不提供开关：

| 项 | ServingGroup 不是对象 | ServingGroup 是对象 |
| --- | --- | --- |
| 归属 | Pod、PodGroup、Service 的 owner 直接是 ModelServing | 沿 owner 链经 ServingGroup 的 UID 解析到 ModelServing；同名重建的 ServingGroup 是另一个 UID |
| 整组破坏的起点 | PodGroup 被删除 | ServingGroup 带 deletionTimestamp；此后 PodGroup、Pod 由垃圾回收删除，先后不作为证据 |
| 故障代理按 owner 选对象 | 直接 owner 的 UID | 代理从 controller 流量中学到每个 ServingGroup 的 owner；没见过的 ServingGroup 记为证据错误，不静默放过 |
| “暂停 controller”与稀疏夹具的写屏障 | 9 类请求 | 另加 `servinggroups` 与 `servinggroups/status` |
| “扩容到一半”的夹具（RUN-628、633、635、637） | 拒绝下一个序号 PodGroup 的 POST | 拒绝下一个序号 ServingGroup 的 POST |
| api-retry RUN-456 的删除故障 | 旧 Pod 的 DELETE | 该 Pod 所在 ServingGroup 的 DELETE（RUN-460 的 Role 滚动仍是 Pod 的 DELETE） |
| RUN-436/439 与 RC-01～04 的切点 | Pod 上的 deletion-scope 标记 | SG 模式：组在终止中（RC-01：组为 Failed 且已清空故障成员，被拒绝的是 ServingGroup 的 DELETE）；Role 模式：实例记录为 Draining |
| controller 完成初始同步的日志 | `initial sync has been done` | `Starting the ModelServing controller` |
| 同序号新旧对象并存 | 新 PodGroup 早于旧 Pod 消失时计为两个物理组 | 仍这样计数，另加过程断言 `GROUP_OBJECT_OVERLAP`：新对象的 Pod 出现时旧对象的 Pod 仍在即违约 |
| 观察流 | Pod、ModelServing、PodGroup、ControllerRevision | 另加 ServingGroup，逐例输出 `servinggroups.json`，结果里带 `servingGroups` 摘要 |

ServingGroup 为对象时另有逐事件的过程断言（`internal/runner/group_invariants.go`）：surge 标记创建时即有、不事后添加；正式范围内的组不以 surge 创建；转正不改 spec、只在序号进入正式范围后发生、不重建 Pod；实例记录的代际号不回退、新对象不沿用旧记录。稳定检查点另要求每组一个存活对象、正式范围内的组已转正、Pod 上的副本与对象一致。

新增 `cases/servinggroup-objects/` RUN-653～665（设计表 NT1、NT2、NT4 的三种删除方式、NT6、NT9、NT18、NT19、NT45；RUN-663～665 是 api-retry 一类注入在 ServingGroup 上的三种：POST、`status` 的写、直读的 LIST 各失败一次），用例声明 `requires: servingGroupObjects`，在 ServingGroup 不是对象的版本上记为 NOT_RUN。RUN-660、661、662 会终止并重建 controller 进程，须单独执行；RUN-661 需要两副本并开启选主；RUN-662 挂住的是 ServingGroup informer 的初始 LIST（initial-sync 的 RUN-451、454 在这类版本上已是“组的 informer 同步了、Pod 的没同步”）。NT3 由 RC-01 在形式（b）下覆盖；NT10、NT11 由上述过程断言在 `servinggroup-compound-v2` 的 RUN-627、648、650、621 上覆盖。RUN-436/439 的说明同步为两种提交形式。没有修改任何既有用例的可执行预期；`boundary-sparse` 八例与 SG-P11 的相反预期仍待裁决。真实执行结果与失败归类单独记录于任务010的证据目录。
