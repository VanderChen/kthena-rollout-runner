#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Independently check actual failed history writes, live references and B/C recovery."""
import collections
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('mid_audit', ROOT/'scripts/audit-midrollout.py')
m = importlib.util.module_from_spec(loader); loader.loader.exec_module(m)
REV = 'modelserving.volcano.sh/revision'


def frontend(state, owner):
    return {uid: o for uid, o in m.mine(state, 'pods', owner).items() if o['metadata']['labels'][m.R] == 'frontend'}


def final_pods(state, owner, baseline, partition, version):
    pods = m.mine(state, 'pods', owner)
    assert len(pods) == 6 and all(m.ready(o) for o in pods.values())
    front = frontend(state, owner); assert len(front) == 3
    protected = sum(o['metadata']['labels'][m.R]=='frontend' and int(o['metadata']['labels'][m.I].rsplit('-',1)[1])<partition for o in baseline.values())
    expected = collections.Counter(['A']*protected + [version]*(3-protected))
    assert collections.Counter(m.version(o) for o in front.values()) == expected
    for uid, o in pods.items():
        lab = o['metadata']['labels']; assert lab.get(m.E) == 'true'
        if lab[m.R] == 'backend' or int(lab[m.I].rsplit('-', 1)[1]) < partition:
            assert uid in baseline and m.version(o) == 'A'
    return pods


