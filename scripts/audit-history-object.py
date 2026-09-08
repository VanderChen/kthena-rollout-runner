#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independently check protected A recovery with actual absent, malformed and foreign history objects."""
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
    assert result['id']==case['id']==source['id'] and result['id'] in ('RUN-523','RUN-525','RUN-526','RUN-527','RUN-528','RUN-530','RUN-531','RUN-532') and result['status']=='PASS' and not result.get('violations')
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
    assert source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
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
    fixture=m.read(p/'step-02-history-object-fixture.json');anomaly=fixture['kind'];injected=fixture.get('injected')
    expected={523:'missing',525:'corrupt-data',526:'missing-role',527:'foreign-owner',528:'missing',530:'corrupt-data',531:'missing-role',532:'foreign-owner'}
    assert anomaly==expected[int(case['id'][4:])] and fixture['original']['metadata']['uid']==cr['metadata']['uid'] and fixture['original']['data']==cr['data']
    removed=m.read(p/'step-02-original-history-delete.json');assert removed['accepted'] and removed['uid']==cr['metadata']['uid']==removed['options']['preconditions']['uid'] and m.ts(removed['received'])<m.ts(receipt['sent'])
    assert any(r['kind']=='controllerrevisions' and r['event']=='DELETED' and r['object']['metadata']['uid']==cr['metadata']['uid'] for r in rows)
    if anomaly=='missing':assert injected is None
    else:
        assert injected['metadata']['name']==cr['metadata']['name'] and injected['metadata']['uid']!=cr['metadata']['uid']
        assert any(r['kind']=='controllerrevisions' and r['event']=='ADDED' and r['object']['metadata']['uid']==injected['metadata']['uid'] and r['object']['data']==injected['data'] for r in rows)
        if anomaly=='corrupt-data':assert injected['data']=={'data':'invalid-history-role-list'} and m.owned(injected,owner)
        if anomaly=='missing-role':assert injected['data']['data']==[r for r in cr['data']['data'] if r['name']!='frontend'] and m.owned(injected,owner)
        if anomaly=='foreign-owner':
            other=m.yaml(p/'step-02-foreign-owner.yaml');assert other['spec']['replicas']==0 and other['metadata']['namespace']==result['namespace'] and other['metadata']['uid']!=owner
            assert injected['data']==cr['data'] and not m.owned(injected,owner) and m.owned(injected,other['metadata']['uid'])
            assert any(r['kind']=='modelservings' and r['object']['metadata']['uid']==other['metadata']['uid'] for r in rows)
    recovered=m.yaml(p/'step-02-restored-A-history.yaml');restored=m.yaml(p/'step-02-restored-A-history-before-recovery.yaml')
    assert recovered['metadata']['uid']==restored['metadata']['uid']!=cr['metadata']['uid'] and recovered['data']==restored['data']==cr['data'] and m.owned(recovered,owner)
    newrow=next(r for r in rows if r['kind']=='controllerrevisions' and r['event']=='ADDED' and r['object']['metadata']['uid']==restored['metadata']['uid'])
    clear=m.ts(newrow['received']);fault_at=m.ts(removed['sent'])
    if injected is not None:
        delete=m.read(p/'step-02-invalid-history-delete.json');assert delete['accepted'] and delete['uid']==injected['metadata']['uid']==delete['options']['preconditions']['uid']
        assert any(r['kind']=='controllerrevisions' and r['event']=='DELETED' and r['object']['metadata']['uid']==injected['metadata']['uid'] for r in rows)
    cps=[m.read(f) for f in p.glob('checkpoint-*.json')]
    held=next(c for c in cps if c['phase']=='actual-A-read-errors-safe-recovery-boundary');finalcp=next(c for c in cps if c['phase']=='protected-A-history-read-restored')
    assert held['elapsedStableNanos']>=30_000_000_000 and m.ts(receipt['received'])<m.ts(held['stableSince'])<m.ts(held['completed'])<clear
    assert finalcp['elapsedStableNanos']>=30_000_000_000 and clear<m.ts(finalcp['stableSince'])
    unknown=m.yaml(p/'step-02-unknown-boundary-resources.yaml');unknown_cr=[o for o in unknown['controllerrevisions'].values() if o['metadata']['name']==cr['metadata']['name']]
    if anomaly=='missing':assert not unknown_cr
    else:assert len(unknown_cr)==1 and unknown_cr[0]['metadata']['uid']==injected['metadata']['uid'] and unknown_cr[0]['data']==injected['data']
    state=collections.defaultdict(dict);data={};oldDeleted=set();created={}
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received'])
        if kind=='controllerrevisions':
            assert uid not in data or data[uid]==o['data'];data[uid]=o['data']
        if kind=='pods' and m.owned(o,owner) and at>=fault_at:
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
    responses={t['request']:t for t in trace if t['action']=='response'}
    requests=[t for t in trace if t['action']=='request' and t.get('namespace')==result['namespace'] and t['resource']=='controllerrevisions' and t['method']=='GET' and t.get('name')==cr['metadata']['name']]
    reads=[responses[t['request']] for t in requests if clear<m.ts(t['at'])<m.ts(finalcp['completed']) and responses.get(t['request'],{}).get('status')==200]
    unknown_reads=[responses[t['request']] for t in requests if m.ts(receipt['received'])<m.ts(t['at'])<m.ts(held['completed']) and responses.get(t['request'],{}).get('status')==(404 if anomaly=='missing' else 200)]
    assert reads and unknown_reads
    diagnostic=(p/'step-02-unknown-history-controller.log').read_text()
    assert any(result['namespace'] in line and cr['metadata']['name'].removeprefix('model-') in line and any(term in line for term in ['Cannot resolve','not found','failed to parse','no Role','failed to read','not controlled','failed to get ControllerRevision']) for line in diagnostic.splitlines())
    state=m.replay(rows,finalcp['stableSince'])
    assert set(m.mine(state,'pods',owner))==set(pods) and all(m.ready(o) for o in m.mine(state,'pods',owner).values())
    for row in rows:
        if not m.ts(finalcp['stableSince'])<m.ts(row['received'])<=m.ts(finalcp['completed']):continue
        uid=row['object']['metadata']['uid']
        if row['event']=='DELETED':state[row['kind']].pop(uid,None)
        else:state[row['kind']][uid]=row['object']
        assert set(m.mine(state,'pods',owner))==set(pods) and all(m.ready(o) for o in m.mine(state,'pods',owner).values())
    final_cr=m.mine(final,'controllerrevisions',owner);before_cr=m.mine(baseline,'controllerrevisions',owner)
    assert set(final_cr)==(set(before_cr)-{cr['metadata']['uid']})|{restored['metadata']['uid']}
    for uid,o in final_cr.items():
        assert o['data']==(cr['data'] if uid==restored['metadata']['uid'] else before_cr[uid]['data'])
    for o in pods.values():
        history=next(v for v in final_cr.values() if v['metadata']['name']=='model-'+o['metadata']['labels'][REV])
        role=next(r for r in history['data']['data'] if r['name']==o['metadata']['labels'][m.R])
        assert m.version(role['entryTemplate' if o['metadata']['labels'].get(m.E)=='true' else 'workerTemplate'])==m.version(o)
    controller=m.yaml(p/'case-controller-before.yaml');after=m.yaml(p/'fault-controller-after.yaml')
    assert controller['metadata']['uid']==after['metadata']['uid'] and after['status']['containerStatuses'][0]['restartCount']==0
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    assert len(m.mine(final,'podgroups',owner))==(3 if mode=='SG' else 1)
    for kind,want in [('configmaps',3 if mode=='SG' else 6),('services',3)]:
        resources=m.mine(final,kind,owner);assert len(resources)==want
        for resource in resources.values():assert any(all(o['metadata']['labels'].get(k)==resource['metadata']['labels'].get(k) for k in (m.G,m.R,m.I)) for o in pods.values())
    return {'id':case['id'],'classification':'PASS','objectFault':anomaly,'watchRows':len(rows),'actualUnknownHistoryReads':len(unknown_reads),'successfulReadsAfterRestore':len(reads),'protectedOriginalUIDs':old,'protectedReplacementUIDs':created,'outsideUIDsPreserved':outside,'originalHistoryUID':cr['metadata']['uid'],'injectedHistoryUID':None if injected is None else injected['metadata']['uid'],'restoredHistoryUID':restored['metadata']['uid'],'unknownWindowNanos':held['elapsedStableNanos'],'finalStableNanos':finalcp['elapsedStableNanos'],'limitation':'Actual API object replaced before protected entry deletion. All source history data remains immutable per UID; only the declared fixture has invalid/foreign content. Final Pods reference correct restored owned histories. No claim of live history existence while intentionally absent.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml','step-02-history-object-fixture.json']}}



def main():
    runid=sys.argv[1];assert runid and '/' not in runid and '..' not in runid
    base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control');done=m.read(control/'completion.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    build=m.read(control/'build.json');assert m.read(base/'environment.json')['runner']['binarySHA256']==build['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    trace=[json.loads(s) for s in open(control/'proxy-trace.jsonl')];reports=[];out=base/'independent-history-object-audit';out=base/(sys.argv[2] if len(sys.argv)>2 else out.name);out.mkdir()
    for result in m.read(base/'summary.json')['results']:
        try:
            if result['status']!='PASS' and result.get('error','').startswith('step 01 establish-protected-A-and-eligible-B: STABILITY_VIOLATION:'):
                loader=importlib.util.spec_from_file_location('precondition',ROOT/'scripts/audit-history-precondition.py');precondition=importlib.util.module_from_spec(loader);loader.loader.exec_module(precondition)
                report=precondition.audit(base/result['id'],trace)
            else:report=audit_case(base/result['id'],trace)
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
