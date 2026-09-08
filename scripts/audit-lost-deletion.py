#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Verify actual old-UID deletion frames are lost throughout same-process recovery."""
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
    result = m.read(p/'result.json'); case = m.yaml(p/'case.yaml'); owner = m.yaml(p/'before-server.yaml')['metadata']['uid']
    assert result['status'] == 'PASS' and result['id'] == case['id'] == case['scenario']['source']['id']
    assert case['baseline'] == '538b2825c06bc1e8c5392d18f18f84faee9fca95'
    config = case['scenario']['source']['config']; mode = config['mode']; budget = config['top'] if mode == 'SG' else config['roles']['f']
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1,len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    cp = next(m.read(x) for x in p.glob('checkpoint-*.json') if m.read(x)['phase'] == case['scenario']['steps'][0]['name'])
    assert cp['elapsedStableNanos'] >= 30_000_000_000
    scope = m.read(p/'step-01-notification-scope.json'); installed = m.read(p/'step-01-notification-installed.json')
    beforeclear = m.read(p/'step-01-notification-before-clear.json'); rule = next(r for r in beforeclear['rules'] if r['id'] == installed['id'])
    receipt = m.read(p/'step-01-request-time.json'); actualafter = m.read(p/'step-01-notification-real-api-after.json')
    controller = m.yaml(p/'step-01-controller-before.yaml'); endcontroller = m.yaml(p/'fault-controller-after.yaml')
    assert controller['metadata']['uid'] == scope['controllerUID'] == endcontroller['metadata']['uid']
    assert controller['status']['containerStatuses'][0]['restartCount'] == endcontroller['status']['containerStatuses'][0]['restartCount']
    args = controller['spec']['containers'][0]['args']
    assert not any(a.startswith('--modelserving-audit-') for a in args), 'this run must use unmodified production default audit flags'
    assert scope['auditPeriodSeconds'] == 300 and scope['auditTimeoutSeconds'] == 30 and scope['runnerWindowSeconds'] == 660
    assert installed['ownerUID'] == scope['ownerUID'] == owner and installed['uids'] == scope['faultUIDs']
    assert len(scope['members']) == 1 and installed['mode'] == 'drop-deletion' and installed['count'] == -1
    assert rule['active'] and not rule.get('endReason') and rule['hits'] >= 2
    assert m.ts(installed['installed']) < m.ts(receipt['sent']) < m.ts(receipt['received']) < m.ts(cp['completed'])
    assert m.ts(cp['completed']) <= m.ts(actualafter['received']) < m.ts(installed['expires'])
    old = scope['members'][0]; uid = old['metadata']['uid']; lab = old['metadata']['labels']
    assert [uid] == scope['faultUIDs'] and m.ready(old) and m.version(old) == 'A' and lab[m.E] == 'true'
    assert int(lab[m.G if mode == 'SG' else m.I].rsplit('-',1)[1]) == 2
    before = m.yaml(p/'step-01-notification-pods-before.yaml')['items']; assert any(o['metadata']['uid'] == uid for o in before)
    drop = [t for t in trace if t.get('ruleID') == installed['id'] and t['action'] == 'drop-deletion-event']
    assert len(drop) == rule['hits'] and all(t['uid'] == uid and t['namespace'] == result['namespace'] and t['name'] == old['metadata']['name'] for t in drop)
    assert {'MODIFIED','DELETED'} <= {t['event'] for t in drop}
    dropped_end = next(t for t in drop if t['event'] == 'DELETED')
    assert all(m.ts(receipt['sent']) < m.ts(t['at']) < m.ts(cp['completed']) for t in drop)
    assert not any(t.get('uid') == uid and t['action'] == 'forward-event' and m.ts(t['at']) >= m.ts(drop[0]['at']) and m.ts(t['at']) <= m.ts(actualafter['received']) for t in trace)
    direct = [r for r in rows if r['kind'] == 'pods' and r['object']['metadata']['uid'] == uid]
    assert any(r['event'] == 'MODIFIED' and r['object']['metadata'].get('deletionTimestamp') for r in direct)
    deleted = [r for r in direct if r['event'] == 'DELETED']; assert len(deleted) == 1
    assert m.ts(deleted[0]['received']) < m.ts(cp['stableSince'])
    after = actualafter['oldMembers']; assert len(after) == 1 and after[0]['oldUID'] == uid and after[0]['currentUID'] != uid and after[0]['directWatchAbsent']
    clear = next(t for t in trace if t.get('ruleID') == installed['id'] and t['action'] == 'rule-cleared')
    assert m.ts(clear['at']) > m.ts(actualafter['received'])
    baseline = m.mine(m.yaml(p/'baseline-resources.yaml'),'pods',owner); state = collections.defaultdict(dict); removals = []
    for row in rows:
        obj = row['object']; kind = row['kind']; key = obj['metadata']['uid']; previous = state[kind].get(key)
        if kind == 'pods' and m.owned(obj,owner) and obj['metadata']['labels'][m.R] == 'frontend' and previous and m.ready(previous) and (row['event'] == 'DELETED' or obj['metadata'].get('deletionTimestamp')):
            available = sum(m.owned(o,owner) and o['metadata']['labels'][m.R] == 'frontend' and m.ready(o) for o in state[kind].values())
            assert available-1 >= 3-budget['u']
            removals.append({'sequence':row['sequence'],'uid':key,'readyBefore':available,'minimum':3-budget['u']})
        if row['event'] == 'DELETED': state[kind].pop(key,None)
        else: state[kind][key] = obj
        if m.ts(cp['stableSince']) <= m.ts(row['received']) <= m.ts(cp['completed']): m.final_pods(state,owner,mode,'B',baseline)
    m.final_pods(m.replay(rows,cp['stableSince']),owner,mode,'B',baseline)
    final = m.yaml(p/'final-resources.yaml'); pods = m.final_pods(final,owner,mode,'B',baseline)
    assert not m.mine(final,'services',owner)
    assert {o['metadata']['name'] for o in m.mine(final,'podgroups',owner).values()} == {o['metadata']['labels'][m.G] for o in pods.values()}
    cms = m.mine(final,'configmaps',owner); assert len(cms) == len(pods)
    for cm in cms.values(): assert sum(all(o['metadata']['labels'].get(k) == cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I)) for o in pods.values()) == 1
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id':result['id'],'classification':'PASS_INDEPENDENT_LOST_NOTIFICATION_CHECK','watchRows':len(rows),'oldUID':uid,'droppedFrames':drop,'directDeletedSequence':deleted[0]['sequence'],'actualOldAbsentProof':after,'sameControllerProcess':True,'defaultAuditPeriodSeconds':300,'finalStableNanos':cp['elapsedStableNanos'],'recoveryNanosAfterDroppedDelete':m.ts(cp['stableSince'])-m.ts(dropped_end['at']),'healthyRemovalChecks':removals,'limitation':'Actual notification loss, real Pod deletion, same-process autonomous convergence and resource association verified. Earlier legal live checks may recover before the periodic 300s audit; no fixed five-minute delay or unique internal recovery cause is claimed. Runner also checks revision, order and plugin content.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml','step-01-notification-before-clear.json']}}


def main():
    runid = sys.argv[1]; assert runid == 'lost-deletion-r1'
    base = ROOT/'artifacts'/runid; control = ROOT/'artifacts/environment-022'/(runid+'-control')
    done = m.read(control/'completion.json'); build = m.read(control/'build.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256'] == build['binarySHA256']
    trace = [json.loads(line) for line in (control/'proxy-trace.jsonl').read_text().splitlines()]
    results = m.read(base/'summary.json')['results']; assert [r['id'] for r in results] == [f'RUN-{n}' for n in range(443,449)]
    out = base/'independent-lost-deletion-audit'; out.mkdir(); reports = []
    for result in results:
        report = audit_case(base/result['id'],trace)
        with (out/(result['id']+'.json')).open('x') as f:json.dump(report,f,indent=2);f.write('\n')
        reports.append(report);print(report['id'],report['classification'])
    with (out/'summary.json').open('x') as f:json.dump({'status':'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},f,indent=2);f.write('\n')


if __name__ == '__main__': main()
