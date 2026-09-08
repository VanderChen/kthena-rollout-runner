#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independent RUN-304 and true container-restart evidence checks."""
import collections
import datetime
import hashlib
import json
import pathlib
import re
import subprocess
import sys

ROOT=pathlib.Path(__file__).resolve().parents[1]
runid=sys.argv[1]; assert runid and '/' not in runid and '..' not in runid
base=ROOT/'artifacts'/runid; control=ROOT/'artifacts/environment-022'/(runid+'-control')
parser=sys.argv[2] if len(sys.argv)>2 else '/private/tmp/runner022-yaml-json'
out=base/'independent-extra-audit'; out.mkdir()
G='modelserving.volcano.sh/group-name'; R='modelserving.volcano.sh/role'; I='modelserving.volcano.sh/role-id'; E='modelserving.volcano.sh/entry'
def read(p): return json.loads(p.read_text())
def yaml(p): return json.loads(subprocess.check_output([parser,str(p)]))[p.name]
def owned(p,uid): return any(o['uid']==uid for o in p['metadata'].get('ownerReferences',[]))
def ready(p): return not p['metadata'].get('deletionTimestamp') and any(c['type']=='Ready' and c['status']=='True' for c in p.get('status',{}).get('conditions',[]))
def version(p): return next(e['value'] for c in p['spec']['containers'] for e in c.get('env',[]) if e['name']=='ROLLOUT_VERSION')
def status(p): return next(c for c in p['status']['containerStatuses'] if c['name']=='workload')
def pods(state,owner): return {uid:p for uid,p in state['pods'].items() if owned(p,owner)}
summary=read(base/'summary.json'); done=read(control/'completion.json');build=read(control/'build.json')
assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
assert read(base/'environment.json')['runner']['binarySHA256']==build['binarySHA256']
assert [r['id'] for r in summary['results']]==['RUN-304']+[f'RUN-{n}' for n in range(389,401)]
reports=[]
for result in summary['results']:
    ident=result['id'];p=base/ident;c=yaml(p/'case.yaml');config=c['scenario']['source']['config'];owner=yaml(p/'before-server.yaml')['metadata']['uid']
    rows=[json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1))
    gaps=[r for r in rows if r['event']=='GAP']; firstGap=min((r['sequence'] for r in gaps),default=len(rows)+1)
    valid=[r for r in rows if r['sequence']<firstGap]
    final=yaml(p/'final-resources.yaml');baseline=yaml(p/'baseline-resources.yaml')
    state=read(p/'fault-proxy-final.json');assert not state['errors'] and not any(r['active'] for r in state.get('rules') or [])
    beforeController=yaml(p/'step-01-controller-before.yaml');afterController=yaml(p/'fault-controller-after.yaml')
    assert beforeController['metadata']['uid']==afterController['metadata']['uid'] and beforeController['status']['containerStatuses'][0]['restartCount']==afterController['status']['containerStatuses'][0]['restartCount']
    report={'id':ident,'rawStatus':result['status'],'rawError':result.get('error'),'watchRows':len(rows),'observationGaps':gaps,'classification':'PENDING_REVIEW','sameControllerProcess':True}
    if ident=='RUN-304':
        scope=read(p/'step-01-fault-scope.json');delete=read(p/'step-01-fault-delete.json')
        assert scope['recovery']=='RoleRecreate' and len(scope['recoveryUIDs'])==2 and delete['accepted'] and delete['options']['preconditions']['uid']==scope['targetUID']
        receipt=read(p/'step-01-submit-B-write-01-receipt.json');assert 'error' not in receipt and receipt['received']<delete['sent']
        held=read(p/'step-01-before-clear.json'); selected=[r for r in held['rules'] if r['id'].startswith(runid+'-run-304-01-')]
        assert len(selected)==9 and all(r['active'] for r in selected) and not held['errors']
        accepted=yaml(p/'step-01-B-server.yaml')['spec'];frontend=next(r for r in accepted['template']['roles'] if r['name']=='frontend')
        assert frontend['replicas']==2 and frontend['workerReplicas']==2
        podState={};destruction=None
        for row in valid:
            if row['kind']!='pods' or not owned(row['object'],owner):continue
            obj=row['object'];uid=obj['metadata']['uid']
            if uid not in scope['recoveryUIDs'] and (row['event']=='DELETED' or obj['metadata'].get('deletionTimestamp')) and destruction is None:
                grouped=collections.defaultdict(list)
                for o in podState.values():grouped[o['metadata']['labels'][G]].append(o)
                # Overestimate availability: each old, complete Ready R1/W1
                # group still counts even though accepted R grew to two.
                available=[]
                for group,items in grouped.items():
                    healthy=[o for o in items if ready(o)]
                    if any(o['metadata']['labels'][R]=='backend' for o in healthy) and any(o['metadata']['labels'][R]=='frontend' and o['metadata']['labels'].get(E)=='true' for o in healthy) and any(o['metadata']['labels'][R]=='frontend' and o['metadata']['labels'].get(E)!='true' for o in healthy):available.append(group)
                group=obj['metadata']['labels'][G]
                assert group in available and len(available)-1<3 and scope['targetUID'] not in podState
                destruction={'sequence':row['sequence'],'received':row['received'],'uid':uid,'name':obj['metadata']['name'],'optimisticReadyGroups':available,'remainingAfter':len(available)-1,'minimum':3,'group2StillHealthy':'model-2' in available,'group2NoDestructionYet':not any(r['kind']=='pods' and r['sequence']<row['sequence'] and r.get('object',{}).get('metadata',{}).get('labels',{}).get(G)=='model-2' and (r['event']=='DELETED' or r['object']['metadata'].get('deletionTimestamp')) for r in valid)}
            if row['event']=='DELETED':podState.pop(uid,None)
            else:podState[uid]=obj
        assert result['status']=='FAIL' and destruction is not None
        report.update(classification='KTHENA_BEHAVIOR_FAILURE',subtype='HEALTHY_INSTANCE_LOSS_EXCEEDS_BUDGET_DURING_R_W_RECOVERY',capacityProof=destruction,limitation='Even generously counting complete old R1/W1 groups, available groups fall 2 to 1 below U=0 minimum3. Later cancellation GAP is preserved. Failure prevents a claim of final protected A/W1/R2 convergence.')
    else:
        target=yaml(p/'step-01-restart-target-before.yaml');restarted=yaml(p/'step-01-restart-target-after.yaml');call=read(p/'step-01-restart-exec.json')
        uid=target['metadata']['uid'];before=status(target);after=status(restarted)
        assert restarted['metadata']['uid']==uid==call['uid'] and target['spec']['restartPolicy']=='Always'
        assert after['restartCount']>before['restartCount'] and after['containerID']!=before['containerID'] and 'running' in after['state']
        last=after['lastState']['terminated'];assert last['exitCode']==42 and last['finishedAt'][:19]>=call['sent'][:19]
        restored=yaml(p/'step-01-resources.yaml');initialPods=pods(baseline,owner);restoredPods=pods(restored,owner)
        assert set(initialPods)==set(restoredPods) and all(ready(o) and version(o)=='A' for o in restoredPods.values())
        assert set(baseline['controllerrevisions'])==set(restored['controllerrevisions'])
        initialMS=next(o for o in baseline['modelservings'].values() if o['metadata']['uid']==owner)
        restoredMS=next(o for o in restored['modelservings'].values() if o['metadata']['uid']==owner)
        assert initialMS['spec']==restoredMS['spec'] and initialMS['metadata']['generation']==restoredMS['metadata']['generation']
        checkpoint=next(read(x) for x in p.glob('checkpoint-*.json') if read(x)['phase']=='exit-entry-process-and-recover-same-pod')
        assert checkpoint['elapsedStableNanos']>=30_000_000_000
        writes=[read(x) for x in p.glob('step-02-write-*-receipt.json') if 'error' not in read(x)]
        assert len(writes)==1 and checkpoint['completed']<writes[0]['sent']
        assert not any(row['received']<writes[0]['sent'] and row['event']=='GAP' for row in rows)
        assert not any(row['kind']=='pods' and row['received']<writes[0]['sent'] and owned(row['object'],owner) and (row['event']=='DELETED' or row['object']['metadata'].get('deletionTimestamp')) for row in valid)
        assert any(row['kind']=='pods' and row['object']['metadata']['uid']==uid and row['received']<writes[0]['sent'] and status(row['object']).get('restartCount',0)>before['restartCount'] for row in valid if row['kind']=='pods' and row['object'].get('status',{}).get('containerStatuses'))
        report['containerRestartProof']={'podUID':uid,'containerIDBefore':before['containerID'],'containerIDAfter':after['containerID'],'restartCountBefore':before['restartCount'],'restartCountAfter':after['restartCount'],'exitCode':42,'allPodUIDsPreservedBeforeB':len(initialPods),'allAReadyBeforeB':True,'noRevisionOrSpecChangeBeforeB':True,'readyStableNanosBeforeB':checkpoint['elapsedStableNanos']}
        if result['status']=='PASS':
            assert not gaps
            finalPods=pods(final,owner);assert len(finalPods)==(6 if config['mode']=='SG' else 9) and all(ready(o) for o in finalPods.values())
            partition=config['top']['p'] if config['mode']=='SG' else config['roles']['f']['p'];units=collections.defaultdict(list)
            for obj in finalPods.values():
                lab=obj['metadata']['labels']
                if lab[R]=='frontend':units[lab[G] if config['mode']=='SG' else lab[I]].append(obj)
                else:assert version(obj)=='A' and obj['metadata']['uid'] in initialPods
            assert len(units)==3
            for key,items in units.items():
                assert len(items)==2 and sum(o['metadata']['labels'].get(E)=='true' for o in items)==1
                n=int(key.rsplit('-',1)[1]);assert all(version(o)==('A' if n<partition else 'B') for o in items)
                if n<partition:assert all(o['metadata']['uid'] in initialPods for o in items)
            assert any(read(x)['phase']=='rollout-B-after-container-recovered' and read(x)['elapsedStableNanos']>=30_000_000_000 for x in p.glob('checkpoint-*.json'))
            report.update(classification='PASS_INDEPENDENT_RESTART_AND_FINAL_POD_CHECK',finalReadyPods=len(finalPods),limitation='Independent restart/UID/version proof supplements the runner budget, plugin, history, PodGroup and stability checks.')
        elif any(reason in result.get('error','') for reason in ('STABILITY_VIOLATION','TIMEOUT')) and 'unexpected ConfigMap' in result['error']:
            name=re.search(r'unexpected ConfigMap ([a-z0-9-]+)',result['error']).group(1)
            cm=next(o for o in final['configmaps'].values() if o['metadata']['name']==name);lab=cm['metadata']['labels']
            def same_role(o): return owned(o,owner) and all(o['metadata']['labels'].get(k)==lab[k] for k in (G,R,I))
            assert owned(cm,owner) and not any(same_role(o) for o in final['pods'].values())
            deleted=[r for r in valid if r['kind']=='pods' and r['event']=='DELETED' and same_role(r['object'])]
            added=next(r for r in valid if r['kind']=='configmaps' and r['event']=='ADDED' and r['object']['metadata']['uid']==cm['metadata']['uid'])
            assert deleted
            if 'STABILITY_VIOLATION' in result['error']: assert max(r['sequence'] for r in deleted)<added['sequence']
            report.update(classification='KTHENA_BEHAVIOR_FAILURE',subtype='RANKTABLE_NOT_CLEANED_WITHIN_TIMEOUT' if 'TIMEOUT' in result['error'] else 'RANKTABLE_RECREATED_AFTER_ROLE_MEMBERS_GONE',orphanName=name,orphanUID=cm['metadata']['uid'],lastRolePodDeleted=max(r['received'] for r in deleted),orphanCreated=added['received'],limitation='The actual in-place restart succeeded; the later normal B rollout failed its required cleanup/stability or bounded timeout check. This does not prove indefinite orphan lifetime.')
    report['evidenceSHA256']={name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml']}
    with (out/(ident+'.json')).open('x') as f:json.dump(report,f,indent=2);f.write('\n')
    reports.append(report);print(ident,report['classification'])
with (out/'summary.json').open('x') as f:json.dump({'status':'PENDING_REVIEW' if any(r['classification']=='PENDING_REVIEW' for r in reports) else 'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},f,indent=2);f.write('\n')
