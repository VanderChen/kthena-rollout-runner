#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Audit actual numerical/zero boundaries from full Watch and admitted requests."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('boundary_reject',ROOT/'scripts/audit-rejection.py')
h=importlib.util.module_from_spec(loader);loader.loader.exec_module(h);m=h.m
REV=h.REV


def plugin_regression(p, rows, owner, baseline, stage, deletions):
    """Prove a new memberless ranktable after actual target state became clean."""
    spec=stage['server']['spec'];d,u,s,part=h.budget(spec,'frontend')
    assert spec['rolloutStrategy']['type']=='RoleRollingUpdate' and part==0
    state=collections.defaultdict(dict);clean=None;failure=None
    def completed(value):
        pods=m.mine(value,'pods',owner)
        assert len(pods)==d+3 and all(m.ready(o) for o in pods.values())
        assert collections.Counter(m.version(o) for o in pods.values())=={'A':3,'B':d}
        for uid,o in pods.items():
            assert m.version(o)==('B' if o['metadata']['labels'][m.R]=='frontend' else 'A')
            if o['metadata']['labels'][m.R]=='backend':assert uid in baseline
        h.plugins(value,owner,spec)
        ms=next(o for o in value['modelservings'].values() if o['metadata']['uid']==owner)
        status=ms['status'];assert status['observedGeneration']>=stage['server']['metadata']['generation']
        assert status['currentRevision']==status['updateRevision'] and status['replicas']==status['availableReplicas']==1
        histories=m.mine(value,'controllerrevisions',owner)
        target=next(o for o in histories.values() if o['metadata']['name']=='model-'+status['updateRevision'])
        requested={r['name']:r for r in spec['template']['roles']}
        for role in target['data']['data']:
            for key in ('entryTemplate','workerTemplate'):assert role.get(key)==requested[role['name']].get(key)
        return set(pods)
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind']
        if clean and kind=='configmaps' and row['event']=='ADDED' and m.owned(o,owner):
            members=[pod for pod in m.mine(state,'pods',owner).values() if all(pod['metadata']['labels'].get(k)==o['metadata']['labels'].get(k) for k in (m.G,m.R,m.I))]
            data=json.loads(o['data']['ranktable.json'])
            if not members and data['status']=='Initializing' and int(data['server_count'])==0:
                failure=row;break
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
        if m.ts(row['received'])>=stage['received']:
            try:uids=completed(state)
            except (AssertionError,KeyError,StopIteration):
                clean=None
                continue
            clean={'sequence':row['sequence'],'at':row['received'],'podUIDs':sorted(uids)}
    assert clean and failure, 'no independent completed-state to orphan-creation transition'
    cm=failure['object'];final=m.yaml(p/'final-resources.yaml');assert cm['metadata']['uid'] in m.mine(final,'configmaps',owner)
    actual=m.mine(final,'pods',owner);assert set(actual)==set(clean['podUIDs']) and all(m.ready(o) for o in actual.values())
    assert not [o for o in actual.values() if all(o['metadata']['labels'].get(k)==cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I))]
    removed=[r for r in rows if r['kind']=='pods' and r['event']=='DELETED' and all(r['object']['metadata']['labels'].get(k)==cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I))]
    assert removed and m.ts(removed[-1]['received'])<m.ts(failure['received'])
    return {'id':m.read(p/'result.json')['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'MEMBERLESS_RANKTABLE_RECREATED_AFTER_TARGET_SETTLED','watchRows':len(rows),'lastCleanState':clean,'unexpectedConfigMap':cm,'birthSequence':failure['sequence'],'birthAt':failure['received'],'lastMemberDeletedAt':removed[-1]['received'],'lastMemberUID':removed[-1]['object']['metadata']['uid'],'healthyDeletionChecks':deletions,'limitation':'The actual target capacity, history status and plugin state first became clean, then a new memberless Initializing ranktable regressed the required stability window. Frozen final evidence retains that UID. This proves a stability/cleanup regression, not permanent leakage; a full final30s window did not complete.'}


def audit_case(p):
    case=m.yaml(p/'case.yaml');result=m.read(p/'result.json');source=case['scenario']['source'];n=int(case['id'][4:])
    assert 540<=n<=572 and result['id']==case['id']==source['id']
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
    assert source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    initial=m.yaml(p/'before-server.yaml');owner=initial['metadata']['uid']
    rows=[json.loads(s) for s in open(p/'observations.jsonl')];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    baseline=m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner)
    assert len(baseline)==initial['spec'].get('replicas',1)*sum(r.get('replicas',1) for r in initial['spec']['template']['roles']) and all(m.ready(o) and m.version(o)=='A' for o in baseline.values())
    stages=[]
    for i,step in enumerate(case['scenario']['steps'],1):
        prefix='step-%02d'%i
        if not (p/(prefix+'-server.yaml')).exists():break
        server=m.yaml(p/(prefix+'-server.yaml'));request=m.yaml(p/(prefix+'-request.yaml'));receipt=m.read(p/(prefix+'-request-time.json'))
        assert server['metadata']['uid']==owner and server['metadata']['generation']==i+1 and request['spec']==step['spec']
        stages.append({'index':i,'step':step,'server':server,'sent':m.ts(receipt['sent']),'received':m.ts(receipt['received'])})
    assert stages,'valid API input acceptance requires separate review of native response if rejected'
    state=collections.defaultdict(dict);history={};deletions=[];violations=[]
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received']);prev=state[kind].get(uid)
        active=next((s for s in reversed(stages) if s['sent']<=at),None)
        if kind=='controllerrevisions' and m.owned(o,owner):assert uid not in history or history[uid]==o['data'];history[uid]=o['data']
        if active is not None and kind=='pods' and m.owned(o,owner):
            spec=active['server']['spec'];role=o['metadata']['labels'][m.R];d,u,s,part=h.budget(spec,role)
            ordinal=int(o['metadata']['labels'][m.G if source['config']['mode']=='SG' else m.I].rsplit('-',1)[1])
            if role=='backend' or uid in baseline and ordinal<part:
                assert uid in baseline and row['event']!='DELETED' and not o['metadata'].get('deletionTimestamp') and m.version(o)=='A'
            elif uid not in baseline:assert m.version(o)=='B' and role=='frontend'
            if prev and m.ready(prev) and (row['event']=='DELETED' or o['metadata'].get('deletionTimestamp')):
                ready={key:pod for key,pod in m.mine(state,'pods',owner).items() if pod['metadata']['labels'][m.R]==role and m.ready(pod)}
                proof={'sequence':row['sequence'],'at':row['received'],'uid':uid,'name':o['metadata']['name'],'readyUIDsBefore':list(ready),'readyBefore':len(ready),'minimum':max(0,d-u)}
                deletions.append(proof)
                if len(ready)-1<max(0,d-u):violations.append(proof)
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
        if active is not None:
            spec=active['server']['spec'];front=[p for p in m.mine(state,'pods',owner).values() if p['metadata']['labels'][m.R]=='frontend' and not p['metadata'].get('deletionTimestamp')]
            d,u,s,part=h.budget(spec,'frontend');assert len(front)<=((d+s) if spec.get('replicas',1)>0 else 0)
    if violations:
        assert result['status']=='FAIL' and 'BUDGET_VIOLATION' in result.get('error','')
        return {'id':case['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'HEALTHY_DELETION_BELOW_NUMERIC_BUDGET','watchRows':len(rows),'firstViolation':violations[0],'healthyDeletionChecks':deletions,'limitation':'Actual accepted numerical boundary followed by independently reconstructed unhealthy capacity loss. Later final convergence is not credited.'}
    if result['status']=='FAIL' and 'STABILITY_VIOLATION: settled predicate regressed: unexpected ConfigMap' in result.get('error',''):
        return plugin_regression(p,rows,owner,baseline,stages[-1],deletions)
    assert result['status']=='PASS' and not result.get('violations') and len(stages)==len(case['scenario']['steps'])
    reports=[]
    for stage in stages:
        index=stage['index'];spec=stage['server']['spec'];step=stage['step'];d,u,s,part=h.budget(spec,'frontend');protected=min(d,part) if n<=565 else 0
        cp=next(m.read(f) for f in p.glob('checkpoint-*.json') if m.read(f)['phase']==step['name']);assert cp['elapsedStableNanos']>=30_000_000_000 and m.ts(cp['stableSince'])>=stage['received']
        final=m.yaml(p/('step-%02d-resources.yaml'%index));pods=m.mine(final,'pods',owner)
        expected=(d if source['config']['mode']=='SG' else d+3) if spec.get('replicas',1)>0 else 0
        assert len(pods)==expected and all(m.ready(o) for o in pods.values())
        front={uid:o for uid,o in pods.items() if o['metadata']['labels'][m.R]=='frontend'}
        counts={}
        if protected:counts['A']=protected
        if d-protected and spec.get('replicas',1)>0:counts['B']=d-protected
        assert collections.Counter(m.version(o) for o in front.values())==counts
        for uid,o in pods.items():
            role=o['metadata']['labels'][m.R]
            if role=='backend' or m.version(o)=='A':assert uid in baseline
        h.plugins(final,owner,spec)
        histories=m.mine(final,'controllerrevisions',owner);ms=next(o for o in final['modelservings'].values() if o['metadata']['uid']==owner);status=ms['status']
        assert status['observedGeneration']>=stage['server']['metadata']['generation'] and status.get('replicas',0)==status.get('availableReplicas',0)==spec.get('replicas',1)
        target=next(o for o in histories.values() if o['metadata']['name']=='model-'+status['updateRevision'])
        history_roles={r['name']:r for r in target['data']['data']};requested_roles={r['name']:r for r in spec['template']['roles']}
        assert set(history_roles)==set(requested_roles)
        for name,role in requested_roles.items():
            for template in ('entryTemplate','workerTemplate'):assert history_roles[name].get(template)==role.get(template)
        # Scaling keeps the same template history; its captured replica counts
        # need not be rewritten to match a later accepted scale request.
        if status.get('currentRevision'):assert any(o['metadata']['name']=='model-'+status['currentRevision'] for o in histories.values())
        if n<=565 and protected==0 or index==2:assert status['currentRevision']==status['updateRevision']
        for pod in pods.values():
            cr=next(o for o in histories.values() if o['metadata']['name']=='model-'+pod['metadata']['labels'][REV]);role=next(r for r in cr['data']['data'] if r['name']==pod['metadata']['labels'][m.R]);assert m.version(role['entryTemplate'])==m.version(pod)
        state=m.replay(rows,cp['stableSince']);assert set(m.mine(state,'pods',owner))==set(pods)
        for row in rows:
            if not m.ts(cp['stableSince'])<m.ts(row['received'])<=m.ts(cp['completed']):continue
            uid=row['object']['metadata']['uid']
            if row['event']=='DELETED':state[row['kind']].pop(uid,None)
            else:state[row['kind']][uid]=row['object']
            actual=m.mine(state,'pods',owner);assert set(actual)==set(pods) and all(m.ready(o) for o in actual.values())
            h.plugins(state,owner,spec)
        reports.append({'phase':step['name'],'effectiveFrontendBudget':[d,u,s,part],'stableNanos':cp['elapsedStableNanos'],'actualPodUIDs':list(pods),'targetHistoryUID':target['metadata']['uid'],'targetHistoryReadable':True})
    if len(reports)==2:assert reports[0]['targetHistoryUID']==reports[1]['targetHistoryUID'],'zero-dimension expansion created a second target history'
    return {'id':case['id'],'classification':'PASS','watchRows':len(rows),'phases':reports,'healthyDeletionChecks':deletions,'limitation':'Full direct Watch and actual API requests verify literal rounding, retained original protected/unchanged UIDs, complete capacity, referenced histories and stable plugin resources. Zero dimensions need readable B target history without requiring an otherwise idle current revision to promote.','evidenceSHA256':{f:hashlib.sha256((p/f).read_bytes()).hexdigest() for f in ('case.yaml','result.json','observations.jsonl','final-resources.yaml')}}


def main():
    runid=sys.argv[1];base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control')
    done=m.read(control/'completion.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==m.read(control/'build.json')['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    out=base/(sys.argv[2] if len(sys.argv)>2 else 'independent-boundary-audit');out.mkdir();reports=[]
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
