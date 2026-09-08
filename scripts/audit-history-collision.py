#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Check precreated conflicts, native held POST 409 and automatic recovery."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('collision_mid',ROOT/'scripts/audit-midrollout.py')
m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)
REV='modelserving.volcano.sh/revision'


def audit_case(p,trace):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');source=case['scenario']['source'];assert result['status']=='PASS' and case['id']==source['id'] in ('RUN-533','RUN-534') and not result.get('violations')
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes();assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5' and source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    original=m.yaml(p/'before-server.yaml');owner=original['metadata']['uid'];base=m.yaml(p/'baseline-resources.yaml');pods=m.mine(base,'pods',owner);assert len(pods)==6 and all(m.ready(o) and m.version(o)=='A' for o in pods.values())
    provenance=m.read(p/'step-01-target-provenance.json');artifact=ROOT/provenance['sourceArtifact'];assert hashlib.sha256(artifact.read_bytes()).hexdigest()==provenance['sourceArtifactSHA256']
    witnessed=m.yaml(artifact)['controllerrevisions'][provenance['observedCRUID']];assert witnessed['metadata']['name']==provenance['name'] and witnessed['data']==provenance['data']
    admitted=m.yaml(p/'step-01-B-dry-run-admitted.yaml');b=m.yaml(p/'step-01-server.yaml');assert b['metadata']['uid']==owner and b['metadata']['generation']==2 and b['spec']==admitted['spec'] and b['spec']['template']['roles']==provenance['data']['data']
    first=m.yaml(p/'step-01-initial-collision-server.yaml');second=m.yaml(p/'step-01-raced-collision-server.yaml');name=provenance['name']
    assert first['metadata']['name']==second['metadata']['name']==name and first['metadata']['uid']!=second['metadata']['uid'] and first['data']==second['data'] and first['metadata']['ownerReferences']==second['metadata']['ownerReferences']
    if case['id']=='RUN-533':assert m.owned(first,owner) and first['data']!=provenance['data'] and first['data']==next(iter(m.mine(base,'controllerrevisions',owner).values()))['data']
    else:
        other=m.yaml(p/'step-01-foreign-owner.yaml');assert other['metadata']['uid']!=owner and other['spec']['replicas']==0 and first['data']==provenance['data'] and not m.owned(first,owner) and m.owned(first,other['metadata']['uid'])
    initial=m.yaml(p/'step-01-source-initial-collision-resources.yaml');assert first['metadata']['uid'] in initial['controllerrevisions'] and initial['modelservings'][owner]['metadata']['generation']==1
    rule=m.read(p/'step-01-post-hold-installed.json');rid=rule['id'];assert rule['mode']=='hold' and rule['methods']==['POST'] and rule['namespace']==result['namespace'] and rule['name']==name and rule['resource']=='controllerrevisions'
    scope=[t for t in trace if t.get('ruleID')==rid];held=[t for t in scope if t['action']=='hold-request'];released=[t for t in scope if t['action']=='release-request'];assert held and len(held)==len(released)==1
    request=held[0]['request'];assert released[0]['request']==request and held[0]['method']=='POST' and held[0]['name']==name
    native=next(t for t in trace if t['action']=='response' and t.get('request')==request);assert native['status']==409 and native['method']=='POST' and native['path']=='/apis/apps/v1/namespaces/'+result['namespace']+'/controllerrevisions'
    assert m.ts(held[0]['at'])<m.ts(released[0]['at'])<m.ts(native['at']) and not any(t['action']=='error-request' for t in scope)
    captured=m.read(p/'step-01-native-already-exists.json');assert captured['response']==native and captured['ruleID']==rid and captured['namespace']==result['namespace']
    tail=[json.loads(s) for s in (p/'step-01-native-proxy-tail.jsonl').read_text().splitlines()];assert native in tail and held[0] in tail and released[0] in tail
    before_delete=m.read(p/'step-01-first-collision-delete.json');clear=m.read(p/'step-01-final-collision-delete.json')
    for receipt,collision in [(before_delete,first),(clear,second)]:assert receipt['accepted'] and receipt['uid']==collision['metadata']['uid']==receipt['options']['preconditions']['uid']
    assert m.ts(rule['installed'])<m.ts(before_delete['sent'])<m.ts(held[0]['at'])
    cps=[m.read(f) for f in p.glob('checkpoint*.json')];pre=next(c for c in cps if c['phase']=='precreated-B-name-conflict-retains-source-A');raced=next(c for c in cps if c['phase']=='native-AlreadyExists-conflict-retains-source-A');finalcp=next(c for c in cps if c['phase']=='collision-cleared-automatic-B')
    assert min(c['elapsedStableNanos'] for c in (pre,raced,finalcp))>=30_000_000_000
    assert m.ts(pre['completed'])<m.ts(before_delete['sent']) and m.ts(native['at'])<m.ts(raced['completed'])<m.ts(clear['sent'])<m.ts(finalcp['stableSince'])
    live=m.yaml(p/'step-01-collision-immutable-before-clear.yaml');assert live['metadata']['uid']==second['metadata']['uid'] and live['data']==second['data'] and live['metadata']['ownerReferences']==second['metadata']['ownerReferences']
    rows=[json.loads(s) for s in (p/'observations.jsonl').read_text().splitlines()];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    state=collections.defaultdict(dict);data={};owners={};born={};deletions=[];minimum=3-source['config']['roles']['f']['u'];begin=m.ts(m.read(p/'step-01-request-time.json')['sent'])
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received']);previous=state[kind].get(uid)
        if kind=='controllerrevisions':
            assert uid not in data or data[uid]==o['data'];data[uid]=o['data']
            assert uid not in owners or owners[uid]==o['metadata'].get('ownerReferences');owners[uid]=o['metadata'].get('ownerReferences')
        if kind=='pods' and m.owned(o,owner):
            lab=o['metadata']['labels']
            if lab[m.R]=='backend':assert uid in pods and not o['metadata'].get('deletionTimestamp') and row['event']!='DELETED' and m.version(o)=='A'
            if previous and m.ready(previous) and (row['event']=='DELETED' or o['metadata'].get('deletionTimestamp')):
                assert at>m.ts(clear['sent']) and lab[m.R]=='frontend'
                ready=[p for p in m.mine(state,'pods',owner).values() if p['metadata']['labels'][m.R]=='frontend' and m.ready(p)];assert len(ready)-1>=minimum
                deletions.append({'sequence':row['sequence'],'uid':uid,'readyBefore':len(ready),'minimum':minimum})
            if row['event']=='ADDED' and m.version(o)=='B':
                assert o['metadata']['name'] not in born;born[o['metadata']['name']]=uid
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
        if begin<=at<m.ts(clear['sent']):assert set(m.mine(state,'pods',owner))==set(pods) and all(m.ready(o) and m.version(o)=='A' for o in m.mine(state,'pods',owner).values())
    for collision in (first,second):assert any(r['kind']=='controllerrevisions' and r['event']=='DELETED' and r['object']['metadata']['uid']==collision['metadata']['uid'] for r in rows)
    final=m.yaml(p/'final-resources.yaml');fp=m.mine(final,'pods',owner);assert len(fp)==6 and all(m.ready(o) for o in fp.values()) and collections.Counter(m.version(o) for o in fp.values())=={'A':3,'B':3} and len(born)==3
    bcr=next(o for o in m.mine(final,'controllerrevisions',owner).values() if o['metadata']['name']==name);assert bcr['data']==provenance['data'] and bcr['metadata']['uid'] not in (first['metadata']['uid'],second['metadata']['uid'])
    for o in fp.values():
        if o['metadata']['labels'][m.R]=='frontend':assert m.version(o)=='B' and 'model-'+o['metadata']['labels'][REV]==name
    state=m.replay(rows,finalcp['stableSince'])
    for row in [None]+[r for r in rows if m.ts(finalcp['stableSince'])<m.ts(r['received'])<=m.ts(finalcp['completed'])]:
        if row:
            o=row['object'];uid=o['metadata']['uid']
            if row['event']=='DELETED':state[row['kind']].pop(uid,None)
            else:state[row['kind']][uid]=o
        assert set(m.mine(state,'pods',owner))==set(fp) and all(m.ready(o) for o in m.mine(state,'pods',owner).values())
        assert len(m.mine(state,'configmaps',owner))==6 and not m.mine(state,'services',owner) and len(m.mine(state,'podgroups',owner))==1
    diagnostic=(p/'step-01-collision-controller.log').read_text();assert any(result['namespace'] in line and name in line and ('different template data' in line or 'not controlled' in line) for line in diagnostic.splitlines())
    before=m.yaml(p/'case-controller-before.yaml');after=m.yaml(p/'fault-controller-after.yaml');assert before['metadata']['uid']==after['metadata']['uid'] and after['status']['containerStatuses'][0]['restartCount']==0
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id':case['id'],'classification':'PASS','watchRows':len(rows),'firstConflictUID':first['metadata']['uid'],'secondConflictUID':second['metadata']['uid'],'nativeAlreadyExists':native,'correctBHistoryUID':bcr['metadata']['uid'],'originalBackendUIDs':[u for u,o in pods.items() if o['metadata']['labels'][m.R]=='backend'],'healthyDeletionChecks':deletions,'precreatedConflictWindowNanos':pre['elapsedStableNanos'],'nativeConflictWindowNanos':raced['elapsedStableNanos'],'finalStableNanos':finalcp['elapsedStableNanos'],'limitation':'Both actual precreated conflict and native held POST/reinsert/release 409 paths verified. Exact conflict UIDs removed by fixture; controller automatically creates correct owned B history and converges. No synthetic AlreadyExists response or Kthena source changes.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ('result.json','observations.jsonl','final-resources.yaml','step-01-native-already-exists.json')}}


