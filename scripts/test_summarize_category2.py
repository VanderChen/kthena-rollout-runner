#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
import importlib.util,pathlib,unittest
loader=importlib.util.spec_from_file_location('category2',pathlib.Path(__file__).with_name('summarize-category2.py'));report=importlib.util.module_from_spec(loader);loader.loader.exec_module(report)

class Category2Integrity(unittest.TestCase):
    def test_product_failure_cannot_be_replaced_by_pass(self):
        with self.assertRaisesRegex(AssertionError,'duplicate valid verdict'):
            report.assemble([('old',[{'id':'RUN-304','classification':'KTHENA_BEHAVIOR_FAILURE'}]),('new',[{'id':'RUN-304','classification':'PASS'}])],{'RUN-304'})
    def test_missing_case_prevents_complete(self):
        with self.assertRaisesRegex(AssertionError,'missing verified cases'):
            report.assemble([('audit',[{'id':'RUN-304','classification':'PASS'}])],{'RUN-304','RUN-305'})
    def test_unreviewed_result_cannot_be_credited(self):
        with self.assertRaisesRegex(AssertionError,'unreviewed outcome'):
            report.assemble([('audit',[{'id':'RUN-304','classification':'PENDING_REVIEW'}])],{'RUN-304'})
    def test_precondition_product_finding_survives_later_valid_fault_pass(self):
        accepted,preserved=report.assemble([('source',[{'id':'RUN-528','classification':'KTHENA_PRECONDITION_CLEANUP_FAILURE'}]),('fault',[{'id':'RUN-528','classification':'PASS'}])],{'RUN-528'})
        self.assertEqual(accepted[0]['status'],'PASS');self.assertEqual(len(preserved),1);self.assertEqual(preserved[0]['classification'],'KTHENA_PRECONDITION_CLEANUP_FAILURE')

if __name__=='__main__':unittest.main()
