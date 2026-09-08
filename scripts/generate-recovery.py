#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Compile explicit fault actions; never interpret catalogue prose at runtime."""
import copy
import hashlib
import importlib.util
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('normal_generator', ROOT/'scripts/generate-normal.py')
normal = importlib.util.module_from_spec(loader)
loader.loader.exec_module(normal)
SOURCE_SHA = '757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'


def plan(row):
    n = int(row['id'][4:])
    if not 305 <= n <= 388:
        raise ValueError('recovery action not implemented: '+row['id'])
    config = row['config']
    if set(config) - {'mode', 'n', 'recovery', 'roles', 'top', 'coordination'}:
        raise ValueError('uncompiled recovery config field')
    a = normal.initial(config, 'controlled')
    b = normal.version(copy.deepcopy(a))
    partition = normal.effective_budget(b, 'partition')
    desired = normal.desired(b)
    member = 'entry' if n % 2 else 'worker'
    assert ('f '+member+' Pod') in row['action']
    assert ('删除受保护ordinal0' if partition else '删除未保护ordinal0') in row['action']
    counts = {'B': desired-partition}
    if partition:
        counts['A'] = partition
    ordinals = {str(i): 'A' for i in range(partition)}
    if normal.effective_budget(b, 'maxSurge') == 0:
        ordinals.update({str(i): 'B' for i in range(partition, desired)})
    target = {'versions': counts, 'ordinals': ordinals}
    if config['mode'] == 'SG':
        target['scope'] = 'SG'
        targets = [target, {'role': 'frontend', 'workers': {'A': 1, 'B': 1}},
                   {'role': 'backend', 'versions': {'A': 1}, 'workers': {'A': 0}}]
    else:
        target.update(role='frontend', workers={'A': 1, 'B': 1})
        targets = [target, {'role': 'backend', 'versions': {'A': 3}, 'workers': {'A': 0},
                            'ordinals': {str(i): 'A' for i in range(3)}}]
    step = normal.step('pending-B-delete-'+member+'-and-recover', b,
                       action='recover-pod', stableSeconds=30, timeoutSeconds=420,
                       podFault={'group': 0, 'role': 'frontend', 'ordinal': 0, 'member': member},
                       expect={'targets': targets})
    return {'format': 'rollout-runner/v3', 'id': row['id'], 'baseline': normal.COMMIT,
            'scenario': {'source': row, 'profile': 'controlled', 'initialSpec': a, 'steps': [step]}}


def main():
    data = normal.SOURCE.read_bytes()
    assert hashlib.sha256(data).hexdigest() == SOURCE_SHA, 'review changed source before generation'
    rows = [r for r in json.loads(data)['cases'] if r['id'].startswith('RUN-') and 305 <= int(r['id'][4:]) <= 388]
    assert len(rows) == 84
    out = ROOT/'cases/recovery'
    out.mkdir(exist_ok=True)
    for row in rows:
        (out/(row['id']+'.yaml')).write_text(normal.yaml(plan(row))+'\n')
    manifest = {'format': 'rollout-runner/suite-v1', 'controllerCommit': normal.COMMIT,
                'sourceSHA256': SOURCE_SHA, 'developmentScope': 'RUN-305..388 only; category2 total236 remains incomplete',
                'cases': rows, 'inputs': {r['id']: hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}}
    (out/'suite.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
    print('Generated84 explicit Pod-deletion recovery cases; remaining category2 actions still required.')


if __name__ == '__main__':
    main()
