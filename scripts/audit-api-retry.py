#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Check actual API error, successful retry and direct continuous rollout evidence."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('mid_audit', ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader); loader.loader.exec_module(m)


def audit_case(p, trace):
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml')
    n = int(result['id'][4:]); assert 455 <= n <= 462
    assert result['id'] == case['id'] == case['scenario']['source']['id']
    assert case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    owner = m.yaml(p/'before-server.yaml')['metadata']['uid']; mode = case['scenario']['source']['config']['mode']
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1, len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    installed = m.read(p/'step-02-api-fault-installed.json'); ruleid = installed['id']
    beforeclear = m.read(p/'step-02-api-fault-before-clear.json')
    finalrule = next(r for r in beforeclear['rules'] if r['id'] == ruleid)
    trace = [t for t in trace if t['sequence'] <= m.read(p/'fault-proxy-final.json')['sequence']]
    hits = [t for t in trace if t.get('ruleID') == ruleid and t['action'] == 'error-request']
    assert installed['mode'] == 'error' and installed['count'] == 1 and installed['hits'] == 0 and installed['active']
    assert installed['namespace'] == result['namespace'] and installed['statusCode'] == 503
    if result['status'] == 'INCONCLUSIVE':
        assert n in (456, 460) and installed['collectionOnly'] and installed['methods'] == ['DELETE']
        assert finalrule['hits'] == 0 and not hits and 'exactly one actual API error' in result['error']
        named = [t for t in trace if t['action'] == 'request' and t.get('namespace') == result['namespace'] and t.get('resource') == 'pods' and t.get('method') == 'DELETE' and t.get('name')]
        assert named and not any(t['action'] == 'request' and t.get('namespace') == result['namespace'] and t.get('resource') == 'pods' and t.get('method') == 'DELETE' and not t.get('name') for t in trace)
        return {'id': result['id'], 'classification': 'RUNNER_INJECTION_MISS', 'watchRows': len(rows), 'actualNamedDeleteRequests': named, 'reason': 'Collection DELETE rule did not match production audit client native named Pod DELETE path; neither product failure nor successful scenario.'}
    assert result['status'] == 'PASS', 'raw failure needs independent classification'
    assert len(hits) == 1 and hits[0]['status'] == 503 and hits[0]['namespace'] == result['namespace']
    assert finalrule['hits'] == 1 and not finalrule['active'] and finalrule['endReason'] == 'count-exhausted' and finalrule['remaining'] == 0
    hit = hits[0]; assert hit['method'] in installed['methods']
    accepted = m.yaml(p/'step-02-fault-server.yaml')
    assert accepted['metadata']['uid'] == owner and accepted['metadata']['generation'] >= 2
    assert m.ts(installed['installed']) < m.ts(hit['at'])
    pods_before = m.yaml(p/'step-02-fault-pods-before.yaml')['items']
    front = [o for o in pods_before if m.owned(o, owner) and o['metadata']['labels'][m.R] == 'frontend']
    assert sum(m.version(o) == 'A' and m.ready(o) for o in front) == 3
    surge = [o for o in front if m.version(o) == 'B']; assert len(surge) == 1 and m.fault(surge[0])
    assert any(r['kind'] == 'modelservings' and r['object']['metadata']['uid'] == owner and r['object']['metadata'].get('generation') == accepted['metadata']['generation'] and m.ts(r['received']) < m.ts(hit['at']) for r in rows)
    operation = (n-455)%4
    if operation == 0:
        assert installed['ownerUID'] == owner and hit['method'] == 'POST' and hit['resource'] == 'pods'
        created = {r['object']['metadata']['uid'] for r in rows if r['kind'] == 'pods' and r['event'] == 'ADDED' and m.owned(r['object'], owner) and r['object']['metadata']['name'] == hit['name'] and m.version(r['object']) == 'B' and m.ts(r['received']) > m.ts(hit['at'])}
        assert len(created) == 1, 'error retry must create one actual B UID for failed name'
    elif operation == 1:
        target = m.yaml(p/'step-02-api-delete-target.yaml'); uid = target['metadata']['uid']
        assert installed['uids'] == [uid] and installed['name'] == target['metadata']['name'] and not installed.get('collectionOnly')
        assert hit['uid'] == uid and hit['name'] == target['metadata']['name'] and hit['method'] == 'DELETE' and m.owned(target, owner)
        assert m.ready(target) and m.version(target) == 'A'
        assert any(r['kind'] == 'pods' and r['event'] == 'DELETED' and r['object']['metadata']['uid'] == uid and m.ts(r['received']) > m.ts(hit['at']) for r in rows)
    elif operation == 2:
        assert hit['uid'] == installed['ownerUID'] == owner and hit['objectGeneration'] == installed['generation'] == accepted['metadata']['generation']
        assert hit['method'] == 'PUT' and hit['path'].endswith('/modelservings/model/status')
    else:
        assert installed['collectionOnly'] and hit['method'] == 'GET' and hit['path'].endswith('/controllerrevisions') and not hit.get('name')
        assert hit['labelSelector'] == 'modelserving.volcano.sh/name=model'
    retry = next(t for t in trace if t['sequence'] > hit['sequence'] and t['action'] == 'response' and t.get('method') == hit['method'] and t.get('path') == hit['path'] and t['status'] in (200, 201, 202))
    request = next(t for t in trace if t.get('request') == retry['request'] and t['action'] == 'request')
    assert request['sequence'] > hit['sequence']
    if operation == 3: assert request['detail'] == 'watch=false initialEvents=false' and request['labelSelector'] == hit['labelSelector']
    cp = next(m.read(x) for x in p.glob('checkpoint-*.json') if m.read(x)['phase'] == case['scenario']['steps'][-1]['name'])
    assert cp['elapsedStableNanos'] >= 30_000_000_000 and m.ts(cp['stableSince']) > m.ts(retry['at'])
    baseline = m.mine(m.yaml(p/'baseline-resources.yaml'), 'pods', owner)
    state = collections.defaultdict(dict); destructive = []; observed_new = collections.defaultdict(set)
    for row in rows:
        obj = row['object']; kind = row['kind']; uid = obj['metadata']['uid']
        if kind == 'pods' and m.owned(obj, owner):
            if row['event'] == 'ADDED' and m.version(obj) == 'B': observed_new[obj['metadata']['name']].add(uid)
            old = state[kind].get(uid)
            if old and m.ready(old) and (row['event'] == 'DELETED' or obj['metadata'].get('deletionTimestamp')) and obj['metadata']['labels'][m.R] == 'frontend':
                available = sum(m.owned(o, owner) and o['metadata']['labels'][m.R] == 'frontend' and m.ready(o) for o in state[kind].values())
                assert available-1 >= 3, 'healthy removal exceeds source U=0 capacity'
                destructive.append({'sequence': row['sequence'], 'uid': uid, 'readyBefore': available})
        if row['event'] == 'DELETED': state[kind].pop(uid, None)
        else: state[kind][uid] = obj
        if m.ts(cp['stableSince']) <= m.ts(row['received']) <= m.ts(cp['completed']): m.final_pods(state, owner, mode, 'B', baseline)
    assert all(len(uids) == 1 for uids in observed_new.values()), 'same B target churned to another UID'
    m.final_pods(m.replay(rows, cp['stableSince']), owner, mode, 'B', baseline)
    final = m.yaml(p/'final-resources.yaml'); pods = m.final_pods(final, owner, mode, 'B', baseline)
    assert not m.mine(final, 'services', owner)
    assert {o['metadata']['name'] for o in m.mine(final, 'podgroups', owner).values()} == {o['metadata']['labels'][m.G] for o in pods.values()}
    cms = m.mine(final, 'configmaps', owner); assert len(cms) == len(pods)
    for cm in cms.values(): assert sum(all(o['metadata']['labels'].get(k) == cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I)) for o in pods.values()) == 1
    oldcontroller = m.yaml(p/'case-controller-before.yaml'); newcontroller = m.yaml(p/'fault-controller-after.yaml')
    assert oldcontroller['metadata']['uid'] == newcontroller['metadata']['uid'] and oldcontroller['status']['containerStatuses'][0]['restartCount'] == newcontroller['status']['containerStatuses'][0]['restartCount']
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id': result['id'], 'classification': 'PASS_INDEPENDENT_API_RETRY_CHECK', 'watchRows': len(rows), 'actualError': hit, 'actualSuccessfulRetry': retry, 'healthyRemovalChecks': destructive, 'finalStableNanos': cp['elapsedStableNanos'], 'finalReadyPods': len(pods), 'sameControllerProcess': True, 'limitation': 'Trace binds injected operation and subsequent successful API response; direct Watch proves actual Pod UID behavior, healthy capacity and final stability. Runner retains order, history, plugin content checks. Trace does not store successful request bodies.', 'evidenceSHA256': {name: hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml','step-02-api-fault-before-clear.json']}}


def main():
    runid = sys.argv[1]; assert runid in ('api-retry-r1', 'api-delete-r3')
    base = ROOT/'artifacts'/runid; control = ROOT/'artifacts/environment-022'/(runid+'-control')
    done = m.read(control/'completion.json'); build = m.read(control/'build.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256'] == build['binarySHA256']
    trace = [json.loads(line) for line in (control/'proxy-trace.jsonl').read_text().splitlines()]
    results = m.read(base/'summary.json')['results']; assert [r['id'] for r in results] == ([f'RUN-{n}' for n in range(455, 463)] if runid == 'api-retry-r1' else ['RUN-456', 'RUN-460'])
    out = base/'independent-api-audit'; out.mkdir(); reports = []
    for result in results:
        report = audit_case(base/result['id'], trace)
        with (out/(result['id']+'.json')).open('x') as f: json.dump(report,f,indent=2);f.write('\n')
        reports.append(report); print(report['id'],report['classification'])
    with (out/'summary.json').open('x') as f: json.dump({'status':'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},f,indent=2);f.write('\n')


if __name__ == '__main__': main()
