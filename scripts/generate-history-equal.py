#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Precreated equivalent owned history retained through a native AlreadyExists."""
import copy,hashlib,importlib.util,json,pathlib
ROOT=pathlib.Path(__file__).resolve().parents[1]
s=importlib.util.spec_from_file_location('recovery_generator',ROOT/'scripts/generate-recovery.py');r=importlib.util.module_from_spec(s);s.loader.exec_module(r);normal=r.normal
raw=normal.SOURCE.read_bytes();assert hashlib.sha256(raw).hexdigest()==r.SOURCE_SHA
row=next(x for x in json.loads(raw)['cases'] if x['id']=='RUN-610');a=normal.initial(row['config'],'controlled');b=normal.version(copy.deepcopy(a))
step=normal.step('native-equivalent-owned-history-collision',b,action='history-equal-collision',stableSeconds=30,timeoutSeconds=420,expect={'noNewRevision':True,'targets':[{'role':'frontend','versions':{'B':3}},{'role':'backend','versions':{'A':3},'ordinals':{'0':'A','1':'A','2':'A'}}]})
case={'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':[step]}}
out=ROOT/'cases/history-equal';out.mkdir(exist_ok=True);(out/'RUN-610.yaml').write_text(normal.yaml(case)+'\n')
(out/'suite.json').write_text(json.dumps({'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':r.SOURCE_SHA,'developmentScope':'RUN610 only: same precreated equivalent owned CR through one injected controller GET404, actual held POST forwarded to API yielding native409, successful GET and automatic B; no synthetic409','cases':[row],'inputs':{'RUN-610':hashlib.sha256((out/'RUN-610.yaml').read_bytes()).hexdigest()}},ensure_ascii=False,indent=2)+'\n')
print('Generated one equivalent native collision case.')
