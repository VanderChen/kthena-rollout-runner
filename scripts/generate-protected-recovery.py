#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""RUN-304: historical template/worker layout with the latest Role replicas."""
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
    row = next(row for row in json.loads(raw)['cases'] if row['id'] == 'RUN-304')
    assert row['config'] == {'coordination': 'omitted', 'mode': 'SG', 'n': 3, 'recovery': 'omitted',
                             'roles': {'b': {'r': 1, 'w': 0}, 'f': {'r': 1, 'w': 1}}, 'top': {'p': 1, 's': 1, 'u': 0}}
    a = normal.initial(row['config'], 'controlled')
    b = normal.version(copy.deepcopy(a))
    normal.role(b).update(replicas=2, workerReplicas=2)
    targets = [{'scope': 'SG', 'versions': {'A': 1, 'B': 2}, 'ordinals': {'0': 'A'}},
               {'group': 0, 'role': 'frontend', 'versions': {'A': 2}, 'workers': {'A': 1}, 'ordinals': {'0': 'A', '1': 'A'}},
               {'role': 'frontend', 'workers': {'A': 1, 'B': 2}},
               {'role': 'backend', 'versions': {'A': 1}, 'workers': {'A': 0}}]
    step = normal.step('pending-B-expand-R-W-delete-protected-entry', b, action='recover-pod',
                       podFault={'group': 0, 'role': 'frontend', 'ordinal': 0, 'member': 'entry'},
                       stableSeconds=30, timeoutSeconds=420, expect={'targets': targets})
    case = {'format': 'rollout-runner/v3', 'id': row['id'], 'baseline': normal.COMMIT,
            'scenario': {'source': row, 'profile': 'controlled', 'initialSpec': a, 'steps': [step]}}
    out = ROOT/'cases/protected-recovery'
    out.mkdir(exist_ok=True)
    (out/'RUN-304.yaml').write_text(normal.yaml(case)+'\n')
    manifest = {'format': 'rollout-runner/suite-v1', 'controllerCommit': normal.COMMIT,
                'sourceSHA256': recovery.SOURCE_SHA, 'developmentScope': 'RUN-304 only; protected A/W1 with latest R2',
                'cases': [row], 'inputs': {'RUN-304': hashlib.sha256((out/'RUN-304.yaml').read_bytes()).hexdigest()}}
    (out/'suite.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
    print('Generated RUN-304 protected historical recovery with latest Role replicas.')


if __name__ == '__main__':
    main()
