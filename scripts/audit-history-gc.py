#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Prove RUN-535 failed the cleanup reference List, retained A/B, and really retried."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('mid_audit',ROOT/'scripts/audit-midrollout.py')
m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)
REV='modelserving.volcano.sh/revision'


def main():
    runid=sys.argv[1];assert runid and '/' not in runid and '..' not in runid
    base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control');p=base/'RUN-535'
    done=m.read(control/'completion.json');build=m.read(control/'build.json');result=m.read(p/'result.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==build['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    assert result['status']=='PASS' and result['id']=='RUN-535' and m.read(base/'summary.json')['selected']==1
    case=m.yaml(p/'case.yaml');raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
    assert case['scenario']['source']==next(r for r in json.loads(raw)['cases'] if r['id']=='RUN-535')
    original=m.yaml(p/'before-server.yaml');owner=original['metadata']['uid'];assert original['spec']['revisionHistoryLimit']==0 and m.yaml(p/'before.yaml')['spec']['revisionHistoryLimit']==0
    bserver=m.yaml(p/'step-01-server.yaml');assert bserver['spec']['revisionHistoryLimit']==0 and bserver['metadata']['generation']==2
    initial=m.yaml(p/'step-02-live-A-B-before-fault-resources.yaml');pods=m.mine(initial,'pods',owner);histories=m.mine(initial,'controllerrevisions',owner)
    assert len(pods)==6 and len(histories)==2 and all(m.ready(o) for o in pods.values())
    assert collections.Counter(m.version(o) for o in pods.values())=={'A':3,'B':3}
    assert all(m.version(o)==('B' if o['metadata']['labels'][m.R]=='frontend' else 'A') for o in pods.values())
    refs={'model-'+o['metadata']['labels'][REV] for o in pods.values()};assert refs=={o['metadata']['name'] for o in histories.values()}
    installed=m.read(p/'step-02-gc-installed.json');rid=installed['id']
    assert installed['namespace']==result['namespace'] and installed['resource']=='pods' and installed['methods']==['GET']
    assert installed['collectionOnly'] and installed['listLimit']==0 and installed['labelSelector']=='modelserving.volcano.sh/name=model' and installed['count']==1 and installed['statusCode']==503
    hitstate=m.read(p/'step-02-gc-hit.json');rule=next(r for r in hitstate['rules'] if r['id']==rid)
    assert rule['hits']==1 and not rule['active'] and rule['endReason']=='count-exhausted' and not hitstate['errors']
    cp=next(m.read(f) for f in p.glob('checkpoint-*.json') if m.read(f)['phase']==case['scenario']['steps'][1]['name']);assert cp['elapsedStableNanos']>=30_000_000_000
    trace=[]
    for line in (control/'proxy-trace.jsonl').open():
        r=json.loads(line)
        if m.ts(installed['installed'])<=m.ts(r['at'])<=m.ts(cp['completed']):trace.append(r)
    hits=[r for r in trace if r.get('ruleID')==rid and r['action']=='error-request'];assert len(hits)==1
    hit=hits[0];assert hit['listLimit']==0 and hit['labelSelector']==installed['labelSelector'] and hit['status']==503 and hit['namespace']==result['namespace']
    responses={r['request']:r for r in trace if r['action']=='response'}
    requests=[r for r in trace if r['action']=='request' and r.get('namespace')==result['namespace']]
    retries=[r for r in requests if r['sequence']>hit['sequence'] and r.get('resource')=='pods' and r['method']=='GET' and r.get('listLimit')==0 and r.get('labelSelector')==installed['labelSelector'] and responses.get(r['request'],{}).get('status')==200]
    assert retries,'actual same unpaged reference List must recover before final checkpoint'
    paged=[r for r in requests if r['sequence']<hit['sequence'] and r.get('resource')=='pods' and r.get('listLimit')==500 and r.get('labelSelector')==installed['labelSelector'] and responses.get(r['request'],{}).get('status')==200]
    assert paged,'ordinary live observation must pass before cleanup fault'
    crlists=[r for r in requests if r['sequence']<hit['sequence'] and r.get('resource')=='controllerrevisions' and r['method']=='GET' and not r.get('name') and responses.get(r['request'],{}).get('status')==200]
    assert crlists
    lines=[s for s in (p/'step-02-gc-controller.log').read_text().splitlines() if result['namespace'] in s and 'list Pods before cleaning ControllerRevisions' in s];assert lines
    rows=[json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    def preserved(state):
        actual=m.mine(state,'pods',owner);crs=m.mine(state,'controllerrevisions',owner)
        assert set(actual)==set(pods) and all(m.ready(o) and m.version(o)==m.version(pods[u]) for u,o in actual.items())
        assert set(crs)==set(histories) and all(o['data']==histories[u]['data'] and not o['metadata'].get('deletionTimestamp') for u,o in crs.items())
    state=m.replay(rows,installed['installed']);preserved(state)
    for row in rows:
        if m.ts(row['received'])<=m.ts(installed['installed']):continue
        obj=row['object'];uid=obj['metadata']['uid'];kind=row['kind']
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=obj
        preserved(state)
    final=m.yaml(p/'final-resources.yaml');preserved(final)
    probes=[m.read(f) for f in p.glob('history-probe-*.json')];assert len(probes)>=30 and all('error' not in v for v in probes)
    for v in probes:
        assert {o['metadata']['uid'] for o in v['pods']['items'] if m.owned(o,owner)}==set(pods)
        assert set(v['histories'])==refs and {cr['metadata']['uid'] for cr in v['histories'].values()}==set(histories)
    before=m.yaml(p/'case-controller-before.yaml');after=m.yaml(p/'fault-controller-after.yaml');assert before['metadata']['uid']==after['metadata']['uid'] and before['status']['containerStatuses'][0]['restartCount']==after['status']['containerStatuses'][0]['restartCount']==0
    assert not m.mine(final,'services',owner) and len(m.mine(final,'podgroups',owner))==1 and len(m.mine(final,'configmaps',owner))==6
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    report={'id':'RUN-535','classification':'PASS','watchRows':len(rows),'exactCleanupListError':hit,'nextActualSuccessfulReferenceList':retries[0],'pagedObservationBeforeError':paged[-1],'cleanupDiagnostic':lines[0],'historyUIDs':sorted(histories),'livePodUIDs':sorted(pods),'historyProbes':len(probes),'finalStableNanos':cp['elapsedStableNanos'],'controllerUID':after['metadata']['uid'],'limitation':'The source fixture has exactly live A/B histories and no deliberately injected unused-history candidate. This case proves failed reference reads do not delete either live history and real subsequent cleanup reads recover; it does not claim a successful deletion of a separate unused revision.','evidenceSHA256':{n:hashlib.sha256((p/n).read_bytes()).hexdigest() for n in ['result.json','observations.jsonl','final-resources.yaml','step-02-gc-hit.json','step-02-gc-controller.log']}}
    out=base/'independent-history-gc-audit';out.mkdir()
    with (out/'RUN-535.json').open('x') as f:json.dump(report,f,indent=2);f.write('\n')
    with (out/'summary.json').open('x') as f:json.dump({'status':'VERIFIED','counts':{'PASS':1},'cases':[report]},f,indent=2);f.write('\n')
    print('RUN-535 PASS',len(rows),'Watch rows;',len(probes),'live reference probes; actual cleanup503 then200.')


if __name__=='__main__':main()
