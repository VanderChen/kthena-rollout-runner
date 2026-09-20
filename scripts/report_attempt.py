#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Build a deterministic, offline report for one exported runner attempt."""

import argparse
import collections
import hashlib
import json
import os
from pathlib import Path
import re
import sys

STATUSES = {"PASS", "FAIL", "ERROR", "INCONCLUSIVE", "TRIGGER_MISSED", "NOT_RUN"}
CASE_FILE = re.compile(r"^(RUN|DENY)-[0-9]+\.yaml$")


def load_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def selected_ids(environment, issues):
    files = environment.get("runner", {}).get("caseSHA256", {})
    if not isinstance(files, dict) or not files:
        raise ValueError("environment.json has no caseSHA256 catalogue")
    catalogue = set()
    for name in files:
        if not CASE_FILE.fullmatch(name):
            raise ValueError(f"invalid case filename in environment: {name}")
        catalogue.add(name.removesuffix(".yaml"))
    selection = environment.get("options", {}).get("Select", "")
    if not selection:
        return sorted(catalogue)
    chosen = selection.split(",")
    if len(set(chosen)) != len(chosen) or not set(chosen) <= catalogue or any(not item for item in chosen):
        issues.append("environment has duplicate or unknown selected case IDs")
    return sorted(set(chosen))


def case_reason(result):
    parts = []
    for key in ("error", "cleanupError"):
        if result.get(key):
            parts.append(str(result[key]))
    if not parts and result.get("violations"):
        parts.append("; ".join(map(str, result["violations"])))
    return "; ".join(parts)


def job_evidence(attempt, environment, complete, all_pass, issues, inputs):
    job_path, pod_path = attempt / "job.json", attempt / "runner-pod.json"
    if not job_path.exists() and not pod_path.exists():
        return {"status": "NOT_EXPORTED", "reason": "Job/Pod objects not exported; Job acceptance is unverified"}
    if not job_path.exists() or not pod_path.exists():
        issues.append("job.json and runner-pod.json must be exported together")
        return {"status": "MISSING", "reason": "Job or runner Pod export is missing"}
    inputs["job.json"], inputs["runner-pod.json"] = sha256(job_path), sha256(pod_path)
    try:
        job, pod = load_json(job_path), load_json(pod_path)
        job_uid = job["metadata"]["uid"]
        pod_uid = pod["metadata"]["uid"]
        owners = pod["metadata"].get("ownerReferences", [])
        containers = [c for c in pod["status"].get("containerStatuses", []) if c.get("name") == "runner"]
        if len(containers) != 1:
            raise ValueError("runner container status is absent or ambiguous")
        container = containers[0]
        termination = container.get("state", {}).get("terminated", {})
        conditions = {c.get("type") for c in job.get("status", {}).get("conditions", []) if c.get("status") == "True"}
        reasons = {c.get("reason") for c in job.get("status", {}).get("conditions", []) if c.get("status") == "True"}
        errors = []
        if not any(o.get("uid") == job_uid and o.get("kind") == "Job" for o in owners):
            errors.append("runner Pod is not owned by exported Job")
        initial_pod = environment.get("runner", {}).get("runnerPod")
        if initial_pod and initial_pod.get("metadata", {}).get("uid") != pod_uid:
            errors.append("runner Pod UID differs from preflight evidence")
        if container.get("restartCount", 0) != 0:
            errors.append("runner container restarted")
        exit_code = termination.get("exitCode")
        if not isinstance(exit_code, int):
            errors.append("runner container has no terminated exit code")
        if not (("Complete" in conditions) ^ ("Failed" in conditions)):
            errors.append("Job has no unique terminal Complete/Failed condition")
        if complete:
            expected = 0 if all_pass else 1
            if exit_code != expected:
                errors.append(f"runner exit code {exit_code} differs from case result expectation {expected}")
            if all_pass and "Complete" not in conditions:
                errors.append("all cases PASS but Job is not Complete")
            if not all_pass and "Failed" not in conditions:
                errors.append("non-PASS cases but Job is not Failed")
            if not all_pass and "BackoffLimitExceeded" not in reasons:
                errors.append("Job failed for a reason other than runner case failure")
        if errors:
            issues.extend("Job: " + error for error in errors)
        status = "CONFLICT" if errors else "VERIFIED" if complete else "TERMINAL_ONLY"
        return {"status": status, "name": job["metadata"].get("name"),
                "pod": pod["metadata"].get("name"), "exitCode": exit_code,
                "condition": "Complete" if "Complete" in conditions else "Failed" if "Failed" in conditions else "Unknown",
                "reason": "; ".join(errors)}
    except (KeyError, TypeError, ValueError, json.JSONDecodeError) as exc:
        issues.append(f"invalid Job/Pod export: {exc}")
        return {"status": "CONFLICT", "reason": str(exc)}


