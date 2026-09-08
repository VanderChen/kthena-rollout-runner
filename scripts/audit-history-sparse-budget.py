#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Prove a post-restoration healthy deletion crossing the sparse source budget."""
import collections
import hashlib
import importlib.util
import json
import pathlib

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('sparse_history',ROOT/'scripts/audit-history-create.py')
h=importlib.util.module_from_spec(loader);loader.loader.exec_module(h);m=h.m
loader=importlib.util.spec_from_file_location('sparse_source',ROOT/'scripts/audit-history-sparse-fixture.py')
f=importlib.util.module_from_spec(loader);loader.loader.exec_module(f)


def audit(p,trace):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');n=int(case['id'][4:]);assert 463<=n<=522 and (n-463)%10>=5
    assert result['status']=='FAIL' and 'BUDGET_VIOLATION' in result['error'] and not (p/'step-02-request.yaml').exists()
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
    source=next(c for c in json.loads(raw)['cases'] if c['id']==case['id']);assert source==case['scenario']['source']
    rows=[json.loads(s) for s in (p/'observations.jsonl').read_text().splitlines()];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    preparation=f.audit(p,rows);original=m.yaml(p/'source-initial-server.yaml');owner=original['metadata']['uid'];base=m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner)
    role=source['config']['roles']['f'];assert isinstance(role['u'],int) and isinstance(role['p'],int);minimum=3-role['u']
    rule=m.read(p/'step-01-create-installed.json');errors=[t for t in trace if t.get('ruleID')==rule['id'] and t['action']=='error-request'];clears=[t for t in trace if t.get('ruleID')==rule['id'] and t['action']=='rule-cleared'];assert errors and len(clears)==1
    assert rule['namespace']==result['namespace'] and rule['ownerUID']==owner and rule['methods']==['POST'] and rule['resource']=='controllerrevisions'
    assert all(t['status']==503 and t['resource']=='controllerrevisions' and t['method']=='POST' for t in errors)
    saved=m.read(p/'step-01-before-create-clear.json');assert not saved['errors'] and next(r for r in saved['rules'] if r['id']==rule['id'])['hits']==len(errors)
    clear=m.ts(clears[0]['at']);cps=[m.read(x) for x in p.glob('checkpoint*.json')];hold=next(c for c in cps if c['phase']=='actual-CR-create-failure-no-template-actions');assert hold['elapsedStableNanos']>=10_000_000_000 and m.ts(hold['completed'])<clear
    b=m.yaml(p/'step-01-server.yaml');assert b['metadata']['uid']==owner and b['metadata']['generation']==4 and m.version(next(r for r in b['spec']['template']['roles'] if r['name']=='frontend')['entryTemplate'])=='B'
    state=collections.defaultdict(dict);deletions=[];violations=[];histories={}
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];previous=state[kind].get(uid);at=m.ts(row['received'])
        if kind=='controllerrevisions' and m.owned(o,owner):
            assert uid not in histories or histories[uid]==o['data'];histories[uid]=o['data']
        if row['sequence']>preparation['lastPreparationSequence'] and kind=='pods' and m.owned(o,owner):
            lab=o['metadata']['labels'];ordinal=int(lab[m.I].rsplit('-',1)[1])
            if lab[m.R]=='backend' or uid in base and ordinal<role['p']:
                assert uid in base and row['event']!='DELETED' and not o['metadata'].get('deletionTimestamp') and m.version(o)=='A'
            if previous and m.ready(previous) and (row['event']=='DELETED' or o['metadata'].get('deletionTimestamp')):
                assert at>clear and lab[m.R]=='frontend' and ordinal>=role['p']
                ready={u:pod for u,pod in h.frontend(state,owner).items() if m.ready(pod)}
                proof={'sequence':row['sequence'],'at':row['received'],'uid':uid,'name':o['metadata']['name'],'ordinal':ordinal,'readyBefore':len(ready),'readyUIDsBefore':list(ready),'minimum':minimum}
                deletions.append(proof)
                if len(ready)-1<minimum:violations.append(proof)
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
        if m.ts(rule['installed'])<=at<clear:
            pods=m.mine(state,'pods',owner);assert set(pods)==set(base) and all(m.ready(v) for v in pods.values())
    assert violations
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id':case['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'HEALTHY_DELETION_BELOW_BUDGET_AFTER_SPARSE_HISTORY_RESTORE','watchRows':len(rows),'sparsePreparation':preparation,'actualFailedCRCreates':len(errors),'deletions':deletions,'firstViolation':violations[0],'limitation':'Source sparse population, actual failed history writes, safe fault interval and later healthy deletion are independently proven. Failure occurs during allowed B rollout; subsequent C, pinned terminating reference and final controller restart were not executed. No Kthena changes.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ('result.json','observations.jsonl','source-boundary.json','final-resources.yaml')}}
