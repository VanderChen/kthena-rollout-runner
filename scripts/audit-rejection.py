#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independently inspect native rejection, atomic storage, actual triggers and continued rollout."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('deny_mid',ROOT/'scripts/audit-midrollout.py')
m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)
REV='modelserving.volcano.sh/revision'


def budget(spec,role):
    sg=spec['rolloutStrategy'].get('type','ServingGroupRollingUpdate')=='ServingGroupRollingUpdate'
    r=next(r for r in spec['template']['roles'] if r['name']==role)
    d=spec.get('replicas',1) if sg else r.get('replicas',1)
    raw=spec['rolloutStrategy'].get('rollingUpdateConfiguration',{}) if sg else r
    def value(key,default):
        v=raw.get(key,default)
        if v is None:v=default
        if isinstance(v,str):
            pct=int(v[:-1]);v=(d*pct//100) if key=='maxUnavailable' else ((d*pct+99)//100)
            if sg and key=='maxUnavailable' and pct>0 and d>0:v=max(1,v)
        return v
    return d,value('maxUnavailable',1),value('maxSurge',0),value('partition',0)


def plugins(state,owner,spec):
    pods=m.mine(state,'pods',owner);assert all(m.ready(o) for o in pods.values())
    assert not m.mine(state,'services',owner),'W0 has no live headless Service cohort'
    cms=m.mine(state,'configmaps',owner);assert len(cms)==len(pods)
    for cm in cms.values():
        members=[o for o in pods.values() if all(o['metadata']['labels'].get(k)==cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I))]
        assert len(members)==1
        data=json.loads(cm['data']['ranktable.json']);assert data['status']=='Completed' and int(data['server_count'])==1
    pgs=m.mine(state,'podgroups',owner);assert len(pgs)==spec.get('replicas',1)
    assert {o['metadata']['name'] for o in pgs.values()}=={o['metadata']['labels'][m.G] for o in pods.values()}
    gang=spec['template'].get('gangPolicy',{}).get('minRoleReplicas',{})
    minimum=sum(gang.get(r['name'],r.get('replicas',1)) for r in spec['template']['roles'])
    for pg in pgs.values():assert pg['spec']['minMember']==minimum


