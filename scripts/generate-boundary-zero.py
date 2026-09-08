#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Zero-replica boundaries and later expansion using actual target history."""
import copy
import hashlib
import importlib.util
import json
import pathlib

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('recovery_generator',ROOT/'scripts/generate-recovery.py')
recovery=importlib.util.module_from_spec(loader);loader.loader.exec_module(recovery);normal=recovery.normal


def main():
    raw=normal.SOURCE.read_bytes();assert hashlib.sha256(raw).hexdigest()==recovery.SOURCE_SHA
    rows=[r for r in json.loads(raw)['cases'] if r['id'] in {f'RUN-{n}' for n in range(566,573)}];assert len(rows)==7
    out=ROOT/'cases/boundary-zero';out.mkdir(exist_ok=True)
    for row in rows:
        a=normal.initial(row['config'],'controlled');b=normal.version(copy.deepcopy(a))
        targets=[]
        if row['config']['mode']=='Role' and row['config']['n']>0:
            targets=[{'role':'frontend','versions':{}},{'role':'backend','versions':{'A':3},'ordinals':{'0':'A','1':'A','2':'A'}}]
        steps=[normal.step('zero-dimension-preload-B-history',b,stableSeconds=30,timeoutSeconds=180,expect={'noReplacement':True,'targets':targets})]
        if row['id'] in ('RUN-567','RUN-569'):
            expanded=copy.deepcopy(b);normal.set_desired(expanded,3)
            for key,value in [('maxUnavailable',1),('maxSurge',0),('partition',0)]:normal.set_budget(expanded,key,value)
            targets=[{'scope':'SG','versions':{'B':3}}] if row['config']['mode']=='SG' else [{'role':'frontend','versions':{'B':3}},{'role':'backend','versions':{'A':3},'ordinals':{'0':'A','1':'A','2':'A'}}]
            steps.append(normal.step('expand-zero-dimension-using-preloaded-B',expanded,stableSeconds=30,timeoutSeconds=420,expect={'noReplacement':True,'noNewRevision':True,'targets':targets}))
        case={'format':'rollout-runner/v2','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':steps}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    (out/'suite.json').write_text(json.dumps({'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,'developmentScope':'Seven zero-replica cases RUN566572; other boundary and rejection cases are separate','cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}},ensure_ascii=False,indent=2)+'\n')
    print('Generated seven zero-replica boundary scenarios.')


if __name__=='__main__':main()
