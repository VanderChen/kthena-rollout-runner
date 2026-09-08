#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Require real cleanup-hook failures plus resource lifecycle correctness."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('mid_audit', ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader); loader.loader.exec_module(m)


def audit_case(p, trace, logs):
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml'); owner = m.yaml(p/'before-server.yaml')['metadata']['uid']
    n = int(result['id'][4:]); assert 536 <= n <= 539 and result['status'] == 'PASS'
    assert case['id'] == result['id'] == case['scenario']['source']['id'] and case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    mode = case['scenario']['source']['config']['mode']; assert all(r['w'] == 0 for r in case['scenario']['source']['config']['roles'].values())
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1,len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    cp = next(m.read(x) for x in p.glob('checkpoint-*.json') if m.read(x)['phase'] == case['scenario']['steps'][-1]['name'])
    assert cp['elapsedStableNanos'] >= 30_000_000_000
    # Namespace teardown is outside every semantic scenario assertion.
    trace = [t for t in trace if t['sequence'] <= m.read(p/'fault-proxy-final.json')['sequence'] and m.ts(t['at']) <= m.ts(cp['completed'])]
    logs = [line for line in logs if result['namespace'] in line and m.ts(line.split(' ',1)[0]) <= m.ts(cp['completed'])]
    installed = m.read(p/'step-02-plugin-installed.json'); saved = m.read(p/'step-02-plugin-before-clear.json')
    rule = next(r for r in saved['rules'] if r['id'] == installed['id'])
    assert rule['hits'] == 2 and not rule['active'] and rule['endReason'] == 'count-exhausted' and rule['remaining'] == 0
    assert installed['count'] == 2 and installed['statusCode'] == 503 and installed['hits'] == 0 and installed['active']
    hits = [t for t in trace if t.get('ruleID') == installed['id'] and t['action'] == 'error-request']; assert len(hits) == 2
    assert all(t['status'] == 503 and t['namespace'] == result['namespace'] for t in hits)
    old = m.yaml(p/'step-02-plugin-old-entry.yaml'); uid = old['metadata']['uid']; assert m.owned(old,owner) and m.ready(old) and m.version(old) == 'A'
    assert int(old['metadata']['labels'][m.G if mode == 'SG' else m.I].rsplit('-',1)[1]) == 2
    before = m.yaml(p/'step-02-fault-pods-before.yaml')['items']
    front = [o for o in before if m.owned(o,owner) and o['metadata']['labels'][m.R] == 'frontend']
    assert sum(m.version(o) == 'A' and m.ready(o) for o in front) == 3
    surge = [o for o in front if m.version(o) == 'B']; assert len(surge) == 1 and m.fault(surge[0])
    accepted = m.yaml(p/'step-02-fault-server.yaml'); assert accepted['metadata']['uid'] == owner and accepted['metadata']['generation'] >= 2
    assert m.ts(installed['installed']) < m.ts(hits[0]['at']) < m.ts(hits[1]['at']) < m.ts(cp['stableSince'])
    plugin = 'headless-service' if n in (536,538) else 'ranktable'
    errors = [line for line in logs if 'plugin '+plugin+' OnRoleDelete:' in line and 'injected external controller API failure' in line]
    assert len(errors) >= 2 and all(any(m.ts(hit['at']) <= m.ts(line.split(' ',1)[0]) <= m.ts(hit['at'])+1_000_000_000 for line in errors) for hit in hits)
    if plugin == 'headless-service':
        absent = m.read(p/'step-02-plugin-service-before.json'); assert absent['notFound'] and absent['name'] == old['metadata']['name']
        assert installed['resource'] == 'services' and installed['methods'] == ['GET'] and installed['name'] == absent['name']
        assert all(t['path'].endswith('/services/'+absent['name']) for t in hits)
        recovered = next(t for t in trace if t['sequence'] > hits[-1]['sequence'] and t['action'] == 'response' and t.get('method') == 'GET' and t.get('path') == hits[-1]['path'] and t.get('status') == 404)
        cleanup = {'resourceExistedInitially':False,'actualHook':'OnRoleDelete','successfulAbsentLookup':recovered,'limitation':'Source W=0 creates no headless Service; proves failure retry and absence, not deletion of a previously existing Service.'}
    else:
        cm = m.yaml(p/'step-02-plugin-configmap-before.yaml'); cmuid = cm['metadata']['uid']
        assert m.owned(cm,owner) and installed['uids'] == [cmuid]
        assert all(t['method'] == 'DELETE' and t['uid'] == cmuid and t['name'] == cm['metadata']['name'] for t in hits)
        recovered = next(t for t in trace if t['sequence'] > hits[-1]['sequence'] and t['action'] == 'response' and t.get('method') == 'DELETE' and t.get('path') == hits[-1]['path'] and t.get('status') == 200)
        directdelete = next(r for r in rows if r['kind'] == 'configmaps' and r['event'] == 'DELETED' and r['object']['metadata']['uid'] == cmuid)
        assert m.ts(directdelete['received']) > m.ts(hits[-1]['at'])
        cleanup = {'resourceExistedInitially':True,'oldConfigMapUID':cmuid,'actualHook':'OnRoleDelete','successfulDelete':recovered,'directDeletedSequence':directdelete['sequence']}
    # A replacement for the target name cannot precede successful cleanup.
    replacements = [r for r in rows if r['kind'] == 'pods' and r['event'] == 'ADDED' and m.owned(r['object'],owner) and r['object']['metadata']['name'] == old['metadata']['name'] and m.version(r['object']) == 'B']
    assert len(replacements) == 1 and m.ts(replacements[0]['received']) > m.ts(recovered['at'])
    replacementuid = replacements[0]['object']['metadata']['uid']
    assert not any(r['kind'] == 'pods' and r['object']['metadata']['uid'] == replacementuid and (r['event'] == 'DELETED' or r['object']['metadata'].get('deletionTimestamp')) for r in rows)
    baseline = m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner); state = collections.defaultdict(dict); removals = []; names = collections.defaultdict(set)
    for row in rows:
        obj = row['object']; kind = row['kind']; key = obj['metadata']['uid']; prev = state[kind].get(key)
        if kind == 'pods' and m.owned(obj,owner):
            if row['event'] == 'ADDED' and m.version(obj) == 'B': names[obj['metadata']['name']].add(key)
            if obj['metadata']['labels'][m.R] == 'frontend' and prev and m.ready(prev) and (row['event'] == 'DELETED' or obj['metadata'].get('deletionTimestamp')):
                available = sum(m.owned(o,owner) and o['metadata']['labels'][m.R] == 'frontend' and m.ready(o) for o in state[kind].values()); assert available-1 >= 3
                removals.append({'sequence':row['sequence'],'uid':key,'readyBefore':available})
        if row['event'] == 'DELETED': state[kind].pop(key,None)
        else: state[kind][key] = obj
        if m.ts(cp['stableSince']) <= m.ts(row['received']) <= m.ts(cp['completed']): m.final_pods(state,owner,mode,'B',baseline)
    assert all(len(uids) == 1 for uids in names.values())
    m.final_pods(m.replay(rows,cp['stableSince']),owner,mode,'B',baseline)
    final = m.yaml(p/'final-resources.yaml'); pods = m.final_pods(final,owner,mode,'B',baseline)
    assert not m.mine(final,'services',owner)
    assert {o['metadata']['name'] for o in m.mine(final,'podgroups',owner).values()} == {o['metadata']['labels'][m.G] for o in pods.values()}
    cms = m.mine(final,'configmaps',owner); assert len(cms) == len(pods)
    for cm in cms.values(): assert sum(all(o['metadata']['labels'].get(k) == cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I)) for o in pods.values()) == 1
    beforecontroller = m.yaml(p/'case-controller-before.yaml'); aftercontroller = m.yaml(p/'fault-controller-after.yaml')
    assert beforecontroller['metadata']['uid'] == aftercontroller['metadata']['uid'] and beforecontroller['status']['containerStatuses'][0]['restartCount'] == aftercontroller['status']['containerStatuses'][0]['restartCount']
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id':result['id'],'classification':'PASS_INDEPENDENT_PLUGIN_CLEANUP_CHECK','watchRows':len(rows),'actualErrors':hits,'hookFailureLogs':errors,'cleanupProof':cleanup,'replacementPodUID':replacementuid,'healthyRemovalChecks':removals,'finalStableNanos':cp['elapsedStableNanos'],'sameControllerProcess':True,'limitation':'Actual hook errors and subsequent cleanup precede target recreation. Final Pod stability and resource ownership verified; controller internal datastore transitions are not directly snapshotted. Existing runner checks revision, order, plugin data and final status.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml','step-02-plugin-before-clear.json']}}


def main():
    runid = sys.argv[1]; assert runid == 'plugin-retry-r1'
    base = ROOT/'artifacts'/runid; control = ROOT/'artifacts/environment-022'/(runid+'-control')
    done = m.read(control/'completion.json'); build = m.read(control/'build.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256'] == build['binarySHA256']
    trace = [json.loads(line) for line in (control/'proxy-trace.jsonl').read_text().splitlines()]; logs = (control/'controller.log').read_text().splitlines()
    results = m.read(base/'summary.json')['results']; assert [r['id'] for r in results] == [f'RUN-{n}' for n in range(536,540)]
    out = base/'independent-plugin-audit'; out.mkdir(); reports = []
    for result in results:
        report = audit_case(base/result['id'],trace,logs)
        with (out/(result['id']+'.json')).open('x') as f:json.dump(report,f,indent=2);f.write('\n')
        reports.append(report);print(report['id'],report['classification'])
    with (out/'summary.json').open('x') as f:json.dump({'status':'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},f,indent=2);f.write('\n')


if __name__ == '__main__': main()
