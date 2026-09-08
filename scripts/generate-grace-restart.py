#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Compile actual same-Pod restarts inside an unfinished B rollout."""
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
    assert row['id'] in ('RUN-431', 'RUN-432')
    config = row['config']
    assert set(config) <= {'mode', 'n', 'recovery', 'roles', 'top', 'coordination', 'restartGracePeriodSeconds'}
    assert config['recovery'] == 'RoleRecreate' and config['restartGracePeriodSeconds'] == 30
    a = normal.initial(config, 'controlled')
    a['template']['restartGracePeriodSeconds'] = config['restartGracePeriodSeconds']
    entry = normal.role(a)['entryTemplate']['spec']
    entry['restartPolicy'] = 'Always'
    entry['containers'][0]['command'] = ['sh', '-c', "trap 'exit 42' USR1; while :; do sleep 1 & wait $!; done"]
    b = normal.version(copy.deepcopy(a))
    if config['mode'] == 'SG':
        targets = [{'scope': 'SG', 'versions': {'B': 3}}, {'role': 'frontend', 'workers': {'B': 1}}]
    else:
        targets = [{'role': 'frontend', 'versions': {'B': 3}, 'workers': {'B': 1}},
                   {'role': 'backend', 'versions': {'A': 3}, 'workers': {'A': 0}, 'ordinals': {'0': 'A', '1': 'A', '2': 'A'}}]
    old = {'kind': 'unit', 'role': 'frontend', 'group': 0, 'ordinal': 0, 'version': 'A', 'ready': True, 'count': 1}
    blocked = {'kind': 'running-not-ready', 'role': 'frontend', 'version': 'B', 'count': 1}
    steps = [normal.step('start-B-and-hold-new-readiness', b, until='conditions', conditions=[old, blocked],
                         release='none', holdSeconds=5, stableSeconds=0, timeoutSeconds=180),
             normal.step('old-entry-restarts-within-template-grace', action='restart-container-grace',
                         containerRestart={'group': 0, 'role': 'frontend', 'ordinal': 0, 'member': 'entry'},
                         until='conditions', conditions=[old, blocked], release='none', holdSeconds=5,
                         stableSeconds=0, timeoutSeconds=45, expect={'noNewRevision': True}),
             normal.step('finish-B-after-bounded-restart', action='resume-after-grace', stableSeconds=30, timeoutSeconds=420,
                         expect={'targets': targets})]
    return {'format': 'rollout-runner/v3', 'id': row['id'], 'baseline': normal.COMMIT,
            'scenario': {'source': row, 'profile': 'controlled', 'initialSpec': a, 'steps': steps}}


def main():
    raw = normal.SOURCE.read_bytes()
    assert hashlib.sha256(raw).hexdigest() == recovery.SOURCE_SHA
    rows = [row for row in json.loads(raw)['cases'] if row['id'] in ('RUN-431', 'RUN-432')]
    assert len(rows) == 2
    out = ROOT/'cases/grace-restart'
    out.mkdir(exist_ok=True)
    for row in rows: (out/(row['id']+'.yaml')).write_text(normal.yaml(plan(row))+'\n')
    manifest = {'format': 'rollout-runner/suite-v1', 'controllerCommit': normal.COMMIT,
                'sourceSHA256': recovery.SOURCE_SHA, 'developmentScope': 'RUN-431..432; actual process exit within accepted 30-second grace during B rollout',
                'cases': rows, 'inputs': {row['id']: hashlib.sha256((out/(row['id']+'.yaml')).read_bytes()).hexdigest() for row in rows}}
    (out/'suite.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
    print('Generated two explicit grace-period container restart scenarios.')


if __name__ == '__main__':
    main()
