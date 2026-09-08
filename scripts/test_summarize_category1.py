#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Read-only Kind evidence regression and isolated tampering counterexamples."""
import importlib.util
import json
import os
import pathlib
import shutil
import tempfile
import unittest

SPEC=importlib.util.spec_from_file_location('category1',pathlib.Path(__file__).with_name('summarize-category1.py'))
REPORT=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(REPORT)
ARTIFACTS=os.getenv('RUNNER_KIND_ARTIFACTS')
@unittest.skipUnless(ARTIFACTS,'set RUNNER_KIND_ARTIFACTS to exported Kind evidence')
class KindEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=pathlib.Path(self.temp.name)/'RUN-183'
        shutil.copytree(pathlib.Path(ARTIFACTS)/'normal-r13-183-intent/RUN-183',self.root)
    def change(self,name,fn):
        p=self.root/'attempt-1'/name;o=json.loads(p.read_text());fn(o);p.write_text(json.dumps(o))
    def test_real_latest_proof_includes_monotonic_window_and_ignores_later_namespace_cleanup(self):
        proof=REPORT.latest_review(self.root)
        self.assertEqual(proof['completion']['finalReadyPods'],6)
        self.assertGreaterEqual(proof['monotonicStableNanos'],30_000_000_000)
    def test_changed_correction_uid_is_rejected(self):
        self.change('step-03-prior-intent-review.json',lambda o:o.update(uid='other'))
        with self.assertRaisesRegex(ValueError,'owner/UID'):REPORT.latest_review(self.root)
    def test_post_request_intent_is_not_exempt(self):
        self.change('step-03-prior-intent-review.json',lambda o:o.update(intentTime=o['request']['Received']))
        with self.assertRaisesRegex(ValueError,'intent not before'):REPORT.latest_review(self.root)
    def test_another_original_violation_cannot_be_erased(self):
        self.change('step-03-prior-intent-before-ledger.json',lambda o:o['violations'].append('OTHER_FAILURE'))
        with self.assertRaisesRegex(ValueError,'other violations'):REPORT.latest_review(self.root)
    def test_another_start_cannot_be_rewritten(self):
        self.change('ledger.json',lambda o:o['starts'][0].update(reason='scale'))
        with self.assertRaisesRegex(ValueError,'another recorded action'):REPORT.latest_review(self.root)
    def test_short_monotonic_window_is_rejected(self):
        self.change('checkpoint-004.json',lambda o:o.update(elapsedStableNanos=29_999_999_999))
        with self.assertRaisesRegex(ValueError,'monotonic final window'):REPORT.latest_review(self.root)
    def test_both_prior_failures_remain_runner_failures(self):
        for run in ['normal-r10-183-addendum','normal-r12-183-intent']:
            review=REPORT.prior_review(pathlib.Path(ARTIFACTS)/run/'RUN-183')
            self.assertEqual(review['relation'],'BEFORE_REQUEST')
            self.assertEqual(review['classification']['classification'],'RUNNER_OBSERVATION_FAILURE')
    def test_full_actual_inventory_preserves_every_execution(self):
        result=REPORT.verify(pathlib.Path(ARTIFACTS))
        self.assertEqual((result['total'],result['executionCount']),(303,310))
        self.assertEqual(result['counts'],{'PASS':266,'FAIL':37})
        self.assertEqual(len(result['confirmedFailures']),37)
        self.assertEqual(len(result['runnerCandidates']),4)
        self.assertEqual(len(result['priorIntentCorrection']['preservedExecutions']),2)
        self.assertEqual(result['startupFailures'][0]['caseExecutions'],0)
if __name__=='__main__':unittest.main()
