#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""RUN-535: a single real unpaged live-reference List failure during history cleanup."""
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
    row=next(r for r in json.loads(raw)['cases'] if r['id']=='RUN-535');config=row['config']
    assert config['revisionHistoryLimit']==0 and config['roles']['f']=={'r':3,'w':0,'u':0,'s':1,'p':0}
    a=normal.initial(config,'controlled');b=normal.version(copy.deepcopy(a))
    targets=[{'role':'frontend','versions':{'B':3}},{'role':'backend','versions':{'A':3},'ordinals':{'0':'A','1':'A','2':'A'}}]
    steps=[normal.step('establish-live-frontend-B-backend-A',b,stableSeconds=10,expect={'targets':targets}),normal.step('fail-GC-reference-List-once-and-retain-both-histories',action='history-gc-list-error',release='none',timeoutSeconds=660,stableSeconds=30,expect={'noReplacement':True,'noNewRevision':True,'targets':targets})]
    case={'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':steps}}
    out=ROOT/'cases/history-gc';out.mkdir(exist_ok=True);path=out/'RUN-535.yaml';path.write_text(normal.yaml(case)+'\n')
    (out/'suite.json').write_text(json.dumps({'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,'developmentScope':'RUN-535: default controller audit with exact namespace + Pod List selector + limit zero; paged observation excluded; no synthetic empty reference list','cases':[row],'inputs':{'RUN-535':hashlib.sha256(path.read_bytes()).hexdigest()}},ensure_ascii=False,indent=2)+'\n')
    print('Generated RUN-535 actual live-reference List failure scenario.')


if __name__=='__main__':main()