def audit_case(p, trace):
    case = m.yaml(p/'case.yaml'); result = m.read(p/'result.json'); original = m.yaml(p/'before-server.yaml'); owner = original['metadata']['uid']
    n = int(case['id'][4:]); assert 463 <= n <= 522 and result['status'] == 'PASS'
    sparse = (n-463)%10 >= 5
    assert case['scenario'].get('fixture','') == ('sparse-history-A' if sparse else '')
    if sparse: original = m.yaml(p/'source-initial-server.yaml')
    sourcepath = ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json'
    raw = sourcepath.read_bytes(); assert hashlib.sha256(raw).hexdigest() == '757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
    assert case['scenario']['source'] == next(r for r in json.loads(raw)['cases'] if r['id'] == case['id'])
    source = case['scenario']['source']; config = source['config']; partition = config['roles']['f']['p']; minimum = 3-config['roles']['f']['u']
    assert ('O={0,3,4}' if sparse else 'O={0,1,2}') in source['initial']
    initial = m.yaml(p/'before.yaml')['spec']; requested = m.yaml(p/'step-01-request.yaml')['spec']; bserver = m.yaml(p/'step-01-server.yaml'); cserver = m.yaml(p/'step-02-server.yaml')
    limit = config.get('revisionHistoryLimit', 'omitted')
    for request in (initial, requested, m.yaml(p/'step-02-request.yaml')['spec']):
        assert request.get('revisionHistoryLimit', 'omitted') == limit
    for server in (original, bserver, cserver):
        assert server['spec']['revisionHistoryLimit'] == (10 if limit == 'omitted' else limit)
    assert bserver['metadata']['generation'] == original['metadata']['generation']+1 and cserver['metadata']['generation'] == original['metadata']['generation']+2
    b = {r['name']:r for r in bserver['spec']['template']['roles']}; c = {r['name']:r for r in cserver['spec']['template']['roles']}
    assert b['backend'] == c['backend']
    assert m.version(b['frontend']['entryTemplate']) == 'B' and m.version(c['frontend']['entryTemplate']) == 'C'
    rows = [json.loads(line) for line in (p/'observations.jsonl').read_text().splitlines()]
    assert [r['sequence'] for r in rows] == list(range(1,len(rows)+1)) and not any(r['event'] == 'GAP' for r in rows)
    preparation = None
    if sparse:
        loader = importlib.util.spec_from_file_location('sparse_fixture',ROOT/'scripts/audit-history-sparse-fixture.py')
        fixture = importlib.util.module_from_spec(loader); loader.loader.exec_module(fixture)
        preparation = fixture.audit(p,rows)
    baseline = m.yaml(p/'baseline-resources.yaml'); basepods = m.mine(baseline,'pods',owner); basecr = m.mine(baseline,'controllerrevisions',owner)
    assert len(basepods) == 6 and len(basecr) == 1 and all(m.ready(o) and m.version(o) == 'A' for o in basepods.values())
    assert {int(o['metadata']['labels'][m.I].rsplit('-',1)[1]) for o in frontend(baseline,owner).values()} == ({0,3,4} if sparse else {0,1,2})
    installed = m.read(p/'step-01-create-installed.json'); rid = installed['id']
    assert installed['resource'] == 'controllerrevisions' and installed['namespace'] == result['namespace'] and installed['ownerUID'] == owner and installed['methods'] == ['POST']
    assert installed['mode'] == 'error' and installed['statusCode'] == 503 and installed['count'] == -1
    saved = m.read(p/'step-01-before-create-clear.json'); rule = next(r for r in saved['rules'] if r['id'] == rid)
    assert rule['active'] and rule['hits'] > 0 and not saved['errors']
    scope = [t for t in trace if t.get('ruleID') == rid]
    injected = [t for t in scope if t['action'] == 'error-request']; cleared = [t for t in scope if t['action'] == 'rule-cleared']
    assert len(injected) == rule['hits'] and len(cleared) == 1
    clear = m.ts(cleared[0]['at']); assert all(t['status'] == 503 and t['method'] == 'POST' and t['resource'] == 'controllerrevisions' and t['namespace'] == result['namespace'] for t in injected)
    checkpoints = [m.read(x) for x in p.glob('checkpoint-*.json')]
    held = next(cp for cp in checkpoints if cp['phase'] == 'actual-CR-create-failure-no-template-actions')
    bcp = next(cp for cp in checkpoints if cp['phase'] == 'B-allowed-target-after-history-recovery')
    ccp = next(cp for cp in checkpoints if cp['phase'] == 'C-allowed-target-after-old-release')
    finalcp = next(cp for cp in checkpoints if cp['phase'] == 'C-retained-after-controller-restart')
    assert held['elapsedStableNanos'] >= 10_000_000_000 and m.ts(injected[0]['at']) < m.ts(held['stableSince']) < m.ts(held['completed']) < clear
    assert finalcp['elapsedStableNanos'] >= 30_000_000_000
    assert m.ts(bcp['completed']) < m.ts(m.read(p/'step-02-request-time.json')['sent'])
    # API creation timestamps have only second precision. Prove the strict
    # persist-before-mutation order using actual upstream response/request
    # nanosecond timestamps, independently of informer delivery ordering.
    bsent = m.ts(m.read(p/'step-01-request-time.json')['sent'])
    csent = m.ts(m.read(p/'step-02-request-time.json')['sent'])
    responses = {t['request']:t for t in trace if t['action']=='response'}
    persistence = []
    for version, begin, end in [('B',bsent,csent),('C',csent,m.ts(finalcp['completed']))]:
        requests = [t for t in trace if t['action']=='request' and t.get('namespace')==result['namespace'] and begin<=m.ts(t['at'])<end]
        created = [responses[t['request']] for t in requests if t['resource']=='controllerrevisions' and t['method']=='POST' and responses.get(t['request'],{}).get('status')==201]
        assert len(created)==1
        mutations = [t for t in requests if t['resource']=='pods' and t['method'] in ('POST','DELETE')]
        assert all(m.ts(t['at'])>m.ts(created[0]['at']) for t in mutations)
        persistence.append({'version':version,'persistedResponse':created[0],'laterPodMutationRequests':len(mutations)})
    def preserved(state):
        pods = m.mine(state,'pods',owner); crs = m.mine(state,'controllerrevisions',owner)
        assert set(pods) == set(basepods) and all(m.ready(o) and m.version(o) == 'A' for o in pods.values())
        assert set(crs) == set(basecr) and all(o['data'] == basecr[u]['data'] for u,o in crs.items())
    preserved(m.yaml(p/'step-01-create-blocked-resources.yaml'))
    state = collections.defaultdict(dict); deletions = []; historydata = {}; revisioncreates = {}; createdpods = []
    for row in rows:
        o = row['object']; uid = o['metadata']['uid']; kind = row['kind']; at = m.ts(row['received']); prev = state[kind].get(uid)
        if kind == 'controllerrevisions' and m.owned(o,owner):
            assert uid not in historydata or historydata[uid] == o['data']; historydata[uid] = o['data']
            if row['event'] == 'ADDED': revisioncreates[o['metadata']['name']] = o
        if kind == 'pods' and m.owned(o,owner):
            if row['event'] == 'ADDED': createdpods.append(o)
            if (not sparse or row['sequence'] > preparation['lastPreparationSequence']) and prev and m.ready(prev) and (row['event'] == 'DELETED' or o['metadata'].get('deletionTimestamp')):
                if o['metadata']['labels'][m.R] == 'frontend':
                    available = sum(m.ready(pod) for pod in frontend(state,owner).values()); assert available-1 >= minimum
                    deletions.append({'sequence':row['sequence'],'uid':uid,'readyBefore':available,'minimum':minimum})
                assert uid not in basepods or o['metadata']['labels'][m.R] == 'frontend' and int(o['metadata']['labels'][m.I].rsplit('-',1)[1]) >= partition
        if row['event'] == 'DELETED': state[kind].pop(uid,None)
        else: state[kind][uid] = o
        if m.ts(installed['installed']) <= at < clear: preserved(state)
        if m.ts(finalcp['stableSince']) <= at <= m.ts(finalcp['completed']): final_pods(state,owner,basepods,partition,'C')
    for o in createdpods:
        cr = revisioncreates['model-'+o['metadata']['labels'][REV]]
        assert m.ts(cr['metadata']['creationTimestamp']) <= m.ts(o['metadata']['creationTimestamp'])
    # Direct reads prove histories still exist even for Pods with a live
    # deletionTimestamp. Full Watch adds immutable data and creation ordering.
    probes = [m.read(x) for x in sorted(p.glob('history-probe-*.json'))]
    assert probes and all('error' not in probe for probe in probes)
    terminating = set()
    for probe in probes:
        for o in probe['pods']['items']:
            if not m.owned(o,owner): continue
            name = 'model-'+o['metadata']['labels'][REV]
            cr = probe['histories'].get(name)
            if cr is None:
                # The runner only accepts a raced GC read after confirming the
                # original Pod disappeared. Require its independent DELETE.
                assert any(r['kind']=='pods' and r['event']=='DELETED' and r['object']['metadata']['uid']==o['metadata']['uid'] and m.ts(r['received'])<=m.ts(probe['completed']) for r in rows)
                continue
            assert m.owned(cr,owner) and not cr['metadata'].get('deletionTimestamp')
            roles = {r['name']:r for r in cr['data']['data']}; assert m.version(roles[o['metadata']['labels'][m.R]]['entryTemplate']) == m.version(o)
            if o['metadata'].get('deletionTimestamp') and 'rollout-runner/hold' in o['metadata'].get('finalizers',[]): terminating.add(o['metadata']['uid'])
    trigger = m.read(p/'step-01-terminating-trigger.json')
    assert trigger['eligible'] == (partition < 3 or sparse)
    if partition < 3 or sparse:
        pinned = set(trigger['pinnedUIDs'].values()); assert len(pinned) == 1 and pinned <= terminating
        holdcp = next(cp for cp in checkpoints if cp['phase'] == 'C-after-allowed-B'); assert holdcp['elapsedStableNanos'] >= 10_000_000_000
        for uid in pinned:
            assert any(r['kind']=='pods' and r['event']=='DELETED' and r['object']['metadata']['uid']==uid and m.ts(r['received'])>m.ts(holdcp['completed']) for r in rows)
    else:
        assert not trigger['pinnedUIDs'] and not deletions
    final_pods(m.yaml(p/'step-01-B-allowed-resources.yaml'),owner,basepods,partition,'B')
    final = m.yaml(p/'final-resources.yaml'); finalpods = final_pods(final,owner,basepods,partition,'C')
    final_pods(m.replay(rows,finalcp['stableSince']),owner,basepods,partition,'C')
    before = m.yaml(p/'step-01-restart-controller-terminated.yaml'); replacement = m.yaml(p/'step-01-restart-controller-replacement.yaml'); after = m.yaml(p/'fault-controller-after.yaml')
    receipt = m.read(p/'step-01-restart-controller-delete.json')
    assert receipt['accepted'] and receipt['uid'] == before['metadata']['uid'] == receipt['options']['preconditions']['uid']
    assert after['metadata']['uid'] == replacement['metadata']['uid'] != before['metadata']['uid']
    assert before['metadata']['ownerReferences'] == replacement['metadata']['ownerReferences']
    assert all(o['status']['containerStatuses'][0]['restartCount'] == 0 for o in (before,replacement,after))
    assert len({o['status']['containerStatuses'][0]['imageID'] for o in (before,replacement,after)}) == 1
    assert m.ts(ccp['completed']) < m.ts(receipt['sent']) < m.ts(finalcp['stableSince'])
    cstate = m.yaml(p/'step-01-C-allowed-resources.yaml'); assert set(finalpods) == set(m.mine(cstate,'pods',owner))
    assert not m.mine(final,'services',owner) and len(m.mine(final,'podgroups',owner)) == 1
    cms = m.mine(final,'configmaps',owner); assert len(cms) == 6
    for cm in cms.values(): assert sum(all(o['metadata']['labels'].get(k)==cm['metadata']['labels'].get(k) for k in (m.G,m.R,m.I)) for o in finalpods.values()) == 1
    proxy = m.read(p/'fault-proxy-final.json'); assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id':case['id'],'classification':'PASS','sparsePreparation':preparation,'watchRows':len(rows),'actualFailedCRCreates':len(injected),'persistenceBeforeTemplateMutations':persistence,'historyProbes':len(probes),'terminatingReferenceUIDs':sorted(terminating),'heldNanos':held['elapsedStableNanos'],'finalStableNanos':finalcp['elapsedStableNanos'],'healthyDeletionChecks':deletions,'limitRequest':limit,'sourcePartition':partition,'controllerReplacementUID':replacement['metadata']['uid'],'limitation':'References are checked using direct API reads throughout waits, with complete independent Watch for immutable Data, actual fault interval and stable final state. A pinned terminating Pod may share its revision with other live Pods. All-protected P=3 has no eligible old termination. No datastore internals are asserted.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml','step-01-before-create-clear.json']}}


