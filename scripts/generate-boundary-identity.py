#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Actual old owner residues must not count as new same-name object capacity."""
import copy,hashlib,importlib.util,json,pathlib
ROOT=pathlib.Path(__file__).resolve().parents[1]
s=importlib.util.spec_from_file_location('recovery_generator',ROOT/'scripts/generate-recovery.py');r=importlib.util.module_from_spec(s);s.loader.exec_module(r);normal=r.normal
raw=normal.SOURCE.read_bytes();assert hashlib.sha256(raw).hexdigest()==r.SOURCE_SHA
row=next(x for x in json.loads(raw)['cases'] if x['id']=='RUN-611');a=normal.initial(row['config'],'controlled');b=normal.version(copy.deepcopy(a))
step=normal.step('same-name-new-owner-with-real-old-residues',b,action='new-identity-boundary',stableSeconds=30,timeoutSeconds=420,expect={'targets':[{'role':'frontend','versions':{'B':3}},{'role':'backend','versions':{'A':3}}]})
case={'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':[step]}}
out=ROOT/'cases/boundary-identity';out.mkdir(exist_ok=True);(out/'RUN-611.yaml').write_text(normal.yaml(case)+'\n')
(out/'suite.json').write_text(json.dumps({'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':r.SOURCE_SHA,'developmentScope':'RUN611 actual deleted old ModelServing with old-UID terminating Pods and CR retained10s by runner finalizers. New same-name UID cannot adopt or count old resources. Release only fixture finalizers, then require automatic new-owned B frontend/A backend and full stable resources. Deliberate fixture name blocking is not a product leak.','cases':[row],'inputs':{'RUN-611':hashlib.sha256((out/'RUN-611.yaml').read_bytes()).hexdigest()}},ensure_ascii=False,indent=2)+'\n')
print('Generated one actual new-owner identity boundary.')
