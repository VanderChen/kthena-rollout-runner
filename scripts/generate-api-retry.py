#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Eight finite failures on actual controller API requests during B rollout."""
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


def main():
    raw = normal.SOURCE.read_bytes()
    assert hashlib.sha256(raw).hexdigest() == recovery.SOURCE_SHA
    rows = [r for r in json.loads(raw)['cases'] if r['id'] in {f'RUN-{n}' for n in range(455, 463)}]
    assert len(rows) == 8
    out = ROOT/'cases/api-retry'; out.mkdir(exist_ok=True)
    for row in rows:
        config = row['config']
        assert set(config) <= {'mode', 'n', 'recovery', 'roles', 'top', 'coordination'}
        a = normal.initial(config, 'controlled'); b = normal.version(copy.deepcopy(a))
        assert normal.effective_budget(b, 'maxUnavailable') == 0 and normal.effective_budget(b, 'maxSurge') == 1
        targets = [{'scope': 'SG', 'versions': {'B': 3}}] if config['mode'] == 'SG' else [
            {'role': 'frontend', 'versions': {'B': 3}, 'workers': {'B': 0}},
            {'role': 'backend', 'versions': {'A': 3}, 'workers': {'A': 0}, 'ordinals': {'0': 'A', '1': 'A', '2': 'A'}}]
        steps = [normal.step('B-surge-blocked-with-all-old-A-healthy', b, until='conditions',
                            conditions=[{'kind': 'running-not-ready', 'role': 'frontend', 'version': 'B', 'count': 1}],
                            release='none', holdSeconds=5, stableSeconds=0, timeoutSeconds=180),
                 normal.step('one-real-API-error-and-autonomous-B-recovery', action='retry-api-error',
                             stableSeconds=30, timeoutSeconds=420, expect={'targets': targets})]
        case = {'format': 'rollout-runner/v3', 'id': row['id'], 'baseline': normal.COMMIT,
                'scenario': {'source': row, 'profile': 'controlled', 'initialSpec': a, 'steps': steps}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    manifest = {'format': 'rollout-runner/suite-v1', 'controllerCommit': normal.COMMIT,
                'sourceSHA256': recovery.SOURCE_SHA,
                'developmentScope': 'RUN-455..462; fault after first B surge exists, before releasing B; Create faults the next owned Pod POST, Delete targets the actual UID-preconditioned native Pod DELETE after audit-client expansion, Status binds B generation, List excludes named GET and Watch',
                'cases': rows, 'inputs': {row['id']: hashlib.sha256((out/(row['id']+'.yaml')).read_bytes()).hexdigest() for row in rows}}
    (out/'suite.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
    print('Generated eight finite actual API retry scenarios.')


if __name__ == '__main__': main()