def audit_old_surge_failure(p, trace):
    result=m.read(p/'result.json');case=m.yaml(p/'case.yaml');source=case['scenario']['source'];config=source['config'];owner=m.yaml(p/'before-server.yaml')['metadata']['uid']
    assert result['id']==case['id']==source['id'] and case['baseline']=='538b2825c06bc1e8c5392d18f18f84faee9fca95' and not result.get('violations')
    assert result['status']=='FAIL' and 'TIMEOUT: C-allowed-target-after-old-release' in result['error']
    n=int(case['id'][4:]);partition=config['roles']['f']['p']
    assert 493<=n<=522 and (n-463)%10<5 and config['roles']['f']['s']==1 and partition<3
    assert config['coordination']['dependencies']=={'f':['b']} and config['coordination']['maxSkew']=='50%'
    original=m.yaml(p/'baseline-resources.yaml');baseline=m.mine(original,'pods',owner)
    assert len(baseline)==6 and all(m.ready(o) and m.version(o)=='A' for o in baseline.values())
    final_pods(m.yaml(p/'step-01-B-allowed-resources.yaml'),owner,baseline,partition,'B')
    request=m.read(p/'step-02-request-time.json');assert request['generation']==3
    checkpoints=[m.read(f) for f in p.glob('checkpoint-*.json')]
    bcp=next(cp for cp in checkpoints if cp['phase']=='B-allowed-target-after-history-recovery')
    held=next(cp for cp in checkpoints if cp['phase']=='C-after-allowed-B')
    assert m.ts(bcp['completed'])<m.ts(request['sent']) and held['elapsedStableNanos']>=10_000_000_000
    rule=m.read(p/'step-01-create-installed.json');hits=[r for r in trace if r.get('ruleID')==rule['id'] and r['action']=='error-request']
    assert hits and all(r['status']==503 and r['method']=='POST' and r['resource']=='controllerrevisions' for r in hits)
    cleared=[r for r in trace if r.get('ruleID')==rule['id'] and r['action']=='rule-cleared'];assert len(cleared)==1 and m.ts(cleared[0]['at'])<m.ts(bcp['completed'])
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    rows=[json.loads(s) for s in open(p/'observations.jsonl')];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5' and source==next(r for r in json.loads(raw)['cases'] if r['id']==case['id'])
    state=collections.defaultdict(dict);data={}
    for row in rows:
        o=row['object'];kind=row['kind'];key=o['metadata']['uid']
        if kind=='controllerrevisions' and m.owned(o,owner):
            assert key not in data or data[key]==o['data'];data[key]=o['data']
        if row['event']=='DELETED':state[kind].pop(key,None)
        else:state[kind][key]=o
        if m.ts(rule['installed'])<=m.ts(row['received'])<m.ts(cleared[0]['at']):
            pods=m.mine(state,'pods',owner);assert set(pods)==set(baseline) and all(m.ready(o) and m.version(o)=='A' for o in pods.values())
            assert set(m.mine(state,'controllerrevisions',owner))==set(m.mine(original,'controllerrevisions',owner))
    expected=collections.Counter({'C':3-partition,'B':1})
    if partition:expected['A']=partition
    def excess(pods):
        mine={o['metadata']['uid']:o for o in pods if m.owned(o,owner)}
        front={uid:o for uid,o in mine.items() if o['metadata']['labels'][m.R]=='frontend'}
        if len(mine)!=7 or len(front)!=4 or not all(m.ready(o) for o in mine.values()) or collections.Counter(m.version(o) for o in front.values())!=expected:return None
        old=next(o for o in front.values() if m.version(o)=='B')
        if int(old['metadata']['labels'][m.I].rsplit('-',1)[1])<3:return None
        for uid,o in mine.items():
            if o['metadata']['labels'][m.R]=='backend' or int(o['metadata']['labels'][m.I].rsplit('-',1)[1])<partition:assert uid in baseline and m.version(o)=='A'
        return old
    probes=[m.read(f) for f in sorted(p.glob('history-probe-*.json'))];assert all('error' not in probe for probe in probes)
    stable=[]
    for probe in probes:
        if excess(probe['pods']['items']):stable.append(probe)
        else:stable=[]
    assert len(stable)>300 and m.ts(stable[-1]['completed'])-m.ts(stable[0]['started'])>=390_000_000_000
    old=excess(stable[-1]['pods']['items']);uid=old['metadata']['uid']
    for probe in stable:
        assert excess(probe['pods']['items'])['metadata']['uid']==uid
        for pod in probe['pods']['items']:
            if not m.owned(pod,owner):continue
            cr=probe['histories']['model-'+pod['metadata']['labels'][REV]];assert m.owned(cr,owner) and not cr['metadata'].get('deletionTimestamp')
    state=m.replay(rows,stable[0]['started']);assert excess(list(state['pods'].values()))['metadata']['uid']==uid
    for row in rows:
        if m.ts(row['received'])<=m.ts(stable[0]['started']):continue
        o=row['object'];key=o['metadata']['uid']
        if row['event']=='DELETED':state[row['kind']].pop(key,None)
        else:state[row['kind']][key]=o
        assert excess(list(state['pods'].values()))['metadata']['uid']==uid
    final=m.yaml(p/'final-resources.yaml');assert excess(list(final['pods'].values()))['metadata']['uid']==uid
    pinned=set(m.read(p/'step-01-terminating-trigger.json')['pinnedUIDs'].values());assert len(pinned)==1 and uid not in pinned
    assert all(any(r['kind']=='pods' and r['event']=='DELETED' and r['object']['metadata']['uid']==pin and m.ts(r['received'])>m.ts(held['completed']) for r in rows) for pin in pinned)
    before=m.yaml(p/'case-controller-before.yaml');after=m.yaml(p/'fault-controller-after.yaml');assert before['metadata']['uid']==after['metadata']['uid'] and after['status']['containerStatuses'][0]['restartCount']==0
    assert not (p/'step-01-restart-controller-delete.json').exists()
    return {'id':case['id'],'classification':'KTHENA_BEHAVIOR_FAILURE','failure':'OLD_SURGE_RETAINED_AFTER_C','watchRows':len(rows),'oldSurge':old,'expectedFrontendCount':3,'actualReadyFrontendCount':4,'frontendVersions':dict(expected),'persistentSince':stable[0]['started'],'persistentUntil':stable[-1]['completed'],'persistentNanos':m.ts(stable[-1]['completed'])-m.ts(stable[0]['started']),'historyProbes':len(probes),'actualFailedCRCreates':len(hits),'faultExplicitlyCleared':cleared[0],'releasedPinnedUIDs':sorted(pinned),'limitation':'Failure is non-convergence caused by an extra live old B surge; referenced A/B/C history retention itself was observed and is required. The requested later controller restart and final C stable window were not exercised because C failed to converge. No Kthena code changes.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml','step-02-request-time.json']}}


