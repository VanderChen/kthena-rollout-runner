#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Render one runner Job from explicit, reusable inputs."""

import argparse
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
DNS_NAME = re.compile(r"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$")


def name(value, label, maximum=63):
    if len(value) > maximum or not DNS_NAME.fullmatch(value):
        raise ValueError(f"invalid {label}: {value}")
    return value


def build_job(args):
    run_id = name(args.run_id, "run ID", 30)
    case_dir = name(args.case_dir, "case directory")
    directory = ROOT / "cases" / case_dir
    if not directory.is_dir():
        raise ValueError(f"case directory does not exist in the image source: {directory}")
    known = {path.stem for pattern in ("RUN-*.yaml", "DENY-*.yaml") for path in directory.glob(pattern)}
    if not known:
        raise ValueError("case directory contains no cases")
    if args.select:
        selected = args.select.split(",")
        if len(set(selected)) != len(selected) or not set(selected) <= known:
            raise ValueError("--select has duplicate or unknown case IDs")
    if args.active_deadline_seconds <= 0 or args.phase_timeout_seconds <= 0:
        raise ValueError("deadlines must be positive")
    proxy = [args.fault_proxy_api, args.fault_proxy_control, args.fault_proxy_token_secret]
    if any(proxy) and not all(proxy):
        raise ValueError("fault cases require API URL, control URL and token Secret together")
    if args.fault_proxy_api and not args.fault_proxy_api.startswith("https://"):
        raise ValueError("fault proxy API must use HTTPS")
    job_name = name(args.job_name or f"rollout-{run_id}", "Job name")
    namespace = name(args.namespace, "namespace")
    arguments = [f"--cases=/cases/{case_dir}", "--artifacts=/artifacts", f"--run-id={run_id}",
                 f"--controller-image={args.controller_image}", f"--controller-commit={args.controller_commit}",
                 f"--phase-timeout={args.phase_timeout_seconds}s"]
    if args.select:
        arguments.append(f"--select={args.select}")
    volumes = [{"name": "results", "persistentVolumeClaim": {"claimName": args.results_pvc}}]
    mounts = [{"name": "results", "mountPath": "/artifacts"}]
    if all(proxy):
        arguments.extend([f"--fault-proxy-api={args.fault_proxy_api}",
                          f"--fault-proxy-control={args.fault_proxy_control}",
                          "--fault-proxy-token-file=/control/token"])
        volumes.append({"name": "control", "secret": {"secretName": args.fault_proxy_token_secret,
                                                       "defaultMode": 256}})
        mounts.append({"name": "control", "mountPath": "/control", "readOnly": True})
    return {"apiVersion": "batch/v1", "kind": "Job", "metadata": {"name": job_name, "namespace": namespace},
            "spec": {"backoffLimit": 0, "activeDeadlineSeconds": args.active_deadline_seconds,
                     "template": {"spec": {"serviceAccountName": "rollout-runner", "restartPolicy": "Never",
                                           "containers": [{"name": "runner", "image": args.runner_image,
                                                           "imagePullPolicy": args.image_pull_policy,
                                                           "args": arguments,
                                                           "resources": {"requests": {"cpu": "100m", "memory": "128Mi"},
                                                                         "limits": {"memory": "1Gi"}},
                                                           "volumeMounts": mounts}],
                                           "volumes": volumes}}}}


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--runner-image", required=True, help="image containing binary and /cases")
    p.add_argument("--controller-image", required=True, help="exact deployed controller image")
    p.add_argument("--controller-commit", required=True, help="source commit used to build controller image")
    p.add_argument("--case-dir", required=True, help="one directory below cases/, such as normal")
    p.add_argument("--select", default="", help="comma-separated case IDs; empty selects entire directory")
    p.add_argument("--run-id", required=True, help="unique attempt ID, at most 30 DNS characters")
    p.add_argument("--job-name", default="", help="defaults to rollout-<run-id>")
    p.add_argument("--namespace", default="rollout-runner")
    p.add_argument("--results-pvc", default="rollout-results")
    p.add_argument("--image-pull-policy", choices=("Always", "IfNotPresent", "Never"), default="IfNotPresent")
    p.add_argument("--active-deadline-seconds", type=int, default=3600)
    p.add_argument("--phase-timeout-seconds", type=int, default=180)
    p.add_argument("--fault-proxy-api", default="", help="HTTPS URL already used by controller kubeconfig")
    p.add_argument("--fault-proxy-control", default="", help="HTTP control URL reachable by runner Job")
    p.add_argument("--fault-proxy-token-secret", default="", help="Secret with key token")
    p.add_argument("--out", required=True, type=Path, help="new Job JSON file")
    return p


def main(argv=None):
    args = parser().parse_args(argv)
    try:
        job = build_job(args)
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text(json.dumps(job, indent=2) + "\n", encoding="utf-8")
    except (OSError, ValueError) as exc:
        print(exc, file=sys.stderr)
        return 2
    print(f"JOB {job['metadata']['namespace']}/{job['metadata']['name']} -> {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
