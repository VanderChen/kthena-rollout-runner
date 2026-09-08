#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Verify Pending is an actual CPU scheduling fault and recovery uses real Pods."""
import collections
import decimal
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('midrollout_audit', ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader); loader.loader.exec_module(m)


def milli(value):
    return int(decimal.Decimal(value[:-1]) if value.endswith('m') else decimal.Decimal(value)*1000)


def pending(p):
    return not p['metadata'].get('deletionTimestamp') and not p['spec'].get('nodeName') and p.get('status', {}).get('phase') == 'Pending'


def audit_case(p, nodes):
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml'); owner = m.yaml(p/'before-server.yaml')['metadata']['uid']
    assert case['id'] == result['id'] == case['scenario']['source']['id'] and case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    assert result['status'] == 'PASS', 'raw failure requires independent review'
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1, len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    before = m.yaml(p/'case-controller-before.yaml'); after = m.yaml(p/'fault-controller-after.yaml')
    assert before['metadata']['uid'] == after['metadata']['uid'] and before['status']['containerStatuses'][0]['restartCount'] == after['status']['containerStatuses'][0]['restartCount']
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    proof = m.read(p/'step-03-resource-stop.json'); holder = m.yaml(p/'step-01-resource-gate-server.yaml'); holderUID = holder['metadata']['uid']
    assert len(nodes) == 1 and nodes[0]['metadata']['uid'] == proof['nodeUID']
    assert milli(nodes[0]['status']['allocatable']['cpu']) == proof['allocatableMilliCPU']
    assert proof['holderUID'] == holderUID and proof['holderRunning'] and holder['status']['phase'] == 'Running'
    allocations = proof['allocations']; assert len({a['uid'] for a in allocations}) == len(allocations)
    assert sum(a['milliCPU'] for a in allocations) == proof['usedMilliCPU']
    assert proof['allocatableMilliCPU'] - proof['usedMilliCPU'] == proof['freeMilliCPU']
    assert next(a['milliCPU'] for a in allocations if a['uid'] == holderUID) == milli(holder['spec']['containers'][0]['resources']['requests']['cpu'])
    blocked = proof['blockedPods']; assert blocked
    for pod in blocked:
        assert m.owned(pod, owner) and m.version(pod) == 'B' and pending(pod)
        assert pod['spec']['schedulerName'] == 'volcano'
        assert milli(pod['spec']['containers'][0]['resources']['requests']['cpu']) == 20 > proof['freeMilliCPU']
        assert any(c['type'] == 'PodScheduled' and c['status'] == 'False' and c['reason'] == 'Unschedulable' and 'Insufficient cpu' in c.get('message', '') for c in pod['status'].get('conditions', [])), 'scheduler must confirm real CPU shortage'
    checkpoints = [m.read(x) for x in p.glob('checkpoint-*.json')]
    held = next(c for c in checkpoints if c['phase'] == 'B-Pending-under-real-resource-pressure')
    assert held['holdSeconds'] == 10 and held['elapsedStableNanos'] >= 10_000_000_000
    start, end = m.ts(held['stableSince']), m.ts(held['completed'])
    state = m.replay(rows, held['stableSince'])
    candidates = {b['metadata']['uid'] for b in blocked} & set(state['pods'])
    witness = next(u for u in candidates if pending(state['pods'][u]))
    assert holderUID in state['pods'] and state['pods'][holderUID]['status']['phase'] == 'Running'
    for row in rows:
        if not start < m.ts(row['received']) <= end: continue
        obj = row['object']; uid = obj['metadata']['uid']; kind = row['kind']
        if row['event'] == 'DELETED': state[kind].pop(uid, None)
        else: state[kind][uid] = obj
        assert witness in state['pods'] and pending(state['pods'][witness]), 'Pending window interrupted'
        assert holderUID in state['pods'] and not state['pods'][holderUID]['metadata'].get('deletionTimestamp')
    config = case['scenario']['source']['config']; mode = config['mode']; budget = config['top'] if mode == 'SG' else config['roles']['f']
    front = [o for o in m.mine(state, 'pods', owner).values() if o['metadata']['labels'][m.R] == 'frontend' and not o['metadata'].get('deletionTimestamp')]
    assert len(front) == 3+budget['s'] and sum(m.ready(o) for o in front) == 3-budget['u'], 'fault did not reach requested availability/surge boundary'
    removed = next(r for r in rows if r['kind'] == 'pods' and r['event'] == 'DELETED' and r['object']['metadata']['uid'] == holderUID)
    assert m.ts(removed['received']) > m.ts(proof['received']) > end
    assert any(r['kind'] == 'pods' and r['object']['metadata']['uid'] in {b['metadata']['uid'] for b in blocked} and m.ready(r['object']) and m.ts(r['received']) > m.ts(removed['received']) for r in rows), 'no original blocked B recovered Ready after capacity release'
    baseline = m.mine(m.yaml(p/'baseline-resources.yaml'), 'pods', owner)
    finalcp = next(c for c in checkpoints if c['phase'] == 'release-capacity-and-complete-B')
    assert finalcp['elapsedStableNanos'] >= 30_000_000_000
    state = m.replay(rows, finalcp['stableSince']); m.final_pods(state, owner, mode, 'B', baseline)
    for row in rows:
        if not m.ts(finalcp['stableSince']) < m.ts(row['received']) <= m.ts(finalcp['completed']): continue
        obj = row['object']; uid = obj['metadata']['uid']; kind = row['kind']
        if row['event'] == 'DELETED': state[kind].pop(uid, None)
        else: state[kind][uid] = obj
        m.final_pods(state, owner, mode, 'B', baseline)
    final = m.yaml(p/'final-resources.yaml'); finalpods = m.final_pods(final, owner, mode, 'B', baseline)
    assert holderUID not in final['pods'] and not m.mine(final, 'services', owner)
    groups = {o['metadata']['labels'][m.G] for o in finalpods.values()}
    pgs = m.mine(final, 'podgroups', owner)
    assert {o['metadata']['name'] for o in pgs.values()} == groups
    for pg in pgs.values(): assert milli(pg['spec']['minResources']['cpu']) == (20 if mode == 'SG' else 75)
    cms = m.mine(final, 'configmaps', owner); assert len(cms) == len(finalpods)
    for cm in cms.values(): assert sum(all(o['metadata']['labels'].get(k) == cm['metadata']['labels'].get(k) for k in (m.G, m.R, m.I)) for o in finalpods.values()) == 1
    return {'id': result['id'], 'classification': 'PASS_INDEPENDENT_CPU_FAULT_AND_RECOVERY_CHECK', 'watchRows': len(rows), 'holderUID': holderUID, 'blockedUIDs': [b['metadata']['uid'] for b in blocked], 'continuousPendingUID': witness, 'freeMilliCPU': proof['freeMilliCPU'], 'requiredMilliCPU': 20, 'schedulerReportedInsufficientCPU': True, 'heldNanos': held['elapsedStableNanos'], 'holderDeletedAt': removed['received'], 'finalReadyPods': len(finalpods), 'finalStableNanos': finalcp['elapsedStableNanos'], 'sameControllerProcess': True, 'limitation': 'Independently checks allocation arithmetic, saved node/holder/blocked Pod specifications, actual scheduler CPU rejection and direct Watch. Non-test allocation CPU values come from the runner live cluster List; runner unit tests cover request accounting. Ordinary order/history/plugin data checks remain the runner responsibility.', 'evidenceSHA256': {n: hashlib.sha256((p/n).read_bytes()).hexdigest() for n in ['result.json', 'observations.jsonl', 'step-03-resource-stop.json', 'final-resources.yaml']}}


