#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Verify a real Lease transition and another legal controller instance."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('mid_audit',ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader);loader.loader.exec_module(m)
DIGEST = 'sha256:7c6ed6c78b37d7afaf104381351554ad75aed56c64d0aca2ec941ce652756265'


def controller(p):
    assert m.ready(p) and p['status']['containerStatuses'][0]['ready'] and p['status']['containerStatuses'][0]['imageID'] == DIGEST
    assert [a for a in p['spec']['containers'][0]['args'] if a.startswith('--leader-elect')] == ['--leader-elect=true']


def audit_case(p, env):
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml')
    assert case['id'] == result['id'] == case['scenario']['source']['id'] and case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    old = m.yaml(p/'step-02-leader-before.yaml'); standby = m.yaml(p/'step-02-standby-before.yaml')
    lease = m.yaml(p/'step-02-lease-before.yaml'); call = m.read(p/'step-02-leader-delete.json')
    controller(old);controller(standby)
    assert old['metadata']['uid'] != standby['metadata']['uid']
    assert lease['spec']['holderIdentity'].split('_',1)[0] == old['metadata']['name']
    assert call['accepted'] and call['options']['gracePeriodSeconds'] == 0
    assert call['uid'] == call['options']['preconditions']['uid'] == old['metadata']['uid'] and call['leaseUID'] == lease['metadata']['uid']
    owner = m.yaml(p/'before-server.yaml')['metadata']['uid']; mode = case['scenario']['source']['config']['mode']
    pods = m.yaml(p/'step-02-workload-pods-before.yaml')['items']
    for version in ('A','B'): assert any(m.owned(o,owner) and o['metadata']['labels'][m.R] == 'frontend' and m.version(o) == version and m.ready(o) for o in pods)
    if result['status'] != 'PASS':
        assert p.parent.name == 'leader-switch-r1' and result['id'] == 'RUN-441' and 'preexisting standby did not take over' in result['error']
        # The following case independently captured the actual winner and Lease.
        winner = m.yaml(p.parent/'RUN-442/case-controller-before.yaml'); nextlease = m.yaml(p.parent/'RUN-442/step-02-lease-before.yaml')
        controller(winner)
        assert winner['metadata']['uid'] not in (old['metadata']['uid'],standby['metadata']['uid'])
        assert nextlease['metadata']['uid'] == lease['metadata']['uid'] and nextlease['spec']['holderIdentity'].split('_',1)[0] == winner['metadata']['name']
        assert nextlease['spec']['leaseTransitions'] == lease['spec']['leaseTransitions']+1
        assert m.ts(winner['metadata']['creationTimestamp']) >= m.ts(call['sent'])//1_000_000_000*1_000_000_000
        assert winner['metadata']['ownerReferences'] == old['metadata']['ownerReferences']
        return {'id':result['id'],'classification':'RUNNER_OVERRESTRICTIVE_LEADER_ORACLE','rawStatus':result['status'],'rawError':result['error'],'actualWinnerUID':winner['metadata']['uid'],'priorStandbyUID':standby['metadata']['uid'],'reason':'Source allows another instance to take over, including a fresh replacement from the same unchanged ReplicaSet. Initial runner unnecessarily required prior standby UID and stopped before final rollout. Original raw FAIL remains; no product failure or PASS credit for this attempt.'}
    new = m.yaml(p/'step-02-leader-after.yaml'); replacement = m.yaml(p/'step-02-new-standby.yaml'); after = m.yaml(p/'fault-controller-after.yaml')
    for obj in (new,replacement,after): controller(obj)
    assert new['metadata']['uid'] != old['metadata']['uid'] and after['metadata']['uid'] == new['metadata']['uid']
    assert replacement['metadata']['uid'] not in (old['metadata']['uid'],new['metadata']['uid'])
    assert new['status']['containerStatuses'][0]['restartCount'] == after['status']['containerStatuses'][0]['restartCount']
    kind = 'preexisting-standby' if new['metadata']['uid'] == standby['metadata']['uid'] else 'new-replacement'
    if kind == 'preexisting-standby': assert new['status']['containerStatuses'][0]['restartCount'] == standby['status']['containerStatuses'][0]['restartCount']
    else:
        assert new['metadata']['ownerReferences'] == old['metadata']['ownerReferences']
        assert m.ts(new['metadata']['creationTimestamp']) >= m.ts(call['sent'])//1_000_000_000*1_000_000_000 and new['status']['containerStatuses'][0]['restartCount'] == 0
    if (p/'step-02-leader-takeover.json').exists(): assert m.read(p/'step-02-leader-takeover.json')['kind'] == kind
    newlease = m.yaml(p/'step-02-lease-after.yaml')
    assert lease['metadata']['uid'] == newlease['metadata']['uid'] and lease['spec']['leaseDurationSeconds'] == newlease['spec']['leaseDurationSeconds'] == 15
    assert newlease['spec']['holderIdentity'].split('_',1)[0] == new['metadata']['name'] and newlease['spec']['leaseTransitions'] == lease['spec']['leaseTransitions']+1
    dep = m.yaml(p/'step-02-deployment-before.yaml'); newdep = m.yaml(p/'step-02-deployment-after.yaml')
    assert dep['spec']['replicas'] == 2 and dep['spec'] == newdep['spec'] == env['controller']['spec'] and dep['metadata']['uid'] == newdep['metadata']['uid']
    logs = (p/'step-02-new-leader.log').read_text().splitlines()
    synced = next(line for line in logs if 'initial sync has been done' in line and m.ts(line.split(' ',1)[0]) > m.ts(call['sent']))
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1,len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    cp = next(m.read(x) for x in p.glob('checkpoint-*.json') if m.read(x)['phase'] == case['scenario']['steps'][-1]['name'])
    assert cp['elapsedStableNanos'] >= 30_000_000_000 and m.ts(cp['stableSince']) > m.ts(synced.split(' ',1)[0])
    baseline = m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner); state = collections.defaultdict(dict); removals = []; names = collections.defaultdict(set)
    for row in rows:
        obj = row['object']; key = obj['metadata']['uid']; resource = row['kind']; prev = state[resource].get(key)
        if resource == 'pods' and m.owned(obj,owner):
            if row['event'] == 'ADDED' and m.version(obj) == 'B': names[obj['metadata']['name']].add(key)
            if obj['metadata']['labels'][m.R] == 'frontend' and prev and m.ready(prev) and (row['event'] == 'DELETED' or obj['metadata'].get('deletionTimestamp')):
                available = sum(m.owned(o,owner) and o['metadata']['labels'][m.R] == 'frontend' and m.ready(o) for o in state[resource].values()); assert available-1 >= 2
                removals.append({'sequence':row['sequence'],'uid':key,'readyBefore':available})
        if row['event'] == 'DELETED': state[resource].pop(key,None)
        else: state[resource][key] = obj
        if m.ts(cp['stableSince']) <= m.ts(row['received']) <= m.ts(cp['completed']): m.final_pods(state,owner,mode,'B',baseline)
    assert all(len(uids) == 1 for uids in names.values())
    m.final_pods(m.replay(rows,cp['stableSince']),owner,mode,'B',baseline)
    final = m.yaml(p/'final-resources.yaml'); pods = m.final_pods(final,owner,mode,'B',baseline)
    assert not m.mine(final,'services',owner)
    assert {o['metadata']['name'] for o in m.mine(final,'podgroups',owner).values()} == {o['metadata']['labels'][m.G] for o in pods.values()}
    cms = m.mine(final,'configmaps',owner);assert len(cms) == len(pods)
    for cm in cms.values(): assert sum(all(o['metadata']['labels'].get(k) == cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I)) for o in pods.values()) == 1
    proxy = m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id':result['id'],'classification':'PASS_INDEPENDENT_LEADER_SWITCH_CHECK','watchRows':len(rows),'oldLeaderUID':old['metadata']['uid'],'newLeaderUID':new['metadata']['uid'],'takeoverKind':kind,'leaseUID':newlease['metadata']['uid'],'leaseTransitionsBefore':lease['spec']['leaseTransitions'],'leaseTransitionsAfter':newlease['spec']['leaseTransitions'],'newInitialSyncLog':synced,'healthyRemovalChecks':removals,'finalStableNanos':cp['elapsedStableNanos'],'limitation':'Actual elected holder UID deletion, single Lease transition to another legal instance, real A/B Ready mixture and subsequent convergence independently verified. Runner retains history, order, plugin data and status checks.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml','step-02-leader-delete.json','step-02-new-leader.log']}}


def main():
    runid = sys.argv[1];assert runid in ('leader-switch-r1','leader-switch-r2')
    base = ROOT/'artifacts'/runid; control = ROOT/'artifacts/environment-022'/(runid+'-control');done = m.read(control/'completion.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and done['testCreatedLeaseRemoved'] and not done['proxyErrors']
    env = m.read(base/'environment.json');assert env['runner']['binarySHA256'] == m.read(control/'build.json')['binarySHA256']
    results = m.read(base/'summary.json')['results'];assert [r['id'] for r in results] == (['RUN-441','RUN-442'] if runid == 'leader-switch-r1' else ['RUN-441'])
    out = base/'independent-leader-audit';out.mkdir();reports = []
    for result in results:
        report = audit_case(base/result['id'],env)
        with (out/(result['id']+'.json')).open('x') as f:json.dump(report,f,indent=2);f.write('\n')
        reports.append(report);print(report['id'],report['classification'])
    with (out/'summary.json').open('x') as f:json.dump({'status':'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},f,indent=2);f.write('\n')


if __name__ == '__main__': main()