def main():
    runid=sys.argv[1];assert '/' not in runid and '..' not in runid
    base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control');done=m.read(control/'completion.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==m.read(control/'build.json')['binarySHA256'] and m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    trace=[json.loads(s) for s in (control/'proxy-trace.jsonl').read_text().splitlines()];out=base/(sys.argv[2] if len(sys.argv)>2 else 'independent-history-collision-audit');out.mkdir();reports=[]
    for result in m.read(base/'summary.json')['results']:
        try:r=audit_case(base/result['id'],trace)
        except (AssertionError,KeyError,StopIteration,FileNotFoundError) as e:
            import traceback
            r={'id':result['id'],'classification':'PENDING_REVIEW','rawStatus':result['status'],'error':repr(e),'traceback':traceback.format_exc()}
        reports.append(r)
        with (out/(result['id']+'.json')).open('x') as f:json.dump(r,f,indent=2);f.write('\n')
        print(r['id'],r['classification'])
    counts=dict(collections.Counter(r['classification'] for r in reports));pending='PENDING_REVIEW' in counts
    with (out/'summary.json').open('x') as f:json.dump({'status':'PENDING_REVIEW' if pending else 'VERIFIED','cases':reports,'counts':counts},f,indent=2);f.write('\n')
    if pending:sys.exit(1)


if __name__=='__main__':main()
