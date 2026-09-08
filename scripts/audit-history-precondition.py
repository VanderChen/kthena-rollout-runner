#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Record actual cleanup regressions before a historical-fault source exists."""
import collections
import hashlib
import importlib.util
import json
import pathlib

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('precondition_mid',ROOT/'scripts/audit-midrollout.py')
m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)


def audit(p,trace):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');owner=m.yaml(p/'before-server.yaml')['metadata']['uid']
    assert result['id'] in ('RUN-528','RUN-529','RUN-530','RUN-531','RUN-532') and result['status']=='FAIL'
    assert result['error'].startswith('step 01 establish-protected-A-and-eligible-B: STABILITY_VIOLATION: settled predicate regressed: unexpected ConfigMap ')
    assert case['scenario']['source']['initial'].startswith('已到P=1的A/B停点') and not list(p.glob('step-02*'))
    assert not any(t['action']=='rule-installed' and t.get('namespace')==result['namespace'] for t in trace)
    rows=[json.loads(s) for s in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    cmname=result['error'].rsplit(' ',1)[1]
    candidates=[r for r in rows if r['kind']=='configmaps' and r['event']=='ADDED' and r['object']['metadata']['name']==cmname]
    assert len(candidates)>=2
    birth=candidates[-1];cm=birth['object'];assert m.owned(cm,owner)
    labels=cm['metadata']['labels'];keys=(m.G,m.R,m.I)
    def matching(o):return m.owned(o,owner) and all(o['metadata']['labels'].get(k)==labels.get(k) for k in keys)
    before=m.replay(rows,birth['received']);assert not any(matching(o) for o in m.mine(before,'pods',owner).values())
    old=[r for r in rows if r['kind']=='pods' and r['event']=='DELETED' and matching(r['object']) and r['sequence']<birth['sequence']];assert len(old)==2
    assert all(m.ts(r['received'])<m.ts(birth['received']) for r in old)
    table=json.loads(cm['data']['ranktable.json']);assert table['status']=='Initializing' and int(table['server_count'])==0
    previous=next(r for r in rows if r['kind']=='configmaps' and r['event']=='DELETED' and r['object']['metadata']['name']==cmname and r['sequence']<birth['sequence'])
    assert previous['object']['metadata']['uid']!=cm['metadata']['uid']
    final=m.yaml(p/'final-resources.yaml');assert cm['metadata']['uid'] in m.mine(final,'configmaps',owner) and not any(matching(o) for o in m.mine(final,'pods',owner).values())
    pods=m.mine(final,'pods',owner);assert len(pods)==9 and all(m.ready(o) for o in pods.values())
    front=[o for o in pods.values() if o['metadata']['labels'][m.R]=='frontend'];assert collections.Counter(m.version(o) for o in front)=={'A':2,'B':4}
    assert sum(o['metadata']['labels'][m.R]=='backend' and m.version(o)=='A' for o in pods.values())==3
    return {'id':result['id'],'classification':'KTHENA_PRECONDITION_CLEANUP_FAILURE','catalogueFaultCoverageCredit':0,'rawStatus':result['status'],'watchRows':len(rows),'lastOldPodDeletedAt':max(old,key=lambda r:m.ts(r['received']))['received'],'newOrphanAt':birth['received'],'newOrphanUID':cm['metadata']['uid'],'orphanName':cmname,'oldPodUIDs':[r['object']['metadata']['uid'] for r in old],'sourceFaultActionExecuted':False,'limitation':'Actual resource cleanup regression during A/B fixture preparation. A complete stable source was not established and no historical fault or protected-entry deletion was executed. Record the Kthena behavior independently; this does not verify the catalogue fault action or prove permanent leakage.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ('result.json','observations.jsonl','final-resources.yaml')}}
