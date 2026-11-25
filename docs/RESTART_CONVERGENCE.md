# 恢复中断后的 controller 重启用例

契约为 [2.11 行为预期表](specs/servinggroup-compound-rollout.zh-CN.md#restart-convergence-cases)，输入为 [restart-convergence/suite.json](../cases/restart-convergence/suite.json)。RC-01～04分别覆盖SG整组、SG内Role、独立Role和协调Role的部分删除重启。旧2.9的范围内健康幸存UID保留结果仅作历史记录。

每例先完成A再提交partition=2的B。真实Failed触发恢复；故障UID消失、删除范围已提交且续删所需的DELETE被代理拒绝后才建立有效切点。提交形式随被测版本的对象模型而定（契约2.14，[API §6.1](specs/modelserving-api-reference.zh-CN.md#deletion-scope-commit)），脚本由 `servinggroups` CRD 是否存在自动识别，不提供开关：ServingGroup不是对象时，原健康成员带有deletion-scope且其DELETE被拒绝；ServingGroup是对象时，RC-01是组0的ServingGroup为Failed、Drained且该对象的DELETE被拒绝（其健康成员由垃圾回收删除，不经过代理），RC-02～04是prefill实例0的记录为Draining且原健康成员的DELETE被拒绝，此时Pod上出现deletion-scope标记反而不成立。重启并把partition 2→1，先完成原标记Role/SG范围的DELETE，范围内全部原UID必须换代；无标记的新UID和范围外成员必须保留。缺员POST阻断期间不得额外删除其他完整单位；放行后先完整恢复保护区历史A，再允许高位B滚动，最后partition 1→0完成全量。新Pod不得继承删除标记，旧操作不得反复删新UID。

RC-04 按已批准的协调 partition 比例校验，同时提交 prefill/decode 的 partition 2→1→0；只有 prefill 更新模板，decode 始终为 A。RC-03 独立 Role 模式仍只改变 prefill 的 partition。spec 更新遇到 resourceVersion Conflict 时最多重读重试5次，非Conflict错误立即报告，保留冲突输入和命令记录。

执行前准备一个**隔离的、允许重启的**单副本 Kind controller、Volcano，以及已配置到该 controller 的 runner fault proxy。观察连接必须直连 apiserver。脚本不改 controller 镜像或启动参数，仅暂停并恢复同一个 Deployment；新 Pod 必须换 UID、同镜像且完成 initialSync（日志 `initial sync has been done`，或 ServingGroup 为对象的版本的 `Starting the ModelServing controller`）。套件不适合指向共享生产 controller。

验证切点需要在 controller 暂停期间提交 partition 更新，admission webhook 必须独立保持可用。若 webhook 与待重启 controller 共用进程，可在隔离集群用同镜像、同证书和现有 ServiceAccount 启动独立 webhook Deployment，临时将 webhook Service 指向它，结束后恢复。当前二进制在全部 controller 禁用时会回退为全部启用；066 夹具先确认集群无 ModelBooster，再用 `--controllers=modelbooster --leader-elect=false`，并检查日志确认 ModelServing/Autoscaler 已禁用。不要禁用校验或移动 partition 更新时机来规避连接失败；admission 不可用或额外 ModelServing controller 干扰均属于无效夹具。

```sh
# 从 workspace 根目录执行；路径、镜像和源码 SHA 使用当前环境真实值。
.venv/bin/python kthena-rollout-runner/scripts/run-restart-convergence.py \
  --kubeconfig "$KUBECONFIG_PATH" \
  --controller-image "$CONTROLLER_IMAGE" \
  --controller-commit "$CONTROLLER_COMMIT" \
  --proxy-url http://127.0.0.1:18581 \
  --proxy-token-file "$PROXY_TOKEN_FILE" \
  --namespace-prefix rc-review-001 \
  --artifacts "$ARTIFACT_DIRECTORY"

# 可用 --case RC-01 定向执行；重跑必须换 namespace-prefix 和 artifacts。
.venv/bin/python -m unittest discover \
  -s kthena-rollout-runner/scripts -p test_restart_convergence.py -v
```

证据保存在每例目录：输入、原/新 controller 对象与日志、代理规则及命中、阶段快照、连续 `watch.jsonl`、中间版本/UID/完整 Ready 审计和结果。根目录记录 executor/suite 哈希与声明的 controller commit/image；源码与镜像的构建对应关系仍须由执行者另附 build-info/imageID 核验，单纯传入 SHA 不能证明镜像源码。

`PASS` 要求阶段断言和 watch 审计都通过；夹具未建立、缺少 watch 证据或环境异常为 `INCONCLUSIVE`；已建立场景中的错误版本、健康误删、Ready 预算突破或无法收敛为 `FAIL`。保留每次尝试与失败原因；不能把部分结果相加宣称全套通过。脚本清除自己安装的代理规则并恢复自己暂停的 controller；保留测试 namespace 和证据供检查，不清理其他任务的资源。

边界：RC-04 配置了 maxSkew=50%，但不代替所有顺序、百分比与依赖组合的专项验证；本套件不迁移 84 个历史恢复用例。None、grace=-1、纯 NotReady 和新进程重新起算故障宽限仍遵循 API §6 及各自用例，不能因为恢复删除标记而放松。契约2.13按controller不在的原因区分无标记离线丢失（[API §6.1](specs/modelserving-api-reference.zh-CN.md#controller-absence-scope)）：本套件四例在停机期间不制造无标记丢失，并且以把Deployment缩到0再恢复的方式构造重启，按2.13属完全故障（赦免），带标记的续删仍必须完成；不覆盖多副本且开启选主的升级、主备切换、主实例被删除或其所在节点故障的空窗；这些情形的专项用例待补，本套件的结果不作为2.13的通过或失败证据。

初始2.6的真实执行结果与无效夹具尝试记录于 [052 报告](../../issues/features/052-runner-spec-contract-alignment-IP/restart-convergence-20261008/README.md)。去除成员CM依赖后的2.9执行、controller提交及完整证据记录于 [066报告](../../issues/bugs/066-members-state-redesign-DONE/PROPOSAL_COMMIT.md#18-runner-实施继续2026-10-09)。每次执行的 `provenance.json` 从suite读取契约版本，并记录输入及脚本SHA256。
