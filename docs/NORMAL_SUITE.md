# 第一大类：303 个运行用例

`cases/normal/` 包含 RUN-001～RUN-303。前60份输入与 `cases/core/` 完全相同；
新增243份使用 `rollout-runner/v2`。源目录原始行和输入SHA-256保存在
`cases/normal/suite.json`，每个v2用例也保留自己的完整来源。

被测源码固定为 production/release-1.0 的
`538b2825c06bc1e8c5392d18f18f84faee9fca95`。历史core目录里的e2578d01是原始
目录基线；`--controller-commit` 单独记录本次实际被测版本。runner不调用controller
内部预算、模板比较或协调函数。

当前r10完整303项Kind验证仍在运行。配置存在不表示Kind通过；完整验收结果另行记录。

| 目录范围 | 能力 |
| --- | --- |
| 001～060 | 原基础预算组合 |
| 061～076 | 省略、null、空对象、默认值与merge patch |
| 077～106 | 多Role、多SG、归一化协调、依赖闭包及灰度停点 |
| 107～131 | 插件、Gang、eviction、成员增删/改名、worker布局 |
| 132～159 | 无操作、非模板策略、历史复用、完成性及同镜像重启 |
| 160～242 | 扩缩容、历史布局、受控Ready停点及finalizer时序 |
| 243～262 | 自然Ready，独立副本请求必须与原滚动实际交错 |
| 263～274 | 单次成功请求同时变更副本数和PodSpec |
| 275～303 | B/C/回滚、动态预算、partition和协调策略变化 |

## 配置和执行

每个阶段明确给出完整spec或merge patch、触发条件、Ready放行方式、持续观察时间和
终态预期。未知动作和无条件触发会在加载时被拒绝。生成器按ID显式映射，运行时不解释
自然语言动作。重新生成需要相邻的issues目录，生成后的runner测试和执行自包含：

```sh
python3 scripts/generate-normal.py
make test
```

v2使用真实API的`entry=true/false`和ControllerRevision绑定entry/worker历史布局。
预算作用域在SG模式是整个ModelServing，在Role模式是每个SG内的每种Role。
物理Ready容量与目标模板完成性分别判断：扩容期间完整的历史A和完整的新B成员可以提供
实际Ready容量，但不能冒充整个SG已完成B模板。

每次首次破坏动作检查真实Ready、D/U/S/P和活动上限；删除后的空缺、已提交UID与受保护
UID跨阶段保留。缩容只有有限的容量回收许可。自然Ready场景在实际删除事件处按观察到的
健康、成本和ordinal选择，不预设整次执行唯一缩容序号。PodGroup删除另有保守上界检查，
Pod事件流保留精确Ready检查。已达到目标的所需容量不能被无故重复替换。

Watch从List的resourceVersion开始，无法连续恢复的观察缺口判INCONCLUSIVE。过程违规
锁存；结束前排空事件后才冻结结果。完整资源、原始Watch、每个请求及API响应、放行UID、
检查点、删除账本、最终资源和JSON/JUnit均保存。

## 触发和终态契约

- HOLD_READY的全部A/B entry/worker出生时不创建ready文件；基线和后续单位分别按UID
  放行。每个目录要求的10秒停点和30秒终态窗口持续运行断言。
- AUTO_READY_INTERLEAVE的Pod自然Ready，不使用Ready门控、延迟、finalizer或暂停
  controller制造窗口。指定旧实例开始替换后，请求前后实时List必须仍证明原滚动未完成。
- 删除完成前恢复副本数的阶段也保存请求前后相同terminating UID；请求前实时发现的
  已接受删除按旧阶段预算入账，不替换Watch缓存。237/241额外使用测试专用finalizer。
- 明确要求时序交错的用例最多三个独立namespace attempt。只有TRIGGER_MISSED重试；
  违规、ERROR或清理失败不重试成PASS。所有attempt保留。
- 263～274直接从健康A基线单请求提交模板和副本数，不先滚动再扩缩。
- 终态检查实际角色/worker布局、明确要求的版本和ordinal、UID保留、PodGroup关联与
  minMember/minResources、Service selector、Ranktable状态/挂载/派生资源回收，以及
  ModelServing状态与当前/目标历史引用。
- 101的目录要求保留旧调用路径；它允许backend在R～R+S内保留至少一个旧依赖，且必须
  报告OldVersionDependencyPresent和UpdateInProgress，不能误宣称全量完成。
- 154～159先以U=1/S=0构造目录指定的B{0,1}，再安装被测策略且校验UID不变，恢复捕获的
  旧current状态。正常reconcile完成晋升后才重启controller做等价核对。
- 151/152使用独立的模板ConfigMap；282/293使用本例namespace中的CPU容量占位Pod。
  这些资源和finalizer均由本例清理，namespace删除使用UID前置条件。

## Kind运行

沿用 `deploy/runner.yaml` 的ServiceAccount、PVC和reader；先准备当前production
controller、Volcano和 `deploy/production-fixture.yaml`。检查实际节点架构后构建并加载：

