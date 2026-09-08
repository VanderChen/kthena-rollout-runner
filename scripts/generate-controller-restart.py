#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Six explicit controller process replacement checkpoints."""
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
    rows=[r for r in json.loads(raw)['cases'] if r['id'].startswith('RUN-') and 435<=int(r['id'][4:])<=440];assert len(rows)==6
    out=ROOT/'cases/controller-restart';out.mkdir(exist_ok=True)
    for row in rows:
        config=row['config'];assert set(config)<={'mode','n','recovery','roles','top','coordination'}
        a=normal.initial(config,'controlled');b=normal.version(copy.deepcopy(a));which=(int(row['id'][4:])-435)%3
        targets=[{'scope':'SG','versions':{'B':3}}] if config['mode']=='SG' else [{'role':'frontend','versions':{'B':3},'workers':{'B':0}},{'role':'backend','versions':{'A':3},'workers':{'A':0},'ordinals':{'0':'A','1':'A','2':'A'}}]
        scope={} if config['mode']=='SG' else {'role':'frontend'}
        if which==0:
            cond=dict(scope,kind='unit',version='B',ordinal=3,ready=False,count=1)
            steps=[normal.step('first-unready-B-surge-created',b,until='conditions',conditions=[cond],release='none',stableSeconds=0),
                   normal.step('terminate-controller-at-first-surge',action='terminate-controller',stableSeconds=30,expect={'targets':targets})]
        elif which==1:
            old=dict(scope,kind='unit',version='A',ordinal=2,count=1)
            term=dict(scope,kind='terminating',version='A',ordinal=2,count=1)
            steps=[normal.step('hold-first-old-deletion',action='pin',conditions=[old],release='none',stableSeconds=0,expect={'noReplacement':True,'noNewRevision':True}),
                   normal.step('old-instance-actually-terminating',b,until='conditions',conditions=[term],stableSeconds=0),
                   normal.step('terminate-controller-during-old-deletion',action='terminate-controller',until='conditions',conditions=[term],release='none',stableSeconds=0),
                   normal.step('release-old-deletion-and-complete',action='unpin',stableSeconds=30,expect={'targets':targets})]
        else:
            steps=[normal.step('complete-B-including-resource-cleanup',b,stableSeconds=30,expect={'targets':targets}),
                   normal.step('terminate-controller-after-cleanup',action='terminate-controller',release='none',stableSeconds=30,expect={'targets':targets,'noReplacement':True,'noNewRevision':True})]
        for step in steps:step['timeoutSeconds']=420
        case={'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':steps}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    manifest={'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,'developmentScope':'RUN435440 only; same-image controller Pod termination and actual initial sync proof','cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}}
    (out/'suite.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
    print('Generated six controller termination checkpoints.')


if __name__=='__main__':main()
