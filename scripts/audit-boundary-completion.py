#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Physical B12 source, old status write, automatic completion and later restart."""
import collections,copy,hashlib,importlib.util,json,pathlib,sys
ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('completion_reject',ROOT/'scripts/audit-rejection.py');h=importlib.util.module_from_spec(loader);loader.loader.exec_module(h);m=h.m


def audit_case(p,trace):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');source=case['scenario']['source'];assert case['id']==source['id'] and 604<=int(case['id'][4:])<=609
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes();assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5' and source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    rows=[json.loads(s) for s in open(p/'observations.jsonl')];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    q=p/'completion-preparation';original=m.yaml(p/'before-server.yaml');owner=original['metadata']['uid'];old_status=m.read(q/'original-A-status.json')
    expanded=m.yaml(q/'step-00-server.yaml');b3=m.yaml(q/'step-01-server.yaml');b2=m.yaml(sorted(q.glob('restore-original-budget-B2-write-*-server.yaml'))[-1]);spec=b2['spec']
    assert [v['metadata']['generation'] for v in (original,expanded,b3,b2)]==[1,2,3,4] and all(v['metadata']['uid']==owner for v in (expanded,b3,b2))
    expected=copy.deepcopy(original['spec']);expected['rolloutStrategy'].pop('roleCoordination',None)
    for role in expected['template']['roles']:role.update(replicas=3,maxUnavailable=1,maxSurge=0,partition=0)
    assert expected==expanded['spec']
    expected_b=copy.deepcopy(expected)
    for role in expected_b['template']['roles']:
        for key in ('entryTemplate','workerTemplate'):
            for c in role.get(key,{}).get('spec',{}).get('containers',[]):
                for env in c.get('env',[]):
                    if env['name']=='ROLLOUT_VERSION':env['value']='B'
    assert expected_b==b3['spec'] and m.yaml(sorted(q.glob('restore-original-budget-B2-write-*-request.yaml'))[-1])['spec']==case['scenario']['steps'][0]['spec']
    six={o['metadata']['uid']:o for o in m.yaml(q/'six-B-pods.yaml')['items'] if m.owned(o,owner)};assert len(six)==6 and all(m.ready(o) and m.version(o)=='B' for o in six.values())
    boundary=m.read(p/'step-01-source-boundary.json');kept=boundary['retainedUIDs'];removed=boundary['removedPreparationUIDs'];assert len(kept)==4 and len(removed)==2 and set(kept.values())|set(removed.values())==set(six)
    ordinal=lambda o:int(o['metadata']['labels'][m.I].rsplit('-',1)[1])
    assert all(ordinal(six[u])==0 for u in removed.values())
    assert {(six[u]['metadata']['labels'][m.R],ordinal(six[u])) for u in kept.values()}=={('frontend',1),('frontend',2),('backend',1),('backend',2)}
    deletes=m.read(q/'external-zero-deletes.json');assert len(deletes)==2
    for receipt in deletes:
        assert receipt['accepted'] and receipt['uid']==removed[receipt['name']]==receipt['options']['preconditions']['uid'] and m.ts(receipt['received'])<m.ts(boundary['at'])
        assert any(r['kind']=='pods' and r['event']=='DELETED' and r['object']['metadata']['uid']==receipt['uid'] and m.ts(r['received'])<m.ts(boundary['at']) for r in rows)
    barrier=m.read(q/'barrier-after-fresh-controller.json');ids=[m.read(q/('sparse-fault-rule-%d.json'%i))['id'] for i in range(9)];assert not barrier['errors'] and barrier['inFlightAllowed']==0
    for rid in ids:
        rule=next(r for r in barrier['rules'] if r['id']==rid);assert rule['active'] and rule['mode']=='error' and rule['statusCode']==503 and set(rule['methods'])=={'POST','PUT','PATCH','DELETE'} and rule['released']==0
    assert sum(r['hits'] for r in barrier['rules'] if r['id'] in ids)>0
    clears=[t for t in trace if t.get('ruleID') in ids and t['action']=='rule-cleared'];assert len(clears)==9 and max(m.ts(t['at']) for t in clears)<m.ts(boundary['at'])
    old_controller=m.yaml(q/'preparation-controller-controller-terminated.yaml');controller=m.yaml(q/'preparation-controller-controller-replacement.yaml')
    restart=m.read(q/'preparation-controller-controller-delete.json');assert restart['accepted'] and restart['uid']==old_controller['metadata']['uid']==restart['options']['preconditions']['uid']
    assert old_controller['metadata']['uid']!=controller['metadata']['uid'] and old_controller['metadata']['ownerReferences']==controller['metadata']['ownerReferences'] and old_controller['spec']['containers'][0]['args']==controller['spec']['containers'][0]['args']
    assert controller['status']['containerStatuses'][0]['restartCount']==0 and controller['status']['containerStatuses'][0]['imageID']=='sha256:7c6ed6c78b37d7afaf104381351554ad75aed56c64d0aca2ec941ce652756265'
    if (q/'orphan-cleanup.json').exists():
        for receipt in m.read(q/'orphan-cleanup.json'):
            assert receipt['accepted'] and receipt['uid']==receipt['options']['preconditions']['uid'] and m.ts(receipt['received'])<m.ts(boundary['at'])
            cm=m.yaml(q/('orphan-zero-'+receipt['name']+'.yaml'));assert cm['metadata']['uid']==receipt['uid'] and m.owned(cm,owner) and int(cm['metadata']['labels'][m.I].rsplit('-',1)[1])==0
            direct=m.yaml(q/('cleanup-members-'+receipt['name']+'.yaml'))['items'];assert {o['metadata']['uid'] for o in direct if m.owned(o,owner)}==set(kept.values())
            live=m.mine(m.replay(rows,receipt['sent']),'pods',owner);assert not any(all(o['metadata']['labels'].get(k)==cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I)) for o in live.values())
    sourcecp=next(m.read(x) for x in q.glob('checkpoint*.json') if m.read(x)['phase']=='actual-sparse-B12-source-stable');assert sourcecp['elapsedStableNanos']>=10_000_000_000 and m.ts(sourcecp['completed'])<m.ts(boundary['at'])
    actual=m.yaml(p/'step-01-actual-B12-source-resources.yaml');pods=m.mine(actual,'pods',owner);assert set(pods)==set(kept.values()) and all(m.ready(o) and m.version(o)=='B' for o in pods.values());h.plugins(actual,owner,spec)
    label=boundary['statusWrite'];status_request=m.yaml(p/(label+'-request.yaml'));accepted=m.yaml(p/(label+'-server.yaml'));receipt=m.read(p/(label+'-receipt.json'))
    assert receipt['accepted'] and status_request['status']==old_status==accepted['status'] and accepted['metadata']['uid']==owner and accepted['metadata']['generation']==4
    assert boundary['oldCurrentRevision']==old_status['currentRevision'] and m.ts(restart['received'])<m.ts(sourcecp['stableSince'])<m.ts(receipt['sent'])
    start=m.ts(receipt['sent']);history={};stable=m.replay(rows,sourcecp['stableSince'])
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];at=m.ts(row['received'])
        if kind=='controllerrevisions' and m.owned(o,owner):assert uid not in history or history[uid]==o['data'];history[uid]=o['data']
        if at<m.ts(sourcecp['stableSince']):continue
        if row['event']=='DELETED':stable[kind].pop(uid,None)
        else:stable[kind][uid]=o
        live=m.mine(stable,'pods',owner);assert set(live)==set(pods) and all(m.ready(o) and m.version(o)=='B' for o in live.values());h.plugins(stable,owner,spec)
    probes=[m.read(x) for x in sorted(p.glob('completion-status-*.json'))];assert probes
    def completed(o):
        s=o.get('status',{});return s.get('currentRevision') and s['currentRevision']==s.get('updateRevision') and s.get('observedGeneration',0)>=4 and s.get('replicas')==s.get('availableReplicas')==s.get('updatedReplicas')==1 and not any(c['type'] in ('UpdateInProgress','CoordinatedRoleRolloutBlocked') and c['status']=='True' for c in s.get('conditions',[]))
    final=m.yaml(p/'final-resources.yaml');finalms=next(o for o in final['modelservings'].values() if o['metadata']['uid']==owner);end=m.yaml(p/'fault-controller-after.yaml')
    assert not any(t['action']=='request' and t.get('namespace')==result['namespace'] and t.get('resource')=='pods' and t.get('method') in ('POST','DELETE','PUT','PATCH') and m.ts(t['at'])>start for t in trace)
    for probe in probes:assert probe['object']['metadata']['uid']==owner and probe['object']['metadata']['generation']==4 and probe['object']['spec']==spec
    if result['status']=='FAIL':
        assert 'TIMEOUT: sparse-B12-automatic-completion-before-restart' in result.get('error','') and not result.get('violations') and not (p/'step-01-after-completion-controller-delete.json').exists()
        assert len(probes)>300 and m.ts(probes[-1]['completed'])-m.ts(probes[0]['started'])>410_000_000_000 and all(not completed(v['object']) for v in probes) and not completed(finalms)
        assert end['metadata']['uid']==controller['metadata']['uid']
        return {'id':case['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'SPARSE_READY_B_TARGETS_DID_NOT_AUTOMATICALLY_COMPLETE','watchRows':len(rows),'retainedBUIDs':kept,'oldStatusWrite':receipt,'directStatusProbes':len(probes),'persistentSince':probes[0]['started'],'persistentUntil':probes[-1]['completed'],'finalStatus':finalms['status'],'limitation':'Four exact B12 Ready UIDs and plugin resources persisted after old-current write. Normal controller did not complete within bounded observation. Final restart was not performed and receives no credit.'}
    assert result['status']=='PASS' and not result.get('violations') and completed(finalms)
    cps=[m.read(x) for x in p.glob('checkpoint*.json')];auto=next(c for c in cps if c['phase']=='sparse-B12-automatic-completion-before-restart');after=next(c for c in cps if c['phase']=='sparse-B12-equivalent-completion-after-restart');assert min(c['elapsedStableNanos'] for c in (auto,after))>=30_000_000_000
    final_restart=m.read(p/'step-01-after-completion-controller-delete.json');replacement=m.yaml(p/'step-01-after-completion-controller-replacement.yaml');assert final_restart['accepted'] and final_restart['uid']==controller['metadata']['uid']==final_restart['options']['preconditions']['uid'] and m.ts(auto['completed'])<m.ts(final_restart['sent'])<m.ts(after['stableSince']) and replacement['metadata']['uid']==end['metadata']['uid']!=controller['metadata']['uid']
    for cp in (auto,after):
        assert completed(next(o for o in m.replay(rows,cp['stableSince'])['modelservings'].values() if o['metadata']['uid']==owner))
        assert all(completed(v['object']) for v in probes if m.ts(cp['stableSince'])<=m.ts(v['started'])<=m.ts(cp['completed']))
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    return {'id':case['id'],'classification':'PASS','watchRows':len(rows),'retainedBUIDs':kept,'oldStatusWrite':receipt,'directStatusProbes':len(probes),'automaticCompleteStableNanos':auto['elapsedStableNanos'],'afterRestartStableNanos':after['elapsedStableNanos'],'limitation':'Real B12 identities and exact original budget established before old status write. Automatic complete state preceded final controller restart; same Pod UIDs and equivalent completed state held afterward.'}


def main():
    runid=sys.argv[1];base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control')
    done=m.read(control/'completion.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==m.read(control/'build.json')['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    trace=[json.loads(s) for s in open(control/'proxy-trace.jsonl')]
    out=base/(sys.argv[2] if len(sys.argv)>2 else 'independent-completion-boundary-audit');out.mkdir();reports=[]
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