```sh
make image GOARCH=arm64 IMAGE=kthena-rollout-runner:normal-candidate
kind load docker-image kthena-rollout-runner:normal-candidate --name kthena-resync-010
```

Job参数必须包含：

```text
--cases=/cases/normal
--artifacts=/artifacts
--run-id=<新的独立名称>
--controller-image=kthena-controller-manager:normal-022-538b2825
--controller-commit=538b2825c06bc1e8c5392d18f18f84faee9fca95
```

省略`--select`执行303项，也可使用逗号分隔ID做串行分片。同一开发集群只运行一个suite。
phase-timeout默认180秒。151/152的ConfigMap更新步骤显式等待最多420秒，覆盖production
默认5分钟live audit及其30秒reconcile deadline；其余谓词和稳定窗口保持不变。验收Job
使用`--phase-timeout=420s`，使派生资源回收也至少有一次默认周期的观察机会。
完整suite需要数小时，Job deadline及主机
防休眠时长应覆盖整次运行。每个分片导出完整目录、Job JSON和runner Pod JSON，核对
summary、Pod退出码、Job终态；测试失败时Job应退出1，不能把Failed抹成Complete。

`environment.json`记录controller实际imageID、runner Pod/imageID、二进制SHA-256、
Go构建信息、全部用例输入SHA-256、CRD和集群版本。最终汇总必须使用同一候选；开发
smoke和旧core历史结果不计入新的303项验收。

进程刚启动时，kubelet可能尚未把runner的imageID写入Pod状态；最终导出的同UID
`runner-pod.json`补齐该身份。导出结束后的Job和Pod对象后，使用：

```sh
python3 scripts/summarize-normal.py --out artifacts/normal-final-report \
  artifacts/<core分片> artifacts/<新增用例分片>
```

汇总器拒绝遗漏、重复、未执行、不同候选/输入、清理失败、未结束的Job以及退出码与
结果矛盾的分片。失败仍保留FAIL；INCONCLUSIVE和TRIGGER_MISSED独立计数。
脚本退出0表示303项证据汇总检查完成，不表示全部用例通过。

### RUN-153副本基线验收

RUN-153原有Go断言检查历史UID和Data不变，报告器另从完整Watch和四个checkpoint
验证`coordinated-role-replica-baseline`：初始frontend/backend均为3，扩容完成、
仅修改策略及重启后均为4；扩容完成后不能回退。历史UID、owner和Data全程保持。
证据缺失、注解未更新或Data被改写时，报告拒绝接受原始PASS；不会覆盖原result.json。
通过时，报告的RUN-153条目包含`replicaBaselineEvidence`及原始文件摘要。

此检查使用r10已保存的实际Kind JSON，无需修改303项输入或重建运行二进制。
当前RUN-153实际记录已通过此附加核验；它证明持久化的基线及重启后的状态，
不宣称执行了目录动作之外的后续B滚动。合成反例测试与Kind证据分别保留。

### RUN-301断言补充执行

核查中发现RUN-301移除coordination的阶段缺少`noNewRevision`断言，已补齐；反例测试
证明旧配置会漏过冗余历史，新配置会拒绝。变更仅增加该断言，实际API动作、模板、
时序以及其他302项输入不变。r10二进制已经支持此断言，因此补测使用同一个r10镜像，
将修正后的RUN-301输入通过ConfigMap挂载；无需修改Kthena，也不覆盖在跑Job的输入。

当前本地收尾脚本已等待完整r10导出，随后自动核验并执行补测；不要重复下发。
以下手工命令供未启用收尾脚本的执行使用，必须先等完整r10结束：

```sh
kubectl --kubeconfig /private/tmp/runner-normal-022.kubeconfig create configmap \
  rollout-normal-r10-301-input --namespace rollout-runner \
  --from-file=RUN-301.yaml=cases/normal/RUN-301.yaml
kubectl --kubeconfig /private/tmp/runner-normal-022.kubeconfig apply \
  -f deploy/normal-301-addendum.yaml
```

两个Job均结束、完整证据导出后，使用变更前已冻结的r10 suite与独立补测目录汇总：

```sh
python3 scripts/summarize-normal.py --out artifacts/normal-final-report \
  --original-suite artifacts/environment-022/r10-frozen-suite.json \
  --addendum-301 artifacts/normal-r10-301-addendum artifacts/normal-r10-303
```

此入口只允许RUN-301新增上述断言，校验原始输入可由修正输入仅删除该断言精确还原，
其余302项和原目录不变；仍校验全部Job终态、镜像/二进制/控制器身份、完整303项覆盖、
原始结果和实际输入哈希。报告保留初次RUN-301结果及证据，明确记录304次执行与303个
独立用例，RUN-301验收取补测的原始状态。补测失败仍报告FAIL，缺失补测不能按新输入验收。

报告器的拒绝与结果保留逻辑用`python3 -B -m unittest discover -s scripts -p 'test_*.py'`
验证；这些合成测试不作为Kind执行证据。补测尚未运行，当前不得宣称该新增断言已完成Kind验证。
