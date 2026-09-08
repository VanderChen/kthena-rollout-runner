#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Audit real Eviction admission and subsequent rollout using direct API evidence."""
import collections
import hashlib
import importlib.util
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('mid_audit', ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader); loader.loader.exec_module(m)


def units(pods, owner, mode):
    front = [o for o in pods.values() if m.owned(o, owner) and o['metadata']['labels'][m.R] == 'frontend']
    groups = collections.defaultdict(list)
    for o in front:
        groups[o['metadata']['labels'][m.G] if mode == 'SG' else o['metadata']['name']].append(o)
    return {key for key, members in groups.items() if len(members) == (3 if mode == 'SG' else 1) and all(m.ready(o) for o in members)}


def final_pods(state, owner, mode, baseline):
    pods = m.mine(state, 'pods', owner)
    assert len(pods) == (9 if mode == 'SG' else 6) and all(m.ready(o) for o in pods.values())
    front = [o for o in pods.values() if o['metadata']['labels'][m.R] == 'frontend']
    assert len(front) == (9 if mode == 'SG' else 3) and all(m.version(o) == 'B' for o in front)
    assert len(units(pods, owner, mode)) == 3
    for uid, o in pods.items():
        assert o['metadata']['labels'][m.E] == 'true'
        if o['metadata']['labels'][m.R] == 'backend': assert uid in baseline and m.version(o) == 'A'
    return pods


