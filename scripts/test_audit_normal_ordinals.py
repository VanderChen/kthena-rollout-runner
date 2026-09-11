# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
import copy
import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location('ordinal_audit', pathlib.Path(__file__).with_name('audit-normal-ordinals.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class OrdinalAuditTest(unittest.TestCase):
    def fixture(self):
        model = {'metadata': {'name': 'model', 'uid': 'owner'}, 'spec': {'replicas': 1, 'template': {'roles': [{'name': 'frontend', 'replicas': 3}]}}}
        pods = {}
        for n in (2, 0, 1):
            for member in ('entry', 'worker'):
                uid = str(n) + member
                labels = dict(zip(('group-name', 'role', 'role-id', 'entry'), ('model-0', 'frontend', 'frontend-' + str(n), str(member == 'entry').lower())))
                pods[uid] = {'metadata': {'uid': uid, 'labels': {'modelserving.volcano.sh/' + k: v for k, v in labels.items()}, 'ownerReferences': [{'uid': 'owner'}]}}
        return {'modelservings': {'owner': model}, 'pods': pods}

    def test_order_and_workers(self):
        self.assertTrue(m.audit_snapshot(self.fixture())['pass'])

    def test_shifted_ready_count_cannot_pass(self):
        data = self.fixture()
        for pod in data['pods'].values():
            labels = pod['metadata']['labels']
            key = 'modelserving.volcano.sh/role-id'
            labels[key] = 'frontend-' + str(m.ordinal(labels[key]) + 1)
        result = m.audit_snapshot(data)
        self.assertFalse(result['pass'])
        self.assertEqual([1, 2, 3], result['checks'][1]['actual'])

    def test_duplicate_entry_and_foreign_owner(self):
        data = self.fixture()
        extra = copy.deepcopy(data['pods']['0entry'])
        data['pods']['extra'] = extra
        self.assertFalse(m.audit_snapshot(data)['pass'])
        extra['metadata']['ownerReferences'] = [{'uid': 'foreign'}]
        self.assertTrue(m.audit_snapshot(data)['pass'])

    def test_new_desired_is_used(self):
        data = self.fixture()
        data['modelservings']['owner']['spec']['template']['roles'][0]['replicas'] = 2
        self.assertFalse(m.audit_snapshot(data)['pass'])
        del data['pods']['2entry'], data['pods']['2worker']
        self.assertTrue(m.audit_snapshot(data)['pass'])

    def test_repeated_numeric_identity(self):
        self.assertFalse(m.check('Role', 3, ['frontend-0', 'frontend-1', 'different-1'])['pass'])


if __name__ == '__main__':
    unittest.main()