def audit_single_role_oracle(p,trace):
    case=m.yaml(p/'case.yaml');result=m.read(p/'result.json');source=case['scenario']['source'];config=source['config']
    assert case['id']==result['id']==source['id']=='RUN-514' and result['status']=='FAIL'
    assert config['roles']['f']=={'p':1,'r':3,'s':1,'u':1,'w':0} and config['coordination']['dependencies']=={'f':['b']}
    want='B-allowed-target-after-history-recovery: SKEW_VIOLATION: group=0 role=frontend starts=2 allowance=1'
    assert result['violations']==[want] and not (p/'step-02-request-time.json').exists()
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()=='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5' and source==next(r for r in json.loads(raw)['cases'] if r['id']=='RUN-514')
    initial=m.yaml(p/'before-server.yaml');owner=initial['metadata']['uid'];b=m.yaml(p/'step-01-server.yaml')
    aroles={r['name']:r for r in initial['spec']['template']['roles']};broles={r['name']:r for r in b['spec']['template']['roles']}
    assert b['metadata']['uid']==owner and b['metadata']['generation']==2 and aroles['backend']==broles['backend']
    assert m.version(aroles['frontend']['entryTemplate'])=='A' and m.version(broles['frontend']['entryTemplate'])=='B'
    baseline=m.yaml(p/'baseline-resources.yaml');base=m.mine(baseline,'pods',owner);assert len(base)==6 and all(m.ready(o) and m.version(o)=='A' for o in base.values())
    rows=[json.loads(s) for s in open(p/'observations.jsonl')];assert [r['sequence'] for r in rows]==list(range(1,len(rows)+1)) and not any(r['event']=='GAP' for r in rows)
    state=collections.defaultdict(dict);deletions=[];data={}
    for row in rows:
        o=row['object'];uid=o['metadata']['uid'];kind=row['kind'];prev=state[kind].get(uid)
        if kind=='controllerrevisions' and m.owned(o,owner):assert uid not in data or data[uid]==o['data'];data[uid]=o['data']
        if kind=='pods' and m.owned(o,owner):
            lab=o['metadata']['labels'];ordinal=int(lab[m.I].rsplit('-',1)[1])
            if lab[m.R]=='backend' or ordinal==0:
                assert uid in base and m.version(o)=='A' and not o['metadata'].get('deletionTimestamp') and row['event']!='DELETED'
            if prev and m.ready(prev) and (row['event']=='DELETED' or o['metadata'].get('deletionTimestamp')):
                available=[pod for pod in frontend(state,owner).values() if m.ready(pod)]
                assert lab[m.R]=='frontend' and m.version(o)=='A' and ordinal in (1,2) and len(available)-1>=2
                deletions.append({'uid':uid,'ordinal':ordinal,'sequence':row['sequence'],'readyBefore':len(available),'readyUIDsBefore':[p['metadata']['uid'] for p in available],'minimum':2})
        if row['event']=='DELETED':state[kind].pop(uid,None)
        else:state[kind][uid]=o
    assert [d['ordinal'] for d in deletions]==[2,1] and all(d['readyBefore']==3 for d in deletions)
    rule=m.read(p/'step-01-create-installed.json');errors=[t for t in trace if t.get('ruleID')==rule['id'] and t['action']=='error-request'];cleared=[t for t in trace if t.get('ruleID')==rule['id'] and t['action']=='rule-cleared']
    assert errors and all(t['status']==503 for t in errors) and len(cleared)==1
    proxy=m.read(p/'fault-proxy-final.json');assert not proxy['errors'] and not any(r['active'] for r in proxy['rules'])
    return {'id':'RUN-514','classification':'RUNNER_ORACLE','failure':'SINGLE_CHANGED_ROLE_SELF_SKEW','watchRows':len(rows),'healthyDeletionChecks':deletions,'unchangedRole':'backend','changedRoles':['frontend'],'actualFailedCRCreates':len(errors),'limitation':'Only frontend is changing; inter-Role skew must not impose a self gate after every other Role is unchanged or complete. Both observed deletions obey descending order, P1 and Ready>=2. Raw run aborted before B convergence and C; requires rerun with corrected runner, no catalogue PASS or product failure credit.','evidenceSHA256':{name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in ['result.json','observations.jsonl','final-resources.yaml']}}


