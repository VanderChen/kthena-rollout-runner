# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
import copy
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import Mock, patch

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

    def test_historical_refill_keeps_out_of_scope_member(self):
        r.assert_refill(self.pods, 'decode', 'old', {'prefill': 'p'}, {'prefill', 'decode'})

    def update_execution(self, case_id='RC-01'):
        execution = r.Execution.__new__(r.Execution)
        execution.case = next(c for c in json.loads(r.SUITE.read_text())['cases'] if c['id'] == case_id)
        execution.phase = 'full-rollout'
        obj = r.workload(execution.case, 'test')
        obj['metadata']['resourceVersion'] = '1'
        obj['status'] = {'observedGeneration': 1}
        execution.get = Mock(side_effect=lambda *args: copy.deepcopy(obj))
        execution.call = Mock()
        execution.save = Mock()
        return execution, obj

    def test_update_retries_conflict_with_latest_object(self):
        execution, first = self.update_execution()
        latest = copy.deepcopy(first)
        latest['metadata'].update(resourceVersion='2', annotations={'retained': 'new'})
        execution.get.side_effect = [first, latest]
        execution.call.side_effect = [r.Inconclusive('Error from server (Conflict): modified'), None]
        with patch.object(r.time, 'sleep'):
            execution.update(1, target=True)
        self.assertEqual(execution.get.call_count, 2)
        submitted = execution.call.call_args.kwargs['obj']
        self.assertEqual(submitted['metadata']['resourceVersion'], '2')
        self.assertEqual(submitted['metadata']['annotations'], {'retained': 'new'})
        self.assertNotIn('status', submitted)
        self.assertEqual(submitted['spec']['rolloutStrategy']['rollingUpdateConfiguration']['partition'], 1)
        for role in submitted['spec']['template']['roles']:
            self.assertEqual(r.version({'spec': role['entryTemplate']['spec']}), 'B')
        self.assertIn('full-rollout-conflict-1-input', [c.args[0] for c in execution.save.call_args_list])

    def test_update_does_not_retry_other_errors(self):
        execution, _ = self.update_execution()
        execution.call.side_effect = r.Inconclusive('Error from server (Invalid): partition')
        with self.assertRaisesRegex(r.Inconclusive, 'Invalid'):
            execution.update(0)
        self.assertEqual(execution.call.call_count, 1)

    def test_update_conflict_retries_are_bounded(self):
        execution, _ = self.update_execution()
        execution.call.side_effect = r.Inconclusive('Error from server (Conflict): modified')
        with patch.object(r.time, 'sleep'), self.assertRaisesRegex(r.Inconclusive, 'Conflict'):
            execution.update(0)
        self.assertEqual(execution.call.call_count, 5)
        self.assertEqual(execution.get.call_count, 5)

    def test_role_partition_updates_preserve_coordination_and_template_scope(self):
        for case_id in ['RC-03', 'RC-04']:
            for partition in [1, 0]:
                with self.subTest(case=case_id, partition=partition):
                    execution, _ = self.update_execution(case_id)
                    execution.update(partition, target=True)
                    roles = {x['name']: x for x in execution.call.call_args.kwargs['obj']['spec']['template']['roles']}
                    self.assertEqual(roles['prefill']['partition'], partition)
                    self.assertEqual(roles['decode']['partition'], partition if case_id == 'RC-04' else 2)
                    self.assertEqual(r.version({'spec': roles['prefill']['entryTemplate']['spec']}), 'B')
                    self.assertEqual(r.version({'spec': roles['decode']['entryTemplate']['spec']}), 'A')

    def test_partial_recovery_uses_live_boundary_without_completion_table(self):
        initial = {'prefill': 'p', 'decode': 'old'}
        self.pods[0]['metadata']['annotations'] = {'workload.kthena.io/deletion-scope': 'ServingGroup'}
        self.assertTrue(r.partial_recovery_established(self.pods[:1], initial, 'decode', 'prefill', 1))

    def test_partial_recovery_rejects_unestablished_boundaries(self):
        initial = {'prefill': 'p', 'decode': 'old'}
        terminating = pod('prefill', 'p')
        terminating['metadata']['deletionTimestamp'] = 'now'
        for name, pods, hits in [
            ('no-rejected-delete', [pod('prefill', 'p')], 0),
            ('failed-uid-still-present', [pod('prefill', 'p'), pod('decode', 'old', ready=False)], 1),
            ('survivor-missing', [], 1),
            ('survivor-replaced', [pod('prefill', 'replacement')], 1),
            ('survivor-terminating', [terminating], 1),
            ('survivor-not-ready', [pod('prefill', 'p', ready=False)], 1),
        ]:
            with self.subTest(name=name):
                self.assertFalse(r.partial_recovery_established(pods, initial, 'decode', 'prefill', hits))

    def group(self, failed=True, drained=True, phase='Established', deleting=False):
        conditions = [{'type': 'Failed', 'status': 'True' if failed else 'False'},
                      {'type': 'Drained', 'status': 'True' if drained else 'False'}]
        meta = {'name': 'current-0', 'uid': 'g'}
        if deleting:
            meta['deletionTimestamp'] = 'now'
        return {'metadata': meta, 'status': {'conditions': conditions, 'roleInstances': [
            {'role': 'prefill', 'index': 0, 'phase': phase}, {'role': 'decode', 'phase': 'Established'}]}}

    def test_group_object_cut_point_replaces_the_pod_marker(self):
        initial = {'prefill': 'p', 'decode': 'old'}
        survivor = [pod('prefill', 'p')]
        self.assertTrue(r.partial_recovery_established(survivor, initial, 'decode', 'prefill', 1, self.group(), 'ServingGroupRecreate'))
        draining = self.group(failed=False, drained=False, phase='Draining')
        self.assertTrue(r.partial_recovery_established(survivor, initial, 'decode', 'prefill', 1, draining, 'RoleRecreate'))

    def test_group_object_cut_point_rejects_unestablished_boundaries(self):
        initial = {'prefill': 'p', 'decode': 'old'}
        marked = pod('prefill', 'p')
        marked['metadata']['annotations'] = {'workload.kthena.io/deletion-scope': 'ServingGroup'}
        draining = self.group(failed=False, drained=False, phase='Draining')
        for name, pods, hits, group, policy in [
            ('failed-not-drained', [pod('prefill', 'p')], 1, self.group(drained=False), 'ServingGroupRecreate'),
            ('drained-not-failed', [pod('prefill', 'p')], 1, self.group(failed=False), 'ServingGroupRecreate'),
            ('group-delete-accepted', [pod('prefill', 'p')], 1, self.group(deleting=True), 'ServingGroupRecreate'),
            ('group-delete-never-rejected', [pod('prefill', 'p')], 0, self.group(), 'ServingGroupRecreate'),
            ('group-absent', [pod('prefill', 'p')], 1, {}, 'ServingGroupRecreate'),
            ('pod-marker-on-group-object-build', [marked], 1, self.group(), 'ServingGroupRecreate'),
            ('role-record-still-established', [pod('prefill', 'p')], 1, self.group(failed=False, drained=False), 'RoleRecreate'),
            ('role-policy-widened-to-group', [pod('prefill', 'p')], 1, self.group(phase='Draining'), 'RoleRecreate'),
            ('role-record-of-another-instance', [pod('prefill', 'p')], 1,
             {'metadata': {'name': 'current-0'}, 'status': {'roleInstances': [{'role': 'prefill', 'index': 1, 'phase': 'Draining'}]}}, 'RoleRecreate'),
            ('failed-uid-still-present', [pod('prefill', 'p'), pod('decode', 'old', ready=False)], 1, draining, 'RoleRecreate'),
        ]:
            with self.subTest(name=name):
                self.assertFalse(r.partial_recovery_established(pods, initial, 'decode', 'prefill', hits, group, policy))

    def test_pod_marker_is_still_required_without_group_objects(self):
        initial = {'prefill': 'p', 'decode': 'old'}
        self.assertFalse(r.partial_recovery_established([pod('prefill', 'p')], initial, 'decode', 'prefill', 1))

    def test_sync_marker_of_either_build(self):
        self.assertTrue(r.synced('I1125 initial sync has been done'))
        self.assertTrue(r.synced('I1125 run.go:124] "Starting the ModelServing controller" workers=5'))
        self.assertFalse(r.synced('I1125 leaderelection.go:258] "Attempting to acquire leader lease..."'))

    def test_rejects_replacing_out_of_scope_member(self):
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
                   {'phase': 'restart-budget-held', 'event': {'type': 'DELETED', 'object': ps[0]}},
                   {'phase': 'historical-refill', 'event': {'type': 'ADDED', 'object': pod(names[0], 'new-prefill')}},
                   {'phase': 'historical-refill', 'event': {'type': 'ADDED', 'object': pod(names[1], 'new')}}]
        args = ({'mode': 'SG'}, r.uidmap(ps), names[1], {}, set(names[:2]))
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

    def test_trace_requires_original_marked_survivor_deletion(self):
        events, args = self.trace()
        events = [e for e in events if not (e['phase'] == 'restart-budget-held' and e['event']['type'] == 'DELETED' and e['event']['object']['metadata']['name'].endswith('prefill-0-0'))]
        with self.assertRaisesRegex(AssertionError, 'original deletion scope'):
            r.audit_events(events, *args)

    def test_scope_requires_replacing_all_original_uids(self):
        initial = {'current-0-prefill-0-0': 'p', 'current-0-decode-0-0': 'd'}
        scope = r.recovery_scope({'policy': 'ServingGroupRecreate'}, initial)
        with self.assertRaisesRegex(AssertionError, 'scope was not completed'):
            r.assert_scope_replaced([pod(n, uid) for n, uid in initial.items()], initial, scope)
        r.assert_scope_replaced([pod(n, uid + '-new') for n, uid in initial.items()], initial, scope)

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
        self.assertIn('完成deletion-scope标记的原UID删除', gen.contract_row(row)['expect'])
        self.assertEqual(row['expect'], 'original immutable source')


if __name__ == '__main__':
    unittest.main()
