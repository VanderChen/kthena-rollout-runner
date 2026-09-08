#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Synthetic artifact tests for strict reporting; these are not Kind evidence."""
import copy
import importlib.util
import json
import pathlib
import tempfile
import unittest

MODULE = importlib.util.spec_from_file_location('summarize_normal', pathlib.Path(__file__).with_name('summarize-normal.py'))
REPORTER = importlib.util.module_from_spec(MODULE)
MODULE.loader.exec_module(REPORTER)
ORIGINAL_301_SHA = 'dc23f58aea34d902dc9b44207ca4cdcafceef02e7e993c914a2c2e049be0fdfc'


class AddendumTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.current = REPORTER.read(REPORTER.ROOT / 'cases/normal/suite.json')
        self.original = copy.deepcopy(self.current)
        self.original['inputs']['RUN-301'] = ORIGINAL_301_SHA
        self.original_path = self.root / 'original-suite.json'
        self.save(self.original_path, self.original)

    def save(self, path, value):
        path.write_text(json.dumps(value))

    def run_artifacts(self, name, ids, suite, failures=()):
        directory = self.root / name
        directory.mkdir()
        rows = []
        for cid in ids:
            result = dict(id=cid, status='FAIL' if cid in failures else 'PASS', durationSeconds=1)
            if cid in failures:
                result['error'] = 'synthetic failure for report validation'
            (directory / cid).mkdir()
            self.save(directory / cid / 'result.json', result)
            if cid == 'RUN-301':
                self.save(directory / cid / 'ledger.json', dict(
                    noNewRevision=suite['inputs'][cid] != ORIGINAL_301_SHA,
                    revisions={'existing-B-history': True}))
            rows.append(result)
        count = sum(r['status'] == 'PASS' for r in rows)
        self.save(directory / 'summary.json', dict(runID=name, kthenaBaseline=suite['controllerCommit'], selected=len(rows), passed=count, results=rows))
        container = dict(name='runner', imageID='sha256:runner-image', restartCount=0,
                         state=dict(terminated=dict(exitCode=0 if count == len(rows) else 1)))
        pod = dict(metadata=dict(name=name + '-pod', uid=name + '-pod', ownerReferences=[dict(uid=name + '-job')]),
                   status=dict(containerStatuses=[container]))
        self.save(directory / 'runner-pod.json', pod)
        condition = dict(type='Complete' if count == len(rows) else 'Failed', status='True')
        if count != len(rows):
            condition['reason'] = 'BackoffLimitExceeded'
        self.save(directory / 'job.json', dict(metadata=dict(uid=name + '-job', name=name), status=dict(conditions=[condition])))
        self.save(directory / 'environment.json', dict(
            baseline=suite['controllerCommit'], options=dict(RunID=name, ControllerImage='production-controller'),
            runner=dict(caseSHA256={cid + '.yaml': digest for cid, digest in suite['inputs'].items()},
                        binarySHA256='same-runner-binary', runnerPod=pod),
            controllerPods=[dict(status=dict(containerStatuses=[dict(ready=True, imageID='sha256:production')]))],
        ))
        return directory

    def complete_pair(self, base_failures=(), extra_failures=()):
        base = self.run_artifacts('baseline', self.original['inputs'], self.original, base_failures)
        extra = self.run_artifacts('addendum', ['RUN-301'], self.current, extra_failures)
        return base, extra

    def test_preserves_initial_failure_and_uses_real_addendum_result(self):
        base, extra = self.complete_pair(base_failures=['RUN-301'])
        report = REPORTER.verify_with_301_addendum([base], self.original_path, extra)
        self.assertEqual((report['total'], report['executionCount']), (303, 304))
        self.assertEqual(report['counts'], {'PASS': 303})
        self.assertEqual(report['assertionAddendum']['originalResult']['status'], 'FAIL')
        self.assertEqual(REPORTER.read(base / 'RUN-301/result.json')['status'], 'FAIL')
        self.assertEqual(REPORTER.read(base / 'summary.json')['passed'], 302)

    def test_failed_addendum_is_not_an_expected_pass(self):
        base, extra = self.complete_pair(extra_failures=['RUN-301'])
        report = REPORTER.verify_with_301_addendum([base], self.original_path, extra)
        self.assertEqual(report['counts'], {'PASS': 302, 'FAIL': 1})
        self.assertEqual(report['assertionAddendum']['acceptedResult']['status'], 'FAIL')

    def test_old_run_cannot_pass_current_input_verification_without_addendum(self):
        base, _ = self.complete_pair()
        with self.assertRaisesRegex(ValueError, 'case inputs differ'):
            REPORTER.verify([base])

    def test_rejects_pass_without_armed_assertion_evidence(self):
        base, extra = self.complete_pair()
        self.save(extra / 'RUN-301/ledger.json', dict(noNewRevision=False, revisions={'B': True}))
        with self.assertRaisesRegex(ValueError, 'assertion was armed'):
            REPORTER.verify_with_301_addendum([base], self.original_path, extra)

    def test_rejects_any_other_case_input_change(self):
        original = copy.deepcopy(self.original)
        original['inputs']['RUN-300'] = 'a-different-input'
        with self.assertRaisesRegex(ValueError, 'only RUN-301'):
            REPORTER.verify_301_input_correction(original, self.current)

    def test_rejects_other_changes_within_original_301(self):
        original = copy.deepcopy(self.original)
        original['inputs']['RUN-301'] = 'a-different-action-or-fixture'
        with self.assertRaisesRegex(ValueError, 'more than the documented assertion'):
            REPORTER.verify_301_input_correction(original, self.current)

    def test_rejects_different_binary(self):
        base, extra = self.complete_pair()
        environment = REPORTER.read(extra / 'environment.json')
        environment['runner']['binarySHA256'] = 'different-binary'
        self.save(extra / 'environment.json', environment)
        with self.assertRaisesRegex(ValueError, 'changes candidate binarySHA256'):
            REPORTER.verify_with_301_addendum([base], self.original_path, extra)

    def test_rejects_running_addendum_job(self):
        base, extra = self.complete_pair()
        pod = REPORTER.read(extra / 'runner-pod.json')
        pod['status']['containerStatuses'][0]['state'] = {'running': {}}
        self.save(extra / 'runner-pod.json', pod)
        with self.assertRaisesRegex(ValueError, 'Pod exit status'):
            REPORTER.verify_with_301_addendum([base], self.original_path, extra)

    def test_rejects_missing_baseline_case(self):
        ids = [cid for cid in self.original['inputs'] if cid != 'RUN-078']
        base = self.run_artifacts('baseline', ids, self.original)
        extra = self.run_artifacts('addendum', ['RUN-301'], self.current)
        with self.assertRaisesRegex(ValueError, 'missing cases: RUN-078'):
            REPORTER.verify_with_301_addendum([base], self.original_path, extra)


if __name__ == '__main__':
    unittest.main()
