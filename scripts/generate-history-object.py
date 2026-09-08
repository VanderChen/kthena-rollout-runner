#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Eight protected A recovery cases with actual absent, corrupt or foreign historical objects."""
import copy
import hashlib
import importlib.util
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('recovery_generator', ROOT/'scripts/generate-recovery.py')
recovery = importlib.util.module_from_spec(loader); loader.loader.exec_module(recovery); normal = recovery.normal


def main():
    raw = normal.SOURCE.read_bytes(); assert hashlib.sha256(raw).hexdigest() == recovery.SOURCE_SHA
    rows = [r for r in json.loads(raw)['cases'] if r['id'] in {f'RUN-{n}' for n in (523,525,526,527,528,530,531,532)}]; assert len(rows) == 8
    out = ROOT/'cases/history-object'; out.mkdir(exist_ok=True)
    for row in rows:
        config = row['config']; assert set(config) <= {'mode','n','recovery','roles','top','coordination'}
        assert config['roles']['f']['w'] == 1
        a = normal.initial(config, 'controlled'); b = normal.version(copy.deepcopy(a))
        targets = [{'scope':'SG','versions':{'A':1,'B':2},'ordinals':{'0':'A'}}] if config['mode'] == 'SG' else [
            {'role':'frontend','versions':{'A':1,'B':2},'workers':{'A':1,'B':1},'ordinals':{'0':'A'}},
            {'role':'backend','versions':{'A':3},'workers':{'A':0},'ordinals':{'0':'A','1':'A','2':'A'}}]
        warmup = normal.step('establish-protected-A-and-eligible-B',b,stableSeconds=10,expect={'targets':targets})
        if config['mode']=='Role': warmup['action']='prepare-history-source'
        fault = normal.step('unavailable-A-history-read-and-protected-recovery',action='history-object-recovery',historyFault={523:'missing',525:'corrupt-data',526:'missing-role',527:'foreign-owner',528:'missing',530:'corrupt-data',531:'missing-role',532:'foreign-owner'}[int(row['id'][4:])],stableSeconds=30,timeoutSeconds=420,expect={'noReplacement':True,'targets':targets})
        case = {'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,
                'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':[warmup,fault]}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    manifest = {'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,
                'developmentScope':'Eight actual historical object faults after P1 A/B stop; exact old entry deletion, 30-second semantic safety window, correct owned original A data restored, automatic protected A/W1 recovery',
                'cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}}
    (out/'suite.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
    print('Generated eight actual historical object fault scenarios.')


if __name__ == '__main__': main()