def build_report(attempt):
    attempt = Path(attempt)
    environment_path = attempt / "environment.json"
    issues = []
    inputs = {}
    plan_path = attempt / "execution-plan.json"
    plan = load_json(plan_path) if plan_path.is_file() else None
    if plan is not None:
        inputs["execution-plan.json"] = sha256(plan_path)
        planned = plan.get("selectedIDs")
        if not isinstance(planned, list) or not planned or not all(isinstance(x, str) for x in planned) or len(set(planned)) != len(planned):
            raise ValueError("execution-plan.json has no unique selected case IDs")
    environment = load_json(environment_path) if environment_path.is_file() else None
    if environment is not None:
        inputs["environment.json"] = sha256(environment_path)
        expected = selected_ids(environment, issues)
        if plan is not None:
            if sorted(planned) != expected or plan.get("runID") != environment.get("options", {}).get("RunID"):
                issues.append("execution plan differs from environment selection/run ID")
            if plan.get("caseSHA256") != environment.get("runner", {}).get("caseSHA256"):
                issues.append("execution plan case SHA differs from environment")
            if plan.get("controllerCommit") != environment.get("baseline"):
                issues.append("execution plan controller commit differs from environment")
    elif plan is not None:
        expected = sorted(planned)
        environment = {"options": {"RunID": plan.get("runID")}, "baseline": plan.get("controllerCommit"), "runner": {}}
        issues.append("environment.json is missing; execution stopped before completed preflight")
    else:
        raise ValueError("environment.json or execution-plan.json is required to recover selected case IDs")
    if not expected:
        raise ValueError("no selected case IDs in attempt evidence")
    run_id = environment.get("options", {}).get("RunID")
    run_error_path = attempt / "run-error.json"
    run_error = ""
    if run_error_path.is_file():
        inputs["run-error.json"] = sha256(run_error_path)
        run_error = load_json(run_error_path).get("error", "")
        if not isinstance(run_error, str):
            raise ValueError("run-error.json error must be a string")
    summary_path = attempt / "summary.json"
    summary = None
    by_id = {}
    if summary_path.is_file():
        inputs["summary.json"] = sha256(summary_path)
        summary = load_json(summary_path)
        if summary.get("runID") != run_id:
            issues.append("summary runID differs from environment")
        if summary.get("kthenaBaseline") != environment.get("baseline"):
            issues.append("summary controller baseline differs from environment")
        if summary.get("selected") != len(expected):
            issues.append("summary selected count differs from environment selection")
        results = summary.get("results")
        if not isinstance(results, list):
            raise ValueError("summary results must be a list")
        if summary.get("passed") != sum(isinstance(r, dict) and r.get("status") == "PASS" for r in results):
            issues.append("summary passed count differs from result statuses")
        for result in results:
            if not isinstance(result, dict) or not isinstance(result.get("id"), str):
                issues.append("summary contains malformed case result")
                continue
            case_id = result["id"]
            by_id.setdefault(case_id, []).append(result)
            if case_id not in expected:
                issues.append(f"summary contains unselected case {case_id}")
    else:
        issues.append("summary.json is missing")
    cases = []
    for case_id in expected:
        records = by_id.get(case_id, [])
        path = attempt / case_id / "result.json"
        source_status = records[0].get("status") if len(records) == 1 else None
        status, reason = "MISSING_RESULT", run_error or "no result in summary.json"
        if len(records) > 1:
            status, reason = "EVIDENCE_CONFLICT", "duplicate result IDs in summary.json"
        elif len(records) == 1:
            result = records[0]
            if source_status not in STATUSES:
                status, reason = "EVIDENCE_CONFLICT", f"unknown runner status: {source_status}"
            elif source_status == "NOT_RUN":
                status, reason = "NOT_RUN", case_reason(result) or "runner did not execute this case"
            elif not path.is_file():
                status, reason = "EVIDENCE_CONFLICT", "per-case result.json is missing"
            else:
                try:
                    detail = load_json(path)
                except (ValueError, json.JSONDecodeError) as exc:
                    status, reason = "EVIDENCE_CONFLICT", f"invalid per-case result.json: {exc}"
                else:
                    if detail != result:
                        status, reason = "EVIDENCE_CONFLICT", "summary and per-case result.json differ"
                    elif source_status == "PASS" and (result.get("error") or result.get("violations") or result.get("cleanupError")):
                        status, reason = "EVIDENCE_CONFLICT", "PASS result contains error, violation or cleanup failure"
                    else:
                        status, reason = source_status, case_reason(result)
        if path.is_file():
            inputs[f"{case_id}/result.json"] = sha256(path)
        if status in {"MISSING_RESULT", "EVIDENCE_CONFLICT"}:
            issues.append(f"{case_id}: {reason}")
        codes = sorted(set(re.findall(r"\b[A-Z][A-Z0-9_]{2,}(?=:)", reason)))
        cases.append({"id": case_id, "status": status, "runnerStatus": source_status,
                      "passed": status == "PASS", "failureCodes": codes, "reason": reason,
                      "evidence": f"{case_id}/result.json" if path.is_file() else None})
    counts = dict(sorted(collections.Counter(row["status"] for row in cases).items()))
    complete = summary is not None and len(by_id) == len(expected) and not any(
        row["status"] in {"MISSING_RESULT", "EVIDENCE_CONFLICT", "NOT_RUN"} for row in cases)
    all_pass = complete and all(row["passed"] for row in cases)
    if all_pass and run_error:
        issues.append("runner reported a fatal error despite all case results being PASS")
    job = job_evidence(attempt, environment, complete, all_pass, issues, inputs)
    if issues or not complete:
        overall = "INCOMPLETE"
    elif all_pass and job["status"] == "VERIFIED":
        overall = "PASS"
    elif any(row["status"] == "FAIL" for row in cases):
        overall = "FAIL"
    else:
        overall = "INCONCLUSIVE"
    return {"schema": "rollout-runner/offline-attempt-report/v1", "runID": run_id,
            "controllerCommit": environment.get("baseline"), "expected": len(expected),
            "counts": counts, "overall": overall, "job": job, "runError": run_error, "issues": issues,
            "inputsSHA256": dict(sorted(inputs.items())), "cases": cases}


