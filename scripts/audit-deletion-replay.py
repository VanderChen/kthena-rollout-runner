#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independently prove real controller delivery of old deletions after new A recovery."""
import collections
import hashlib
import importlib.util
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('mid_audit', ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader); loader.loader.exec_module(m)


def frontend_units(pods, owner, mode):
    grouped = collections.defaultdict(list)
    for o in pods.values():
        lab = o['metadata']['labels']
        if m.owned(o, owner) and lab[m.R] == 'frontend': grouped[lab[m.G] if mode == 'SG' else lab[m.I]].append(o)
    return grouped


def complete_ready(members):
    return len(members) == 2 and sum(o['metadata']['labels'].get(m.E) == 'true' for o in members) == 1 and all(m.ready(o) for o in members)


def final_pods(state, owner, mode, baseline, protected):
    pods = m.mine(state, 'pods', owner); assert len(pods) == (6 if mode == 'SG' else 9) and all(m.ready(o) for o in pods.values())
    units = frontend_units(pods, owner, mode); assert len(units) == 3
    versions = collections.Counter()
    for key, members in units.items():
        assert complete_ready(members)
        version = 'A' if int(key.rsplit('-', 1)[1]) == 0 else 'B'
        assert all(m.version(o) == version for o in members)
        versions[version] += 1
    assert dict(versions) == {'A': 1, 'B': 2}
    assert set(protected) <= set(pods)
    for uid, o in pods.items():
        if o['metadata']['labels'][m.R] == 'backend': assert uid in baseline and m.version(o) == 'A'
    return pods


