#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Actual Ready high surge must not open the stable target dependency gate."""
import copy,hashlib,importlib.util,json,pathlib
ROOT=pathlib.Path(__file__).resolve().parents[1]
s=importlib.util.spec_from_file_location('recovery_generator',ROOT/'scripts/generate-recovery.py');r=importlib.util.module_from_spec(s);s.loader.exec_module(r);normal=r.normal
raw=normal.SOURCE.read_bytes();assert hashlib.sha256(raw).hexdigest()==r.SOURCE_SHA
row=next(x for x in json.loads(raw)['cases'] if x['id']=='RUN-573');a=normal.initial(row['config'],'controlled');b=normal.version(copy.deepcopy(a),('frontend','backend'))
step=normal.step('high-surge-versus-stable-target-readiness',b,action='stable-dependency-boundary',stableSeconds=30,timeoutSeconds=420,expect={'targets':[{'role':'frontend','versions':{'B':3}},{'role':'backend','versions':{'B':3}}]})
case={'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':[step]}}
out=ROOT/'cases/boundary-dependency';out.mkdir(exist_ok=True);(out/'RUN-573.yaml').write_text(normal.yaml(case)+'\n')
(out/'suite.json').write_text(json.dumps({'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':r.SOURCE_SHA,'developmentScope':'Actual backend B surge Ready with old stable A slots, then no proxy hold while stable B is NotReady; frontend gate must stay closed, release stable B and automatically complete both Roles','cases':[row],'inputs':{'RUN-573':hashlib.sha256((out/'RUN-573.yaml').read_bytes()).hexdigest()}},ensure_ascii=False,indent=2)+'\n')
print('Generated one actual stable dependency boundary case.')
