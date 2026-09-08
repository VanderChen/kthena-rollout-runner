#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Six lost old-Pod deletion notifications with default production live audit."""
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
    rows = [r for r in json.loads(raw)['cases'] if r['id'] in {f'RUN-{n}' for n in range(443,449)}]; assert len(rows) == 6
    out = ROOT/'cases/lost-deletion'; out.mkdir(exist_ok=True)
    for row in rows:
        config = row['config']; assert set(config) <= {'mode','n','recovery','roles','top','coordination'}
        assert config['roles']['f']['w'] == 0
        a = normal.initial(config, 'controlled'); b = normal.version(copy.deepcopy(a))
        targets = [{'scope':'SG','versions':{'B':3}}] if config['mode'] == 'SG' else [
            {'role':'frontend','versions':{'B':3},'workers':{'B':0}},
            {'role':'backend','versions':{'A':3},'workers':{'A':0},'ordinals':{'0':'A','1':'A','2':'A'}}]
        step = normal.step('drop-old-instance-notifications-and-recover-with-live-API', b,
                           action='drop-old-deletions', stableSeconds=30, timeoutSeconds=660,
                           expect={'targets':targets})
        case = {'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,
                'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':[step]}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    manifest = {'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,
                'developmentScope':'RUN-443..448; drop all terminating/deleted frames for captured first old frontend UID throughout same-controller recovery; preserve default 300s audit and 30s audit timeout, 660s case window',
                'cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}}
    (out/'suite.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
    print('Generated six old-UID notification loss scenarios.')


if __name__ == '__main__': main()
