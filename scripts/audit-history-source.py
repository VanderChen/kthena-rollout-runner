#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independently bound the preparation before five real Role history faults."""
import collections
import importlib.util
import json
import pathlib
import sys

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('source_mid',ROOT/'scripts/audit-midrollout.py')
m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)


def verify_source(p,rows,owner):
    case=m.yaml(p/'case.yaml')
    assert case['id'] in ['RUN-'+str(n) for n in (528,529,530,531,532)]
    assert case['scenario']['steps'][0]['action']=='prepare-history-source'
    prep=p/'source-preparation';boundary=m.read(p/'source-history-boundary.json')
    assert boundary['preparationRestart'] and boundary['generation']==2
    assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1))
    assert not any(r['event']=='GAP' for r in rows)
    original={o['metadata']['uid']:o for o in m.yaml(prep/'original-A-pods.yaml')['items'] if m.owned(o,owner)}
    assert len(original)==9 and all(m.ready(o) and m.version(o)=='A' for o in original.values())
    old={o['metadata']['name']:uid for uid,o in original.items() if o['metadata']['labels'][m.R]=='frontend' and int(o['metadata']['labels'][m.I].rsplit('-',1)[1])>0}
    assert len(old)==4 and old==boundary['oldPreparationUIDs']
    retained={o['metadata']['uid']:o for o in m.yaml(prep/'retained-pods.yaml')['items'] if m.owned(o,owner)}
    assert len(retained)==9 and all(m.ready(o) for o in retained.values())
    assert not set(old.values())&set(retained)
    assert set(original)-set(old.values())==set(retained)&set(original)
    assert all(m.version(o)=='B' for uid,o in retained.items() if uid not in original)
    assert boundary['retainedUIDs']=={o['metadata']['name']:uid for uid,o in retained.items()}
    first=m.mine(m.yaml(p/'step-01-first-stop-resources.yaml'),'pods',owner)
    assert set(first)==set(retained)
    before=m.yaml(prep/'controller-controller-terminated.yaml');after=m.yaml(prep/'controller-controller-replacement.yaml')
    receipt=m.read(prep/'controller-controller-delete.json')
    assert before['metadata']['uid']==m.yaml(p/'case-controller-before.yaml')['metadata']['uid']
    assert receipt['accepted'] and receipt['uid']==before['metadata']['uid']==receipt['options']['preconditions']['uid']
    assert before['metadata']['uid']!=after['metadata']['uid']==boundary['controllerUID']
    assert before['metadata']['ownerReferences']==after['metadata']['ownerReferences']
    assert before['spec']['containers']==after['spec']['containers']
    connected=m.read(ROOT/'artifacts/environment-022'/(p.parent.name+'-control')/'controller-connected.json')
    deployment=m.yaml(prep/'controller-controller-deployment.yaml')
    assert deployment['metadata']['uid']==connected['metadata']['uid'] and deployment['spec']==connected['spec']
    assert after['status']['containerStatuses'][0]['restartCount']==0
    assert 'initial sync has been done' in (prep/'controller-new-controller.log').read_text()
    assert m.ts(receipt['received'])<m.ts(boundary['at'])
    cleanup=m.read(prep/'preparation-cleanup.json') if (prep/'preparation-cleanup.json').exists() else []
    groups={tuple(o['metadata']['labels'][k] for k in (m.G,m.R,m.I)) for uid,o in original.items() if uid in old.values()}
    cleaned=[]
    for d in cleanup:
        assert d['accepted'] and d['uid']==d['options']['preconditions']['uid']
        assert m.ts(receipt['received'])<m.ts(d['sent'])<=m.ts(d['received'])<m.ts(boundary['at'])
        cm=m.yaml(prep/('orphan-'+d['name']+'.yaml'))
        assert m.owned(cm,owner) and cm['metadata']['uid']==d['uid']
        key=tuple(cm['metadata']['labels'][k] for k in (m.G,m.R,m.I));assert key in groups
        actual=m.yaml(prep/('before-cleanup-'+d['name']+'-pods.yaml'))['items']
        observed=m.mine(m.replay(rows,d['sent']),'pods',owner).values()
        for pods in (actual,observed):
            assert not any(m.owned(o,owner) and tuple(o['metadata']['labels'][k] for k in (m.G,m.R,m.I))==key for o in pods)
            assert not any(o['metadata']['uid'] in old.values() for o in pods)
        assert any(r['kind']=='configmaps' and r['event']=='DELETED' and r['object']['metadata']['uid']==d['uid'] and r['sequence']<=boundary['lastPreparationSequence'] for r in rows)
        cleaned.append(d['uid'])
    for uid in old.values():
        assert any(r['kind']=='pods' and r['event']=='DELETED' and r['object']['metadata']['uid']==uid and m.ts(r['received'])<m.ts(receipt['sent']) for r in rows)
    cp=next(m.read(f) for f in p.glob('checkpoint-*.json') if m.read(f)['phase']=='mixed-history-source-established')
    assert cp['elapsedStableNanos']>=10_000_000_000 and m.ts(cp['completed'])<=m.ts(boundary['at'])
    assert all(m.ts(d['received'])<m.ts(cp['stableSince']) for d in cleanup)
    state=m.replay(rows,cp['stableSince'])
    def check():
        pods=m.mine(state,'pods',owner)
        assert set(pods)==set(retained) and all(m.ready(o) for o in pods.values())
        assert len(m.mine(state,'podgroups',owner))==1
        for kind,want in [('configmaps',6),('services',3)]:
            resources=m.mine(state,kind,owner);assert len(resources)==want
            for o in resources.values():
                assert any(all(pod['metadata']['labels'].get(k)==o['metadata']['labels'].get(k) for k in (m.G,m.R,m.I)) for pod in pods.values())
    check()
    for row in rows:
        if not m.ts(cp['stableSince'])<m.ts(row['received'])<=m.ts(boundary['at']):continue
        uid=row['object']['metadata']['uid']
        if row['event']=='DELETED':state[row['kind']].pop(uid,None)
        else:state[row['kind']][uid]=row['object']
        check()
    final={o['metadata']['uid']:o for o in m.yaml(prep/'boundary-pods.yaml')['items'] if m.owned(o,owner)}
    assert set(final)==set(retained) and all(m.ready(o) for o in final.values())
    assert max(r['sequence'] for r in rows if m.ts(r['received'])<=m.ts(boundary['at']))==boundary['lastPreparationSequence']
    fault=m.read(p/'step-02-external-delete.json');assert m.ts(boundary['at'])<m.ts(fault['sent'])
    return {'controllerUID':after['metadata']['uid'],'sourceStableNanos':cp['elapsedStableNanos'],'finitePreparationCleanupUIDs':cleaned,'boundary':boundary,'limitation':'Preparation-only restart and finite orphan cleanup precede the independently verified source boundary. Earlier five production cleanup findings remain valid; this is not product cleanup recovery credit.'}


