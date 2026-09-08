#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""History creation failures, B/C live references and restart: actual sparse ordinals {0,3,4}."""
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
    ids = {f'RUN-{n}' for n in range(463,523) if (n-463)%10 >= 5}
    rows = [r for r in json.loads(raw)['cases'] if r['id'] in ids]; assert len(rows) == 30
    out = ROOT/'cases/history-sparse'; out.mkdir(exist_ok=True)
    for row in rows:
        assert 'O={0,3,4}' in row['initial']
        config = row['config']; assert set(config) <= {'mode','n','recovery','roles','coordination','revisionHistoryLimit'}
        a = normal.initial(config, 'controlled'); b = normal.version(copy.deepcopy(a))
        step = normal.step('persist-B-before-actions-then-C-live-references-and-restart', b,
                           action='history-create-recovery', stableSeconds=30, timeoutSeconds=420)
        case = {'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,
                'scenario':{'fixture':'sparse-history-A','source':row,'profile':'controlled','initialSpec':a,'steps':[step]}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    manifest = {'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,
                'developmentScope':'30 sparse history cases; exact source fixture built and independently bounded before fault injection',
                'cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}}
    (out/'suite.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
    print('Generated 30 sparse-ordinal history creation recovery scenarios.')


if __name__ == '__main__': main()
