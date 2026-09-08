#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Prove real old UID residues, new identity isolation and fixture-release convergence."""
import collections,hashlib,importlib.util,json,pathlib,sys
ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('identity_reject',ROOT/'scripts/audit-rejection.py');h=importlib.util.module_from_spec(loader);loader.loader.exec_module(h);m=h.m


def audit_case(p,trace):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');source=case['scenario']['source'];assert case['id']==source['id']=='RUN-611'
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes();assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5' and source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    old=m.yaml(p/'before-server.yaml');old_owner=old['metadata']['uid'];baseline=m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',old_owner);assert len(baseline)==6 and all(m.ready(o) and m.version(o)=='A' for o in baseline.values())
    deleted=m.read(p/'step-01-old-model-delete.json');assert deleted['accepted'] and deleted['uid']==old_owner==deleted['options']['preconditions']['uid'] and deleted['options']['propagationPolicy']=='Background'
    residues={o['metadata']['uid']:o for o in m.yaml(p/'step-01-old-residue-pods.yaml')['items']};cr=m.yaml(p/'step-01-old-residue-history.yaml');old_name=cr['metadata']['name'];old_cruid=cr['metadata']['uid']
    assert set(residues)==set(baseline) and all(m.owned(o,old_owner) and o['metadata'].get('deletionTimestamp') and 'rollout-runner/hold' in o['metadata'].get('finalizers',[]) for o in residues.values())
    assert m.owned(cr,old_owner) and cr['metadata'].get('deletionTimestamp') and 'rollout-runner/hold' in cr['metadata'].get('finalizers',[])
    for o in residues.values():
        pin=m.yaml(p/('step-01-old-pinned-'+o['metadata']['name']+'.yaml'));assert pin['metadata']['uid']==o['metadata']['uid'] and m.owned(pin,old_owner) and 'rollout-runner/hold' in pin['metadata']['finalizers']
    pin=m.yaml(p/'step-01-old-pinned-history.yaml');assert pin['metadata']['uid']==old_cruid and pin['data']==cr['data']
    new=m.yaml(p/'step-01-new-model-server.yaml');owner=new['metadata']['uid'];created=m.read(p/'step-01-new-model-create.json');assert created['accepted'] and owner!=old_owner and new['metadata']['generation']==1 and new['metadata']['name']==old['metadata']['name']=='model'
    assert m.ts(deleted['received'])<m.ts(created['sent']) and m.yaml(p/'step-01-new-model-request.yaml')['spec']==case['scenario']['steps'][0]['spec']
    rows=[json.loads(s) for s in open(p/'observations.jsonl')];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    assert any(r['kind']=='modelservings' and r['event']=='DELETED' and r['object']['metadata']['uid']==old_owner for r in rows)
    forbidden=set(baseline)|{old_cruid};adopted=[r for r in rows if r['object']['metadata']['uid'] in forbidden and m.owned(r['object'],owner)]
    probes=[m.read(f) for f in sorted(p.glob('step-01-identity-window-*.json'))];bad_capacity=[];bad_history=[]
    for probe in probes:
        ms=probe['model'];assert ms['metadata']['uid']==owner and ms['metadata']['generation']==1 and ms['spec']==new['spec']
        pods={o['metadata']['uid']:o for o in probe['pods']['items']};assert set(baseline)<=set(pods)
        fresh={uid:o for uid,o in pods.items() if m.owned(o,owner)};groups={o['metadata']['labels'][m.G] for o in fresh.values()};status=ms.get('status',{})
        if status.get('availableReplicas',0)>0 or status.get('replicas',0)>len(groups):bad_capacity.append(probe)
        if any('model-'+status.get(field,'')==old_name for field in ('currentRevision','updateRevision')):bad_history.append(probe)
        assert not any(m.ready(o) for o in fresh.values()),'new owned Pod Ready before runner release requires separate control review'
    if adopted or bad_capacity or bad_history:
        assert result['status']=='FAIL'
        return {'id':case['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'OLD_UID_ADOPTED' if adopted else 'OLD_OWNER_HISTORY_REFERENCED' if bad_history else 'OLD_CAPACITY_COUNTED_AS_NEW_IDENTITY','watchRows':len(rows),'oldOwnerUID':old_owner,'newOwnerUID':owner,'firstAdoption':adopted[0] if adopted else None,'firstBadNativeProbe':(bad_history or bad_capacity or [None])[0],'limitation':'Real old parent deleted and distinct new UID created; terminating old-UID residues were deliberately retained. Failure is identity/capacity/history use, not the runner finalizer blocking cleanup. Later convergence receives no credit.'}
    assert result['status']=='PASS' and not result.get('violations') and len(probes)>=10 and m.ts(probes[-1]['completed'])-m.ts(probes[0]['started'])>=10_000_000_000
    boundary=m.read(p/'step-01-source-boundary.json');assert boundary['oldOwnerUID']==old_owner and boundary['newOwnerUID']==owner and boundary['retainedOldHistoryUID']==old_cruid and set(boundary['retainedOldPods'].values())==set(baseline)
    for probe in probes:
        for o in probe['pods']['items']:
            if o['metadata']['uid'] in baseline:assert m.owned(o,old_owner)
        actual=probe['oldHistory'];assert actual['metadata']['uid']==old_cruid and m.owned(actual,old_owner) and actual['data']==cr['data']
    released=m.read(p/'step-01-fixture-releases.json');assert len(released)==7 and {r['uid'] for r in released}==forbidden and all(r['accepted'] and m.ts(r['sent'])>m.ts(probes[-1]['completed']) for r in released)
    for release in released:
        assert any(r['event']=='DELETED' and r['object']['metadata']['uid']==release['uid'] and m.ts(r['received'])>=m.ts(release['sent']) for r in rows)
    state=collections.defaultdict(dict);history={}
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received'])
        if kind=='controllerrevisions':assert uid not in history or history[uid]==o['data'];history[uid]=o['data']
        if kind=='modelservings' and uid==owner:
            s=o.get('status',{});assert not any('model-'+s.get(field,'')==old_name for field in ('currentRevision','updateRevision'))
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
        if at>=m.ts(probes[0]['started']) and at<=m.ts(probes[-1]['completed']):
            actual=m.mine(state,'pods',owner);assert not set(actual)&set(baseline) and not any(m.ready(o) for o in actual.values())
    final=m.yaml(p/'final-resources.yaml');pods=m.mine(final,'pods',owner);assert len(pods)==6 and not set(pods)&forbidden and all(m.ready(o) for o in pods.values()) and collections.Counter(m.version(o) for o in pods.values())=={'A':3,'B':3}
    assert not m.mine(final,'pods',old_owner) and not m.mine(final,'controllerrevisions',old_owner);h.plugins(final,owner,new['spec'])
    crs=m.mine(final,'controllerrevisions',owner);assert len(crs)==1 and old_cruid not in crs
    target=next(iter(crs.values()));assert target['metadata']['name']!=old_name
    requested={r['name']:r for r in new['spec']['template']['roles']}
    for role in target['data']['data']:
        for key in ('entryTemplate','workerTemplate'):assert role.get(key)==requested[role['name']].get(key)
    for pod in pods.values():assert 'model-'+pod['metadata']['labels'][h.REV]==target['metadata']['name'] and m.version(pod)==('B' if pod['metadata']['labels'][m.R]=='frontend' else 'A')
    ms=next(o for o in final['modelservings'].values() if o['metadata']['uid']==owner);assert ms['status']['replicas']==ms['status']['availableReplicas']==ms['status']['updatedReplicas']==1 and ms['status']['currentRevision']==ms['status']['updateRevision']
    cp=next(m.read(f) for f in p.glob('checkpoint*.json') if m.read(f)['phase']=='new-owner-automatic-population-after-fixture-release');assert cp['elapsedStableNanos']>=30_000_000_000
    state=m.replay(rows,cp['stableSince'])
    for row in [None]+[r for r in rows if m.ts(cp['stableSince'])<m.ts(r['received'])<=m.ts(cp['completed'])]:
        if row:
            uid=row['object']['metadata']['uid']
            if row['event']=='DELETED':state[row['kind']].pop(uid,None)
            else:state[row['kind']][uid]=row['object']
        assert set(m.mine(state,'pods',owner))==set(pods) and all(m.ready(o) for o in m.mine(state,'pods',owner).values());h.plugins(state,owner,new['spec'])
    controller=m.yaml(p/'case-controller-before.yaml');end=m.yaml(p/'fault-controller-after.yaml');assert controller['metadata']['uid']==end['metadata']['uid'] and end['status']['containerStatuses'][0]['restartCount']==0
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    return {'id':case['id'],'classification':'PASS','watchRows':len(rows),'oldOwnerUID':old_owner,'newOwnerUID':owner,'oldPodUIDs':list(baseline),'oldHistoryUID':old_cruid,'newPodUIDs':list(pods),'newHistoryUID':target['metadata']['uid'],'nativeIdentityProbes':len(probes),'identityWindowNanos':m.ts(probes[-1]['completed'])-m.ts(probes[0]['started']),'finalStableNanos':cp['elapsedStableNanos'],'limitation':'Actual terminating old-owner residues retained10s. New UID never adopts, counts or references old identities; runner-only finalizers then released and normal GC/population converges. This tests terminating residues, not indefinitely healthy orphan Pods, and does not label deliberate finalizer blocking as a product leak.'}


def main():
    runid=sys.argv[1];base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control')
    done=m.read(control/'completion.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==m.read(control/'build.json')['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    trace=[json.loads(s) for s in open(control/'proxy-trace.jsonl')]
    out=base/(sys.argv[2] if len(sys.argv)>2 else 'independent-identity-boundary-audit');out.mkdir();reports=[]
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
