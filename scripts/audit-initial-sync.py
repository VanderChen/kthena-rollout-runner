#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Require an actual held startup request, preserved A, and automatic B recovery."""
import collections
import hashlib
import importlib.util
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('mid_audit', ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader); loader.loader.exec_module(m)


def audit_case(p, trace):
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml'); owner = m.yaml(p/'before-server.yaml')['metadata']['uid']
    assert result['id'] in ('RUN-450', 'RUN-451', 'RUN-453', 'RUN-454') and result['status'] == 'PASS'
    assert case['id'] == result['id'] == case['scenario']['source']['id'] and case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    config = case['scenario']['source']['config']; mode = config['mode']; budget = config['top'] if mode == 'SG' else config['roles']['f']
    assert budget['u'] == budget['p'] == 0 and budget['s'] == 1 and all(r['w'] == 0 for r in config['roles'].values())
    baseline = m.yaml(p/'baseline-resources.yaml'); basepods = m.mine(baseline, 'pods', owner); baserevisions = m.mine(baseline, 'controllerrevisions', owner)
    assert len(basepods) == (3 if mode == 'SG' else 6) and all(m.ready(o) and m.version(o) == 'A' for o in basepods.values())
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1, len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    checkpoints = [m.read(x) for x in p.glob('checkpoint-*.json')]
    held = next(cp for cp in checkpoints if cp['phase'] == 'actual-selected-informer-unsynced-with-A-retained')
    finalcp = next(cp for cp in checkpoints if cp['phase'] == case['scenario']['steps'][-1]['name'])
    assert held['elapsedStableNanos'] >= 10_000_000_000 and finalcp['elapsedStableNanos'] >= 30_000_000_000
    installed = m.read(p/'step-01-initial-installed.json'); saved = m.read(p/'step-01-initial-before-clear.json'); rule = next(r for r in saved['rules'] if r['id'] == installed['id'])
    resource = 'modelservings' if result['id'] in ('RUN-450', 'RUN-453') else 'pods'
    assert installed['initialSync'] and installed['resource'] == resource and installed['methods'] == ['GET'] and installed['mode'] == 'hold'
    assert not installed.get('namespace') and installed['count'] == -1 and installed['durationSeconds'] == 90
    assert rule['active'] and rule['hits'] > 0 and rule['released'] == 0 and not rule.get('endReason')
    assert not saved['errors'] and all(r['id'] == rule['id'] for r in saved['rules'] if r['active'])
    release = m.read(p/'step-01-initial-release-time.json'); deleted = m.read(p/'step-01-controller-delete.json')
    before = m.yaml(p/'step-01-controller-terminated.yaml'); replacement = m.yaml(p/'step-01-controller-replacement.yaml'); after = m.yaml(p/'fault-controller-after.yaml')
    assert deleted['accepted'] and deleted['uid'] == before['metadata']['uid'] and deleted['options']['preconditions']['uid'] == deleted['uid']
    assert replacement['metadata']['uid'] != deleted['uid'] and replacement['metadata']['uid'] == after['metadata']['uid'] == release['controllerUID']
    assert replacement['metadata']['ownerReferences'] == before['metadata']['ownerReferences']
    assert replacement['status']['containerStatuses'][0]['restartCount'] == after['status']['containerStatuses'][0]['restartCount'] == 0
    assert before['status']['containerStatuses'][0]['imageID'] == replacement['status']['containerStatuses'][0]['imageID'] == after['status']['containerStatuses'][0]['imageID']
    scope = [t for t in trace if t.get('ruleID') == installed['id']]
    hits = [t for t in scope if t['action'] == 'hold-request']; assert len(hits) == rule['hits']
    assert all(t['method'] == 'GET' and not t.get('namespace') and t['resource'] == resource and m.ts(deleted['sent']) < m.ts(t['at']) < m.ts(held['stableSince']) for t in hits)
    requests = [next(t for t in trace if t['action'] == 'request' and t['request'] == hit['request']) for hit in hits]
    assert all(t['detail'] in ('watch=false initialEvents=false', 'watch=true initialEvents=true') for t in requests)
    released = [t for t in scope if t['action'] == 'release-request']; assert len(released) == len(hits)
    assert {t['request'] for t in hits} == {t['request'] for t in released} and all(m.ts(t['at']) >= m.ts(release['sent']) for t in released)
    # A paused request is never sent upstream until explicitly released.
    for hit in hits:
        responses = [t for t in trace if t['action'] == 'response' and t['request'] == hit['request']]
        assert len(responses) == 1 and responses[0]['status'] == 200 and m.ts(responses[0]['at']) >= m.ts(release['sent'])
    receipt = m.read(p/'step-01-request-time.json'); accepted = m.yaml(p/'step-01-server.yaml')
    assert accepted['metadata']['uid'] == owner and receipt['generation'] == accepted['metadata']['generation'] == 2
    assert max(m.ts(t['at']) for t in hits) < m.ts(receipt['sent']) < m.ts(receipt['received']) < m.ts(held['stableSince']) < m.ts(held['completed']) < m.ts(release['sent'])
    admission = m.read(p/'step-01-admission-warmup.json'); assert admission[-1]['accepted'] and admission[-1]['requestGeneration'] == admission[-1]['responseGeneration'] == 1
    assert admission[-1]['dryRun'] == 'All' and admission[-1]['responseUID'] == owner
    unsyncedlog = (p/'step-01-controller-unsynced.log').read_text(); syncedlog = (p/'step-01-controller-synced.log').read_text()
    assert 'initial sync has been done' not in unsyncedlog
    synced = [line for line in syncedlog.splitlines() if 'initial sync has been done' in line]; assert len(synced) == 1 and m.ts(synced[0].split(' ', 1)[0]) >= m.ts(release['sent'])
    def preserved(state):
        pods = m.mine(state, 'pods', owner); revisions = m.mine(state, 'controllerrevisions', owner)
        assert set(pods) == set(basepods) and all(m.ready(o) and m.version(o) == 'A' for o in pods.values())
        assert set(revisions) == set(baserevisions)
        assert all(o['data'] == baserevisions[uid]['data'] for uid, o in revisions.items())
    preserved(m.replay(rows, installed['installed']))
    preserved(m.yaml(p/'step-01-unsynced-resources.yaml'))
    state = collections.defaultdict(dict); removals = []; newnames = collections.defaultdict(set)
    for row in rows:
        obj = row['object']; kind = row['kind']; uid = obj['metadata']['uid']; prev = state[kind].get(uid)
        if kind == 'pods' and m.owned(obj, owner):
            if row['event'] == 'ADDED' and m.version(obj) == 'B': newnames[obj['metadata']['name']].add(uid)
            if obj['metadata']['labels'][m.R] == 'frontend' and prev and m.ready(prev) and (row['event'] == 'DELETED' or obj['metadata'].get('deletionTimestamp')):
                available = sum(m.owned(o, owner) and o['metadata']['labels'][m.R] == 'frontend' and m.ready(o) for o in state[kind].values())
                assert available-1 >= 3
                removals.append({'sequence': row['sequence'], 'uid': uid, 'readyBefore': available})
        if row['event'] == 'DELETED': state[kind].pop(uid, None)
        else: state[kind][uid] = obj
        if m.ts(installed['installed']) <= m.ts(row['received']) < m.ts(release['sent']): preserved(state)
        if m.ts(finalcp['stableSince']) <= m.ts(row['received']) <= m.ts(finalcp['completed']): m.final_pods(state, owner, mode, 'B', basepods)
    assert all(len(uids) == 1 for uids in newnames.values())
    m.final_pods(m.replay(rows, finalcp['stableSince']), owner, mode, 'B', basepods)
    final = m.yaml(p/'final-resources.yaml'); pods = m.final_pods(final, owner, mode, 'B', basepods)
    assert not m.mine(final, 'services', owner)
    assert {o['metadata']['name'] for o in m.mine(final, 'podgroups', owner).values()} == {o['metadata']['labels'][m.G] for o in pods.values()}
    cms = m.mine(final, 'configmaps', owner); assert len(cms) == len(pods)
    for cm in cms.values(): assert sum(all(o['metadata']['labels'].get(k) == cm['metadata']['labels'].get(k) for k in (m.G, m.R, m.I)) for o in pods.values()) == 1
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id': result['id'], 'classification': 'PASS_INDEPENDENT_INITIAL_SYNC_CHECK', 'watchRows': len(rows), 'actualStartupRequests': requests, 'holdHits': hits, 'releaseProof': released, 'heldNanos': held['elapsedStableNanos'], 'sameOriginalAUIDsWhileUnsynced': sorted(basepods), 'newControllerUID': replacement['metadata']['uid'], 'actualInitialSyncCompletionLog': synced[0], 'healthyRemovalChecks': removals, 'finalStableNanos': finalcp['elapsedStableNanos'], 'limitation': 'Startup synchronization is proved externally by a held actual initial List/WatchList and the same process completing initial sync only after explicit release. Dry-run unchanged A warms real admission, then one real B update is accepted while unsynced. Complete direct Watch preserves A identity/history during the hold and supplements runner revision, order, plugin data and status assertions.', 'evidenceSHA256': {name: hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json', 'observations.jsonl', 'final-resources.yaml', 'step-01-initial-before-clear.json', 'step-01-controller-synced.log']}}


def main():
    base = ROOT/'artifacts/initial-sync-r2'; control = ROOT/'artifacts/environment-022/initial-sync-r2-control'
    done = m.read(control/'completion.json'); build = m.read(control/'build.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256'] == build['binarySHA256']
    assert m.read(control/'controller-before.json')['spec'] == m.read(control/'controller-restored.json')['spec']
    start = m.ts(m.read(base/'RUN-450/step-01-initial-installed.json')['installed'])
    trace = []
    with (control/'proxy-trace.jsonl').open() as f:
        for line in f:
            record = json.loads(line)
            if m.ts(record['at']) >= start: trace.append(record)
    reports = [audit_case(base/('RUN-'+str(n)), trace) for n in (450, 451, 453, 454)]
    out = base/'independent-initial-sync-audit'; out.mkdir()
    for report in reports:
        with (out/(report['id']+'.json')).open('x') as f: json.dump(report, f, indent=2); f.write('\n')
        print(report['id'], report['classification'], report['watchRows'])
    with (out/'summary.json').open('x') as f: json.dump({'status': 'VERIFIED', 'cases': reports, 'counts': {'PASS': len(reports)}}, f, indent=2); f.write('\n')


if __name__ == '__main__': main()
