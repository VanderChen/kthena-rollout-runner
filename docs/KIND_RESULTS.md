# 60 项基础组合：原始过程证据复核

Attempt：core-60-r4b。逐条重放原始 List/Watch，未使用 controller 内部预算算法。

本表对应 SG 30 项 + Role 30 项，不包含其余 624 个目录场景。
用例ID链接至可复用的源配置；逐例原始结果保存在 artifacts/core-60-r4b/RUN-NNN/。
D=3；U/S/P 为有效值。省略与显式配置仍分别保存在各例 case.yaml 和 before/after.yaml。
所有停点 hold=10 秒；Ready/活动数量以逻辑 SG 或 Role 为单位，不以原始 Pod 数计。

最低 Ready 必须 ≥ 3-U；最大活动数量必须 ≤ 3+S；旧启动顺序必须等于被允许 ordinal 的降序。
PG歧义列统计 PG 删除已到达、相应 Ready 信用尚未到达的接收前缀，需要单独复核而不能当作跨资源全序。

| 用例 | 模式 | U/S/P | 旧启动顺序 | 最低 Ready | 最大活动 | 停点/放行 | PG歧义 | 结果 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| [RUN-001](../cases/core/RUN-001.yaml) | SG | 1/0/0 | 2→1→0 | 2 | 3 | 4/3 | 0 | PASS |
| [RUN-002](../cases/core/RUN-002.yaml) | SG | 1/0/0 | 2→1→0 | 2 | 3 | 4/3 | 0 | PASS |
| [RUN-003](../cases/core/RUN-003.yaml) | SG | 1/0/1 | 2→1 | 2 | 3 | 3/2 | 0 | PASS |
| [RUN-004](../cases/core/RUN-004.yaml) | SG | 1/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-005](../cases/core/RUN-005.yaml) | SG | 1/0/0 | 2→1→0 | 2 | 3 | 4/3 | 0 | PASS |
| [RUN-006](../cases/core/RUN-006.yaml) | SG | 1/0/0 | 2→1→0 | 2 | 3 | 4/3 | 0 | PASS |
| [RUN-007](../cases/core/RUN-007.yaml) | SG | 1/0/1 | 2→1 | 2 | 3 | 3/2 | 0 | PASS |
| [RUN-008](../cases/core/RUN-008.yaml) | SG | 1/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-009](../cases/core/RUN-009.yaml) | SG | 1/1/0 | 2→1→0 | 2 | 4 | 4/3 | 0 | PASS |
| [RUN-010](../cases/core/RUN-010.yaml) | SG | 1/1/0 | 2→1→0 | 2 | 4 | 4/3 | 0 | PASS |
| [RUN-011](../cases/core/RUN-011.yaml) | SG | 1/1/1 | 2→1 | 2 | 4 | 3/2 | 0 | PASS |
| [RUN-012](../cases/core/RUN-012.yaml) | SG | 1/1/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-013](../cases/core/RUN-013.yaml) | SG | 0/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-014](../cases/core/RUN-014.yaml) | SG | 0/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-015](../cases/core/RUN-015.yaml) | SG | 0/1/0 | 2→1→0 | 3 | 4 | 4/3 | 0 | PASS |
| [RUN-016](../cases/core/RUN-016.yaml) | SG | 0/1/0 | 2→1→0 | 3 | 4 | 4/3 | 0 | PASS |
| [RUN-017](../cases/core/RUN-017.yaml) | SG | 0/1/1 | 2→1 | 3 | 4 | 3/2 | 0 | PASS |
| [RUN-018](../cases/core/RUN-018.yaml) | SG | 0/1/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-019](../cases/core/RUN-019.yaml) | SG | 2/0/0 | 2→1→0 | 1 | 3 | 4/3 | 0 | PASS |
| [RUN-020](../cases/core/RUN-020.yaml) | SG | 2/0/0 | 2→1→0 | 1 | 3 | 4/3 | 0 | PASS |
| [RUN-021](../cases/core/RUN-021.yaml) | SG | 2/0/1 | 2→1 | 1 | 3 | 3/2 | 0 | PASS |
| [RUN-022](../cases/core/RUN-022.yaml) | SG | 2/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-023](../cases/core/RUN-023.yaml) | SG | 2/0/0 | 2→1→0 | 1 | 3 | 4/3 | 0 | PASS |
| [RUN-024](../cases/core/RUN-024.yaml) | SG | 2/0/0 | 2→1→0 | 1 | 3 | 4/3 | 0 | PASS |
| [RUN-025](../cases/core/RUN-025.yaml) | SG | 2/0/1 | 2→1 | 1 | 3 | 3/2 | 0 | PASS |
| [RUN-026](../cases/core/RUN-026.yaml) | SG | 2/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-027](../cases/core/RUN-027.yaml) | SG | 2/1/0 | 2→1→0 | 1 | 4 | 4/3 | 0 | PASS |
| [RUN-028](../cases/core/RUN-028.yaml) | SG | 2/1/0 | 2→1→0 | 1 | 4 | 4/3 | 0 | PASS |
| [RUN-029](../cases/core/RUN-029.yaml) | SG | 2/1/1 | 2→1 | 1 | 4 | 3/2 | 0 | PASS |
| [RUN-030](../cases/core/RUN-030.yaml) | SG | 2/1/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-031](../cases/core/RUN-031.yaml) | Role | 1/0/0 | 2→1→0 | 2 | 3 | 4/3 | 0 | PASS |
| [RUN-032](../cases/core/RUN-032.yaml) | Role | 1/0/0 | 2→1→0 | 2 | 3 | 4/3 | 0 | PASS |
| [RUN-033](../cases/core/RUN-033.yaml) | Role | 1/0/1 | 2→1 | 2 | 3 | 3/2 | 0 | PASS |
| [RUN-034](../cases/core/RUN-034.yaml) | Role | 1/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-035](../cases/core/RUN-035.yaml) | Role | 1/0/0 | 2→1→0 | 2 | 3 | 4/3 | 0 | PASS |
| [RUN-036](../cases/core/RUN-036.yaml) | Role | 1/0/0 | 2→1→0 | 2 | 3 | 4/3 | 0 | PASS |
| [RUN-037](../cases/core/RUN-037.yaml) | Role | 1/0/1 | 2→1 | 2 | 3 | 3/2 | 0 | PASS |
| [RUN-038](../cases/core/RUN-038.yaml) | Role | 1/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-039](../cases/core/RUN-039.yaml) | Role | 1/1/0 | 2→1→0 | 2 | 4 | 4/3 | 0 | PASS |
| [RUN-040](../cases/core/RUN-040.yaml) | Role | 1/1/0 | 2→1→0 | 2 | 4 | 4/3 | 0 | PASS |
| [RUN-041](../cases/core/RUN-041.yaml) | Role | 1/1/1 | 2→1 | 2 | 4 | 3/2 | 0 | PASS |
| [RUN-042](../cases/core/RUN-042.yaml) | Role | 1/1/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-043](../cases/core/RUN-043.yaml) | Role | 0/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-044](../cases/core/RUN-044.yaml) | Role | 0/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-045](../cases/core/RUN-045.yaml) | Role | 0/1/0 | 2→1→0 | 3 | 4 | 4/3 | 0 | PASS |
| [RUN-046](../cases/core/RUN-046.yaml) | Role | 0/1/0 | 2→1→0 | 3 | 4 | 4/3 | 0 | PASS |
| [RUN-047](../cases/core/RUN-047.yaml) | Role | 0/1/1 | 2→1 | 3 | 4 | 3/2 | 0 | PASS |
| [RUN-048](../cases/core/RUN-048.yaml) | Role | 0/1/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-049](../cases/core/RUN-049.yaml) | Role | 2/0/0 | 2→1→0 | 1 | 3 | 4/3 | 0 | PASS |
| [RUN-050](../cases/core/RUN-050.yaml) | Role | 2/0/0 | 2→1→0 | 1 | 3 | 4/3 | 0 | PASS |
| [RUN-051](../cases/core/RUN-051.yaml) | Role | 2/0/1 | 2→1 | 1 | 3 | 3/2 | 0 | PASS |
| [RUN-052](../cases/core/RUN-052.yaml) | Role | 2/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-053](../cases/core/RUN-053.yaml) | Role | 2/0/0 | 2→1→0 | 1 | 3 | 4/3 | 0 | PASS |
| [RUN-054](../cases/core/RUN-054.yaml) | Role | 2/0/0 | 2→1→0 | 1 | 3 | 4/3 | 0 | PASS |
| [RUN-055](../cases/core/RUN-055.yaml) | Role | 2/0/1 | 2→1 | 1 | 3 | 3/2 | 0 | PASS |
| [RUN-056](../cases/core/RUN-056.yaml) | Role | 2/0/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |
| [RUN-057](../cases/core/RUN-057.yaml) | Role | 2/1/0 | 2→1→0 | 1 | 4 | 4/3 | 0 | PASS |
| [RUN-058](../cases/core/RUN-058.yaml) | Role | 2/1/0 | 2→1→0 | 1 | 4 | 4/3 | 0 | PASS |
| [RUN-059](../cases/core/RUN-059.yaml) | Role | 2/1/1 | 2→1 | 1 | 4 | 3/2 | 0 | PASS |
| [RUN-060](../cases/core/RUN-060.yaml) | Role | 2/1/3 | 不启动 | 3 | 3 | 1/0 | 0 | PASS |

复核通过：60/60。事件总数：3911；停点总数：172；显式放行总数：112。

每例 JSON 还记录 journal SHA-256、最低承诺容量、运行时长等；原始 before/after、server 默认化结果、baseline/final 资源和 journal 均同目录保留。

边界：本批 W=0、固定副本、健康初态、受控 Ready、1 秒终止宽限；不等价于长 Terminating、故障注入、coordination 或真实模型/GPU 验证。
