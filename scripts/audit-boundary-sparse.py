#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independent physical source, ordinal protection and budget boundary review."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT=pathlib.Path(__file__).resolve().parents[1]
def load(name,file):
    loader=importlib.util.spec_from_file_location(name,ROOT/'scripts'/file)
    value=importlib.util.module_from_spec(loader);loader.loader.exec_module(value);return value
h=load('sparse_reject','audit-rejection.py');m=h.m
f=load('sparse_fixture','audit-history-sparse-fixture.py')


def audit_case(p,trace):
    case=m.yaml(p/'case.yaml');result=m.read(p/'result.json');source=case['scenario']['source'];n=int(case['id'][4:])
    assert 574<=n<=603 and result['id']==case['id']==source['id']
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
    assert source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    rows=[json.loads(s) for s in open(p/'observations.jsonl')]
    assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    prep=f.audit(p,rows);boundary=m.read(p/'source-boundary.json');initial=m.yaml(p/'source-initial-server.yaml');owner=initial['metadata']['uid']
    base=m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner)
    step=case['scenario']['steps'][0];server=m.yaml(p/'step-01-server.yaml');request=m.yaml(p/'step-01-request.yaml');receipt=m.read(p/'step-01-request-time.json')
    identical=n in (594,595,596,600,601,602);trap=n in (592,598);sg=source['config']['mode']=='SG';axis=m.G if sg else m.I
    ordinal=lambda o:int(o['metadata']['labels'][axis].rsplit('-',1)[1])
    assert request['spec']==step['spec'] and server['metadata']['uid']==owner and server['metadata']['generation']==(3 if identical else 4)
    assert m.ts(receipt['sent'])>m.ts(boundary['at']) and (server['spec']==initial['spec'])==identical
    spec=server['spec'];d,u,s,part=h.budget(spec,'frontend');assert d==3
    protected={uid:o for uid,o in base.items() if o['metadata']['labels'][m.R]=='backend' or identical or trap or ordinal(o)<part}
    expected_old=sum(o['metadata']['labels'][m.R]=='frontend' for o in protected.values());counts={}
    if expected_old:counts['A']=expected_old
    if expected_old<3:counts['B']=3-expected_old
    state=collections.defaultdict(dict);history={};deletions=[];budget_bad=[];order_bad=[];pg_bad=[]
    source_pgs=m.mine(m.yaml(p/'baseline-resources.yaml'),'podgroups',owner)
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received']);prev=state[kind].get(uid)
        if kind=='controllerrevisions' and m.owned(o,owner):
            assert uid not in history or history[uid]==o['data'];history[uid]=o['data']
        if sg and row['sequence']>prep['lastPreparationSequence'] and kind=='podgroups' and row['event']=='DELETED' and uid in source_pgs:
            members={key:pod for key,pod in m.mine(state,'pods',owner).items() if pod['metadata']['labels'][m.G]==o['metadata']['name']}
            ready={key:pod for key,pod in m.mine(state,'pods',owner).items() if m.ready(pod)}
            if members and set(members)<=set(base) and all(m.ready(pod) for pod in members.values()) and len(ready)-len(members)<max(0,d-u):
                requests=[t for t in trace if t['action']=='request' and t.get('namespace')==result['namespace'] and t.get('resource')=='podgroups' and t.get('method')=='DELETE' and t.get('name')==o['metadata']['name'] and m.ts(receipt['sent'])<m.ts(t['at'])<=at]
                assert requests;native=min(requests,key=lambda t:m.ts(t['at']));response=next(t for t in trace if t['action']=='response' and t['request']==native['request']);assert response['status']==200
                pg_bad.append({'sequence':row['sequence'],'at':row['received'],'podGroupUID':uid,'podGroupName':o['metadata']['name'],'originalReadyMemberUIDs':list(members),'readyBefore':len(ready),'minimum':max(0,d-u),'actualDELETE':native,'nativeResponse':response})
        if row['sequence']>prep['lastPreparationSequence'] and kind=='pods' and m.owned(o,owner):
            if uid in protected:assert row['event']!='DELETED' and not o['metadata'].get('deletionTimestamp') and m.version(o)=='A'
            if uid not in base:assert (m.version(o)=='B' or sg and ordinal(o)<part and m.version(o)=='A') and o['metadata']['labels'][m.R]=='frontend' and not identical and not trap
            if prev and m.ready(prev) and (row['event']=='DELETED' or o['metadata'].get('deletionTimestamp')):
                assert at>m.ts(receipt['sent']) and o['metadata']['labels'][m.R]=='frontend' and ordinal(o)>=part
                ready={key:pod for key,pod in m.mine(state,'pods',owner).items() if pod['metadata']['labels'][m.R]=='frontend' and m.ready(pod)}
                proof={'sequence':row['sequence'],'at':row['received'],'uid':uid,'name':o['metadata']['name'],'ordinal':ordinal(o),'readyUIDsBefore':list(ready),'readyBefore':len(ready),'minimum':max(0,d-u)}
                deletions.append(proof)
                if len(ready)-1<max(0,d-u):budget_bad.append(proof)
                higher={key:pod for key,pod in ready.items() if key in base and m.version(pod)=='A' and ordinal(pod)>ordinal(o) and ordinal(pod)>=part}
                if higher:
                    requests=[t for t in trace if t['action']=='request' and t.get('namespace')==result['namespace'] and t.get('resource')=='pods' and t.get('method')=='DELETE' and t.get('name')==o['metadata']['name'] and m.ts(receipt['sent'])<m.ts(t['at'])<=at]
                    assert requests
                    first=min(requests,key=lambda t:m.ts(t['at']))
                    if not any(t['action']=='request' and t.get('namespace')==result['namespace'] and t.get('resource')=='pods' and t.get('method')=='DELETE' and t.get('name') in [v['metadata']['name'] for v in higher.values()] and m.ts(receipt['sent'])<m.ts(t['at'])<=m.ts(first['at']) for t in trace):order_bad.append(dict(proof,higherReadyOriginalUIDs=list(higher),firstActualDELETE=first))
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
        if row['sequence']>prep['lastPreparationSequence']:
            front=[o for o in m.mine(state,'pods',owner).values() if o['metadata']['labels'][m.R]=='frontend' and not o['metadata'].get('deletionTimestamp')]
            assert len(front)<=d+s
    if pg_bad:
        assert result['status']=='FAIL' and 'PodGroup deletion exceeds released capacity' in result.get('error','')
        return {'id':case['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'ORIGINAL_SG_REPLACEMENT_STARTED_WITHOUT_READY_BUDGET','watchRows':len(rows),'sourcePreparation':prep,'firstViolation':pg_bad[0],'limitation':'Native API200 and full Watch prove deletion of a second original healthy SG PodGroup while only2 Ready source units remain and U1 requires2. The prior replacement has no Ready credit. The final journal freezes on PG deletion before that groups Pod deletion notification, so no later Pod Watch/convergence credit is claimed. New low-ordinal A creation is recorded but not alone treated as the budget failure.'}
    if budget_bad or order_bad:
        assert result['status']=='FAIL'
        return {'id':case['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'HEALTHY_DELETION_BELOW_SPARSE_BOUNDARY_BUDGET' if budget_bad else 'LOWER_ORDINAL_DELETED_BEFORE_READY_ELIGIBLE_HIGHER_SOURCE','watchRows':len(rows),'sourcePreparation':prep,'firstViolation':(budget_bad or order_bad)[0],'budgetViolations':budget_bad,'orderViolations':order_bad,'limitation':'Actual accepted boundary and original sparse identities verified; failure before final convergence, no later completion credit.'}
    assert result['status']=='PASS' and not result.get('violations')
    cp=next(m.read(x) for x in p.glob('checkpoint-*.json') if m.read(x)['phase']==step['name'])
    assert cp['elapsedStableNanos']>=30_000_000_000 and m.ts(cp['stableSince'])>=m.ts(receipt['received'])
    final=m.yaml(p/'final-resources.yaml');pods=m.mine(final,'pods',owner);assert len(pods)==(3 if sg else 6) and all(m.ready(o) for o in pods.values())
    front={uid:o for uid,o in pods.items() if o['metadata']['labels'][m.R]=='frontend'}
    assert collections.Counter(m.version(o) for o in front.values())==counts
    assert all(uid in pods and pods[uid]['spec']==o['spec'] for uid,o in protected.items())
    if identical or trap or part>4:assert set(pods)==set(base)
    h.plugins(final,owner,spec)
    histories=m.mine(final,'controllerrevisions',owner);ms=next(o for o in final['modelservings'].values() if o['metadata']['uid']==owner);status=ms['status']
    assert status['observedGeneration']>=server['metadata']['generation'] and status.get('replicas',0)==status.get('availableReplicas',0)==spec['replicas']
    target=next(o for o in histories.values() if o['metadata']['name']=='model-'+status['updateRevision'])
    roles={r['name']:r for r in target['data']['data']}
    for role in spec['template']['roles']:
        for template in ('entryTemplate','workerTemplate'):assert roles[role['name']].get(template)==role.get(template)
    if identical:assert len(histories)==1 and status['currentRevision']==status['updateRevision']
    elif trap:
        assert status['currentRevision']!=status['updateRevision'] and any(c['type']=='UpdateInProgress' and c['status']=='True' for c in status['conditions'])
    elif expected_old==0:assert status['currentRevision']==status['updateRevision']
    for pod in pods.values():
        cr=next(o for o in histories.values() if o['metadata']['name']=='model-'+pod['metadata']['labels'][h.REV]);role=next(r for r in cr['data']['data'] if r['name']==pod['metadata']['labels'][m.R]);assert m.version(role['entryTemplate'])==m.version(pod)
    stable=m.replay(rows,cp['stableSince']);assert set(m.mine(stable,'pods',owner))==set(pods)
    for row in rows:
        if not m.ts(cp['stableSince'])<m.ts(row['received'])<=m.ts(cp['completed']):continue
        uid=row['object']['metadata']['uid']
        if row['event']=='DELETED':stable[row['kind']].pop(uid,None)
        else:stable[row['kind']][uid]=row['object']
        actual=m.mine(stable,'pods',owner);assert set(actual)==set(pods) and all(m.ready(o) for o in actual.values());h.plugins(stable,owner,spec)
    before=m.yaml(p/'fixture-preparation/fixture-restart-controller-replacement.yaml');after=m.yaml(p/'fault-controller-after.yaml');assert before['metadata']['uid']==after['metadata']['uid'] and after['status']['containerStatuses'][0]['restartCount']==0
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    return {'id':case['id'],'classification':'PASS','watchRows':len(rows),'sourcePreparation':prep,'effectiveFrontendBudget':[d,u,s,part],'protectedOriginalUIDs':list(protected),'actualPodUIDs':list(pods),'stableNanos':cp['elapsedStableNanos'],'healthyDeletionChecks':deletions,'blockedIncompleteTrap':trap,'identicalSpec':identical,'limitation':'Finite preparation only precedes source boundary; actual original ordinal protection, full Watch, plugin resources and readable history verified. Zero-budget traps pass only by safe incomplete waiting.'}


def main():
    runid=sys.argv[1];base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control')
    done=m.read(control/'completion.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==m.read(control/'build.json')['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    trace=[json.loads(s) for s in open(control/'proxy-trace.jsonl')]
    out=base/(sys.argv[2] if len(sys.argv)>2 else 'independent-sparse-boundary-audit');out.mkdir();reports=[]
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
