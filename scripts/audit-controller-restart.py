#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Bind real controller process replacement to each source checkpoint."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('midrollout_audit', ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader); loader.loader.exec_module(m)


def audit_case(p, deployment):
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml'); owner = m.yaml(p/'before-server.yaml')['metadata']['uid']
    assert case['id'] == result['id'] == case['scenario']['source']['id'] and case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    assert result['status'] == 'PASS', 'raw failure requires independent classification'
    n = int(result['id'][4:]); which = (n-435)%3; mode = case['scenario']['source']['config']['mode']
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1, len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    calls = list(p.glob('step-*-controller-delete.json')); assert len(calls) == 1
    prefix = calls[0].name.split('-controller-delete')[0]; call = m.read(calls[0])
    old = m.yaml(p/(prefix+'-controller-terminated.yaml')); new = m.yaml(p/(prefix+'-controller-replacement.yaml'))
    initial = m.yaml(p/'case-controller-before.yaml'); finalcontroller = m.yaml(p/'fault-controller-after.yaml')
    assert call['accepted'] and call['options']['gracePeriodSeconds'] == 0
    assert call['options']['preconditions']['uid'] == call['uid'] == old['metadata']['uid'] == initial['metadata']['uid']
    assert new['metadata']['uid'] != old['metadata']['uid'] and finalcontroller['metadata']['uid'] == new['metadata']['uid']
    assert m.ready(old) and m.ready(new) and m.ready(finalcontroller)
    assert m.ts(new['metadata']['creationTimestamp']) >= (m.ts(call['sent'])//1_000_000_000)*1_000_000_000
    assert new['status']['containerStatuses'][0]['restartCount'] == finalcontroller['status']['containerStatuses'][0]['restartCount'] == 0
    assert old['status']['containerStatuses'][0]['restartCount'] == initial['status']['containerStatuses'][0]['restartCount']
    digest = 'sha256:7c6ed6c78b37d7afaf104381351554ad75aed56c64d0aca2ec941ce652756265'
    assert all(pod['status']['containerStatuses'][0]['imageID'] == digest for pod in (old, new, finalcontroller))
    afterdep = m.yaml(p/(prefix+'-controller-deployment.yaml'))
    assert afterdep['metadata']['uid'] == deployment['metadata']['uid'] and afterdep['spec'] == deployment['spec']
    lines = (p/(prefix+'-new-controller.log')).read_text().splitlines()
    synced = next(line for line in lines if 'initial sync has been done' in line)
    synced_at = synced.split(' ', 1)[0]; assert m.ts(synced_at) > m.ts(call['sent'])
    checkpoints = [m.read(x) for x in p.glob('checkpoint-*.json')]
    baseline = m.mine(m.yaml(p/'baseline-resources.yaml'), 'pods', owner)
    proof = {'oldControllerUID': old['metadata']['uid'], 'newControllerUID': new['metadata']['uid'], 'deleteSent': call['sent'], 'initialSyncAt': synced_at, 'sameProductionImage': True, 'deploymentSpecUnchanged': True}
    if which == 0:
        trigger = m.yaml(p/'step-01-resources.yaml')
        new_surge = [o for o in m.mine(trigger, 'pods', owner).values() if m.version(o) == 'B']
        assert len(new_surge) == 1 and not m.ready(new_surge[0]) and not new_surge[0]['metadata'].get('deletionTimestamp')
        lab = new_surge[0]['metadata']['labels']; assert int(lab[m.G if mode == 'SG' else m.I].rsplit('-', 1)[1]) == 3
        assert any(r['kind'] == 'pods' and r['event'] == 'ADDED' and r['object']['metadata']['uid'] == new_surge[0]['metadata']['uid'] and m.ts(r['received']) < m.ts(call['sent']) for r in rows)
        proof.update(checkpoint='first B surge created and not Ready', surgeUID=new_surge[0]['metadata']['uid'])
    elif which == 1:
        actual = [m.yaml(x) for x in p.glob(prefix+'-live-terminating-*.yaml')]; assert len(actual) == 1
        pod = actual[0]; uid = pod['metadata']['uid']; lab = pod['metadata']['labels']
        assert m.version(pod) == 'A' and uid in baseline and pod['metadata'].get('deletionTimestamp') and 'rollout-runner/hold' in pod['metadata']['finalizers']
        assert int(lab[m.G if mode == 'SG' else m.I].rsplit('-', 1)[1]) == 2
        after_restart = m.yaml(p/(prefix+'-resources.yaml'))['pods'][uid]
        assert after_restart['metadata'].get('deletionTimestamp') and 'rollout-runner/hold' in after_restart['metadata']['finalizers']
        assert any(r['kind'] == 'pods' and r['event'] == 'DELETED' and r['object']['metadata']['uid'] == uid and m.ts(r['received']) > m.ts(synced_at) for r in rows)
        proof.update(checkpoint='old A ordinal2 still Terminating across controller replacement', oldTerminatingUID=uid)
    else:
        before = m.yaml(p/'step-01-resources.yaml'); beforepods = m.final_pods(before, owner, mode, 'B', baseline)
        completed = next(c for c in checkpoints if c['phase'] == 'complete-B-including-resource-cleanup')
        assert completed['elapsedStableNanos'] >= 30_000_000_000 and m.ts(completed['completed']) < m.ts(call['sent'])
        final = m.yaml(p/'final-resources.yaml')
        assert set(beforepods) == set(m.mine(final, 'pods', owner))
        assert set(m.mine(before, 'controllerrevisions', owner)) == set(m.mine(final, 'controllerrevisions', owner))
        assert not any(r['kind'] in ('pods', 'controllerrevisions') and m.owned(r['object'], owner) and m.ts(r['received']) > m.ts(call['sent']) and (r['event'] in ('ADDED', 'DELETED') or r['object']['metadata'].get('deletionTimestamp')) for r in rows)
        proof.update(checkpoint='B cleanup stable before restart, no Pod or revision replacement after restart', retainedPodUIDs=sorted(beforepods))
    finalcp = next(c for c in checkpoints if c['phase'] == case['scenario']['steps'][-1]['name'])
    assert finalcp['elapsedStableNanos'] >= 30_000_000_000 and m.ts(finalcp['stableSince']) > m.ts(synced_at)
    state = m.replay(rows, finalcp['stableSince']); m.final_pods(state, owner, mode, 'B', baseline)
    for row in rows:
        if not m.ts(finalcp['stableSince']) < m.ts(row['received']) <= m.ts(finalcp['completed']): continue
        obj = row['object']; uid = obj['metadata']['uid']; kind = row['kind']
        if row['event'] == 'DELETED': state[kind].pop(uid, None)
        else: state[kind][uid] = obj
        m.final_pods(state, owner, mode, 'B', baseline)
    final = m.yaml(p/'final-resources.yaml'); pods = m.final_pods(final, owner, mode, 'B', baseline)
    assert not m.mine(final, 'services', owner)
    assert {o['metadata']['name'] for o in m.mine(final, 'podgroups', owner).values()} == {o['metadata']['labels'][m.G] for o in pods.values()}
    cms = m.mine(final, 'configmaps', owner); assert len(cms) == len(pods)
    for cm in cms.values(): assert sum(all(o['metadata']['labels'].get(k) == cm['metadata']['labels'].get(k) for k in (m.G, m.R, m.I)) for o in pods.values()) == 1
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    return {'id': result['id'], 'classification': 'PASS_INDEPENDENT_CONTROLLER_REPLACEMENT_AND_FINAL_CHECK', 'watchRows': len(rows), 'controllerProof': proof, 'finalReadyPods': len(pods), 'finalStableNanos': finalcp['elapsedStableNanos'], 'limitation': 'Actual controller Pod termination, new same-image synced process and source checkpoint independently verified. Final Pod stability and resource association supplement runner budget, order, history and plugin data checks.', 'evidenceSHA256': {n: hashlib.sha256((p/n).read_bytes()).hexdigest() for n in ['result.json', 'observations.jsonl', 'final-resources.yaml', prefix+'-new-controller.log', prefix+'-controller-delete.json']}}


def main():
    runid = sys.argv[1]; assert runid and '/' not in runid and '..' not in runid
    base = ROOT/'artifacts'/runid; control = ROOT/'artifacts/environment-022'/(runid+'-control')
    done = m.read(control/'completion.json'); build = m.read(control/'build.json'); env = m.read(base/'environment.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and not done['proxyErrors']
    assert env['runner']['binarySHA256'] == build['binarySHA256']
    results = m.read(base/'summary.json')['results']; assert [r['id'] for r in results] == [f'RUN-{n}' for n in range(435, 441)]
    out = base/'independent-controller-audit'; out.mkdir(); reports = []
    for result in results:
        try: report = audit_case(base/result['id'], env['controller'])
        except (AssertionError, KeyError, StopIteration) as err:
            report = {'id': result['id'], 'classification': 'PENDING_REVIEW', 'rawStatus': result['status'], 'rawError': result.get('error'), 'auditError': repr(err)}
        with (out/(result['id']+'.json')).open('x') as f: json.dump(report, f, indent=2); f.write('\n')
        reports.append(report); print(report['id'], report['classification'], report.get('auditError', ''))
    pending = any(r['classification'] == 'PENDING_REVIEW' for r in reports)
    with (out/'summary.json').open('x') as f: json.dump({'status': 'PENDING_REVIEW' if pending else 'VERIFIED', 'cases': reports, 'counts': dict(collections.Counter(r['classification'] for r in reports))}, f, indent=2); f.write('\n')
    if pending: sys.exit(1)


if __name__ == '__main__': main()