def main():
    runid=sys.argv[1];base=ROOT/'artifacts'/runid;control=ROOT/'artifacts/environment-022'/(runid+'-control')
    done=m.read(control/'completion.json');assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256']==m.read(control/'build.json')['binarySHA256']
    assert m.read(control/'controller-before.json')['spec']==m.read(control/'controller-restored.json')['spec']
    trace=[json.loads(s) for s in open(control/'proxy-trace.jsonl')]
    out=base/(sys.argv[2] if len(sys.argv)>2 else 'independent-history-source-audit');out.mkdir();reports=[]
    for result in m.read(base/'summary.json')['results']:
        try:
            name='read' if result['id']=='RUN-529' else 'object'
            loader=importlib.util.spec_from_file_location('source_'+name,ROOT/('scripts/audit-history-'+name+'.py'))
            module=importlib.util.module_from_spec(loader);loader.loader.exec_module(module)
            report=module.audit_case(base/result['id'],trace)
        except (AssertionError,KeyError,StopIteration,FileNotFoundError) as e:
            import traceback
            report={'id':result['id'],'classification':'PENDING_REVIEW','rawStatus':result['status'],'rawError':result.get('error'),'auditError':repr(e),'traceback':traceback.format_exc()}
        reports.append(report);(out/(result['id']+'.json')).write_text(json.dumps(report,indent=2)+'\n');print(report['id'],report['classification'])
    pending=any(r['classification']=='PENDING_REVIEW' for r in reports)
    (out/'summary.json').write_text(json.dumps({'status':'PENDING_REVIEW' if pending else 'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},indent=2)+'\n')
    if pending:sys.exit(1)

if __name__=='__main__':main()