def markdown(report, attempt, out):
    def cell(value):
        return str(value or "").replace("|", "\\|").replace("\n", " ").replace("\r", " ")

    lines = [f"# Runner attempt {report['runID']} 故障报告", "",
             f"总体结论：**{report['overall']}**；Job 证据：**{report['job']['status']}**。", "",
             f"选中 {report['expected']} 项；" + "，".join(f"{key} {value}" for key, value in report["counts"].items()) + "。", "",
             "| 用例 | 结论 | runner 状态 | 代码 | 原因 | 原始结果 |", "| --- | --- | --- | --- | --- | --- |"]
    if report["runError"]:
        lines[4:4] = [f"Runner 退出原因：{report['runError']}", ""]
    for row in report["cases"]:
        verdict = "PASS" if row["passed"] else "FAIL" if row["status"] == "FAIL" else "未形成通过结论"
        link = ""
        if row["evidence"]:
            target = os.path.relpath(attempt / row["evidence"], out)
            link = f"[result.json]({target})"
        lines.append(f"| {row['id']} | {verdict} | {cell(row['status'])} | {cell(', '.join(row['failureCodes']))} | {cell(row['reason'])} | {link} |")
    if report["issues"]:
        lines.extend(["", "## 证据问题", ""])
        lines.extend("- " + issue for issue in report["issues"])
    lines.extend(["", "FAIL 是 runner 的用例违约结论；INCONCLUSIVE、ERROR、NOT_RUN 和缺失结果均未通过，但不自动归为产品故障。", ""])
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--attempt", required=True, type=Path, help="exported attempt directory")
    parser.add_argument("--out", type=Path, help="report directory; defaults to attempt")
    args = parser.parse_args(argv)
    out = args.out or args.attempt
    try:
        report = build_report(args.attempt)
        out.mkdir(parents=True, exist_ok=True)
        (out / "failure-report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        (out / "failure-report.md").write_text(markdown(report, args.attempt, out), encoding="utf-8")
    except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError) as exc:
        print(f"cannot build offline report: {exc}", file=sys.stderr)
        return 2
    print(f"REPORT {report['overall']} cases={report['expected']} job={report['job']['status']} out={out}")
    return 0 if report["overall"] == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())