def audit_case(p):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');n=int(case['id'][5:]);source=case['scenario']['source']
    assert 1<=n<=97 and result['id']==case['id']==source['id'] and case['format']=='rollout-runner/v4'
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
    assert source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    rows=[json.loads(s) for s in open(p/'observations.jsonl')]
    assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    index,step=next((i,s) for i,s in enumerate(case['scenario']['steps'],1) if s['action'].startswith('reject-'))
    prefix='step-%02d'%index;before=m.yaml(p/(prefix+'-accepted-before.yaml'));owner=before['metadata']['uid'];spec=before['spec'];generation=before['metadata']['generation']
    initial=m.yaml(p/'before-server.yaml');assert initial['metadata']['uid']==owner and initial['metadata']['generation']==1
    active=n in (54,55,78,79,96,97);assert generation==(2 if active or n in (94,95) else 1)
    receipts=[(f,m.read(f)) for f in sorted(p.glob(prefix+'-reject-*-receipt.json'))];assert receipts
    for file,receipt in receipts:
        attempt=file.name.removesuffix('-receipt.json');request=m.yaml(p/(attempt+('-merge-patch.yaml' if receipt['method']=='PATCH' else '-request.yaml')))
        old=m.yaml(p/(attempt+'-before.yaml'))
        assert old['metadata']['uid']==owner and old['metadata']['generation']==generation and old['spec']==spec
        if receipt['method']=='PUT':assert request['spec']==step['spec']
        else:
            assert request['spec']==step['patch']['spec'] and set(request)=={'metadata','spec'}
        assert request['metadata']['uid']==owner and request['metadata']['resourceVersion']==receipt['requestResourceVersion']==old['metadata']['resourceVersion']
        assert m.ts(receipt['sent'])<=m.ts(receipt['received'])
    if result['status']!='PASS' and 'ADMISSION_UNEXPECTED_ACCEPT' in result.get('error',''):
        accepted=[(f,r) for f,r in receipts if r['accepted']];assert len(accepted)==1
        file,receipt=accepted[0];actual=m.yaml(p/(file.name.removesuffix('-receipt.json')+'-unexpected-accepted.yaml'))
        assert actual['metadata']['uid']==owner
        return {'id':case['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'ADMISSION_UNEXPECTED_ACCEPT','watchRows':len(rows),'actualAcceptedRequest':receipt,'storedResponseGeneration':actual['metadata']['generation'],'source':source,'limitation':'Actual request required to be rejected was accepted by the API. Later continuation and immutable UID window were not verified; no source changes and no product rerun.'}
    if result['status']=='INCONCLUSIVE' and 'request did not reach a conclusive admission rejection' in result.get('error',''):
        receipt=receipts[-1][1];status=receipt['status'];message=status.get('message','')
        assert not receipt['accepted'] and status['code'] in (400,422) and status['status']=='Failure' and not status.get('reason')
        assert message.startswith('admission webhook "') and '" denied the request:' in message
        assert all(r['status']['code']==409 for _,r in receipts[:-1])
        final=m.yaml(p/'final-resources.yaml');actual=next(o for o in final['modelservings'].values() if o['metadata']['uid']==owner)
        assert actual['metadata']['generation']==generation and actual['spec']==spec
        return {'id':case['id'],'classification':'RUNNER_ORACLE','failure':'EXPLICIT_NATIVE_WEBHOOK_DENIAL_WITHOUT_REASON_MISCLASSIFIED','watchRows':len(rows),'actualAdmissionStatus':status,'requestReceipt':receipt,'limitation':'The native API explicitly rejected the request, but runner incorrectly required a reason field and stopped. Frozen accepted spec is unchanged. Required post-rejection stable window and in-flight continuation did not execute; zero valid catalogue verdict credit pending a corrected supplement.'}
    assert result['status']=='PASS' and not result.get('violations')
    proof=m.read(p/(prefix+'-rejection.json'));status=proof['status']
    assert not receipts[-1][1]['accepted'] and status==receipts[-1][1]['status']
    assert status['code'] in (400,403,422)
    if status['code']==403:assert 'admission webhook' in status['message'] and 'denied' in status['message']
    else:assert status.get('reason') in ('BadRequest','Invalid') or (status.get('status')=='Failure' and not status.get('reason') and status.get('message','').startswith('admission webhook "') and '" denied the request:' in status['message'])
    assert all(r['status']['code']==409 for _,r in receipts[:-1])
    assert proof['attemptPrefix']==receipts[-1][0].name.removesuffix('-receipt.json')
    after=m.yaml(p/(proof['attemptPrefix']+'-after.yaml'));assert after['metadata']['uid']==owner and after['metadata']['generation']==generation and after['spec']==spec
    snapshots=[m.read(f) for f in sorted(p.glob('rejection-state-*.json'))];assert len(snapshots)>=2
    for sample in snapshots:
        o=sample['object'];assert o['metadata']['uid']==owner and o['metadata']['generation']==generation and o['spec']==spec
    trigger=m.yaml(p/(proof['attemptPrefix']+'-trigger-pods.yaml'))['items'];trigger={o['metadata']['uid']:o for o in trigger if m.owned(o,owner)}
    assert len(trigger)==sum(r.get('replicas',1) for r in spec['template']['roles'])*spec.get('replicas',1)+(1 if n in (54,55,78,79) else 0)
    for c in step.get('conditions',[]):
        found=[o for o in trigger.values() if o['metadata']['labels'][m.R]==c['role'] and m.version(o)==c['version'] and m.ready(o)==c['ready'] and ('ordinal' not in c or int(o['metadata']['labels'][m.I].rsplit('-',1)[1])==c['ordinal'])]
        assert len(found)>=c['count']
    if not active:assert all(m.ready(o) for o in trigger.values())
    cps=[m.read(f) for f in p.glob('checkpoint-*.json')];held=next(c for c in cps if c['phase']==step['name']);finalcp=next(c for c in cps if c['phase']==case['scenario']['steps'][-1]['name'])
    assert held['elapsedStableNanos']>=(10 if active else 30)*1_000_000_000 and finalcp['elapsedStableNanos']>=30_000_000_000
    probe_times=[m.ts(sample['at']) for sample in snapshots]
    assert probe_times[0]<=m.ts(held['stableSince']) and probe_times[-1]>=m.ts(finalcp['completed'])-2_000_000_000
    assert all(0<b-a<=2_000_000_000 for a,b in zip(probe_times,probe_times[1:])), 'native rejection probes contain an unreviewed timing gap'
    assert m.ts(proof['received'])<m.ts(held['stableSince'])
    for sample in [held['stableSince'],held['completed']]:
        state=m.replay(rows,sample);pods=m.mine(state,'pods',owner)
        assert set(pods)==set(trigger)
        if not active:assert all(m.ready(o) for o in pods.values())
    base=m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner);assert all(m.version(o)=='A' and m.ready(o) for o in base.values())
    accepted_at=m.ts(m.read(p/'step-01-request-time.json')['sent']) if active or n in (94,95) else m.ts(proof['sent'])
    original_roles={r['name']:r for r in initial['spec']['template']['roles']};current_roles={r['name']:r for r in spec['template']['roles']}
    changed={r for r in current_roles if original_roles[r]['entryTemplate']!=current_roles[r]['entryTemplate']}
    state=collections.defaultdict(dict);data={};deletes=[]
    histories_before=m.mine(m.replay(rows,proof['sent']),'controllerrevisions',owner)
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received']);prev=state[kind].get(uid)
        if kind=='controllerrevisions' and m.owned(o,owner):
            assert uid not in data or data[uid]==o['data'];data[uid]=o['data']
            if at>=m.ts(proof['sent']):assert uid in histories_before and row['event']!='DELETED'
        if kind=='modelservings' and o['metadata']['name']=='model' and at>=m.ts(proof['sent']):
            assert uid==owner and row['event']!='DELETED' and o['metadata']['generation']==generation and o['spec']==spec
        if kind=='pods' and m.owned(o,owner) and at>=accepted_at:
            role=o['metadata']['labels'][m.R];d,u,s,part=budget(spec,role);ordinal=int(o['metadata']['labels'][m.G if source['config']['mode']=='SG' else m.I].rsplit('-',1)[1])
            if not active or role not in changed or uid in base and ordinal<part:
                assert uid in base and row['event']!='DELETED' and not o['metadata'].get('deletionTimestamp') and m.version(o)=='A'
            if prev and m.ready(prev) and (row['event']=='DELETED' or o['metadata'].get('deletionTimestamp')):
                ready={key:pod for key,pod in m.mine(state,'pods',owner).items() if pod['metadata']['labels'][m.R]==role and m.ready(pod)}
                assert len(ready)-1>=max(0,d-u)
                deletes.append({'at':row['received'],'uid':uid,'readyBefore':len(ready),'minimum':max(0,d-u)})
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
    final=m.yaml(p/'final-resources.yaml');pods=m.mine(final,'pods',owner)
    if not active:assert set(pods)==set(base)
    assert all(m.ready(o) for o in pods.values())
    for target in case['scenario']['steps'][-1]['expect']['targets']:
        selected=list(pods.values()) if target.get('scope')=='SG' else [o for o in pods.values() if o['metadata']['labels'][m.R]==target['role']]
        assert collections.Counter(m.version(o) for o in selected)==target['versions']
    plugins(final,owner,spec)
    finalcr=m.mine(final,'controllerrevisions',owner);assert set(finalcr)==set(histories_before)
    for uid,o in finalcr.items():assert o['data']==histories_before[uid]['data']
    for pod in pods.values():
        cr=next(c for c in finalcr.values() if c['metadata']['name']=='model-'+pod['metadata']['labels'][REV])
        r=next(r for r in cr['data']['data'] if r['name']==pod['metadata']['labels'][m.R]);assert m.version(r['entryTemplate'])==m.version(pod)
    state=m.replay(rows,finalcp['stableSince']);assert set(m.mine(state,'pods',owner))==set(pods)
    for row in rows:
        if not m.ts(finalcp['stableSince'])<m.ts(row['received'])<=m.ts(finalcp['completed']):continue
        uid=row['object']['metadata']['uid']
        if row['event']=='DELETED':state[row['kind']].pop(uid,None)
        else:state[row['kind']][uid]=row['object']
        actual=m.mine(state,'pods',owner);assert set(actual)==set(pods) and all(m.ready(o) for o in actual.values())
        plugins(state,owner,spec)
    controller=m.yaml(p/'case-controller-before.yaml');end=m.yaml(p/'fault-controller-after.yaml');assert controller['metadata']['uid']==end['metadata']['uid'] and end['status']['containerStatuses'][0]['restartCount']==0
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    return {'id':case['id'],'classification':'PASS','watchRows':len(rows),'actualAdmissionStatus':status,'optimisticConcurrencyRetries':len(receipts)-1,'acceptedGeneration':generation,'inFlightContinuation':active,'storedSpecProbes':len(snapshots),'heldStableNanos':held['elapsedStableNanos'],'finalStableNanos':finalcp['elapsedStableNanos'],'healthyDeletionChecks':deletes,'source':source,'limitation':'Actual API rejection and immutable stored spec/generation/UID; six in-flight cases retain the accepted rollout rather than freezing Pod UIDs. Status may advance, history data is immutable and no new history is introduced by the rejected request.','evidenceSHA256':{f:hashlib.sha256((p/f).read_bytes()).hexdigest() for f in ('case.yaml','result.json','observations.jsonl','final-resources.yaml',prefix+'-rejection.json')}}


def main():
    runid=sys.argv[1];base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control')
    done=m.read(control/'completion.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==m.read(control/'build.json')['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    out=base/(sys.argv[2] if len(sys.argv)>2 else 'independent-rejection-audit');out.mkdir();reports=[]
    for result in m.read(base/'summary.json')['results']:
        try:report=audit_case(base/result['id'])
        except (AssertionError,KeyError,StopIteration,FileNotFoundError) as e:
            import traceback
            report={'id':result['id'],'classification':'PENDING_REVIEW','rawStatus':result['status'],'rawError':result.get('error'),'auditError':repr(e),'traceback':traceback.format_exc()}
        reports.append(report);(out/(result['id']+'.json')).write_text(json.dumps(report,indent=2)+'\n');print(report['id'],report['classification'])
    pending=any(r['classification']=='PENDING_REVIEW' for r in reports)
    (out/'summary.json').write_text(json.dumps({'status':'PENDING_REVIEW' if pending else 'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},indent=2)+'\n')
    if pending:sys.exit(1)

if __name__=='__main__':main()
