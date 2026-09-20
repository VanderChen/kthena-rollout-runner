#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0

import json
from pathlib import Path
import tempfile
import unittest

import render_fault_proxy
import render_job


class ManifestTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.output = Path(self.temp.name) / "manifest.json"

    def job_args(self, extra=()):
        return ["--runner-image=registry.example/runner:v1", "--controller-image=controller:v1",
                "--controller-commit=e2578d01859bb98d9a85846bafbfb2c771a6f117",
                "--case-dir=core", "--select=RUN-001", "--run-id=case-001",
                f"--out={self.output}", *extra]

    def test_runner_job_uses_cases_bundled_in_image(self):
        self.assertEqual(render_job.main(self.job_args()), 0)
        job = json.loads(self.output.read_text())
        container = job["spec"]["template"]["spec"]["containers"][0]
        self.assertEqual(container["image"], "registry.example/runner:v1")
        self.assertIn("--cases=/cases/core", container["args"])
        self.assertIn("--select=RUN-001", container["args"])
        self.assertEqual(job["spec"]["backoffLimit"], 0)

    def test_fault_job_requires_complete_proxy_configuration(self):
        args = self.job_args(["--fault-proxy-api=https://fault.rollout-runner.svc:8080"])
        self.assertEqual(render_job.main(args), 2)
        args.extend(["--fault-proxy-control=http://fault.rollout-runner.svc:8081",
                     "--fault-proxy-token-secret=fault-control"])
        self.assertEqual(render_job.main(args), 0)
        job = json.loads(self.output.read_text())
        pod = job["spec"]["template"]["spec"]
        self.assertIn("--fault-proxy-token-file=/control/token", pod["containers"][0]["args"])
        self.assertEqual(pod["volumes"][1]["secret"]["secretName"], "fault-control")

    def test_unknown_case_is_rejected_before_job_creation(self):
        self.assertEqual(render_job.main(self.job_args(["--select=RUN-999"])), 2)
        self.assertFalse(self.output.exists())

    def test_proxy_image_is_explicit_in_pod(self):
        self.assertEqual(render_fault_proxy.main(["--proxy-image=registry.example/fault:v2",
                                                 "--name=fault-001", "--token-secret=fault-control",
                                                 "--tls-secret=fault-tls", f"--out={self.output}"]), 0)
        manifest = json.loads(self.output.read_text())
        pod, service = manifest["items"]
        self.assertEqual(pod["spec"]["containers"][0]["image"], "registry.example/fault:v2")
        self.assertEqual(service["metadata"]["name"], "fault-001")


if __name__ == "__main__":
    unittest.main()
