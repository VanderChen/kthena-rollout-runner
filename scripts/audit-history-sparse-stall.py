#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Prove a complete sparse source that never updates after actual history-create recovery."""
import collections
import hashlib
import importlib.util
import json
import pathlib

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('stall_sparse',ROOT/'scripts/audit-history-sparse-fixture.py')
f=importlib.util.module_from_spec(loader);loader.loader.exec_module(f);m=f.m
REV='modelserving.volcano.sh/revision'


def audit(p,trace):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');n=int(case['id'][4:]);source=case['scenario']['source']
    assert 463<=n<=522 and (n-463)%10>=5 and result['status']=='FAIL'
    assert 'TIMEOUT: B-allowed-target-after-history-recovery (target versions map[A:3] want map[A:1 B:2])' in result['error']
    assert not result.get('violations') and not result.get('normalStarts') and not (p/'step-02-request.yaml').exists()
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
    assert source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    rows=[json.loads(s) for s in open(p/'observations.jsonl')];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    prep=f.audit(p,rows);owner=m.yaml(p/'source-initial-server.yaml')['metadata']['uid'];base=m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner)
    b=m.yaml(p/'step-01-server.yaml');assert b['metadata']['uid']==owner and b['metadata']['generation']==4
    target=next(r for r in b['spec']['template']['roles'] if r['name']=='frontend');assert m.version(target['entryTemplate'])=='B'
    config=source['config']['roles']['f'];assert config['p'] in (1,3) and config['u']+config['s']>0
    eligible={uid:o['metadata']['name'] for uid,o in base.items() if o['metadata']['labels'][m.R]=='frontend' and int(o['metadata']['labels'][m.I].rsplit('-',1)[1])>=config['p']};assert len(eligible)==2
    rule=m.read(p/'step-01-create-installed.json');errors=[t for t in trace if t.get('ruleID')==rule['id'] and t['action']=='error-request'];clears=[t for t in trace if t.get('ruleID')==rule['id'] and t['action']=='rule-cleared'];assert errors and len(clears)==1
    assert rule['namespace']==result['namespace'] and rule['ownerUID']==owner and rule['resource']=='controllerrevisions' and rule['methods']==['POST']
    assert all(t['status']==503 and t['method']=='POST' and t['resource']=='controllerrevisions' for t in errors)
    hold=next(m.read(p) for p in p.glob('checkpoint-*.json') if m.read(p)['phase']=='actual-CR-create-failure-no-template-actions');assert hold['elapsedStableNanos']>=10_000_000_000 and m.ts(hold['completed'])<m.ts(clears[0]['at'])
    clear=m.ts(clears[0]['at']);state=collections.defaultdict(dict);data={}
    for row in rows:
        uid=row['object']['metadata']['uid'];o=row['object'];kind=row['kind'];at=m.ts(row['received'])
        if kind=='controllerrevisions' and m.owned(o,owner):assert uid not in data or data[uid]==o['data'];data[uid]=o['data']
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
        if row['sequence']>prep['lastPreparationSequence']:
            pods=m.mine(state,'pods',owner);assert set(pods)==set(base) and all(m.ready(o) and m.version(o)=='A' for o in pods.values())
    final=m.yaml(p/'final-resources.yaml');pods=m.mine(final,'pods',owner);assert set(pods)==set(base) and all(m.ready(o) for o in pods.values())
    probes=[m.read(p) for p in sorted(p.glob('history-probe-*.json'))];probes=[p for p in probes if p['phase']=='B-allowed-target-after-history-recovery' and m.ts(p['started'])>clear]
    assert len(probes)>300 and m.ts(probes[-1]['completed'])-m.ts(probes[0]['started'])>=410_000_000_000
    for probe in probes:
        assert not probe.get('error');actual={o['metadata']['uid']:o for o in probe['pods']['items'] if m.owned(o,owner)};assert set(actual)==set(base) and all(m.ready(o) and m.version(o)=='A' for o in actual.values())
    assert max(m.ts(b['started'])-m.ts(a['completed']) for a,b in zip(probes,probes[1:]))<3_000_000_000
    histories=m.mine(final,'controllerrevisions',owner)
    bhistory=next(o for o in histories.values() if m.version(next(r for r in o['data']['data'] if r['name']=='frontend')['entryTemplate'])=='B')
    responses={t['request']:t for t in trace if t['action']=='response'}
    creates=[t for t in trace if t['action']=='request' and t.get('namespace')==result['namespace'] and t.get('resource')=='controllerrevisions' and t.get('method')=='POST' and clear<m.ts(t['at']) and responses.get(t['request'],{}).get('status')==201];assert creates
    assert not any(t['action']=='request' and t.get('namespace')==result['namespace'] and t.get('resource')=='pods' and t.get('method') in ('POST','DELETE') and clear<m.ts(t['at']) for t in trace)
    before=m.yaml(p/'fixture-preparation/fixture-restart-controller-replacement.yaml');after=m.yaml(p/'fault-controller-after.yaml');assert before['metadata']['uid']==after['metadata']['uid'] and after['status']['containerStatuses'][0]['restartCount']==0
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    return {'id':case['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'ELIGIBLE_SPARSE_A_NEVER_UPDATES_AFTER_HISTORY_CREATE_RECOVERY','watchRows':len(rows),'sourcePreparation':prep,'actualFailedCRCreates':len(errors),'successfulBCreation':creates[0],'BHistoryUID':bhistory['metadata']['uid'],'eligibleOriginalUIDs':eligible,'allRetainedUIDs':list(base),'persistentSince':probes[0]['started'],'persistentUntil':probes[-1]['completed'],'persistentNanos':m.ts(probes[-1]['completed'])-m.ts(probes[0]['started']),'directPodReferenceProbes':len(probes),'limitation':'Actual B was accepted and its history persisted after fault clear, but both eligible original high ordinals stayed Ready A with no Pod creation/deletion for over 410 seconds. This proves bounded non-convergence, not an internal root cause or infinite stall. C, terminating references and final restart were not executed.','evidenceSHA256':{f:hashlib.sha256((p/f).read_bytes()).hexdigest() for f in ('result.json','observations.jsonl','final-resources.yaml','source-boundary.json')}}
