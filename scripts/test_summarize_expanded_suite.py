#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
import copy
import importlib.util
import pathlib
import unittest

loader=importlib.util.spec_from_file_location('expanded',pathlib.Path(__file__).with_name('summarize-expanded-suite.py'))
m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)


class CompleteVerdicts(unittest.TestCase):
    def setUp(self):
        self.report={'category':'3','controllerCommit':m.BASELINE,'sourceSHA256':m.SOURCE_SHA,'status':'COMPLETE_CATEGORY_3','counts':{'PASS':168,'FAIL':1},'results':[{'id':cid,'status':'PASS','classification':'PASS','source':{'id':cid,'categoryPath':['3']}} for cid in sorted(m.EXPECTED['3'])]}
        self.report['results'][-1].update(status='FAIL',classification='KTHENA_BEHAVIOR_FAILURE')

    def test_complete_report_keeps_product_failure(self):
        self.assertEqual(m.validate_category('3',self.report),{'PASS':168,'FAIL':1})

    def test_independent_pass_family_preserved(self):
        self.report['results'][0]['classification']='PASS_INDEPENDENT_API_RETRY_CHECK'
        self.assertEqual(m.validate_category('3',self.report),{'PASS':168,'FAIL':1})

    def test_missing_or_duplicate_id_cannot_complete(self):
        for change in ('missing','duplicate'):
            r=copy.deepcopy(self.report)
            if change=='missing':r['results'].pop()
            else:r['results'][-1]['id']=r['results'][0]['id'];r['results'][-1]['source']['id']=r['results'][0]['id']
            with self.assertRaisesRegex(AssertionError,'missing, duplicate'):m.validate_category('3',r)

    def test_runner_miss_cannot_become_product_verdict(self):
        self.report['results'][-1]['classification']='RUNNER_INJECTION_MISS'
        with self.assertRaisesRegex(AssertionError,'runner interruption'):m.validate_category('3',self.report)

    def test_baseline_count_and_failure_tampering_rejected(self):
        for change in ('baseline','counts','failure'):
            r=copy.deepcopy(self.report)
            if change=='baseline':r['controllerCommit']='another'
            elif change=='counts':r['counts']={'PASS':169}
            else:r['results'][-1]['status']='PASS';r['counts']={'PASS':169}
            with self.assertRaises(AssertionError):m.validate_category('3',r)


if __name__=='__main__':unittest.main()
