# Runner checkpoint — 2026-09-15

Snapshot recorded on 2026-09-15 (Asia/Shanghai).

- Source branch: `fix/031-runner-normal-final-ordinals`.
- Preserved implementation and evidence: `f7297af229b8d48aa620d14c8ee981214661641c`.
- Source commit time: `2026-09-11T17:35:57+08:00`.
- Annotated snapshot tag at that source commit: `runner-canonical-20260915`.
- Contract: normal baselines and settled endpoints require SG and Role ordinals `0..replicas-1`; in-flight surge is allowed.
- This checkpoint documentation commit adds this record only. Legacy compatibility is developed separately on `fix/032-runner-legacy-quality`.

Canonical verification remains in [ORDINAL_VERIFICATION.md](ORDINAL_VERIFICATION.md). The original branch and dated tag retain this requirement and its failure evidence. Git records the exact checkpoint commit and tag creation times.
