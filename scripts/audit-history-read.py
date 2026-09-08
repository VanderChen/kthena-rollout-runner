#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independently check protected historical A recovery under actual named GET errors."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('read_mid',ROOT/'scripts/audit-midrollout.py')
m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)
REV='modelserving.volcano.sh/revision'


def audit_case(p,trace):
    case=m.yaml(p/'case.yaml');result=m.read(p/'result.json');source=case['scenario']['source']
    assert result['id']==case['id']==source['id'] and result['id'] in ('RUN-524','RUN-529') and result['status']=='PASS' and not result.get('violations')
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
    assert source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id']) and 'API读取失败' in source['initial']
    original=m.yaml(p/'before-server.yaml');owner=original['metadata']['uid'];mode=source['config']['mode']
    b=m.yaml(p/'step-01-server.yaml');assert b['metadata']['uid']==owner and b['metadata']['generation']==2
    rows=[json.loads(s) for s in open(p/'observations.jsonl')]
    assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    baseline=m.yaml(p/'step-01-resources.yaml');base=m.mine(baseline,'pods',owner)
    captured={o['metadata']['uid']:o for o in m.yaml(p/'step-02-before-fault-pods.yaml')['items'] if m.owned(o,owner)}
    assert set(base)==set(captured) and len(base)==(6 if mode=='SG' else 9) and all(m.ready(o) for o in base.values())
    front={uid:o for uid,o in base.items() if o['metadata']['labels'][m.R]=='frontend'}
    assert len(front)==6 and collections.Counter(m.version(o) for o in front.values())=={'A':2,'B':4}
    for o in front.values():
        lab=o['metadata']['labels'];ordinal=int(lab[m.G if mode=='SG' else m.I].rsplit('-',1)[1]);assert m.version(o)==('A' if ordinal==0 else 'B')
    scope=m.read(p/'step-02-fault-scope.json');old=scope['recoveryUIDs'];outside=scope['outsideRecoveryUIDs']
    assert scope['ownerUID']==owner and scope['recovery']=='RoleRecreate' and len(old)==2 and set(old)|set(outside)==set(base)
    assert all(m.version(base[u])=='A' and base[u]['metadata']['labels'][m.R]=='frontend' for u in old)
    receipt=m.read(p/'step-02-external-delete.json');assert receipt['accepted'] and receipt['uid']==scope['targetUID']==receipt['options']['preconditions']['uid'] and receipt['name']==scope['targetName']
    assert base[receipt['uid']]['metadata']['labels'].get(m.E)=='true'
    cr=m.yaml(p/'step-02-original-A-history.yaml');assert m.owned(cr,owner)
    for uid in old:assert 'model-'+base[uid]['metadata']['labels'][REV]==cr['metadata']['name']
    installed=m.read(p/'step-02-read-installed.json');rid=installed['id']
    assert installed['namespace']==result['namespace'] and installed['resource']=='controllerrevisions' and installed['name']==cr['metadata']['name'] and installed['methods']==['GET']
    assert installed['mode']=='error' and installed['statusCode']==503 and installed['count']==-1 and m.ts(installed['installed'])<m.ts(receipt['sent'])
    before=m.read(p/'step-02-before-read-clear.json');rule=next(r for r in before['rules'] if r['id']==rid)
    assert rule['active'] and rule['hits']>=2 and not before['errors']
    errors=[t for t in trace if t.get('ruleID')==rid and t['action']=='error-request']
    clears=[t for t in trace if t.get('ruleID')==rid and t['action']=='rule-cleared'];assert len(clears)==1 and len(errors)==rule['hits']
    assert all(t['resource']=='controllerrevisions' and t['method']=='GET' and t['name']==cr['metadata']['name'] and t['status']==503 and t['namespace']==result['namespace'] for t in errors)
    clear=m.ts(clears[0]['at'])
    cps=[m.read(f) for f in p.glob('checkpoint-*.json')]
    held=next(c for c in cps if c['phase']=='actual-A-read-errors-safe-recovery-boundary');finalcp=next(c for c in cps if c['phase']=='protected-A-history-read-restored')
    assert held['elapsedStableNanos']>=30_000_000_000 and m.ts(errors[0]['at'])<m.ts(held['stableSince'])<m.ts(held['completed'])<clear
    assert finalcp['elapsedStableNanos']>=30_000_000_000 and clear<m.ts(finalcp['stableSince'])
    state=collections.defaultdict(dict);data={};oldDeleted=set();created={}
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received'])
        if kind=='controllerrevisions' and m.owned(o,owner):
            assert uid not in data or data[uid]==o['data'];data[uid]=o['data']
        if kind=='pods' and m.owned(o,owner) and at>=m.ts(installed['installed']):
            if row['event']=='DELETED':oldDeleted.add(uid)
            if uid in outside:assert row['event']!='DELETED' and not o['metadata'].get('deletionTimestamp') and m.version(o)==m.version(base[uid])
            elif uid not in old:
                assert o['metadata']['name'] in old.values() and m.version(o)=='A' and o['metadata']['labels'][REV]==base[scope['targetUID']]['metadata']['labels'][REV]
                if o['metadata']['name'] in created:assert created[o['metadata']['name']]==uid
                created[o['metadata']['name']]=uid
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
    assert set(old)<=oldDeleted and set(created)==set(old.values())
    final=m.yaml(p/'final-resources.yaml');pods=m.mine(final,'pods',owner)
    assert len(pods)==len(base) and all(m.ready(o) for o in pods.values()) and set(pods)==set(outside)|set(created.values())
    for name,uid in created.items():
        o=pods[uid];oldPod=next(o for o in base.values() if o['metadata']['name']==name)
        assert m.version(o)=='A' and o['metadata']['labels'].get(m.E)==oldPod['metadata']['labels'].get(m.E)
    recovered=m.yaml(p/'step-02-restored-A-history.yaml');assert recovered['metadata']['uid']==cr['metadata']['uid'] and recovered['data']==cr['data']
    responses={t['request']:t for t in trace if t['action']=='response'}
    reads=[responses[t['request']] for t in trace if t['action']=='request' and t.get('namespace')==result['namespace'] and t['resource']=='controllerrevisions' and t['method']=='GET' and t.get('name')==cr['metadata']['name'] and clear<m.ts(t['at'])<m.ts(finalcp['completed']) and responses.get(t['request'],{}).get('status')==200]
    assert reads
    diagnostic=(p/'step-02-unknown-history-controller.log').read_text()
    assert any(result['namespace'] in line and cr['metadata']['name'].removeprefix('model-') in line and ('injected external controller API failure' in line or 'failed to get ControllerRevision' in line) for line in diagnostic.splitlines())
    state=m.replay(rows,finalcp['stableSince'])
    assert set(m.mine(state,'pods',owner))==set(pods) and all(m.ready(o) for o in m.mine(state,'pods',owner).values())
    for row in rows:
        if not m.ts(finalcp['stableSince'])<m.ts(row['received'])<=m.ts(finalcp['completed']):continue
        uid=row['object']['metadata']['uid']
        if row['event']=='DELETED':state[row['kind']].pop(uid,None)
        else:state[row['kind']][uid]=row['object']
        assert set(m.mine(state,'pods',owner))==set(pods) and all(m.ready(o) for o in m.mine(state,'pods',owner).values())
    assert set(m.mine(final,'controllerrevisions',owner))==set(m.mine(baseline,'controllerrevisions',owner))
    probes=[m.read(f) for f in p.glob('history-probe-*.json')];assert probes and all('error' not in probe for probe in probes)
    for probe in probes:
        for pod in probe['pods']['items']:
            if not m.owned(pod,owner):continue
            history=probe['histories'].get('model-'+pod['metadata']['labels'][REV])
            if history is None:
                assert any(r['kind']=='pods' and r['event']=='DELETED' and r['object']['metadata']['uid']==pod['metadata']['uid'] and m.ts(r['received'])<=m.ts(probe['completed']) for r in rows);continue
            assert m.owned(history,owner) and not history['metadata'].get('deletionTimestamp')
            roles={r['name']:r for r in history['data']['data']};role=roles[pod['metadata']['labels'][m.R]];member='entryTemplate' if pod['metadata']['labels'].get(m.E)=='true' else 'workerTemplate'
            assert m.version(role[member])==m.version(pod)
    controller=m.yaml(p/'case-controller-before.yaml');after=m.yaml(p/'fault-controller-after.yaml')
    assert controller['metadata']['uid']==after['metadata']['uid'] and after['status']['containerStatuses'][0]['restartCount']==0
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    assert len(m.mine(final,'podgroups',owner))==(3 if mode=='SG' else 1)
    for kind,want in [('configmaps',3 if mode=='SG' else 6),('services',3)]:
        resources=m.mine(final,kind,owner);assert len(resources)==want
        for resource in resources.values():assert any(all(o['metadata']['labels'].get(k)==resource['metadata']['labels'].get(k) for k in (m.G,m.R,m.I)) for o in pods.values())
    return {'id':case['id'],'classification':'PASS','watchRows':len(rows),'actualFailedNamedReads':len(errors),'successfulReadsAfterClear':len(reads),'protectedOriginalUIDs':old,'protectedReplacementUIDs':created,'outsideUIDsPreserved':outside,'originalHistoryUID':cr['metadata']['uid'],'unknownWindowNanos':held['elapsedStableNanos'],'finalStableNanos':finalcp['elapsedStableNanos'],'historyProbes':len(probes),'limitation':'Read failures are controller-proxy-only; direct observer verifies retained real history. Recovery may replace the actual two-member Role cohort, never other healthy Pods. No assertion about internal caches.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml','step-02-before-read-clear.json']}}


def main():
    runid=sys.argv[1];assert runid and '/' not in runid and '..' not in runid
    base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control');done=m.read(control/'completion.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    build=m.read(control/'build.json');assert m.read(base/'environment.json')['runner']['binarySHA256']==build['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    trace=[json.loads(s) for s in open(control/'proxy-trace.jsonl')];reports=[];out=base/'independent-history-read-audit';out.mkdir()
    for result in m.read(base/'summary.json')['results']:
        try:report=audit_case(base/result['id'],trace)
        except (AssertionError,KeyError,StopIteration,FileNotFoundError) as e:
            import traceback
            report={'id':result['id'],'classification':'PENDING_REVIEW','rawStatus':result['status'],'rawError':result.get('error'),'auditError':repr(e),'traceback':traceback.format_exc()}
        reports.append(report)
        with (out/(result['id']+'.json')).open('x') as f:json.dump(report,f,indent=2);f.write('\n')
        print(report['id'],report['classification'])
    pending=any(r['classification']=='PENDING_REVIEW' for r in reports)
    with (out/'summary.json').open('x') as f:json.dump({'status':'PENDING_REVIEW' if pending else 'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},f,indent=2);f.write('\n')
    if pending:sys.exit(1)


if __name__=='__main__':main()
