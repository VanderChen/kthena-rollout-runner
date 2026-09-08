#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
import hashlib
import importlib.util
import json
import pathlib
import tempfile
import unittest

loader = importlib.util.spec_from_file_location('category3', pathlib.Path(__file__).with_name('summarize-category3.py'))
report = importlib.util.module_from_spec(loader)
loader.loader.exec_module(report)


class Category3EvidenceIntegrity(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.base = self.root / 'artifacts/boundary-test'
        self.control = self.root / 'artifacts/environment-022/boundary-test-control'
        self.base.mkdir(parents=True)
        self.control.mkdir(parents=True)
        self.rows = [{'id': 'RUN-540', 'classification': 'PASS'}]
        self.write(self.base / 'audit.json', {'status': 'VERIFIED', 'cases': self.rows})
        self.item = {'run': 'boundary-test', 'audit': 'audit.json', 'sha256': hashlib.sha256((self.base / 'audit.json').read_bytes()).hexdigest()}
        self.done = {'runID': 'boundary-test', 'status': 'COLLECTED_PENDING_CASE_REVIEW', 'controllerSpecRestored': True, 'historicalModelServingUIDsPreserved': 9, 'selected': ['RUN-540']}
        self.write(self.control / 'completion.json', self.done)
        controller = {'metadata': {'uid': 'original-controller'}, 'spec': {'replicas': 1}}
        for name in ('controller-before.json', 'controller-restored.json'):
            self.write(self.control / name, controller)
        self.environment = {'baseline': report.BASELINE, 'runner': {'binarySHA256': 'original-binary'}}
        self.write(self.base / 'environment.json', self.environment)
        build = {'binarySHA256': 'original-binary', 'workingTree': '', 'runnerCommit': 'source-commit', 'image': 'runner:immutable', 'imageID': 'sha256:original', 'immutableBuildEvidence': 'immutable-build.json'}
        self.write(self.control / 'build.json', build)
        self.write(self.root / 'immutable-build.json', build)

    def write(self, path, data):
        path.write_text(json.dumps(data))

    def test_actual_review_and_restoration_accept(self):
        self.assertEqual(report.read_review(self.root, self.item)[1], self.rows)

    def test_changed_audit_is_rejected(self):
        self.write(self.base / 'audit.json', {'status': 'VERIFIED', 'cases': []})
        with self.assertRaisesRegex(AssertionError, 'digest mismatch'):
            report.read_review(self.root, self.item)

    def test_lost_case_prevents_aggregate_credit(self):
        self.done['selected'].append('RUN-541')
        self.write(self.control / 'completion.json', self.done)
        with self.assertRaisesRegex(AssertionError, 'every selected case'):
            report.read_review(self.root, self.item)

    def test_dispatch_edits_do_not_change_verified_immutable_binary(self):
        path = self.control / 'build.json'
        build = report.read_json(path)
        build['workingTree'] = ' M scripts/unrelated-audit.py\n'
        self.write(path, build)
        self.assertEqual(report.read_review(self.root, self.item)[1], self.rows)

    def test_retagged_image_is_rejected(self):
        path = self.control / 'build.json'
        build = report.read_json(path)
        build['imageID'] = 'sha256:different'
        self.write(path, build)
        with self.assertRaisesRegex(AssertionError, 'immutable build proof'):
            report.read_review(self.root, self.item)

    def test_baseline_and_binary_mismatch_are_rejected(self):
        for key in ('baseline', 'binary'):
            with self.subTest(key=key):
                environment = json.loads(json.dumps(self.environment))
                if key == 'baseline':
                    environment['baseline'] = 'other-production-commit'
                else:
                    environment['runner']['binarySHA256'] = 'other-binary'
                self.write(self.base / 'environment.json', environment)
                with self.assertRaisesRegex(AssertionError, 'different'):
                    report.read_review(self.root, self.item)

    def test_restore_flag_cannot_hide_controller_replacement(self):
        self.write(self.control / 'controller-restored.json', {'metadata': {'uid': 'different'}, 'spec': {'replicas': 1}})
        with self.assertRaises(AssertionError):
            report.read_review(self.root, self.item)

    def test_rejection_ids_are_required_for_completeness(self):
        self.assertEqual(len(report.EXPECTED), 169)
        with self.assertRaisesRegex(AssertionError, 'missing verified cases'):
            report.category2.assemble([('audit', [{'id': case, 'classification': 'PASS'} for case in report.EXPECTED if not case.startswith('DENY-')])], report.EXPECTED)

    def test_product_failure_cannot_be_replaced_by_pass(self):
        with self.assertRaisesRegex(AssertionError, 'duplicate valid verdict'):
            report.category2.assemble([('first', [{'id': 'RUN-540', 'classification': 'KTHENA_BEHAVIOR_FAILURE'}]), ('retry', self.rows)], {'RUN-540'})


if __name__ == '__main__':
    unittest.main()