def main():
    runid = sys.argv[1]; assert runid and '/' not in runid and '..' not in runid
    base = ROOT/'artifacts'/runid; control = ROOT/'artifacts/environment-022'/(runid+'-control')
    done = m.read(control/'completion.json'); build = m.read(control/'build.json')
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved']==9 and not done['proxyErrors']
    assert m.read(base/'environment.json')['runner']['binarySHA256'] == build['binarySHA256']
    assert m.read(control/'controller-before.json')['spec'] == m.read(control/'controller-restored.json')['spec']
    results = m.read(base/'summary.json')['results']; trace = []
    start = min(m.ts(m.read(base/r['id']/'step-01-create-installed.json')['installed']) for r in results)
    for line in (control/'proxy-trace.jsonl').open():
        record = json.loads(line)
        if m.ts(record['at']) >= start: trace.append(record)
    out = base/(sys.argv[2] if len(sys.argv)>2 else 'independent-history-audit'); out.mkdir(); reports = []
    for result in results:
        try:
            if result['status']=='PASS':report=audit_case(base/result['id'],trace)
            elif result['id']=='RUN-514' and 'SKEW_VIOLATION' in result.get('error',''):report=audit_single_role_oracle(base/result['id'],trace)
            elif (int(result['id'][4:])-463)%10>=5 and 'BUDGET_VIOLATION' in result.get('error',''):
                loader=importlib.util.spec_from_file_location('sparse_budget',ROOT/'scripts/audit-history-sparse-budget.py');budget=importlib.util.module_from_spec(loader);loader.loader.exec_module(budget)
                report=budget.audit(base/result['id'],trace)
            else:report=audit_old_surge_failure(base/result['id'],trace)
        except (AssertionError,KeyError,StopIteration,FileNotFoundError) as err:
            import traceback
            report = {'id':result['id'],'classification':'PENDING_REVIEW','rawStatus':result['status'],'rawError':result.get('error'),'auditError':repr(err),'traceback':traceback.format_exc()}
        reports.append(report)
        with (out/(result['id']+'.json')).open('x') as f: json.dump(report,f,indent=2); f.write('\n')
        print(report['id'],report['classification'],report.get('auditError',''))
    pending = any(r['classification']=='PENDING_REVIEW' for r in reports)
    with (out/'summary.json').open('x') as f: json.dump({'status':'PENDING_REVIEW' if pending else 'VERIFIED','cases':reports,'counts':dict(collections.Counter(r['classification'] for r in reports))},f,indent=2); f.write('\n')
    if pending: sys.exit(1)


if __name__ == '__main__': main()
