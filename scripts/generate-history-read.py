#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Two protected A recovery cases with exact historical revision API read failure."""
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
    rows = [r for r in json.loads(raw)['cases'] if r['id'] in {f'RUN-{n}' for n in (524,529)}]; assert len(rows) == 2
    out = ROOT/'cases/history-read'; out.mkdir(exist_ok=True)
    for row in rows:
        config = row['config']; assert set(config) <= {'mode','n','recovery','roles','top','coordination'}
        assert config['roles']['f']['w'] == 1
        a = normal.initial(config, 'controlled'); b = normal.version(copy.deepcopy(a))
        targets = [{'scope':'SG','versions':{'A':1,'B':2},'ordinals':{'0':'A'}}] if config['mode'] == 'SG' else [
            {'role':'frontend','versions':{'A':1,'B':2},'workers':{'A':1,'B':1},'ordinals':{'0':'A'}},
            {'role':'backend','versions':{'A':3},'workers':{'A':0},'ordinals':{'0':'A','1':'A','2':'A'}}]
        warmup = normal.step('establish-protected-A-and-eligible-B',b,stableSeconds=10,expect={'targets':targets})
        if config['mode']=='Role': warmup['action']='prepare-history-source'
        fault = normal.step('unavailable-A-history-read-and-protected-recovery',action='history-read-recovery',stableSeconds=30,timeoutSeconds=420,expect={'noReplacement':True,'noNewRevision':True,'targets':targets})
        case = {'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,
                'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':[warmup,fault]}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    manifest = {'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,
                'developmentScope':'RUN-524/529; actual P1 A/B stop then exact A revision GET errors, protected entry deletion, bounded 30-second safety observation and automatic historical A/W1 recovery',
                'cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}}
    (out/'suite.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
    print('Generated two protected historical read failure scenarios.')


if __name__ == '__main__': main()
