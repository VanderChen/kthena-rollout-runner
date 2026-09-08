#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Actual high surge source, cleared proxy gate and stable-target dependencies."""
import collections,hashlib,importlib.util,json,pathlib,sys
ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('dependency_reject',ROOT/'scripts/audit-rejection.py');h=importlib.util.module_from_spec(loader);loader.loader.exec_module(h);m=h.m

def audit_case(p,trace):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');source=case['scenario']['source'];assert case['id']==source['id']=='RUN-573'
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes();assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5' and source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    assert result['status']=='PASS' and not result.get('violations')
    before=m.yaml(p/'before-server.yaml');owner=before['metadata']['uid'];baseline=m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner);assert len(baseline)==6 and all(m.ready(o) and m.version(o)=='A' for o in baseline.values())
    b=m.yaml(p/'step-01-server.yaml');receipt=m.read(p/'step-01-request-time.json');assert b['metadata']['uid']==owner and b['metadata']['generation']==2 and m.yaml(p/'step-01-request.yaml')['spec']==case['scenario']['steps'][0]['spec']
    assert all(m.version(r['entryTemplate'])=='B' for r in b['spec']['template']['roles'])
    boundary=m.read(p/'step-01-source-boundary.json');sourcepods={o['metadata']['uid']:o for o in m.yaml(p/'step-01-source-pods.yaml')['items'] if m.owned(o,owner)}
    ordinal=lambda o:int(o['metadata']['labels'][m.I].rsplit('-',1)[1])
    assert boundary['ownerUID']==owner and boundary['generation']==2 and len(sourcepods)==7 and set(baseline)<set(sourcepods) and all(m.ready(o) for o in sourcepods.values())
    high=next(o for uid,o in sourcepods.items() if uid not in baseline);assert m.version(high)=='B' and high['metadata']['labels'][m.R]=='backend' and ordinal(high)==3
    for uid in baseline:assert sourcepods[uid]['spec']==baseline[uid]['spec'] and m.version(sourcepods[uid])=='A'
    ids=boundary['preparationRules'];assert len(ids)==3;clears=[];holds=[]
    for i,rid in enumerate(ids):
        rule=m.read(p/('step-01-dependency-rule-%d.json'%i));assert rule['id']==rid and rule['methods']==['DELETE'] and rule['mode']=='hold' and rule['namespace']==result['namespace'] and rule['name'] in [o['metadata']['name'] for o in baseline.values() if o['metadata']['labels'][m.R]=='backend']
        cleared=[t for t in trace if t.get('ruleID')==rid and t['action']=='rule-cleared'];assert len(cleared)==1;clears+=cleared
        holds += [t for t in trace if t.get('ruleID')==rid and t['action']=='hold-request']
    assert holds and all(m.ts(t['at'])<m.ts(boundary['at']) for t in holds)
    clear=max(m.ts(t['at']) for t in clears);assert clear>m.ts(boundary['at'])
    cps=[m.read(f) for f in p.glob('checkpoint*.json')];gate=next(c for c in cps if c['phase']=='cleared-proxy-stable-backend-B-not-ready-gate');finalcp=next(c for c in cps if c['phase']=='stable-backend-ready-then-automatic-frontend')
    assert gate['elapsedStableNanos']>=10_000_000_000 and m.ts(gate['stableSince'])>clear and finalcp['elapsedStableNanos']>=30_000_000_000
    rows=[json.loads(s) for s in open(p/'observations.jsonl')];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    state=collections.defaultdict(dict);first_stable=None;front_starts=[];deletions=[];histories={}
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received']);prev=state[kind].get(uid)
        if kind=='controllerrevisions' and m.owned(o,owner):assert uid not in histories or histories[uid]==o['data'];histories[uid]=o['data']
        if kind=='pods' and m.owned(o,owner) and at>m.ts(receipt['sent']):
            role=o['metadata']['labels'][m.R]
            if role=='backend' and m.version(o)=='B' and ordinal(o)<3 and m.ready(o) and first_stable is None:first_stable=row
            deleting=prev and m.ready(prev) and (row['event']=='DELETED' or o['metadata'].get('deletionTimestamp'))
            if role=='frontend' and (deleting or uid not in baseline and row['event']=='ADDED'):
                assert first_stable is not None and m.ts(first_stable['received'])<at
                front_starts.append({'sequence':row['sequence'],'at':row['received'],'uid':uid,'event':row['event']})
            if deleting:
                assert at>clear
                live=m.mine(state,'pods',owner);ready=[v for v in live.values() if v['metadata']['labels'][m.R]==role and m.ready(v)];assert len(ready)-1>=3
                old={r:[v for v in live.values() if v['metadata']['labels'][m.R]==r and m.version(v)=='A' and not v['metadata'].get('deletionTimestamp')] for r in ('frontend','backend')}
                ready_stable={r:sum(v['metadata']['labels'][m.R]==r and m.version(v)=='B' and ordinal(v)<3 and m.ready(v) for v in live.values()) for r in old}
                if role=='backend' and len(old[role])==1:assert not old['frontend']
                if all(v<3 for v in ready_stable.values()):assert 3-len(old[role])+1<=min(3,min(ready_stable.values())+2)
                deletions.append({'at':row['received'],'uid':uid,'readyBefore':len(ready),'minimum':3,'stableTargetReadyCounts':ready_stable})
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
        if m.ts(gate['stableSince'])<=at<=m.ts(gate['completed']):
            pods=m.mine(state,'pods',owner);front={uid:o for uid,o in pods.items() if o['metadata']['labels'][m.R]=='frontend'};assert set(front)=={uid for uid,o in baseline.items() if o['metadata']['labels'][m.R]=='frontend'} and all(m.ready(o) for o in front.values())
            assert any(o['metadata']['uid']==high['metadata']['uid'] and m.ready(o) for o in pods.values())
            assert any(o['metadata']['labels'][m.R]=='backend' and ordinal(o)==2 and m.version(o)=='B' and not m.ready(o) for o in pods.values())
        if at>m.ts(receipt['sent']):
            for role in ('frontend','backend'):assert sum(o['metadata']['labels'][m.R]==role and not o['metadata'].get('deletionTimestamp') for o in m.mine(state,'pods',owner).values())<=4
    assert first_stable and m.ts(first_stable['received'])>m.ts(gate['completed']) and front_starts
    final=m.yaml(p/'final-resources.yaml');pods=m.mine(final,'pods',owner);assert len(pods)==6 and all(m.ready(o) and m.version(o)=='B' for o in pods.values()) and not set(pods)&set(baseline);h.plugins(final,owner,b['spec'])
    crs=m.mine(final,'controllerrevisions',owner);ms=next(o for o in final['modelservings'].values() if o['metadata']['uid']==owner);assert ms['status']['currentRevision']==ms['status']['updateRevision']
    for pod in pods.values():
        cr=next(o for o in crs.values() if o['metadata']['name']=='model-'+pod['metadata']['labels'][h.REV]);role=next(r for r in cr['data']['data'] if r['name']==pod['metadata']['labels'][m.R]);assert m.version(role['entryTemplate'])=='B'
    stable=m.replay(rows,finalcp['stableSince'])
    for row in [None]+[r for r in rows if m.ts(finalcp['stableSince'])<m.ts(r['received'])<=m.ts(finalcp['completed'])]:
        if row:
            uid=row['object']['metadata']['uid']
            if row['event']=='DELETED':stable[row['kind']].pop(uid,None)
            else:stable[row['kind']][uid]=row['object']
        assert set(m.mine(stable,'pods',owner))==set(pods) and all(m.ready(o) for o in m.mine(stable,'pods',owner).values());h.plugins(stable,owner,b['spec'])
    controller=m.yaml(p/'case-controller-before.yaml');end=m.yaml(p/'fault-controller-after.yaml');assert controller['metadata']['uid']==end['metadata']['uid'] and end['status']['containerStatuses'][0]['restartCount']==0
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    return {'id':case['id'],'classification':'PASS','watchRows':len(rows),'sourceHighReadyUID':high['metadata']['uid'],'sourceStableOriginalUIDs':list(baseline),'postClearNotReadyGateNanos':gate['elapsedStableNanos'],'firstStableTargetReady':{'at':first_stable['received'],'sequence':first_stable['sequence'],'uid':first_stable['object']['metadata']['uid']},'frontendStarts':front_starts,'healthyDeletionChecks':deletions,'finalStableNanos':finalcp['elapsedStableNanos'],'limitation':'Actual high-only Ready source retained by finite preparation holds. Decisive10s gate occurs after every hold cleared and stable B is NotReady. Frontend starts only after actual stable B Ready; budgets, skew, last-old dependency path and final B are independently checked.'}


def main():
    runid=sys.argv[1];base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control')
    done=m.read(control/'completion.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==m.read(control/'build.json')['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    trace=[json.loads(s) for s in open(control/'proxy-trace.jsonl')]
    out=base/(sys.argv[2] if len(sys.argv)>2 else 'independent-dependency-boundary-audit');out.mkdir();reports=[]
    for result in m.read(base/'summary.json')['results']:
        try:report=audit_case(base/result['id'],trace)
        except (AssertionError,KeyError,StopIteration,FileNotFoundError) as e:
            import traceback
            report={'id':result['id'],'classification':'PENDING_REVIEW','rawStatus':result['status'],'rawError':result.get('error'),'auditError':repr(e),'traceback':traceback.format_exc()}
        reports.append(report);(out/(result['id']+'.json')).write_text(json.dumps(report,indent=2)+'\n');print(report['id'],report['classification'])
    pending=any(r['classification']=='PENDING_REVIEW' for r in reports)
    (out/'summary.json').write_text(json.dumps({'status':'PENDING_REVIEW' if pending else 'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},indent=2)+'\n')
    if pending:sys.exit(1)

if __name__=='__main__':main()
