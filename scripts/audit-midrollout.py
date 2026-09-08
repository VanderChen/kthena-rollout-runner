#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independently check actual fault witnesses and continuous final Pod state."""
import collections
import datetime
import hashlib
import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
PARSER = '/private/tmp/runner022-yaml-json'
G = 'modelserving.volcano.sh/group-name'
R = 'modelserving.volcano.sh/role'
I = 'modelserving.volcano.sh/role-id'
E = 'modelserving.volcano.sh/entry'


def read(p): return json.loads(p.read_text())
def yaml(p): return json.loads(subprocess.check_output([PARSER, str(p)]))[p.name]
def ts(s):
    match = re.fullmatch(r'(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?Z', s)
    assert match, 'expected UTC RFC3339Nano evidence timestamp'
    seconds = int(datetime.datetime.fromisoformat(match[1]+'+00:00').timestamp())
    return seconds*1_000_000_000+int((match[2] or '').ljust(9, '0'))
def owned(p, owner): return any(o['uid'] == owner for o in p['metadata'].get('ownerReferences', []))
def ready(p): return not p['metadata'].get('deletionTimestamp') and any(c['type'] == 'Ready' and c['status'] == 'True' for c in p.get('status', {}).get('conditions', []))
def version(p): return next(e['value'] for c in p['spec']['containers'] for e in c.get('env', []) if e['name'] == 'ROLLOUT_VERSION')
def status(p): return next((c for c in p.get('status', {}).get('containerStatuses', []) if c['name'] == 'workload'), {})
def fault(p, image=False):
    if ready(p) or p['metadata'].get('deletionTimestamp'): return False
    s = status(p)
    if image: return s.get('state', {}).get('waiting', {}).get('reason') == 'ImagePullBackOff'
    return p.get('status', {}).get('phase') == 'Running' and 'running' in s.get('state', {}) and not s.get('ready', False)
def mine(state, kind, owner): return {u: p for u, p in state.get(kind, {}).items() if owned(p, owner)}


def replay(rows, until=None):
    state = collections.defaultdict(dict)
    for row in rows:
        if until is not None and ts(row['received']) > ts(until): break
        if row['event'] == 'GAP': raise AssertionError('observation gap cannot support PASS')
        obj = row['object']; kind = row['kind']; uid = obj['metadata']['uid']
        if row['event'] == 'DELETED': state[kind].pop(uid, None)
        else: state[kind][uid] = obj
    return state


def final_pods(state, owner, mode, desired_version, baseline):
    pods = mine(state, 'pods', owner)
    assert len(pods) == (3 if mode == 'SG' else 6), 'unexpected final member count'
    assert all(ready(p) for p in pods.values()), 'final Pod not Ready or still terminating'
    front = [p for p in pods.values() if p['metadata']['labels'][R] == 'frontend']
    assert len(front) == 3 and all(version(p) == desired_version for p in front), 'wrong final frontend version/count'
    for uid, p in pods.items():
        assert p['metadata']['labels'][E] == 'true', 'W=0 must contain entries only'
        if p['metadata']['labels'][R] == 'backend':
            assert uid in baseline and version(p) == 'A', 'unchanged backend was replaced'
    return pods


