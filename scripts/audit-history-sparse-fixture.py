#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Prove the physical sparse source boundary independently of its runner ledger."""
import collections
import hashlib
import importlib.util
import pathlib

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('sparse_mid',ROOT/'scripts/audit-midrollout.py')
m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)


def audit(p,rows):
    q=p/'fixture-preparation';boundary=m.read(p/'source-boundary.json')
    original=m.yaml(p/'before-server.yaml');source=m.yaml(p/'source-initial-server.yaml');owner=original['metadata']['uid']
    expanded=m.yaml(q/'step-00-server.yaml');restore=m.yaml(q/'restore-source-R3-server.yaml')
    assert original['metadata']['generation']==1 and expanded['metadata']['generation']==2 and source['metadata']['generation']==3
    assert all(o['metadata']['uid']==owner for o in (expanded,restore,source)) and restore['spec']==source['spec']==original['spec']
    roles={r['name']:r for r in expanded['spec']['template']['roles']};assert roles['frontend']['replicas']==5
    spec=source['spec']
    assert {r['name']:r['replicas'] for r in spec['template']['roles']}=={'frontend':3,'backend':3}
    source_roles={r['name']:r for r in source['spec']['template']['roles']}
    for name,role in roles.items():
        restored=dict(role);restored['replicas']=3;assert restored==source_roles[name]
    eight={o['metadata']['uid']:o for o in m.yaml(q/'expanded-pods.yaml')['items'] if m.owned(o,owner)}
    assert len(eight)==8 and all(m.ready(o) and m.version(o)=='A' for o in eight.values())
    removed=boundary['deletedPreparationUIDs'];retained=boundary['retainedUIDs']
    assert len(removed)==2 and len(retained)==6 and set(removed.values())|set(retained.values())==set(eight)
    for name,uid in removed.items():
        o=eight[uid];assert o['metadata']['name']==name and o['metadata']['labels'][m.R]=='frontend' and int(o['metadata']['labels'][m.I].rsplit('-',1)[1]) in (1,2)
    receipts=m.read(q/'external-deletes.json');assert len(receipts)==2
    for receipt in receipts:
        assert receipt['accepted'] and removed[receipt['name']]==receipt['uid']==receipt['options']['preconditions']['uid']
        assert receipt['options']['gracePeriodSeconds']==0 and m.ts(receipt['received'])<m.ts(boundary['at'])
        assert any(r['kind']=='pods' and r['event']=='DELETED' and r['object']['metadata']['uid']==receipt['uid'] and r['sequence']<=boundary['lastPreparationSequence'] for r in rows)
    rules=m.read(q/'fully-delivered-before-restart.json');assert not rules['errors']
    selected=[m.read(q/('sparse-fault-rule-%d.json'%i))['id'] for i in range(9)]
    for rid in selected:
        rule=next(r for r in rules['rules'] if r['id']==rid);assert not rule['active'] and rule['hits']==rule['released']
    assert sum(r['hits'] for r in rules['rules'] if r['id'] in selected)>0, 'fixture pause never intercepted actual controller traffic'
    gen=m.yaml(q/'source-generation-before-restart.yaml');assert gen['status']['observedGeneration']>=3
    before=m.yaml(q/'fixture-restart-controller-terminated.yaml');after=m.yaml(q/'fixture-restart-controller-replacement.yaml')
    receipt=m.read(q/'fixture-restart-controller-delete.json')
    assert receipt['accepted'] and receipt['uid']==before['metadata']['uid']==receipt['options']['preconditions']['uid']
    assert before['metadata']['uid']!=after['metadata']['uid'] and before['metadata']['ownerReferences']==after['metadata']['ownerReferences']
    assert all(o['status']['containerStatuses'][0]['restartCount']==0 for o in (before,after))
    assert before['status']['containerStatuses'][0]['imageID']==after['status']['containerStatuses'][0]['imageID']=='sha256:7c6ed6c78b37d7afaf104381351554ad75aed56c64d0aca2ec941ce652756265'
    assert before['spec']['containers'][0]['args']==after['spec']['containers'][0]['args']
    assert m.ts(receipt['received'])<m.ts(boundary['at'])
    base=m.yaml(p/'baseline-resources.yaml');pods=m.mine(base,'pods',owner)
    assert set(pods)==set(retained.values()) and all(m.ready(o) and m.version(o)=='A' for o in pods.values())
    for uid,o in pods.items():assert o['spec']==eight[uid]['spec'] and o['metadata']['labels']==eight[uid]['metadata']['labels']
    groups=collections.defaultdict(set)
    for o in pods.values():groups[o['metadata']['labels'][m.R]].add(int(o['metadata']['labels'][m.I].rsplit('-',1)[1]))
    assert dict(groups)=={'frontend':{0,3,4},'backend':{0,1,2}}
    pg=m.mine(base,'podgroups',owner);assert len(pg)==1 and next(iter(pg.values()))['spec']['minMember']==6
    assert len(m.mine(base,'configmaps',owner))==6 and not m.mine(base,'services',owner)
    crs=m.mine(base,'controllerrevisions',owner);assert len(crs)==1
    for o in pods.values():assert 'model-'+o['metadata']['labels']['modelserving.volcano.sh/revision']==next(iter(crs.values()))['metadata']['name']
    prep=[r for r in rows if r['sequence']<=boundary['lastPreparationSequence']]
    assert prep and m.ts(prep[-1]['received'])<=m.ts(boundary['at'])
    state=collections.defaultdict(dict)
    for row in prep:
        uid=row['object']['metadata']['uid']
        if row['event']=='DELETED':state[row['kind']].pop(uid,None)
        else:state[row['kind']][uid]=row['object']
    assert set(m.mine(state,'pods',owner))==set(pods)
    if (q/'preparation-resource-cleanup.json').exists():
        for item in m.read(q/'preparation-resource-cleanup.json'):
            assert item['accepted'] and item['uid']==item['options']['preconditions']['uid'] and m.ts(item['received'])<m.ts(boundary['at'])
            cm=m.yaml(q/('orphan-preparation-'+item['name']+'.yaml'))
            assert cm['metadata']['uid']==item['uid'] and m.owned(cm,owner) and cm['metadata']['labels'][m.R]=='frontend'
            assert int(cm['metadata']['labels'][m.I].rsplit('-',1)[1]) in (1,2)
    cps=[m.read(x) for x in q.glob('checkpoint-*.json')]
    cp=next(cp for cp in cps if cp['phase']=='sparse-source-A-established');assert cp['elapsedStableNanos']>=10_000_000_000
    assert m.ts(cp['completed'])<=m.ts(boundary['at'])
    return {'lastPreparationSequence':boundary['lastPreparationSequence'],'sourceGeneration':3,'retainedUIDs':retained,'deletedPreparationUIDs':removed,'preparationControllerUID':after['metadata']['uid'],'stableNanos':cp['elapsedStableNanos'],'sourceBoundarySHA256':hashlib.sha256((p/'source-boundary.json').read_bytes()).hexdigest()}
