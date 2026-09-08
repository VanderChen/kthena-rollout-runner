# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independent physical-capacity and finite-UID audit for RUN-305..388.

Requires a completed, exported Kind run and its control evidence. Does not
rewrite raw results or replace the runner plugin/history/PodGroup oracle.
"""
import collections,datetime,hashlib,json,pathlib,subprocess,sys
root=pathlib.Path(__file__).resolve().parents[1]
runid=sys.argv[1]
assert runid and '/' not in runid and '..' not in runid
base=root/'artifacts'/runid
control=root/'artifacts/environment-022'/(runid+'-control')
parser=sys.argv[2] if len(sys.argv)>2 else '/private/tmp/runner022-yaml-json'
out=base/'independent-recovery-audit';out.mkdir()
G='modelserving.volcano.sh/group-name';R='modelserving.volcano.sh/role';I='modelserving.volcano.sh/role-id';E='modelserving.volcano.sh/entry'
def read(p):return json.loads(p.read_text())
def yaml(p):return json.loads(subprocess.check_output([parser,str(p)]))[p.name]
def owned(p,uid):return any(o['uid']==uid for o in p['metadata'].get('ownerReferences',[]))
def ready(p):return not p['metadata'].get('deletionTimestamp') and any(c['type']=='Ready' and c['status']=='True' for c in p.get('status',{}).get('conditions',[]))
def version(p):return next(e['value'] for c in p['spec']['containers'] for e in c.get('env',[]) if e['name']=='ROLLOUT_VERSION')
def units(pods,mode):
    grouped=collections.defaultdict(list)
    for p in pods.values():
        lab=p['metadata']['labels']
        if mode=='Role' and lab[R]!='frontend':continue
        key=lab[G] if mode=='SG' else lab[G]+'/'+lab[R]+'/'+lab[I]
        grouped[key].append(p)
    result={}
    for key,items in grouped.items():
        layout=collections.Counter((p['metadata']['labels'][R],p['metadata']['labels'].get(E)=='true') for p in items)
        expected={('frontend',True):1,('frontend',False):1}
        if mode=='SG':expected[('backend',True)]=1
        # Counting complete Ready layouts regardless of A/B gives a conservative
        # upper bound; no Kthena or runner ledger helper is used here.
        result[key]={'ready':dict(layout)==expected and all(ready(p) for p in items),'pods':[{'name':p['metadata']['name'],'uid':p['metadata']['uid'],'ready':ready(p),'version':version(p),'deletionTimestamp':p['metadata'].get('deletionTimestamp')} for p in items]}
    return result
summary=read(base/'summary.json');build=read(control/'build.json');environment=read(base/'environment.json')
assert environment['runner']['binarySHA256']==build['binarySHA256']
assert summary['selected']==len(summary['results'])
assert [r['id'] for r in summary['results']]==read(control/'start.json')['selected']
assert all(305<=int(r['id'][4:])<=388 for r in summary['results'])
assert read(control/'completion.json')['controllerSpecRestored'] and read(control/'completion.json')['historicalModelServingUIDsPreserved']==9
trace=[json.loads(l) for l in (control/'proxy-trace.jsonl').read_text().splitlines()]
assert [r['sequence'] for r in trace]==list(range(1,len(trace)+1))
reports=[]
for result in summary['results']:
    ident=result['id'];p=base/ident;c=yaml(p/'case.yaml');config=c['scenario']['source']['config'];mode=config['mode']
    scope=read(p/'step-01-fault-scope.json');owner=scope['ownerUID'];target=scope['targetUID']
    delete=read(p/'step-01-fault-delete.json');assert delete['accepted'] and delete['options']['preconditions']['uid']==target
    receipt=read(p/'step-01-submit-B-write-01-receipt.json');assert 'error' not in receipt and receipt['received']<delete['sent']
    beforeClear=read(p/'step-01-before-clear.json')
    rulePrefix=runid+'-'+ident.lower()+'-01-'
    selectedRules=[r for r in beforeClear['rules'] if r['id'].startswith(rulePrefix)]
    assert len(selectedRules)==9 and all(r['active'] for r in selectedRules) and not beforeClear['errors']
    afterState=read(p/'fault-proxy-final.json');assert not afterState['errors'] and not any(r['active'] for r in afterState['rules'])
    controllerBefore=yaml(p/'step-01-controller-before.yaml');controllerAfter=yaml(p/'fault-controller-after.yaml')
    assert controllerBefore['metadata']['uid']==controllerAfter['metadata']['uid'] and controllerBefore['status']['containerStatuses'][0]['restartCount']==controllerAfter['status']['containerStatuses'][0]['restartCount']
    rules=[r for r in trace if r.get('ruleID','').startswith(rulePrefix)]
    resume=min(r['at'] for r in rules if r['action']=='rule-cleared')
    allRows=[json.loads(l) for l in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in allRows]==list(range(1,len(allRows)+1))
    gaps=[r for r in allRows if r['event']=='GAP']
    # A proven earlier safety failure is not erased by a later shutdown gap.
    # Never infer through that gap or use a gapped stream to corroborate PASS.
    firstGap=min((r['sequence'] for r in gaps),default=len(allRows)+1)
    rows=[r for r in allRows if r['sequence']<firstGap]
    if gaps: assert result['status']=='FAIL'
    deletedTarget=[r for r in rows if r['kind']=='pods' and r['event']=='DELETED' and r['object']['metadata']['uid']==target]
    assert len(deletedTarget)==1 and deletedTarget[0]['received']<resume
    actualBefore=yaml(p/'step-01-fault-pods-before.yaml')['items']
    assert all(ready(o) for o in actualBefore if owned(o,owner))
    assert len(scope['recoveryUIDs'])==({'None':1,'RoleRecreate':2,'ServingGroupRecreate':3}[scope['recovery']])
    d=config['n'] if mode=='SG' else config['roles']['f']['r']
    budget=config['top'] if mode=='SG' else config['roles']['f'];minimum=max(d-budget['u'],0)
    pods={};violations=[]
    for row in rows:
        if row['kind']!='pods' or not owned(row['object'],owner):continue
        obj=row['object'];uid=obj['metadata']['uid'];lab=obj['metadata']['labels']
        destructive=row['event']=='DELETED' or bool(obj['metadata'].get('deletionTimestamp'))
        if destructive and uid not in scope['recoveryUIDs'] and row['received']>delete['received']:
            key=lab[G] if mode=='SG' else lab[G]+'/'+lab[R]+'/'+lab[I]
            states=units(pods,mode);available=sum(u['ready'] for u in states.values())
            if states.get(key,{}).get('ready') and available-1<minimum:
                violations.append({'observationSequence':row['sequence'],'received':row['received'],'deletionUID':uid,'deletionName':obj['metadata']['name'],'unit':key,'completeReadyBefore':available,'completeReadyAfter':available-1,'minimum':minimum,'externalFaultAlreadyDeleted':target not in pods,'unitsBefore':states})
        if row['event']=='DELETED':pods.pop(uid,None)
        else:pods[uid]=obj
    report={'id':ident,'rawStatus':result['status'],'rawError':result.get('error'),'watchRows':len(allRows),'replayedPrefixRows':len(rows),'observationGaps':gaps,'mode':mode,'scope':scope,'minimum':minimum,'budgetViolations':violations,'faultDeletedBeforeResume':True,'sameControllerProcess':True,'facilityErrors':[],'traceRulePrefix':rulePrefix}
    if result['status']=='FAIL':
        assert all(v['externalFaultAlreadyDeleted'] for v in violations)
        report.update(classification='KTHENA_BEHAVIOR_FAILURE',subtype='HEALTHY_INSTANCE_LOSS_EXCEEDS_BUDGET_AFTER_EXTERNAL_POD_LOSS',limitation='Observed external API/watch pause and resume schedule; cross-resource Watch delivery is not globally ordered. This is a live capacity semantic failure in that schedule, not a claim about an isolated unique internal code cause. Final convergence was not reached after the latched failure. Any recorded later observation gap is preserved; only the continuous prefix before its first occurrence is used for this capacity proof.')
        if not violations:
            report.update(classification='PENDING_REVIEW',subtype='RAW_FAILURE_NOT_EXPLAINED_BY_INDEPENDENT_PHYSICAL_CAPACITY_REPLAY')
    else:
        assert result['status']=='PASS' and not violations
        assert len(pods)==9 and all(ready(o) for o in pods.values())
        assert all(uid not in pods for uid in scope['recoveryUIDs'])
        currentUnits=units(pods,mode)
        assert len(currentUnits)==3 and all(u['ready'] for u in currentUnits.values())
        partition=budget['p']
        versionCounts=collections.Counter()
        for key,unit in currentUnits.items():
            entries=[o for o in unit['pods'] if '-frontend-' in o['name']]
            vs={o['version'] for o in entries};assert len(vs)==1
            v=next(iter(vs));versionCounts[v]+=1
            n=int(key.rsplit('-',1)[1])
            assert v==('A' if n<partition else 'B')
        expected={'B':3-partition}
        if partition:expected['A']=partition
        assert dict(versionCounts)==expected
        if not budget['s']:assert {int(key.rsplit('-',1)[1]) for key in currentUnits}=={0,1,2}
        assert all(version(o)=='A' for o in pods.values() if o['metadata']['labels'][R]=='backend')
        protected=[]
        for o in actualBefore:
            lab=o['metadata']['labels'];uid=o['metadata']['uid']
            n=int(lab[G].rsplit('-',1)[1]) if mode=='SG' else int(lab[I].rsplit('-',1)[1])
            if (n<partition or (mode=='Role' and lab[R]=='backend')) and uid not in scope['recoveryUIDs']:
                protected.append(uid);assert uid in pods
        replacementUIDs=[]
        if partition:
            for uid,name in scope['recoveryUIDs'].items():
                replacements={r['object']['metadata']['uid'] for r in rows if r['kind']=='pods' and r['event']=='ADDED' and r['object']['metadata']['name']==name and r['object']['metadata']['uid']!=uid}
                assert len(replacements)==1
                replacement=next(iter(replacements));assert replacement in pods and version(pods[replacement])=='A'
                replacementUIDs.append(replacement)
        checkpoints=[read(x) for x in p.glob('checkpoint-*.json')]
        assert any(x['phase'].startswith('pending-B') and x['elapsedStableNanos']>=30_000_000_000 for x in checkpoints)
        report.update(classification='PASS_INDEPENDENT_CAPACITY_AND_RECOVERY_CHECK',protectedHistoricalReplacementUIDs=replacementUIDs,retainedProtectedUIDs=protected,finalReadyPods=9,stableSecondsAtLeast=30,limitation='Runner additionally checks plugin/history/PG facts; this independent replay directly checks Pod capacity, exact fault identity and historical protected recovery.')
    report['evidenceSHA256']={name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','step-01-fault-scope.json','step-01-fault-delete.json','fault-proxy-final.json']}
    with (out/(ident+'.json')).open('x') as f:json.dump(report,f,indent=2);f.write('\n')
    reports.append(report)
    print(ident,report['classification'],'watch',len(rows),'live-capacity violations',len(violations))
with (out/'summary.json').open('x') as f:json.dump({'status':'PENDING_REVIEW' if any(r['classification']=='PENDING_REVIEW' for r in reports) else 'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports)),'sourceUnmodified':True},f,indent=2);f.write('\n')
