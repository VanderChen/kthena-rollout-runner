#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Separate real bounded process recovery from the later rollout outcome."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('midrollout_audit', ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader); loader.loader.exec_module(m)


def audit_case(p):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');owner=m.yaml(p/'before-server.yaml')['metadata']['uid']
    assert result['id'] in ('RUN-431','RUN-432') and result['id']==case['id']==case['scenario']['source']['id']
    spec=m.yaml(p/'step-02-grace-server.yaml')['spec'];assert spec['template']['restartGracePeriodSeconds']==30 and spec['recoveryPolicy']=='RoleRecreate'
    rows=[json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    target=m.yaml(p/'step-02-restart-target-before.yaml');restarted=m.yaml(p/'step-02-restart-target-after.yaml');ready=m.yaml(p/'step-02-grace-ready.yaml')
    call=m.read(p/'step-02-restart-exec.json');proof=m.read(p/'step-02-grace-recovery.json');uid=target['metadata']['uid']
    assert uid==restarted['metadata']['uid']==ready['metadata']['uid']==call['uid']==proof['entryUID']
    assert target['spec']['restartPolicy']=='Always' and m.version(target)=='A' and m.ready(target) and m.ready(ready)
    old,new=m.status(target),m.status(restarted)
    assert new['restartCount']==old['restartCount']+1 and new['containerID']!=old['containerID'] and 'running' in new['state']
    assert m.status(ready)['restartCount']==new['restartCount'] and m.status(ready)['containerID']==new['containerID']
    assert new['lastState']['terminated']['exitCode']==42 and m.ts(new['lastState']['terminated']['finishedAt'])>=(m.ts(call['sent'])//1_000_000_000)*1_000_000_000
    assert proof['apiAndWatchReady'] and 0<proof['elapsedNanos']<=30_000_000_000 and m.ts(proof['readyReceived'])-m.ts(call['sent'])<=30_000_000_000
    assert any(r['kind']=='pods' and r['object']['metadata']['uid']==uid and m.ts(call['sent'])<m.ts(r['received'])<=m.ts(proof['readyReceived']) and m.ready(r['object']) and m.status(r['object']).get('restartCount',0)>old['restartCount'] for r in rows)
    checkpoints=[m.read(x) for x in p.glob('checkpoint-*.json')];recovered=next(c for c in checkpoints if c['phase']=='old-entry-restarts-within-template-grace')
    assert recovered['elapsedStableNanos']>=5_000_000_000
    bwrite=next(m.read(x) for x in p.glob('step-01-write-*-receipt.json') if 'error' not in m.read(x));assert m.ts(bwrite['received'])<m.ts(call['sent'])
    before=m.yaml(p/'step-02-restart-pods-before.yaml')['items'];assert any(m.owned(o,owner) and m.version(o)=='B' and m.fault(o) for o in before)
    workers=proof['healthyWorkers'];assert len(workers)==1;worker=workers[0];wuid=worker['metadata']['uid']
    assert m.owned(worker,owner) and m.ready(worker) and m.version(worker)=='A' and worker['metadata']['labels'].get(m.E)!='true'
    assert all(worker['metadata']['labels'][k]==target['metadata']['labels'][k] for k in (m.G,m.R,m.I))
    for row in rows:
        if row['kind']!='pods' or not m.ts(call['sent'])<=m.ts(row['received'])<=m.ts(recovered['completed']):continue
        obj=row['object'];key=obj['metadata']['uid']
        if key in (uid,wuid):assert row['event']!='DELETED' and not obj['metadata'].get('deletionTimestamp')
        if key==wuid:assert m.ready(obj)
    after_recovery=m.yaml(p/'step-02-resources.yaml');assert m.ready(after_recovery['pods'][uid]) and m.ready(after_recovery['pods'][wuid])
    first=m.yaml(p/'case-controller-before.yaml');last=m.yaml(p/'fault-controller-after.yaml')
    assert first['metadata']['uid']==last['metadata']['uid'] and first['status']['containerStatuses'][0]['restartCount']==last['status']['containerStatuses'][0]['restartCount']
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    report={'id':result['id'],'rawStatus':result['status'],'rawError':result.get('error'),'watchRows':len(rows),'classification':'PENDING_REVIEW','graceRecoveryProof':{'entryUID':uid,'workerUID':wuid,'containerIDBefore':old['containerID'],'containerIDAfter':new['containerID'],'restartCountBefore':old['restartCount'],'restartCountAfter':new['restartCount'],'exitCode':42,'elapsedNanos':proof['elapsedNanos'],'acceptedTemplateGraceSeconds':30,'BAlreadyAccepted':True,'workerStayedReadyAndUnreplaced':True},'sameControllerProcess':True}
    final=m.yaml(p/'final-resources.yaml');pods=m.mine(final,'pods',owner)
    if result['status']=='PASS':
        assert result['id']=='RUN-431' and len(pods)==6 and all(m.ready(o) and m.version(o)=='B' for o in pods.values())
        grouped=collections.defaultdict(list)
        for o in pods.values():grouped[o['metadata']['labels'][m.G]].append(o)
        assert len(grouped)==3 and all(len(v)==2 and sum(o['metadata']['labels'].get(m.E)=='true' for o in v)==1 for v in grouped.values())
        finalcp=next(c for c in checkpoints if c['phase']=='finish-B-after-bounded-restart');assert finalcp['elapsedStableNanos']>=30_000_000_000
        report.update(classification='PASS_INDEPENDENT_GRACE_AND_FINAL_CHECK',finalStableNanos=finalcp['elapsedStableNanos'],limitation='Independent true restart, bounded Ready recovery and worker/UID proof supplement ordinary rollout, plugin, history and stable final checks.')
    else:
        assert result['status']=='FAIL' and result['id']=='RUN-432' and 'TIMEOUT' in result['error'] and 'unexpected ConfigMap' in result['error']
        name=re.search(r'unexpected ConfigMap ([a-z0-9-]+)',result['error']).group(1);cm=next(o for o in final['configmaps'].values() if o['metadata']['name']==name)
        assert m.owned(cm,owner)
        def matches(o):return m.owned(o,owner) and all(o['metadata']['labels'].get(k)==cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I))
        assert not any(matches(o) for o in pods.values())
        deleted=[r for r in rows if r['kind']=='pods' and r['event']=='DELETED' and matches(r['object'])];assert len(deleted)>=2
        added=next(r for r in rows if r['kind']=='configmaps' and r['event']=='ADDED' and r['object']['metadata']['uid']==cm['metadata']['uid'])
        assert len(pods)==9 and all(m.ready(o) for o in pods.values()) and all(m.version(o)==('B' if o['metadata']['labels'][m.R]=='frontend' else 'A') for o in pods.values())
        report.update(classification='KTHENA_BEHAVIOR_FAILURE',subtype='RANKTABLE_NOT_CLEANED_WITHIN_POST_RESTART_ROLLOUT_TIMEOUT',orphanName=name,orphanUID=cm['metadata']['uid'],orphanCreated=added['received'],lastRolePodDeleted=max(deleted,key=lambda r:r['sequence'])['received'],orphanCreatedAfterLastRolePodDeletion=added['sequence']>max(r['sequence'] for r in deleted),rolloutTimeoutSeconds=420,limitation='Same-UID recovery passed in about3.1s; later B rollout left a ranktable without members through the420s timeout. This proves bounded cleanup failure, not indefinite lifetime or a unique internal cause.')
    report['evidenceSHA256']={n:hashlib.sha256((p/n).read_bytes()).hexdigest() for n in ['result.json','observations.jsonl','step-02-grace-recovery.json','final-resources.yaml']}
    return report


def main():
    runid=sys.argv[1];assert runid and '/' not in runid and '..' not in runid
    base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control')
    done=m.read(control/'completion.json');build=m.read(control/'build.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==build['binarySHA256']
    results=m.read(base/'summary.json')['results'];assert [r['id'] for r in results]==['RUN-431','RUN-432']
    out=base/'independent-grace-audit';out.mkdir();reports=[]
    for result in results:
        report=audit_case(base/result['id']);reports.append(report)
        with (out/(result['id']+'.json')).open('x') as f:json.dump(report,f,indent=2);f.write('\n')
        print(report['id'],report['classification'],report['graceRecoveryProof']['elapsedNanos'])
    with (out/'summary.json').open('x') as f:json.dump({'status':'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},f,indent=2);f.write('\n')


if __name__=='__main__':main()
