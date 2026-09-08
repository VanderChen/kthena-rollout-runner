#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Verify one unchanged owned history through native AlreadyExists and automatic B."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys
ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('equal_reject',ROOT/'scripts/audit-rejection.py')
h=importlib.util.module_from_spec(loader);loader.loader.exec_module(h);m=h.m


def audit_case(p,trace):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');source=case['scenario']['source']
    assert result['status']=='PASS' and case['id']==source['id']=='RUN-610' and not result.get('violations')
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5' and source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    original=m.yaml(p/'before-server.yaml');owner=original['metadata']['uid'];base=m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner)
    assert len(base)==6 and all(m.ready(o) and m.version(o)=='A' for o in base.values())
    pre=m.yaml(p/'step-01-equivalent-precreated-server.yaml');retained=m.yaml(p/'step-01-equivalent-retained.yaml');name=pre['metadata']['name'];cruid=pre['metadata']['uid']
    assert m.owned(pre,owner) and retained['metadata']['uid']==cruid and retained['data']==pre['data'] and retained['metadata']['ownerReferences']==pre['metadata']['ownerReferences']
    provenance=m.read(p/'step-01-target-provenance.json');artifact=ROOT/provenance['sourceArtifact'];assert hashlib.sha256(artifact.read_bytes()).hexdigest()==provenance['sourceArtifactSHA256']
    witnessed=m.yaml(artifact)['controllerrevisions'][provenance['observedCRUID']];assert witnessed['metadata']['name']==name==provenance['name'] and witnessed['data']==pre['data']==provenance['data']
    admitted=m.yaml(p/'step-01-B-dry-run-admitted.yaml');server=m.yaml(p/'step-01-server.yaml');request=m.yaml(p/'step-01-request.yaml');receipt=m.read(p/'step-01-request-time.json')
    assert server['metadata']['uid']==owner and server['metadata']['generation']==2 and server['spec']==admitted['spec'] and server['spec']['template']['roles']==pre['data']['data'] and request['spec']==case['scenario']['steps'][0]['spec']
    initial=m.yaml(p/'step-01-source-equivalent-resources.yaml');assert cruid in initial['controllerrevisions'] and initial['modelservings'][owner]['metadata']['generation']==1 and set(m.mine(initial,'pods',owner))==set(base)
    post=m.read(p/'step-01-collision-rule-0.json');get=m.read(p/'step-01-collision-rule-1.json')
    for rule in (post,get):assert rule['namespace']==result['namespace'] and rule['resource']=='controllerrevisions' and rule['name']==name
    assert post['mode']=='hold' and post['methods']==['POST'] and get['mode']=='error' and get['methods']==['GET'] and get['statusCode']==404 and get['count']==1
    injected=[t for t in trace if t.get('ruleID')==get['id'] and t['action']=='error-request'];assert len(injected)==1 and injected[0]['status']==404
    held=[t for t in trace if t.get('ruleID')==post['id'] and t['action']=='hold-request'];released=[t for t in trace if t.get('ruleID')==post['id'] and t['action']=='release-request'];assert len(held)==len(released)==1 and held[0]['request']==released[0]['request']
    responses={t['request']:t for t in trace if t['action']=='response'};native=responses[held[0]['request']]
    assert native['status']==409 and native['method']=='POST' and native['path']=='/apis/apps/v1/namespaces/'+result['namespace']+'/controllerrevisions'
    assert m.ts(injected[0]['at'])<m.ts(held[0]['at'])<m.ts(released[0]['at'])<m.ts(native['at'])
    assert m.read(p/'step-01-native-already-exists.json')['response']==native
    reads=[t for t in trace if t['action']=='request' and t.get('namespace')==result['namespace'] and t.get('resource')=='controllerrevisions' and t.get('name')==name and t['method']=='GET' and m.ts(t['at'])>m.ts(native['at']) and responses.get(t['request'],{}).get('status')==200];assert reads
    assert not any(t.get('ruleID')==post['id'] and t['action']=='error-request' for t in trace)
    rows=[json.loads(s) for s in open(p/'observations.jsonl')];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    state=collections.defaultdict(dict);histories={};born={};deletions=[];known=set(m.mine(initial,'controllerrevisions',owner))
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received']);prev=state[kind].get(uid)
        if kind=='controllerrevisions' and m.owned(o,owner):
            assert uid in known and row['event']!='DELETED' and (uid not in histories or histories[uid]==o['data']);histories[uid]=o['data']
            if uid==cruid:assert o['data']==pre['data'] and o['metadata']['ownerReferences']==pre['metadata']['ownerReferences']
        if kind=='pods' and m.owned(o,owner):
            if o['metadata']['labels'][m.R]=='backend':assert uid in base and row['event']!='DELETED' and not o['metadata'].get('deletionTimestamp') and m.version(o)=='A'
            if m.version(o)=='B' and row['event']=='ADDED':
                assert o['metadata']['name'] not in born;born[o['metadata']['name']]=uid
            if prev and m.ready(prev) and (row['event']=='DELETED' or o['metadata'].get('deletionTimestamp')):
                ready=[v for v in m.mine(state,'pods',owner).values() if v['metadata']['labels'][m.R]=='frontend' and m.ready(v)]
                assert len(ready)-1>=3 and at>m.ts(receipt['sent']);deletions.append({'at':row['received'],'uid':uid,'readyBefore':len(ready),'minimum':3})
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
        front=[v for v in m.mine(state,'pods',owner).values() if v['metadata']['labels'][m.R]=='frontend' and not v['metadata'].get('deletionTimestamp')];assert len(front)<=4
    final=m.yaml(p/'final-resources.yaml');pods=m.mine(final,'pods',owner);assert len(pods)==6 and all(m.ready(o) for o in pods.values()) and collections.Counter(m.version(o) for o in pods.values())=={'A':3,'B':3} and len(born)==3
    assert set(m.mine(final,'controllerrevisions',owner))==known and final['controllerrevisions'][cruid]['data']==pre['data'];h.plugins(final,owner,server['spec'])
    for o in pods.values():
        if o['metadata']['labels'][m.R]=='frontend':assert m.version(o)=='B' and 'model-'+o['metadata']['labels'][h.REV]==name
    cp=next(m.read(f) for f in p.glob('checkpoint*.json') if m.read(f)['phase']=='equivalent-history-reused-automatic-B');assert cp['elapsedStableNanos']>=30_000_000_000
    state=m.replay(rows,cp['stableSince'])
    for row in [None]+[r for r in rows if m.ts(cp['stableSince'])<m.ts(r['received'])<=m.ts(cp['completed'])]:
        if row:
            uid=row['object']['metadata']['uid']
            if row['event']=='DELETED':state[row['kind']].pop(uid,None)
            else:state[row['kind']][uid]=row['object']
        assert set(m.mine(state,'pods',owner))==set(pods) and all(m.ready(o) for o in m.mine(state,'pods',owner).values());h.plugins(state,owner,server['spec'])
    before=m.yaml(p/'case-controller-before.yaml');after=m.yaml(p/'fault-controller-after.yaml');assert before['metadata']['uid']==after['metadata']['uid'] and after['status']['containerStatuses'][0]['restartCount']==0
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    return {'id':case['id'],'classification':'PASS','watchRows':len(rows),'equivalentHistoryUID':cruid,'injectedControllerRead404':injected[0],'nativeAlreadyExists':native,'nativeSuccessfulReadAfter409':reads[0],'actualBPodUIDs':born,'healthyDeletionChecks':deletions,'finalStableNanos':cp['elapsedStableNanos'],'limitation':'One explicit controller-only stale GET404 opened the Create race. The held POST was forwarded to the real API and returned native409. The precreated equivalent owned CR retained its exact UID/Data/owner throughout; B converged without duplicate Pods.'}


def main():
    runid=sys.argv[1];base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control')
    done=m.read(control/'completion.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==m.read(control/'build.json')['binarySHA256'] and m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    trace=[json.loads(s) for s in open(control/'proxy-trace.jsonl')];out=base/(sys.argv[2] if len(sys.argv)>2 else 'independent-history-equal-audit');out.mkdir()
    try:report=audit_case(base/'RUN-610',trace)
    except (AssertionError,KeyError,StopIteration,FileNotFoundError) as e:
        import traceback
        report={'id':'RUN-610','classification':'PENDING_REVIEW','error':repr(e),'traceback':traceback.format_exc()}
    (out/'RUN-610.json').write_text(json.dumps(report,indent=2)+'\n');pending=report['classification']=='PENDING_REVIEW'
    (out/'summary.json').write_text(json.dumps({'status':'PENDING_REVIEW' if pending else 'VERIFIED','cases':[report],'counts':{report['classification']:1}},indent=2)+'\n');print(report['classification'])
    if pending:sys.exit(1)

if __name__=='__main__':main()