def audit_case(p):
    result = read(p/'result.json'); c = yaml(p/'case.yaml'); owner = yaml(p/'before-server.yaml')['metadata']['uid']
    assert c['id'] == result['id'] == c['scenario']['source']['id'] and c['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    n = int(result['id'][4:]); config = c['scenario']['source']['config']; mode = config['mode']
    assert n in [x for x in range(402, 431) if x >= 425 or (x-401) % 4 != 0]
    assert result['status'] == 'PASS', 'raw failure requires independent classification'
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1, len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    baseline = mine(yaml(p/'baseline-resources.yaml'), 'pods', owner)
    controller = yaml(p/'case-controller-before.yaml'); after = yaml(p/'fault-controller-after.yaml')
    assert controller['metadata']['uid'] == after['metadata']['uid'] and controller['status']['containerStatuses'][0]['restartCount'] == after['status']['containerStatuses'][0]['restartCount']
    proxy = read(p/'fault-proxy-final.json')
    assert not proxy['errors'] and not any(r['active'] for r in proxy.get('rules') or [])
    checkpoints = [read(x) for x in p.glob('checkpoint-*.json')]
    held = next(cp for cp in checkpoints if cp['holdSeconds'] == 10)
    assert held['elapsedStableNanos'] >= 10_000_000_000
    held_start, held_end = ts(held['stableSince']), ts(held['completed'])
    finalcp = next(cp for cp in checkpoints if cp['phase'] == c['scenario']['steps'][-1]['name'])
    assert finalcp['stableSeconds'] == 30 and finalcp['elapsedStableNanos'] >= 30_000_000_000
    releases = [read(x) for x in p.glob('release-*.json')]
    if n < 425:
        assert not any(held_start <= ts(r['at']) <= held_end for r in releases), 'readiness released inside required fault hold'
    snapshot = yaml(p/('step-02-resources.yaml' if n >= 425 or (n-401) % 4 == 2 else 'step-01-resources.yaml'))
    heldpods = mine(snapshot, 'pods', owner)
    proof = {'heldSince': held['stableSince'], 'heldUntil': held['completed'], 'heldNanos': held['elapsedStableNanos']}
    expected = 'B'
    if n >= 425:
        pinned = [yaml(x) for x in p.glob('step-01-pinned-*.yaml')]
        assert len(pinned) == 1
        target = pinned[0]; uid = target['metadata']['uid']; lab = target['metadata']['labels']
        ordinal = int(lab[G if mode == 'SG' else I].rsplit('-', 1)[1])
        assert ordinal == 2 and version(target) == 'A' and target['metadata'].get('finalizers')
        current = heldpods[uid]
        assert current['metadata'].get('deletionTimestamp') and set(target['metadata']['finalizers']).issubset(current['metadata']['finalizers'])
        for state_time in [held['stableSince'], held['completed']]:
            observed = replay(rows, state_time)['pods'][uid]
            assert observed['metadata'].get('deletionTimestamp') and observed['metadata'].get('finalizers')
        assert any(r['kind'] == 'pods' and r['event'] == 'DELETED' and r['object']['metadata']['uid'] == uid and ts(r['received']) > held_end for r in rows)
        proof.update(kind='actual old Pod Terminating with injected finalizer', uid=uid)
    elif (n-401) % 4 == 2:
        call = read(p/'step-02-readiness-exec.json'); withdrawn = yaml(p/'step-02-readiness-withdrawn.yaml')
        uid = call['uid']; before = yaml(p/'step-02-readiness-before.yaml')['items']
        target = next(o for o in before if o['metadata']['uid'] == uid)
        assert call['command'] == ['rm', '-f', '/tmp/ready'] and not call['error']
        assert ready(target) and version(target) == 'B' and any(owned(o, owner) and ready(o) and version(o) == 'A' for o in before)
        assert withdrawn['metadata']['uid'] == uid and fault(withdrawn) and fault(heldpods[uid])
        assert status(target)['containerID'] == status(withdrawn)['containerID'] and status(target)['restartCount'] == status(withdrawn)['restartCount']
        restore = next(r for r in sorted(releases, key=lambda r: ts(r['at'])) if ts(r['at']) > held_end)
        assert [o['metadata']['uid'] for o in restore['pods']] == [uid], 'restore must release exact withdrawn Pod'
        assert any(r['kind'] == 'pods' and r['object']['metadata']['uid'] == uid and ts(r['received']) > ts(restore['at']) and ready(r['object']) for r in rows)
        proof.update(kind='actual Ready to NotReady to Ready on same running container and Pod UID', uid=uid, execSent=call['sent'], restoredAt=restore['at'])
    else:
        image = (n-401) % 4 == 3
        witnesses = [o for o in heldpods.values() if version(o) == 'B' and fault(o, image)]
        assert witnesses, 'missing actual container state witness'
        uid = witnesses[0]['metadata']['uid']
        assert any(r['kind'] == 'pods' and r['object']['metadata']['uid'] == uid and ts(r['received']) <= held_end and fault(r['object'], image) for r in rows)
        if image:
            assert witnesses[0]['spec']['containers'][0]['image'] == '127.0.0.1:1/rollout-runner-missing:case-'+str(n)
            b = yaml(p/'step-01-server.yaml'); cserver = yaml(p/'step-02-server.yaml')
            assert b['metadata']['generation'] < cserver['metadata']['generation']
            expected = 'C'
        proof.update(kind='actual ImagePullBackOff then accepted pullable C' if image else 'actual Running container with failed readiness', uid=uid)
    # Reconstruct the full held interval, including state at its beginning.
    state = replay(rows, held['stableSince'])
    def held_state():
        candidates = mine(state, 'pods', owner)
        if n >= 425:
            obj = candidates[uid]; assert obj['metadata'].get('deletionTimestamp') and obj['metadata'].get('finalizers')
        elif (n-401) % 4 == 2:
            assert uid in candidates and fault(candidates[uid]), 'withdrawn B did not remain Running/NotReady'
        else:
            # Image retries may alternate ErrImagePull/ImagePullBackOff;
            # the checkpoint proves BackOff, the whole window must stay unready.
            assert uid in candidates and not ready(candidates[uid]) and not candidates[uid]['metadata'].get('deletionTimestamp')
            if (n-401) % 4 == 1: assert fault(candidates[uid])
    held_state()
    for row in rows:
        if not held_start < ts(row['received']) <= held_end: continue
        obj = row['object']; key = obj['metadata']['uid']
        if row['event'] == 'DELETED': state[row['kind']].pop(key, None)
        else: state[row['kind']][key] = obj
        held_state()
    # W=0 fixtures make a complete frontend unit exactly one actual Ready Pod.
    state = collections.defaultdict(dict); destructive = []
    budget = config['top'] if mode == 'SG' else config['roles']['f']
    for row in rows:
        obj = row['object']; key = obj['metadata']['uid']; kind = row['kind']
        if kind == 'pods' and owned(obj, owner) and obj['metadata']['labels'][R] == 'frontend':
            old = state[kind].get(key)
            if old and ready(old) and (row['event'] == 'DELETED' or obj['metadata'].get('deletionTimestamp')):
                available = sum(owned(o, owner) and o['metadata']['labels'][R] == 'frontend' and ready(o) for o in state[kind].values())
                assert available-1 >= 3-budget['u'], 'healthy deletion exceeds current actual readiness budget'
                destructive.append({'sequence': row['sequence'], 'uid': key, 'readyBefore': available, 'minimum': 3-budget['u']})
        if row['event'] == 'DELETED': state[kind].pop(key, None)
        else: state[kind][key] = obj
        if ts(finalcp['stableSince']) <= ts(row['received']) <= ts(finalcp['completed']):
            final_pods(state, owner, mode, expected, baseline)
    final = yaml(p/'final-resources.yaml'); finalpods = final_pods(final, owner, mode, expected, baseline)
    final_pods(replay(rows, finalcp['stableSince']), owner, mode, expected, baseline)
    groups = {p['metadata']['labels'][G] for p in finalpods.values()}
    assert {o['metadata']['name'] for o in mine(final, 'podgroups', owner).values()} == groups
    assert not mine(final, 'services', owner), 'W=0 fixture leaves an unexpected headless Service'
    cms = mine(final, 'configmaps', owner)
    assert len(cms) == len(finalpods), 'ranktable count mismatch'
    for cm in cms.values():
        lab = cm['metadata']['labels']
        members = [p for p in finalpods.values() if all(p['metadata']['labels'].get(k) == lab.get(k) for k in (G, R, I))]
        assert len(members) == 1, 'orphan or ambiguously owned ranktable'
    return {'id': result['id'], 'classification': 'PASS_INDEPENDENT_FAULT_AND_FINAL_CHECK', 'watchRows': len(rows), 'faultProof': proof, 'healthyDeletionChecks': destructive, 'finalVersion': expected, 'finalReadyPods': len(finalpods), 'finalStableNanos': finalcp['elapsedStableNanos'], 'sameControllerProcess': True, 'limitation': 'Independent actual fault, healthy deletion budget, continuous final Pods and final resource ownership checks supplement runner order, revision, PodGroup content and plugin data checks.', 'evidenceSHA256': {name: hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json', 'observations.jsonl', 'final-resources.yaml']}}


def main():
    runid = sys.argv[1]; assert runid and '/' not in runid and '..' not in runid
    base = ROOT/'artifacts'/runid; control = ROOT/'artifacts/environment-022'/(runid+'-control')
    done = read(control/'completion.json'); build = read(control/'build.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and not done['proxyErrors']
    assert read(base/'environment.json')['runner']['binarySHA256'] == build['binarySHA256']
    results = read(base/'summary.json')['results']; expected = [f'RUN-{n}' for n in range(402, 431) if n >= 425 or (n-401) % 4 != 0]
    assert [r['id'] for r in results] == expected
    out = base/'independent-midrollout-audit'; out.mkdir()
    reports = []
    for result in results:
        try: report = audit_case(base/result['id'])
        except (AssertionError, KeyError, StopIteration) as err:
            report = {'id': result['id'], 'classification': 'PENDING_REVIEW', 'rawStatus': result['status'], 'rawError': result.get('error'), 'auditError': repr(err)}
        with (out/(result['id']+'.json')).open('x') as f: json.dump(report, f, indent=2); f.write('\n')
        reports.append(report); print(report['id'], report['classification'], report.get('auditError', ''))
    pending = any(r['classification'] == 'PENDING_REVIEW' for r in reports)
    with (out/'summary.json').open('x') as f: json.dump({'status': 'PENDING_REVIEW' if pending else 'VERIFIED', 'cases': reports, 'counts': dict(collections.Counter(r['classification'] for r in reports))}, f, indent=2); f.write('\n')
    if pending: sys.exit(1)


if __name__ == '__main__': main()
