# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
import copy
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('restart', ROOT / 'scripts/run-restart-convergence.py')
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


def pod(name, uid, ver='A', ready=True):
    return {'kind': 'Pod', 'metadata': {'name': name, 'uid': uid},
            'spec': {'containers': [{'env': [{'name': 'VERSION', 'value': ver}]}]},
            'status': {'phase': 'Running', 'conditions': [{'type': 'Ready', 'status': 'True' if ready else 'False'}]}}


class RestartVerdictTests(unittest.TestCase):
    def setUp(self):
        self.pods = [pod('prefill', 'p'), pod('decode', 'new')]

    def test_historical_refill_keeps_healthy_survivor(self):
        r.assert_refill(self.pods, 'decode', 'old', {'prefill': 'p'}, {'prefill', 'decode'})

    def test_rejects_replacing_whole_old_batch(self):
        self.pods[0]['metadata']['uid'] = 'replayed'
        with self.assertRaisesRegex(AssertionError, 'unexpected replacement'):
            r.assert_refill(self.pods, 'decode', 'old', {'prefill': 'p'}, {'prefill', 'decode'})

    def test_rejects_deleting_healthy_survivor_before_replacement(self):
        self.pods[0]['metadata']['deletionTimestamp'] = 'now'
        with self.assertRaisesRegex(AssertionError, 'unexpected deletion'):
            r.assert_retained(self.pods, {'prefill': 'p'})

    def test_rejects_latest_template_for_protected_partial_refill(self):
        self.pods[1]['spec']['containers'][0]['env'][0]['value'] = 'B'
        with self.assertRaisesRegex(AssertionError, 'historical A'):
            r.assert_refill(self.pods, 'decode', 'old', {'prefill': 'p'}, {'prefill', 'decode'})

    def test_rejects_entry_ready_with_missing_or_unready_worker(self):
        with self.assertRaisesRegex(AssertionError, 'incomplete historical layout'):
            r.assert_refill(self.pods[:1], 'decode', 'old', {'prefill': 'p'}, {'prefill', 'decode'})
        self.pods[1]['status']['conditions'][0]['status'] = 'False'
        with self.assertRaisesRegex(AssertionError, 'complete historical A'):
            r.assert_refill(self.pods, 'decode', 'old', {'prefill': 'p'}, {'prefill', 'decode'})

    def trace(self):
        names = ['current-0-prefill-0-0', 'current-0-decode-0-0', 'current-1-prefill-0-0', 'current-1-decode-0-0']
        ps = [pod(n, str(i)) for i, n in enumerate(names)]
        events = [{'phase': 'prepare', 'event': {'type': 'ADDED', 'object': p}} for p in ps]
        failed = copy.deepcopy(ps[1]); failed['status'] = {'phase': 'Failed', 'conditions': []}
        events += [{'phase': 'partial-recovery', 'event': {'type': 'MODIFIED', 'object': failed}},
                   {'phase': 'restart-budget-held', 'event': {'type': 'DELETED', 'object': failed}},
                   {'phase': 'historical-refill', 'event': {'type': 'ADDED', 'object': pod(names[1], 'new')}}]
        args = ({'mode': 'SG'}, r.uidmap(ps), names[1], {names[0]: '0'}, set(names[:2]))
        return events, args

    def test_trace_accepts_refill_then_legal_full_rollout(self):
        events, args = self.trace()
        events.append({'phase': 'full-rollout', 'event': {'type': 'DELETED', 'object': events[0]['event']['object']}})
        self.assertEqual(r.audit_events(events, *args)['firstRefillUID'], 'new')

    def test_trace_rejects_new_uid_deleted_while_protected(self):
        events, args = self.trace()
        events.append({'phase': 'legal-high-rollout', 'event': {'type': 'DELETED', 'object': events[-1]['event']['object']}})
        with self.assertRaisesRegex(AssertionError, 'replacement deleted'):
            r.audit_events(events, *args)

    def test_trace_rejects_extra_healthy_delete_while_refill_is_unready(self):
        events, args = self.trace()
        events[-1]['event']['object']['status']['conditions'][0]['status'] = 'False'
        events.append({'phase': 'historical-refill', 'event': {'type': 'DELETED', 'object': events[2]['event']['object']}})
        with self.assertRaisesRegex(AssertionError, 'Ready units fell'):
            r.audit_events(events, *args)

    def test_trace_missing_trigger_is_inconclusive(self):
        events, args = self.trace()
        with self.assertRaises(r.Inconclusive):
            r.audit_events(events[:4], *args)

    def test_quiet_held_interval_needs_no_synthetic_pod_event(self):
        events, args = self.trace()
        events[5]['phase'] = 'partial-recovery'
        self.assertEqual(r.audit_events(events, *args)['firstRefillUID'], 'new')

    def test_suite_has_both_units_and_warm_scope_guards(self):
        cases = json.loads(r.SUITE.read_text())['cases']
        self.assertEqual(len(cases), 4)
        for c in cases:
            obj = r.workload(c, 'test')
            self.assertEqual(obj['spec']['schedulerName'], 'volcano')
            self.assertFalse(c['mode'] == 'Role' and c['policy'] == 'ServingGroupRecreate')

    def test_legacy_generator_preserves_source_and_applies_approved_override(self):
        spec = importlib.util.spec_from_file_location('generator', ROOT / 'scripts/generate-controller-restart.py')
        gen = importlib.util.module_from_spec(spec); spec.loader.exec_module(gen)
        row = {'id': 'RUN-436', 'expect': 'original immutable source'}
        self.assertIn('不重放原删除批次', gen.contract_row(row)['expect'])
        self.assertEqual(row['expect'], 'original immutable source')


if __name__ == '__main__':
    unittest.main()
