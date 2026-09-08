#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Actual precreated history conflicts followed by native AlreadyExists races."""
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
    ids = {'RUN-533','RUN-534'}
    rows = [r for r in json.loads(raw)['cases'] if r['id'] in ids]; assert len(rows) == 2
    out = ROOT/'cases/history-collision'; out.mkdir(exist_ok=True)
    for row in rows:
        config = row['config']; assert set(config) <= {'mode','n','recovery','roles','coordination','revisionHistoryLimit','crCollision'}
        a = normal.initial(config, 'controlled'); b = normal.version(copy.deepcopy(a))
        step = normal.step('precreated-conflict-native-AlreadyExists-and-automatic-B',b,action='history-collision-recovery',stableSeconds=30,timeoutSeconds=420,expect={'targets':[{'role':'frontend','versions':{'B':3}},{'role':'backend','versions':{'A':3},'ordinals':{'0':'A','1':'A','2':'A'}}]})
        case = {'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,
                'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':[step]}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    manifest = {'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,
                'developmentScope':'RUN-533/534 actual precreated collision and controlled native POST race; both blocks must retain original A, then clear only exact fixture UID and automatically complete B',
                'cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}}
    (out/'suite.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
    print('Generated two actual native history collision scenarios.')


if __name__ == '__main__': main()
