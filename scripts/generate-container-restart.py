#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Compile the twelve true same-Pod container restart scenarios."""
import copy
import hashlib
import importlib.util
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('recovery_generator', ROOT/'scripts/generate-recovery.py')
recovery = importlib.util.module_from_spec(loader)
loader.loader.exec_module(recovery)
normal = recovery.normal


def plan(row):
    assert 389 <= int(row['id'][4:]) <= 400
    config = row['config']
    assert set(config) <= {'mode', 'n', 'recovery', 'roles', 'top', 'coordination'}
    assert config['recovery'] == 'None'
    a = normal.initial(config, 'controlled')
    entry = normal.role(a)['entryTemplate']['spec']
    entry['restartPolicy'] = 'Always'
    entry['containers'][0]['command'] = ['sh', '-c', "trap 'exit 42' USR1; while :; do sleep 1 & wait $!; done"]
    b = normal.version(copy.deepcopy(a))
    d, p = normal.desired(b), normal.effective_budget(b, 'partition')
    versions = {'B': d-p}
    if p:
        versions['A'] = p
    ordinals = {str(i): 'A' for i in range(p)}
    if normal.effective_budget(b, 'maxSurge') == 0:
        ordinals.update({str(i): 'B' for i in range(p, d)})
    target = {'versions': versions, 'ordinals': ordinals}
    if config['mode'] == 'SG':
        target['scope'] = 'SG'
        targets = [target, {'role': 'frontend', 'workers': {'A': 1, 'B': 1}}]
    else:
        target.update(role='frontend', workers={'A': 1, 'B': 1})
        targets = [target, {'role': 'backend', 'versions': {'A': 3}, 'workers': {'A': 0}, 'ordinals': {str(i): 'A' for i in range(3)}}]
    steps = [normal.step('exit-entry-process-and-recover-same-pod', action='restart-container',
                         containerRestart={'group': 0, 'role': 'frontend', 'ordinal': 0, 'member': 'entry'},
                         release='none', stableSeconds=30, timeoutSeconds=180,
                         expect={'noReplacement': True, 'noNewRevision': True}),
             normal.step('rollout-B-after-container-recovered', b, stableSeconds=30, timeoutSeconds=420,
                         expect={'targets': targets})]
    return {'format': 'rollout-runner/v3', 'id': row['id'], 'baseline': normal.COMMIT,
            'scenario': {'source': row, 'profile': 'controlled', 'initialSpec': a, 'steps': steps}}


def main():
    raw = normal.SOURCE.read_bytes()
    assert hashlib.sha256(raw).hexdigest() == recovery.SOURCE_SHA
    rows = [row for row in json.loads(raw)['cases'] if row['id'].startswith('RUN-') and 389 <= int(row['id'][4:]) <= 400]
    assert len(rows) == 12
    out = ROOT/'cases/container-restart'
    out.mkdir(exist_ok=True)
    for row in rows:
        (out/(row['id']+'.yaml')).write_text(normal.yaml(plan(row))+'\n')
    manifest = {'format': 'rollout-runner/suite-v1', 'controllerCommit': normal.COMMIT,
                'sourceSHA256': recovery.SOURCE_SHA, 'developmentScope': 'RUN-389..400 only; real process exit, never Pod deletion',
                'cases': rows, 'inputs': {row['id']: hashlib.sha256((out/(row['id']+'.yaml')).read_bytes()).hexdigest() for row in rows}}
    (out/'suite.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
    print('Generated 12 explicit in-place container restart scenarios.')


if __name__ == '__main__':
    main()
