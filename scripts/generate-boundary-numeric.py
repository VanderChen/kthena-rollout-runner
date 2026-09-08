#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Positive-replica numerical boundary scenarios, preserving literal API inputs."""
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
    rows=[r for r in json.loads(raw)['cases'] if r['id'] in {f'RUN-{n}' for n in range(540,566)}];assert len(rows)==26
    out=ROOT/'cases/boundary-numeric';out.mkdir(exist_ok=True)
    for row in rows:
        a=normal.initial(row['config'],'controlled');b=normal.version(copy.deepcopy(a))
        desired=normal.desired(b);protected=min(desired,normal.effective_budget(b,'partition'))
        versions={}
        if protected:versions['A']=protected
        if desired>protected:versions['B']=desired-protected
        target={'versions':versions,'ordinals':{str(i):'A' for i in range(protected)}}
        if row['config']['mode']=='SG':target['scope']='SG'
        else:target['role']='frontend'
        targets=[target]
        if row['config']['mode']=='Role':targets.append({'role':'backend','versions':{'A':3},'ordinals':{'0':'A','1':'A','2':'A'}})
        step=normal.step('literal-numeric-boundary-A-to-allowed-B',b,stableSeconds=30,timeoutSeconds=420,expect={'noReplacement':protected==desired,'targets':targets})
        case={'format':'rollout-runner/v2','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':[step]}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    (out/'suite.json').write_text(json.dumps({'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,'developmentScope':'26 positive-replica numeric cases RUN540565; zero-replica RUN566572 and remaining category3 cases pending','cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}},ensure_ascii=False,indent=2)+'\n')
    print('Generated 26 positive-replica numeric boundary scenarios.')


if __name__=='__main__':main()
