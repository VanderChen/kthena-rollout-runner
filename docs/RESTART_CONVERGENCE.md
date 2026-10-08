# 恢复中断后的 controller 重启用例

契约为 [2.6 行为预期表](specs/servinggroup-compound-rollout.zh-CN.md#restart-convergence-cases)，输入为 [restart-convergence/suite.json](../cases/restart-convergence/suite.json)。RC-01～04 分别覆盖 SG 整组恢复、SG 内 Role 恢复、独立 Role 恢复和协调 Role 恢复。它们使用独立 Python 入口，与 `recovery-contract` 相同，不由通用 Go Job case loader 执行，也不替换旧 RUN-435～440。

每例先完成 A，再提交受 partition 保护的 B。真实 Failed Pod 触发恢复；代理仅拒绝 controller 对健康幸存 Pod 的 DELETE 和对缺员的 POST，runner 直接观察 apiserver。必须看到原故障 UID 消失、幸存 Pod DELETE 命中和对应 `created.json` 完成事实清除，才能确认“恢复已经开始但尚未完成”的重启切点。这是测试夹具的实施证据，不新增用户 API 或持久删除事务要求。

重启后 partition 2→1。POST 暂不放行时，缺员占用唯一的 maxUnavailable，所有健康 UID 必须保留。放行后首次补建必须是历史 A，完整 Ready 后高位可滚 B；再将 partition 降为 0，允许先前保留的低位 A 正常滚 B。最后再制造一次真实故障，验证 controller 连续运行时仍完成配置的全部 Role/SG 恢复范围。

执行前准备一个**隔离的、允许重启的**单副本 Kind controller、Volcano，以及已配置到该 controller 的 runner fault proxy。观察连接必须直连 apiserver。脚本不改 controller 镜像或启动参数，仅暂停并恢复同一个 Deployment；新 Pod 必须换 UID、同镜像且完成 initialSync。套件不适合指向共享生产 controller。

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

边界：RC-04 配置了 maxSkew=50%，但不代替所有顺序、百分比与依赖组合的专项验证；本套件不迁移 84 个历史恢复用例。None、grace=-1、纯 NotReady 和故障起点跨重启计时仍遵循 API §6 及各自用例，不能因为不要求延续旧删除批次而放松。

本轮真实执行结果与无效夹具尝试记录于 [052 报告](../../issues/features/052-runner-spec-contract-alignment-IP/restart-convergence-20261008/README.md)。
