# Verification status

Production baseline: e2578d01859bb98d9a85846bafbfb2c771a6f117.

- 2026-09-06: Independent Git repository and RUN-001 through RUN-060 created.
- All 60 raw configs and omitted/explicit U/S/P fields compared with the original
  feature020 catalogue: match.
- Runner go test ./..., go test -race ./..., go vet ./...: passed before r3 build.
- Production worktree non-e2e Go regression: passed; no controller changes.
- Kind smoke-01 (prototype image): interrupted after finding the runner's
  ControllerRevision hash/name mapping bug. RUN-001 physically rolled 2→1→0;
  this attempt is not reported as passing.
- Kind smoke-02 (r2 image): 7/7 PASS:
  RUN-001, RUN-004, RUN-010, RUN-016, RUN-024, RUN-031, RUN-047.
  Job completed at 2026-09-06T06:50:41Z.
- Final candidate r3 adds explicit server-default checks, PG resource checks,
  cleanup barrier, checkpoint artifacts and a late-Watch failure-latch regression.
- Full 60-case r3 Job: deployed and running as rollout-runner/rollout-core-60,
  UID 2f77057e-5667-45c1-af75-478b8304dd20, attempt core-60-r3.
  Image ID: sha256:66ed8f6b3f06275679b6f6f5f78cc4ae95bf604f303322ca4ae558921f70c9db.
  No 60/60 completion claim yet.

The smoke results cannot substitute for a complete suite. Final evidence will
be added after the full Job finishes and its artifacts are audited.

During r3 evidence review, an additional synthetic negative test demonstrated a
runner loophole: a target that becomes Ready without explicit release was not
rejected. r4 adds a per-Pod-UID CONTROL_VIOLATION guard (also for incomplete Roles).
This is a runner self-check defect, not evidence of a production controller bug.
The strengthened final candidate will run all 60 again in a separate r4 attempt;
r3 results remain preserved and will not be substituted for final r4 execution.
