#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Corroborate non-budget recovery failures without changing raw results."""
import collections
import copy
import hashlib
import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
runid = sys.argv[1]
assert runid and '/' not in runid and '..' not in runid
base = ROOT/'artifacts'/runid
core = json.loads((base/'independent-recovery-audit/summary.json').read_text())
out = base/'other-recovery-audit'; out.mkdir()
parser = sys.argv[2] if len(sys.argv)>2 else '/private/tmp/runner022-yaml-json'
G='modelserving.volcano.sh/group-name'; R='modelserving.volcano.sh/role'; I='modelserving.volcano.sh/role-id'


def yaml(path): return json.loads(subprocess.check_output([parser,str(path)]))[path.name]
def owned(obj,uid): return any(o['uid']==uid for o in obj['metadata'].get('ownerReferences',[]))
def version(obj): return next(e['value'] for c in obj['spec']['containers'] for e in c.get('env',[]) if e['name']=='ROLLOUT_VERSION')
def normalize_pod_spec(obj):
    spec=copy.deepcopy(obj['spec'])
    names={v['name'] for v in spec.get('volumes',[]) if any('serviceAccountToken' in source for source in v.get('projected',{}).get('sources',[]))}
    for v in spec.get('volumes',[]):
        if v['name'] in names: v['name']='ADMISSION_SERVICEACCOUNT_VOLUME'
    for c in spec.get('containers',[]):
        for mount in c.get('volumeMounts',[]):
            if mount['name'] in names: mount['name']='ADMISSION_SERVICEACCOUNT_VOLUME'
    return spec


reports=[]
for item in core['cases']:
    if item['classification']!='PENDING_REVIEW': continue
    ident=item['id']; p=base/ident; owner=item['scope']['ownerUID']
    rows=[json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1))
    report={'id':ident,'rawStatus':item['rawStatus'],'rawError':item['rawError'],'classification':'PENDING_REVIEW','watchRows':len(rows),'capacityAudit':'independent-recovery-audit/'+ident+'.json'}
    if ident=='RUN-345' and 'RECOVERY_DUPLICATE_CREATION' in item['rawError']:
        name=item['scope']['targetName']; old=item['scope']['targetUID']
        added=[r for r in rows if r['kind']=='pods' and r['event']=='ADDED' and r['object']['metadata']['name']==name and r['object']['metadata']['uid']!=old]
        assert len(added)==2 and all(owned(r['object'],owner) and version(r['object'])=='B' for r in added)
        first,second=added; firstUID=first['object']['metadata']['uid']; secondUID=second['object']['metadata']['uid']
        assert firstUID!=secondUID
        deleted=[r for r in rows if r['kind']=='pods' and r['event']=='DELETED' and r['object']['metadata']['uid']==firstUID]
        assert len(deleted)==1 and first['sequence']<deleted[0]['sequence']<second['sequence']
        revision=first['object']['metadata']['labels']['modelserving.volcano.sh/revision']
        assert revision==second['object']['metadata']['labels']['modelserving.volcano.sh/revision']
        assert normalize_pod_spec(first['object'])==normalize_pod_spec(second['object'])
        receipts=[json.loads(x.read_text()) for x in p.glob('*-submit-B-write-*-receipt.json')]
        assert sum('error' not in r for r in receipts)==1
        report.update(classification='KTHENA_BEHAVIOR_FAILURE',subtype='RECOVERY_ROLLOUT_TARGET_RECREATED_TWICE',name=name,firstReplacementUID=firstUID,secondReplacementUID=secondUID,revision=revision,firstAdded=first['received'],firstDeleted=deleted[0]['received'],secondAdded=second['received'],normalizedSpecsEqual=True,limitation='Sequential creation/deletion/recreation of the same accepted B target, violating the source recovery/rollout deduplication requirement. This is not a claim of simultaneous duplicate Pods or a capacity violation; final convergence was not reached after the latched failure.')
    elif 'STABILITY_VIOLATION' in item['rawError'] and 'unexpected ConfigMap' in item['rawError']:
        name=re.search(r'unexpected ConfigMap ([a-z0-9-]+)',item['rawError']).group(1)
        final=yaml(p/'final-resources.yaml')
        maps=[o for o in final['configmaps'].values() if o['metadata']['name']==name]
        assert len(maps)==1; cm=maps[0]; labels=cm['metadata']['labels']; uid=cm['metadata']['uid']
        assert owned(cm,owner) and not cm['metadata'].get('deletionTimestamp') and labels['ranktable-level']=='role'
        def same_role(o): return all(o['metadata']['labels'].get(k)==labels[k] for k in (G,R,I)) and owned(o,owner)
        assert not any(same_role(o) for o in final['pods'].values())
        gone=[r for r in rows if r['kind']=='pods' and r['event']=='DELETED' and same_role(r['object'])]
        created=[r for r in rows if r['kind']=='configmaps' and r['event']=='ADDED' and r['object']['metadata']['uid']==uid]
        assert gone and len(created)==1 and max(r['sequence'] for r in gone)<created[0]['sequence']
        oldRemoved=[r for r in rows if r['kind']=='configmaps' and r['event']=='DELETED' and r['object']['metadata']['name']==name and r['object']['metadata']['uid']!=uid]
        assert oldRemoved and max(r['sequence'] for r in oldRemoved)<created[0]['sequence']
        data=json.loads(cm['data']['ranktable.json']); assert data['status']=='Initializing' and str(data['server_count'])=='0'
        report.update(classification='KTHENA_BEHAVIOR_FAILURE',subtype='RANKTABLE_RECREATED_AFTER_ROLE_MEMBERS_GONE',name=name,orphanUID=uid,roleLabels={k:labels[k] for k in (G,R,I)},lastPodDeletion=max(r['received'] for r in gone),orphanCreated=created[0]['received'],oldConfigMapUIDsDeleted=[r['object']['metadata']['uid'] for r in oldRemoved],finalRolePodCount=0,finalRanktable=data,limitation='A new orphan ranktable appeared in the required stable window after old role members and earlier ranktables were deleted. The raw stability regression is preserved; this does not establish how long the orphan would persist beyond the stopped case.')
    report['evidenceSHA256']={name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml']}
    with (out/(ident+'.json')).open('x') as f: json.dump(report,f,indent=2);f.write('\n')
    reports.append(report);print(ident,report['classification'],report.get('subtype',''))
with (out/'summary.json').open('x') as f: json.dump({'status':'PENDING_REVIEW' if any(r['classification']=='PENDING_REVIEW' for r in reports) else 'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},f,indent=2);f.write('\n')
