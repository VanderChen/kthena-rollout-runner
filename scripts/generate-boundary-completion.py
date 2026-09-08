#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Actual B12 members must complete before any final controller restart."""
import copy,hashlib,importlib.util,json,pathlib
ROOT=pathlib.Path(__file__).resolve().parents[1]
s=importlib.util.spec_from_file_location('recovery_generator',ROOT/'scripts/generate-recovery.py');r=importlib.util.module_from_spec(s);s.loader.exec_module(r);normal=r.normal
raw=normal.SOURCE.read_bytes();assert hashlib.sha256(raw).hexdigest()==r.SOURCE_SHA
rows=[x for x in json.loads(raw)['cases'] if x['id'] in {'RUN-'+str(n) for n in range(604,610)}];assert len(rows)==6
out=ROOT/'cases/boundary-completion';out.mkdir(exist_ok=True)
for row in rows:
 a=normal.initial(row['config'],'controlled');b=normal.version(copy.deepcopy(a),('f','b'))
 targets=[{'role':role,'versions':{'B':2},'ordinals':{'1':'B','2':'B'}} for role in ('frontend','backend')]
 step=normal.step('sparse-target-automatic-completion-then-restart',b,action='sparse-completion-boundary',release='none',stableSeconds=30,timeoutSeconds=420,expect={'noReplacement':True,'noNewRevision':True,'requireCompleted':True,'targets':targets})
 case={'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':[step]}}
 (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
(out/'suite.json').write_text(json.dumps({'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':r.SOURCE_SHA,'developmentScope':'Six actual B12 sources with exact original R2 budget and coordination. Preparation uses real R3 B creation, finite B0 removal and fresh source sync. Old A current status is then restored; automatic completion is mandatory before final controller restart. No source cleanup or restart may manufacture post-boundary completion.','cases':rows,'inputs':{x['id']:hashlib.sha256((out/(x['id']+'.yaml')).read_bytes()).hexdigest() for x in rows}},ensure_ascii=False,indent=2)+'\n')
print('Generated six automatic sparse completion cases.')
