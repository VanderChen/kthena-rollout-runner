#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Explicit readiness/image/termination actions; Pending resource faults follow separately."""
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
    n = int(row['id'][4:])
    assert 401 <= n <= 430 and (n >= 425 or (n-401) % 4 != 0)
    config = row['config']
    assert set(config) <= {'mode', 'n', 'recovery', 'roles', 'top', 'coordination'}
    a = normal.initial(config, 'controlled')
    b = normal.version(copy.deepcopy(a))
    target = {'scope': 'SG', 'versions': {'B': 3}} if config['mode'] == 'SG' else {'role': 'frontend', 'versions': {'B': 3}, 'workers': {'B': 0}}
    targets = [target]
    if config['mode'] == 'Role':
        targets.append({'role': 'backend', 'versions': {'A': 3}, 'workers': {'A': 0}, 'ordinals': {'0': 'A', '1': 'A', '2': 'A'}})
    not_ready = {'kind': 'running-not-ready', 'role': 'frontend', 'version': 'B', 'count': 1}
    if n >= 425:
        selected = {'kind': 'unit', 'version': 'A', 'ordinal': 2, 'count': 1}
        term = {'kind': 'terminating', 'version': 'A', 'ordinal': 2, 'count': 1}
        if config['mode'] == 'Role':
            selected['role'] = 'frontend'; term['role'] = 'frontend'
        steps = [normal.step('pin-first-old-instance', action='pin', conditions=[selected], release='none', stableSeconds=0, expect={'noReplacement': True, 'noNewRevision': True}),
                 normal.step('B-with-old-instance-terminating', b, until='conditions', conditions=[term], holdSeconds=10, stableSeconds=0),
                 normal.step('remove-finalizer-and-complete-B', action='unpin', stableSeconds=30, expect={'targets': targets})]
    elif (n-401) % 4 == 1:
        steps = [normal.step('B-running-with-failed-readiness', b, until='conditions', conditions=[not_ready], release='none', holdSeconds=10, stableSeconds=0),
                 normal.step('release-readiness-and-complete-B', stableSeconds=30, expect={'targets': targets})]
    elif (n-401) % 4 == 2:
        first_ready = {'kind': 'unit', 'role': 'frontend', 'version': 'B', 'ready': True, 'count': 1}
        steps = [normal.step('first-B-Ready-with-healthy-A', b, until='conditions', conditions=[first_ready], stableSeconds=0),
                 normal.step('withdraw-first-B-readiness', action='drop-ready', until='conditions', conditions=[not_ready], release='none', holdSeconds=10, stableSeconds=0),
                 normal.step('restore-same-B-readiness-and-complete', action='restore-ready', stableSeconds=30, expect={'targets': targets})]
    else:
        bad = normal.role(b)['entryTemplate']['spec']['containers'][0]
        # The node-local closed port produces a real pull failure without
        # depending on a public registry or modifying an image behind a tag.
        bad['image'] = '127.0.0.1:1/rollout-runner-missing:case-'+str(n)
        bad['imagePullPolicy'] = 'Always'
        c = normal.version(copy.deepcopy(a), v='C')
        target['versions'] = {'C': 3}
        if 'workers' in target: target['workers'] = {'C': 0}
        steps = [normal.step('B-real-ImagePullBackOff', b, until='conditions', conditions=[{'kind': 'image-pull-backoff', 'role': 'frontend', 'version': 'B', 'count': 1}], release='none', holdSeconds=10, stableSeconds=0),
                 normal.step('replace-broken-B-with-pullable-C', c, stableSeconds=30, expect={'targets': targets})]
    for step in steps: step['timeoutSeconds'] = 420
    return {'format': 'rollout-runner/v3', 'id': row['id'], 'baseline': normal.COMMIT,
            'scenario': {'source': row, 'profile': 'controlled', 'initialSpec': a, 'steps': steps}}


def main():
    raw = normal.SOURCE.read_bytes()
    assert hashlib.sha256(raw).hexdigest() == recovery.SOURCE_SHA
    rows = [row for row in json.loads(raw)['cases'] if row['id'].startswith('RUN-') and 401 <= int(row['id'][4:]) <= 430 and (int(row['id'][4:]) >= 425 or (int(row['id'][4:])-401) % 4 != 0)]
    assert len(rows) == 24
    out = ROOT/'cases/midrollout-faults'
    out.mkdir(exist_ok=True)
    for row in rows: (out/(row['id']+'.yaml')).write_text(normal.yaml(plan(row))+'\n')
    manifest = {'format': 'rollout-runner/suite-v1', 'controllerCommit': normal.COMMIT,
                'sourceSHA256': recovery.SOURCE_SHA, 'developmentScope': '24 readiness/image/termination scenarios; six Pending resource cases remain unimplemented',
                'cases': rows, 'inputs': {row['id']: hashlib.sha256((out/(row['id']+'.yaml')).read_bytes()).hexdigest() for row in rows}}
    (out/'suite.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
    print('Generated 24 explicit readiness/image/termination scenarios.')


if __name__ == '__main__':
    main()
