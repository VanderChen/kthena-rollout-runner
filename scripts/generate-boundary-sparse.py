#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Thirty actual sparse ordinal boundaries; sparse A is built before testing."""
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
    rows=[r for r in json.loads(raw)['cases'] if r['id'] in {'RUN-'+str(n) for n in range(574,604)}];assert len(rows)==30
    out=ROOT/'cases/boundary-sparse';out.mkdir(exist_ok=True)
    for row in rows:
        n=int(row['id'][4:]);a=normal.initial(row['config'],'controlled');b=copy.deepcopy(a)
        identical=n in (594,595,596,600,601,602);trap=n in (592,598)
        if not identical:normal.version(b)
        partition=normal.effective_budget(a,'partition');old=sum(i<partition for i in (0,3,4))
        if identical or trap:old=3
        versions={};ordinals={str(i):'A' for i in (0,3,4) if identical or trap or i<partition}
        if old:versions['A']=old
        if old<3:versions['B']=3-old
        target={'versions':versions,'ordinals':ordinals}
        if row['config']['mode']=='SG':target['scope']='SG'
        else:target['role']='frontend'
        targets=[target]
        if row['config']['mode']=='Role':targets.append({'role':'backend','versions':{'A':3},'ordinals':{'0':'A','1':'A','2':'A'}})
        expect={'noReplacement':old==3,'noNewRevision':identical,'targets':targets,'blockedByBudget':trap}
        step=normal.step('actual-sparse-ordinal-boundary',b,stableSeconds=30,timeoutSeconds=420,release='none' if old==3 else 'one',expect=expect)
        case={'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,'scenario':{'fixture':'sparse-boundary-A','source':row,'profile':'controlled','initialSpec':a,'steps':[step]}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    (out/'suite.json').write_text(json.dumps({'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,'developmentScope':'Thirty SG/Role actual sparse O034 cases with finite preparation-only cleanup before source boundary; zero-budget traps must stay incomplete, identical spec preserves all UIDs, partition is ordinal-based','cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}},ensure_ascii=False,indent=2)+'\n')
    print('Generated 30 actual sparse boundary cases.')

if __name__=='__main__':main()