def main():
    runid = sys.argv[1]; assert runid and '/' not in runid and '..' not in runid
    base = ROOT/'artifacts'/runid; control = ROOT/'artifacts/environment-022'/(runid+'-control')
    done = m.read(control/'completion.json'); build = m.read(control/'build.json'); env = m.read(base/'environment.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and not done['proxyErrors']
    assert env['runner']['binarySHA256'] == build['binarySHA256']
    results = m.read(base/'summary.json')['results']; assert [r['id'] for r in results] == [f'RUN-{n}' for n in (401,405,409,413,417,421)]
    out = base/'independent-pending-audit'; out.mkdir(); reports = []
    for result in results:
        try: report = audit_case(base/result['id'], env['nodes'])
        except (AssertionError, KeyError, StopIteration) as err:
            report = {'id': result['id'], 'classification': 'PENDING_REVIEW', 'rawStatus': result['status'], 'rawError': result.get('error'), 'auditError': repr(err)}
        with (out/(result['id']+'.json')).open('x') as f: json.dump(report, f, indent=2); f.write('\n')
        reports.append(report); print(report['id'], report['classification'], report.get('auditError', ''))
    pending = any(r['classification'] == 'PENDING_REVIEW' for r in reports)
    with (out/'summary.json').open('x') as f: json.dump({'status': 'PENDING_REVIEW' if pending else 'VERIFIED', 'cases': reports, 'counts': dict(collections.Counter(r['classification'] for r in reports))}, f, indent=2); f.write('\n')
    if pending: sys.exit(1)


if __name__ == '__main__': main()
