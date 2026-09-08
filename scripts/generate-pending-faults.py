#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Six resource-constrained Pending scenarios with an explicit capacity proof."""
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
    rows = [r for r in json.loads(raw)['cases'] if r['id'] in {'RUN-401','RUN-405','RUN-409','RUN-413','RUN-417','RUN-421'}]
    assert len(rows) == 6
    out = ROOT/'cases/pending-faults'; out.mkdir(exist_ok=True)
    for row in rows:
        config = row['config']
        assert set(config) <= {'mode','n','recovery','roles','top','coordination'}
        a = normal.initial(config, 'controlled')
        b = normal.version(copy.deepcopy(a))
        # Replacing one old 5m Pod releases only 5m. A 20m B stays genuinely
        # unschedulable even for U=1/S=0 while the remaining capacity is held.
        normal.role(b)['entryTemplate']['spec']['containers'][0]['resources']['requests']['cpu'] = '20m'
        condition = {'kind':'pending','role':'frontend','version':'B','count':1}
        targets = [{'scope':'SG','versions':{'B':3}}] if config['mode']=='SG' else [{'role':'frontend','versions':{'B':3},'workers':{'B':0}}, {'role':'backend','versions':{'A':3},'workers':{'A':0},'ordinals':{'0':'A','1':'A','2':'A'}}]
        steps = [normal.step('occupy-free-node-CPU', action='block-resources', release='none', stableSeconds=0, expect={'noReplacement':True,'noNewRevision':True}),
                 normal.step('B-Pending-under-real-resource-pressure', b, until='conditions', conditions=[condition], release='none', holdSeconds=10, stableSeconds=0),
                 normal.step('prove-insufficient-node-CPU-for-pending-B', action='verify-resource-stop', until='conditions', conditions=[condition], release='none', stableSeconds=0),
                 normal.step('release-capacity-and-complete-B', action='restore-resources', stableSeconds=30, expect={'targets':targets})]
        for step in steps: step['timeoutSeconds'] = 420
        case = {'format':'rollout-runner/v3','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':steps}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    manifest = {'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,'developmentScope':'six actual resource Pending cases; B fixture CPU20m exceeds one released A CPU5m','cases':rows,'inputs':{row['id']:hashlib.sha256((out/(row['id']+'.yaml')).read_bytes()).hexdigest() for row in rows}}
    (out/'suite.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
    print('Generated six Pending scenarios with independent node CPU proof.')


if __name__ == '__main__': main()
