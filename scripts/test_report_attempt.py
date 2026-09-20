#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0

import json
from pathlib import Path
import tempfile
import unittest

import report_attempt


class AttemptReportTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "attempt"
        self.root.mkdir()
        self.ids = ["RUN-001", "RUN-002"]
        self.write("environment.json", {"baseline": "commit", "options": {"RunID": "attempt", "Select": ""},
                                        "runner": {"caseSHA256": {f"{case}.yaml": "hash" for case in self.ids},
                                                   "runnerPod": {"metadata": {"uid": "pod-uid"}}}})

    def write(self, name, value):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(value), encoding="utf-8")

    def results(self, *statuses):
        rows = []
        for case, status in zip(self.ids, statuses):
            row = {"id": case, "status": status}
            if status != "PASS":
                row["error"] = "COMPOUND_READY_BUDGET: Ready below N-U" if status == "FAIL" else "source not observed"
            rows.append(row)
            self.write(f"{case}/result.json", row)
        self.write("summary.json", {"runID": "attempt", "kthenaBaseline": "commit", "selected": len(self.ids),
                                    "passed": sum(s == "PASS" for s in statuses), "results": rows})

    def job(self, exit_code, condition):
        self.write("job.json", {"metadata": {"name": "job", "uid": "job-uid"}, "status": {"conditions": [
            {"type": condition, "status": "True", "reason": "BackoffLimitExceeded" if condition == "Failed" else "Completed"}]}})
        self.write("runner-pod.json", {"metadata": {"name": "pod", "uid": "pod-uid", "ownerReferences": [
            {"uid": "job-uid", "kind": "Job"}]}, "status": {"containerStatuses": [
            {"name": "runner", "restartCount": 0, "state": {"terminated": {"exitCode": exit_code}}}]}})

    def test_complete_pass_and_markdown(self):
        self.results("PASS", "PASS")
        self.job(0, "Complete")
        self.assertEqual(report_attempt.main(["--attempt", str(self.root)]), 0)
        report = json.loads((self.root / "failure-report.json").read_text())
        self.assertEqual(report["overall"], "PASS")
        self.assertEqual(report["counts"], {"PASS": 2})
        self.assertEqual(report["job"]["status"], "VERIFIED")
        self.assertIn("| RUN-001 | PASS | PASS |", (self.root / "failure-report.md").read_text())

    def test_product_failure_keeps_reason(self):
        self.results("PASS", "FAIL")
        self.job(1, "Failed")
        self.assertEqual(report_attempt.main(["--attempt", str(self.root)]), 1)
        report = json.loads((self.root / "failure-report.json").read_text())
        self.assertEqual(report["overall"], "FAIL")
        self.assertEqual(report["cases"][1]["status"], "FAIL")
        self.assertIn("COMPOUND_READY_BUDGET", report["cases"][1]["reason"])
        self.assertEqual(report["cases"][1]["failureCodes"], ["COMPOUND_READY_BUDGET"])

    def test_inconclusive_is_not_product_failure(self):
        self.results("PASS", "INCONCLUSIVE")
        self.job(1, "Failed")
        report = report_attempt.build_report(self.root)
        self.assertEqual(report["overall"], "INCONCLUSIVE")
        self.assertEqual(report["cases"][1]["status"], "INCONCLUSIVE")
        self.assertFalse(report["cases"][1]["passed"])

    def test_missing_result_is_explicit(self):
        self.results("PASS")
        self.job(1, "Failed")
        report = report_attempt.build_report(self.root)
        self.assertEqual(report["overall"], "INCOMPLETE")
        self.assertEqual(report["cases"][1]["status"], "MISSING_RESULT")
        self.assertEqual(report["job"]["status"], "TERMINAL_ONLY")

    def test_summary_and_case_conflict_cannot_pass(self):
        self.results("PASS", "PASS")
        self.write("RUN-002/result.json", {"id": "RUN-002", "status": "FAIL"})
        self.job(0, "Complete")
        report = report_attempt.build_report(self.root)
        self.assertEqual(report["cases"][1]["status"], "EVIDENCE_CONFLICT")
        self.assertEqual(report["overall"], "INCOMPLETE")

    def test_duplicate_case_cannot_pass(self):
        self.results("PASS", "PASS")
        summary = json.loads((self.root / "summary.json").read_text())
        summary["results"].append(summary["results"][0])
        summary["passed"] = 3
        self.write("summary.json", summary)
        report = report_attempt.build_report(self.root)
        self.assertEqual(report["cases"][0]["status"], "EVIDENCE_CONFLICT")

    def test_job_exit_conflict_cannot_pass(self):
        self.results("PASS", "PASS")
        self.job(1, "Failed")
        report = report_attempt.build_report(self.root)
        self.assertEqual(report["overall"], "INCOMPLETE")
        self.assertEqual(report["job"]["status"], "CONFLICT")

    def test_local_attempt_never_claims_job_acceptance(self):
        self.results("PASS", "FAIL")
        report = report_attempt.build_report(self.root)
        self.assertEqual(report["job"]["status"], "NOT_EXPORTED")
        self.assertEqual(report["overall"], "FAIL")

    def test_preflight_failure_uses_execution_plan(self):
        (self.root / "environment.json").unlink()
        self.write("execution-plan.json", {"runID": "attempt", "controllerCommit": "commit",
                                           "selectedIDs": self.ids,
                                           "caseSHA256": {f"{case}.yaml": "hash" for case in self.ids}})
        self.job(1, "Failed")
        report = report_attempt.build_report(self.root)
        self.assertEqual(report["overall"], "INCOMPLETE")
        self.assertEqual(report["counts"], {"MISSING_RESULT": 2})
        self.assertEqual(report["job"]["status"], "TERMINAL_ONLY")


if __name__ == "__main__":
    unittest.main()