def audit_case(p, logs):
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml'); owner = m.yaml(p/'before-server.yaml')['metadata']['uid']
    assert result['id'] in ('RUN-433', 'RUN-434') and result['status'] == 'PASS'
    assert case['id'] == result['id'] == case['scenario']['source']['id'] and case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    config = case['scenario']['source']['config']; mode = config['mode']
    assert config['roles']['f']['r'] == 3 and all(r['w'] == 0 for r in config['roles'].values())
    budget = config['top'] if mode == 'SG' else config['roles']['f']; assert budget['u'] == budget['s'] == 1 and budget['p'] == 0
    response = m.read(p/'step-02-eviction-response.json'); request = m.yaml(p/'step-02-eviction-request.yaml')
    uid = response['uid']; name = response['target']; ns = result['namespace']
    assert response['accepted'] and not response['error'] and response['httpStatus'] == 201
    assert json.loads(response['body'])['status'] == 'Success' and json.loads(response['body'])['code'] == 201
    assert request['apiVersion'] == 'policy/v1' and request['kind'] == 'Eviction' and request['deleteOptions']['preconditions']['uid'] == uid
    assert request['metadata'] == {'name': name, 'namespace': ns}
    webhook = m.yaml(p/'step-02-eviction-webhook.yaml'); assert len(webhook['webhooks']) == 1
    w = webhook['webhooks'][0]; assert w['failurePolicy'] == 'Fail' and w['namespaceSelector'] == {'matchLabels': {'rollout-runner/run': 'eviction-r1'}}
    assert w['clientConfig']['service'] == {'name': 'kthena-controller-manager-webhook', 'namespace': 'kthena-system', 'path': '/validate-eviction', 'port': 443} and w['clientConfig']['caBundle']
    assert w['rules'] == [{'apiGroups': [''], 'apiVersions': ['v1'], 'operations': ['CREATE'], 'resources': ['pods/eviction'], 'scope': 'Namespaced'}]
    server = m.yaml(p/'step-02-eviction-server.yaml'); assert server['metadata']['uid'] == owner and server['metadata']['generation'] >= 2
    assert server['spec']['rolloutStrategy']['evictionStrategy'] == {'protectionLevel': 'Role', 'roleMinAvailable': {'frontend': 1}}
    before = m.yaml(p/'step-02-fault-pods-before.yaml')['items']; target = next(o for o in before if o['metadata']['uid'] == uid)
    assert m.owned(target, owner) and m.ready(target) and m.version(target) == 'A' and target['metadata']['name'] == 'model-0-frontend-0-0'
    assert any(m.owned(o, owner) and m.ready(o) and m.version(o) == 'B' for o in before)
    scope = m.read(p/'step-02-fault-scope.json'); assert scope['recoveryUIDs'] == {uid: name} and scope['targetUID'] == uid and scope['ownerUID'] == owner
    tracker = m.yaml(p/'step-02-eviction-tracker.yaml'); trackeruid = tracker['metadata']['uid']
    assert m.owned(tracker, owner) and tracker['metadata']['name'] == 'kthena-eviction-tracker-model'
    entries = json.loads(tracker['data']['entries']); key = 'Role/'+ns+'/model/model-0/frontend/frontend-0'
    assert set(entries) == {key} and entries[key]['triggerPodUID'] == uid and entries[key]['triggerPodName'] == name
    expiry = m.ts(entries[key]['expiresAt']); assert m.ts(response['sent'])+60_000_000_000 <= expiry <= m.ts(response['received'])+60_000_000_000
    admission = [line for line in logs if ns in line and m.ts(response['sent']) <= m.ts(line.split(' ', 1)[0]) <= m.ts(response['received'])]
    decisions = [line for line in admission if 'Role eviction state' in line and 'targetReady=true' in line and 'minAvailable=1 allowed=true' in line]
    assert len(decisions) == 1 and any('Updated eviction disruption tracker' in line for line in admission)
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1, len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    deleted = [r for r in rows if r['kind'] == 'pods' and r['event'] == 'DELETED' and r['object']['metadata']['uid'] == uid]
    assert len(deleted) == 1 and m.ts(deleted[0]['received']) > m.ts(response['sent'])
    gone = m.read(p/'step-02-eviction-old-absent.json'); assert gone['oldUID'] == uid and gone['directWatchAbsent'] and (gone['notFound'] or gone['current']['metadata']['uid'] != uid)
    cp = next(m.read(x) for x in p.glob('checkpoint-*.json') if m.read(x)['phase'] == case['scenario']['steps'][-1]['name'])
    assert cp['elapsedStableNanos'] >= 30_000_000_000
    baseline = m.mine(m.yaml(p/'baseline-resources.yaml'), 'pods', owner)
    state = collections.defaultdict(dict); removals = []
    for row in rows:
        obj = row['object']; kind = row['kind']; objuid = obj['metadata']['uid']; prev = state[kind].get(objuid)
        if kind == 'pods' and m.owned(obj, owner) and obj['metadata']['labels'][m.R] == 'frontend' and prev and m.ready(prev) and (row['event'] == 'DELETED' or obj['metadata'].get('deletionTimestamp')):
            available = units(state[kind], owner, mode); unit = obj['metadata']['labels'][m.G] if mode == 'SG' else obj['metadata']['name']
            if unit in available:
                external = objuid == uid
                if not external: assert len(available)-1 >= 2, 'ordinary rollout deleted a healthy unit below latest readiness floor'
                removals.append({'sequence': row['sequence'], 'uid': objuid, 'readyUnitsBefore': len(available), 'externalEviction': external})
        if row['event'] == 'DELETED': state[kind].pop(objuid, None)
        else: state[kind][objuid] = obj
        if m.ts(cp['stableSince']) <= m.ts(row['received']) <= m.ts(cp['completed']): final_pods(state, owner, mode, baseline)
    assert sum(r['externalEviction'] for r in removals) == 1
    final_pods(m.replay(rows, cp['stableSince']), owner, mode, baseline)
    final = m.yaml(p/'final-resources.yaml'); pods = final_pods(final, owner, mode, baseline)
    assert not m.mine(final, 'services', owner)
    assert {o['metadata']['name'] for o in m.mine(final, 'podgroups', owner).values()} == {o['metadata']['labels'][m.G] for o in pods.values()}
    cms = m.mine(final, 'configmaps', owner); finaltracker = cms.pop(trackeruid)
    assert finaltracker['metadata']['name'] == tracker['metadata']['name'] and not finaltracker['metadata'].get('deletionTimestamp')
    assert json.loads(finaltracker['data']['entries']) == entries and len(cms) == len(pods)
    for cm in cms.values(): assert sum(all(o['metadata']['labels'].get(k) == cm['metadata']['labels'].get(k) for k in (m.G, m.R, m.I)) for o in pods.values()) == 1
    controllers = [m.yaml(p/name) for name in ('case-controller-before.yaml', 'step-02-controller-before.yaml', 'fault-controller-after.yaml')]
    assert len({o['metadata']['uid'] for o in controllers}) == 1 and len({o['status']['containerStatuses'][0]['restartCount'] for o in controllers}) == 1
    assert controllers[1]['spec']['containers'][0]['args'].count('--enable-eviction-webhook=true') == 1
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id': result['id'], 'classification': 'PASS_INDEPENDENT_EVICTION_CHECK', 'watchRows': len(rows), 'actualEviction': response, 'admissionDecisionLogs': decisions, 'trackerUID': trackeruid, 'oldDeletedSequence': deleted[0]['sequence'], 'healthyRemovalChecks': removals, 'finalReadyPods': len(pods), 'finalStableNanos': cp['elapsedStableNanos'], 'sameControllerProcess': True, 'limitation': 'Independent eviction budget and rollout budget are checked separately. The exact old UID is the sole external disruption exception. Source requires eventual B, without prescribing the first transient replacement version. The observed admission tracker is legitimate budget state; its lazy TTL data is not a source-required garbage collection assertion. Existing runner validates revision, order, plugin data and status.', 'evidenceSHA256': {name: hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json', 'observations.jsonl', 'final-resources.yaml', 'step-02-eviction-response.json', 'step-02-eviction-tracker.yaml']}}


def main():
    base = ROOT/'artifacts/eviction-r1'; control = ROOT/'artifacts/environment-022/eviction-r1-control'
    done = m.read(control/'completion.json'); build = m.read(control/'build.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and not done['proxyErrors']
    assert done['testEvictionWebhookRemoved'] and done['existingAdmissionWebhooksPreserved']
    assert m.read(base/'environment.json')['runner']['binarySHA256'] == build['binarySHA256']
    assert m.read(control/'controller-before.json')['spec'] == m.read(control/'controller-restored.json')['spec']
    before = m.read(control/'existing-webhooks-before.json'); after = m.read(control/'existing-webhooks-after.json')
    assert before == after
    logs = (control/'controller.log').read_text().splitlines()
    reports = [audit_case(base/('RUN-'+str(n)), logs) for n in (433, 434)]
    out = base/'independent-eviction-audit'; out.mkdir()
    for report in reports:
        with (out/(report['id']+'.json')).open('x') as f: json.dump(report, f, indent=2); f.write('\n')
        print(report['id'], report['classification'], report['watchRows'])
    with (out/'summary.json').open('x') as f: json.dump({'status': 'VERIFIED', 'cases': reports, 'counts': {'PASS': len(reports)}}, f, indent=2); f.write('\n')


if __name__ == '__main__': main()