def audit_case(p, trace):
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml'); owner = m.yaml(p/'before-server.yaml')['metadata']['uid']
    assert result['id'] in ('RUN-449', 'RUN-452') and result['status'] == 'PASS'
    assert case['id'] == result['id'] == case['scenario']['source']['id'] and case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    config = case['scenario']['source']['config']; mode = config['mode']; budget = config['top'] if mode == 'SG' else config['roles']['f']
    assert config['roles']['f']['w'] == 1 and budget == ({'u': 0, 's': 1, 'p': 1} if mode == 'SG' else {'r': 3, 'w': 1, 'u': 0, 's': 1, 'p': 1})
    baseline = m.yaml(p/'baseline-resources.yaml'); basepods = m.mine(baseline, 'pods', owner)
    scope = m.read(p/'step-01-fault-scope.json'); old = scope['recoveryUIDs']; assert len(old) == 2 and scope['ownerUID'] == owner and scope['recovery'] == 'RoleRecreate'
    before = m.yaml(p/'step-01-fault-pods-before.yaml')['items']; original = [o for o in before if o['metadata']['uid'] in old]
    assert len(original) == 2 and complete_ready(original) and all(m.version(o) == 'A' and o['metadata']['labels'][m.G] == 'model-0' and o['metadata']['labels'][m.I] == 'frontend-0' for o in original)
    deletes = m.read(p/'step-01-concurrent-deletes.json'); assert len(deletes) == 2 and {r['uid'] for r in deletes} == set(old)
    assert all(r['accepted'] and r['options']['preconditions']['uid'] == r['uid'] and r['name'] == old[r['uid']] for r in deletes)
    assert max(m.ts(r['sent']) for r in deletes)-min(m.ts(r['sent']) for r in deletes) < 100_000_000
    write = next(m.read(x) for x in p.glob('step-01-submit-B-write-*-receipt.json') if 'error' not in m.read(x))
    assert write['generation'] == 2 and m.ts(write['received']) < min(m.ts(r['sent']) for r in deletes)
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1, len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    checkpoints = [m.read(x) for x in p.glob('checkpoint-*.json')]
    protectedcp = next(cp for cp in checkpoints if cp['phase'] == 'protected-A-new-UIDs-ready-before-replay')
    finalcp = next(cp for cp in checkpoints if cp['phase'] == case['scenario']['steps'][-1]['name'])
    assert protectedcp['elapsedStableNanos'] >= 3_000_000_000 and finalcp['elapsedStableNanos'] >= 30_000_000_000
    limit = m.read(p/'fault-proxy-final.json')['sequence']; trace = [r for r in trace if r['sequence'] <= limit and m.ts(r['at']) <= m.ts(finalcp['completed'])]
    ruleprefix = 'deletion-replay-r1-'+result['id'].lower()+'-01-'
    barriers = [r for r in m.read(p/'step-01-before-controller-resume.json')['rules'] if r['id'].startswith(ruleprefix)]
    assert len(barriers) == 9 and all(r['active'] for r in barriers)
    resumed = [r for r in trace if r['action'] == 'rule-cleared' and r.get('ruleID', '').startswith(ruleprefix)]; assert len(resumed) == 9
    firstresume = min(m.ts(r['at']) for r in resumed)
    assert not any(o['metadata']['uid'] in old for o in m.yaml(p/'step-01-old-absent.yaml')['items'])
    for uid in old:
        deleted = [r for r in rows if r['kind'] == 'pods' and r['event'] == 'DELETED' and r['object']['metadata']['uid'] == uid]
        assert len(deleted) == 1 and m.ts(deleted[0]['received']) < firstresume
    protected = {o['metadata']['uid']: o for o in m.yaml(p/'step-01-new-protected-before-replay.yaml')}
    assert len(protected) == 2 and not set(protected).intersection(old) and {o['metadata']['name'] for o in protected.values()} == set(old.values())
    assert complete_ready(list(protected.values())) and all(m.version(o) == 'A' for o in protected.values())
    oldhash = {o['metadata']['name']: o['metadata']['labels']['modelserving.volcano.sh/revision'] for o in original}
    assert all(o['metadata']['labels']['modelserving.volcano.sh/revision'] == oldhash[o['metadata']['name']] for o in protected.values())
    installed = m.read(p/'step-01-replay-installed.json'); ruleid = installed['id']; request = m.read(p/'step-01-replay-request.json'); receipt = m.read(p/'step-01-replay-response.json')
    ready = m.read(p/'step-01-replay-ready.json'); capturerule = next(r for r in ready['rules'] if r['id'] == ruleid)
    assert capturerule['active'] and capturerule['hits'] == 2 and set(capturerule['captured']) == set(old)
    assert not any(r['active'] and r['id'] != ruleid for r in ready['rules'])
    assert request['order'] == [capturerule['captureOrder'][1], capturerule['captureOrder'][0]]*2 == receipt['order']
    captured = [r for r in trace if r.get('ruleID') == ruleid and r['action'] == 'capture-deletion-event']; replayed = [r for r in trace if r.get('ruleID') == ruleid and r['action'] == 'replay-deletion-event']
    assert len(captured) == 2 and len(replayed) == 4 and [r['uid'] for r in replayed] == receipt['order']
    hashes = {r['uid']: r['payloadSHA256'] for r in captured}; assert hashes == capturerule['captured']
    assert all(r['request'] == receipt['request'] and r['payloadSHA256'] == hashes[r['uid']] for r in replayed)
    assert receipt['payloadSHA256'] == [hashes[uid] for uid in receipt['order']]
    assert m.ts(protectedcp['completed']) < m.ts(replayed[0]['at']) < m.ts(receipt['completed']) < m.ts(finalcp['stableSince'])
    for uid in old:
        assert any(r['action'] == 'forward-event' and r.get('uid') == uid and r.get('event') == 'DELETED' and r['request'] == receipt['request'] and r['sequence'] < replayed[0]['sequence'] for r in trace)
    # Prove new identities had also reached the very controller Watch stream
    # that later received old deletion duplicates, not merely the direct observer.
    for uid in protected:
        assert any(r['action'] == 'forward-event' and r.get('uid') == uid and r.get('event') == 'ADDED' and r['request'] == receipt['request'] and r['sequence'] < replayed[0]['sequence'] for r in trace)
        assert not any(r['kind'] == 'pods' and r['object']['metadata']['uid'] == uid and (r['event'] == 'DELETED' or r['object']['metadata'].get('deletionTimestamp')) for r in rows)
        assert sum(r['kind'] == 'pods' and r['event'] == 'ADDED' and r['object']['metadata']['name'] == protected[uid]['metadata']['name'] for r in rows) == 2
    state = collections.defaultdict(dict); removals = []; names = collections.defaultdict(set)
    for row in rows:
        obj = row['object']; kind = row['kind']; uid = obj['metadata']['uid']; prev = state[kind].get(uid)
        if kind == 'pods' and m.owned(obj, owner):
            if row['event'] == 'ADDED' and m.version(obj) == 'B': names[obj['metadata']['name']].add(uid)
            if uid not in old and prev and m.ready(prev) and obj['metadata']['labels'][m.R] == 'frontend' and (row['event'] == 'DELETED' or obj['metadata'].get('deletionTimestamp')):
                units = frontend_units(state[kind], owner, mode); key = obj['metadata']['labels'][m.G if mode == 'SG' else m.I]
                if complete_ready(units.get(key, [])):
                    available = sum(complete_ready(unit) for unit in units.values()); assert available-1 >= 3
                    removals.append({'sequence': row['sequence'], 'uid': uid, 'completeReadyBefore': available})
        if kind == 'controllerrevisions' and uid in baseline['controllerrevisions'] and row['event'] != 'DELETED': assert obj['data'] == baseline['controllerrevisions'][uid]['data']
        if row['event'] == 'DELETED': state[kind].pop(uid, None)
        else: state[kind][uid] = obj
        if m.ts(finalcp['stableSince']) <= m.ts(row['received']) <= m.ts(finalcp['completed']): final_pods(state, owner, mode, basepods, protected)
    assert all(len(uids) == 1 for uids in names.values())
    final_pods(m.replay(rows, finalcp['stableSince']), owner, mode, basepods, protected)
    final = m.yaml(p/'final-resources.yaml'); pods = final_pods(final, owner, mode, basepods, protected)
    groups = {o['metadata']['labels'][m.G] for o in pods.values()}; assert {o['metadata']['name'] for o in m.mine(final, 'podgroups', owner).values()} == groups
    cms = m.mine(final, 'configmaps', owner); services = m.mine(final, 'services', owner)
    assert len(cms) == (3 if mode == 'SG' else 6) and len(services) == 3
    for kind in ('configmaps', 'services'):
        for obj in m.mine(final, kind, owner).values():
            members = [o for o in pods.values() if all(o['metadata']['labels'].get(k) == obj['metadata']['labels'].get(k) for k in (m.G, m.R, m.I))]
            assert len(members) == (1 if obj['metadata']['labels'][m.R] == 'backend' else 2) and not obj['metadata'].get('deletionTimestamp')
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    finalrule = next(r for r in proxy['rules'] if r['id'] == ruleid); assert finalrule['hits'] == 2 and finalrule['replayed'] == 4
    beforecontroller = m.yaml(p/'step-01-controller-before.yaml'); aftercontroller = m.yaml(p/'fault-controller-after.yaml')
    assert beforecontroller['metadata']['uid'] == aftercontroller['metadata']['uid'] and beforecontroller['status']['containerStatuses'][0]['restartCount'] == aftercontroller['status']['containerStatuses'][0]['restartCount']
    return {'id': result['id'], 'classification': 'PASS_INDEPENDENT_ACTUAL_DELETION_REPLAY_CHECK', 'watchRows': len(rows), 'concurrentOldDeletes': deletes, 'protectedHistoricalAReplacementUIDs': sorted(protected), 'actualReplayedOldUIDs': receipt['order'], 'controllerWatchRequest': receipt['request'], 'payloadHashesIdentical': True, 'newAUIDsForwardedBeforeOldReplay': True, 'healthyRemovalChecks': removals, 'finalReadyPods': len(pods), 'finalStableNanos': finalcp['elapsedStableNanos'], 'sameControllerProcess': True, 'limitation': 'Actual proxy stream delivery plus continuous direct API Watch proves new protected A identities survive old duplicate/reversed DELETED frames. Final resource ownership and absence of terminating objects supplement runner history data, ordering, plugin content and status checks; controller internal datastore transitions are not directly snapshotted.', 'evidenceSHA256': {name: hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json', 'observations.jsonl', 'final-resources.yaml', 'step-01-replay-response.json', 'step-01-fault-scope.json']}}


def audit_pre_replay_failure(p, trace):
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml'); config = case['scenario']['source']['config']
    assert result['id'] == case['id'] == case['scenario']['source']['id'] == 'RUN-452' and result['status'] == 'FAIL' and 'BUDGET_VIOLATION' in result['error']
    assert case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95' and config['mode'] == 'Role'
    assert config['roles']['f'] == {'r': 3, 'w': 1, 'u': 0, 's': 1, 'p': 1}
    scope = m.read(p/'step-01-fault-scope.json'); owner = scope['ownerUID']; old = scope['recoveryUIDs']
    assert owner == m.yaml(p/'before-server.yaml')['metadata']['uid'] and len(old) == 2 and scope['recovery'] == 'RoleRecreate'
    original = [o for o in m.yaml(p/'step-01-fault-pods-before.yaml')['items'] if o['metadata']['uid'] in old]
    assert complete_ready(original) and all(m.version(o) == 'A' and o['metadata']['labels'][m.G] == 'model-0' and o['metadata']['labels'][m.I] == 'frontend-0' for o in original)
    deletes = m.read(p/'step-01-concurrent-deletes.json'); assert len(deletes) == 2 and {r['uid'] for r in deletes} == set(old)
    assert all(r['accepted'] and r['options']['preconditions']['uid'] == r['uid'] and r['name'] == old[r['uid']] for r in deletes)
    assert max(m.ts(r['sent']) for r in deletes)-min(m.ts(r['sent']) for r in deletes) < 100_000_000
    write = next(m.read(x) for x in p.glob('step-01-submit-B-write-*-receipt.json') if 'error' not in m.read(x))
    assert write['generation'] == 2 and m.ts(write['received']) < min(m.ts(r['sent']) for r in deletes)
    prefix = 'deletion-replay-r1-run-452-01-'
    barriers = [r for r in m.read(p/'step-01-before-controller-resume.json')['rules'] if r['id'].startswith(prefix)]
    assert len(barriers) == 9 and all(r['active'] for r in barriers)
    trace = [r for r in trace if r['sequence'] <= m.read(p/'fault-proxy-final.json')['sequence']]
    resumed = [r for r in trace if r['action'] == 'rule-cleared' and r.get('ruleID', '').startswith(prefix)]
    firstresume = min(m.ts(r['at']) for r in resumed)
    assert not any(o['metadata']['uid'] in old for o in m.yaml(p/'step-01-old-absent.yaml')['items'])
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1, len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    for uid in old:
        deleted = [r for r in rows if r['kind'] == 'pods' and r['event'] == 'DELETED' and r['object']['metadata']['uid'] == uid]
        assert len(deleted) == 1 and m.ts(deleted[0]['received']) < firstresume
    state = {}; violations = []
    for row in rows:
        if row['kind'] != 'pods' or not m.owned(row['object'], owner): continue
        obj = row['object']; uid = obj['metadata']['uid']; prev = state.get(uid)
        if uid not in old and prev and m.ready(prev) and obj['metadata']['labels'][m.R] == 'frontend' and (row['event'] == 'DELETED' or obj['metadata'].get('deletionTimestamp')):
            units = frontend_units(state, owner, 'Role'); key = obj['metadata']['labels'][m.I]; available = sum(complete_ready(v) for v in units.values())
            if complete_ready(units.get(key, [])) and available-1 < 3:
                assert all(u not in state for u in old) and m.ts(row['received']) > firstresume
                violations.append({'sequence': row['sequence'], 'received': row['received'], 'uid': uid, 'name': obj['metadata']['name'], 'completeReadyBefore': available, 'completeReadyAfter': available-1, 'minimum': 3, 'bothExternalOldUIDsAlreadyAbsent': True, 'unitsBefore': {k: [{'name': o['metadata']['name'], 'uid': o['metadata']['uid'], 'ready': m.ready(o), 'version': m.version(o)} for o in v] for k, v in units.items()}})
        if row['event'] == 'DELETED': state.pop(uid, None)
        else: state[uid] = obj
    assert violations
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    ruleid = m.read(p/'step-01-replay-installed.json')['id']; rule = next(r for r in proxy['rules'] if r['id'] == ruleid)
    assert rule['hits'] == 2 and not rule.get('replayed', 0) and not rule.get('replayRequested', False) and not (p/'step-01-replay-response.json').exists()
    assert not any(r['action'] == 'replay-deletion-event' and r.get('ruleID') == ruleid for r in trace)
    before = m.yaml(p/'step-01-controller-before.yaml'); after = m.yaml(p/'fault-controller-after.yaml')
    assert before['metadata']['uid'] == after['metadata']['uid'] and before['status']['containerStatuses'][0]['restartCount'] == after['status']['containerStatuses'][0]['restartCount']
    return {'id': result['id'], 'classification': 'KTHENA_BEHAVIOR_FAILURE', 'subtype': 'HEALTHY_ROLE_DELETION_BELOW_BUDGET_BEFORE_REPLAY', 'watchRows': len(rows), 'rawError': result['error'], 'concurrentOldDeletes': deletes, 'budgetViolations': violations, 'capturedOldDeletionFrames': 2, 'replayedEvents': 0, 'sameControllerProcess': True, 'limitation': 'The complete implemented scenario stops at a prior source-required recovery safety failure. Both original protected members were actually deleted and controller resumed; deletion of another healthy frontend leaves 2 complete Ready units below the U=0 minimum3 while recovered A is not Ready. The later duplicate/reverse event step and final convergence were not exercised in this execution; do not claim RUN-452 replay robustness was verified. No product source was modified.', 'evidenceSHA256': {name: hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json', 'observations.jsonl', 'step-01-fault-scope.json', 'step-01-concurrent-deletes.json', 'fault-proxy-final.json']}}


def main():
    base = ROOT/'artifacts/deletion-replay-r1'; control = ROOT/'artifacts/environment-022/deletion-replay-r1-control'
    done = m.read(control/'completion.json'); build = m.read(control/'build.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256'] == build['binarySHA256']
    trace = [json.loads(line) for line in (control/'proxy-trace.jsonl').read_text().splitlines()]
    reports = [audit_case(base/'RUN-449', trace), audit_pre_replay_failure(base/'RUN-452', trace)]
    out = base/'independent-deletion-replay-audit'; out.mkdir()
    for report in reports:
        with (out/(report['id']+'.json')).open('x') as f: json.dump(report, f, indent=2); f.write('\n')
        print(report['id'], report['classification'], report['watchRows'])
    with (out/'summary.json').open('x') as f: json.dump({'status': 'VERIFIED', 'cases': reports, 'counts': {'PASS': 1, 'KTHENA_BEHAVIOR_FAILURE': 1}}, f, indent=2); f.write('\n')


if __name__ == '__main__': main()
