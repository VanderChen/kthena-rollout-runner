# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
import importlib.util
from pathlib import Path
import unittest
s=importlib.util.spec_from_file_location('api_contract',Path(__file__).with_name('run-api-contract.py'))
m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
class AdmissionEvidenceTest(unittest.TestCase):
    def test_only_native_denial_satisfies_rejection(self):
        for error,expected in [
            ('Error from server (BadRequest): admission webhook "modelserving" denied the request: budget invalid',True),
            ('Error from server (Invalid): ModelServing "test" is invalid: spec.foo: Invalid value',True),
            ('The ModelServing "test" is invalid: spec.template: Required value',True),
            ('Error from server (Forbidden): modelservings is forbidden: User cannot create',False),
            ('Error from server (InternalError): failed calling webhook: timeout',False),
            ('Error from server (Conflict): object modified',False),
            ('Unable to connect: is invalid: dial tcp timeout',False),
        ]:
            with self.subTest(error=error): self.assertEqual(expected,m.is_rejection({'exitCode':1,'stderr':error}))
        self.assertFalse(m.is_rejection({'exitCode':0,'stderr':''}))
if __name__=='__main__':unittest.main()
