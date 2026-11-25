#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""ServingGroup object cases (contract 2.14): same-name recreation, user deletion, occupied
ordinal and the surge mark. They exist only for a build whose ServingGroups are objects and
declare that with scenario.requires; any other build reports them as NOT_RUN.

The scenarios are rows NT1, NT2, NT4, NT6, NT9, NT18, NT19 and NT45 of the ServingGroup object design
(kthena docs/proposal/modelserving-servinggroup-cr/04-behavior-expectations.md, section 8.7), and the
ServingGroup requests that row 10 of its section 8.6 adds to the api-retry faults (RUN-663..665).
Each row states the errors the case must detect; the assertions that detect them are the
declared expect.servingGroups facts, the two-step recovery order and the process invariants of
internal/runner/group_invariants.go, each covered by a rejecting unit test.
"""
import copy
import hashlib
import importlib.util
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('normal_generator', ROOT/'scripts/generate-normal.py')
normal = importlib.util.module_from_spec(loader)
loader.loader.exec_module(normal)

SOURCE = 'kthena/docs/proposal/modelserving-servinggroup-cr/04-behavior-expectations.md#8.7'
CONTRACT = '2.14'


def config(n, workers=0, u=1, s=0, recovery='omitted'):
    return {'coordination': 'omitted', 'mode': 'SG', 'n': n, 'plugins': 'omitted', 'recovery': recovery,
            'roles': {'f': {'r': 1, 'w': workers}}, 'top': {'p': 0, 's': s, 'u': u}}


def group(ordinal, **facts):
    return dict({'name': 'model-%d' % ordinal}, **facts)


def same(ordinal, step='baseline'):
    """The group was not touched: one object ever, the same object and the same Pods."""
    return group(ordinal, objects=1, exists=True, terminating=False, uidSameAs=step, podUIDsSameAs=step)


def recreated(ordinal, step='baseline', **facts):
    """A same-name recreation: a second object, another UID, the same Pod names, new Pod UIDs."""
    return group(ordinal, objects=2, exists=True, terminating=False, uidDiffersFrom=step,
                 podNamesSameAs=step, podUIDsDisjointFrom=step, **facts)


def nt1():
    c = config(3)
    a = normal.initial(c, 'controlled')
    b = normal.version(copy.deepcopy(a))
    old = {'kind': 'unit', 'version': 'A', 'ordinal': 2, 'count': 1}
    terminating = {'kind': 'terminating', 'version': 'A', 'ordinal': 2, 'count': 1}
    steps = [
        normal.step('pin-the-pod-of-group-2', action='pin', conditions=[old], release='none', stableSeconds=0,
                    expect={'noReplacement': True, 'noNewRevision': True}),
        normal.step('rolling-replacement-of-group-2-begins', b, until='conditions', conditions=[terminating], stableSeconds=0),
        normal.step('old-object-keeps-the-name-while-its-pod-remains', until='conditions', conditions=[terminating],
                    release='none', holdSeconds=20, stableSeconds=0,
                    expect={'servingGroups': [group(2, objects=1, exists=True, terminating=True, uidSameAs='baseline', podUIDsSameAs='baseline')]}),
        normal.step('release-the-pod-and-converge', action='unpin', stableSeconds=30,
                    expect={'targets': [{'scope': 'SG', 'versions': {'B': 3}}],
                            'servingGroups': [recreated(2, foregroundDeletions=1), recreated(1), recreated(0)]}),
    ]
    return ('NT1', c, a, steps,
            '滚动替换的同名重建：N=3、U=1、S=0，A→B，钉住组2的Pod使其不消失',
            'ServingGroup model-2带deletionTimestamp且仍在；其间没有第二个model-2对象，也没有model-2的新Pod。放开后旧对象消失，新对象UID不同，Pod名字与原来相同',
            '旧Pod还在时新对象已经建出来；新对象沿用了旧的账本')


def nt2():
    c = config(2, workers=1, recovery='ServingGroupRecreate')
    a = normal.initial(c, 'controlled')
    a['template']['restartGracePeriodSeconds'] = 0
    for key in ('entryTemplate', 'workerTemplate'):
        spec = normal.role(a)[key]['spec']
        spec['restartPolicy'] = 'Always'
        spec['containers'][0]['command'] = ['sh', '-c', "trap 'exit 42' USR1; while :; do sleep 1 & wait $!; done"]
    steps = [
        normal.step('entry-of-group-0-becomes-faulty-and-the-group-is-recreated', action='fault-member',
                    containerRestart={'group': 0, 'role': 'frontend', 'ordinal': 0, 'member': 'entry'}, stableSeconds=30,
                    expect={'twoStepGroupRecovery': True, 'noNewRevision': True,
                            'targets': [{'scope': 'SG', 'versions': {'A': 2}}],
                            'servingGroups': [recreated(0, seen=['Failed=True', 'Drained=True'], foregroundDeletions=1),
                                              dict(same(1), neverSeen=['Failed=True'])]}),
    ]
    return ('NT2', c, a, steps,
            '整组恢复的两步删除：ServingGroupRecreate、grace=0，组0的entry容器异常重启且不Ready，同组worker健康',
            '先只有故障成员被删；它消失之前健康成员没有deletionTimestamp；之后组进入终止中，健康成员被删；全部消失之后在同一个序号上重建，版本按序号和partition；组1不动',
            '故障成员没停就删了其余成员；重建落到别的序号；重建用错版本')


def nt4(policy):
    c = config(3, workers=1)
    a = normal.initial(c, 'controlled')
    if policy == 'Orphan':
        deleted = group(1, objects=2, exists=True, terminating=False, owned=True, uidDiffersFrom='baseline', podUIDsSameAs='baseline')
        expect = 'ServingGroup对象回来（新UID），成员Pod的UID不变并被收养，版本取Pod的revision标签'
        reject = 'orphan的Pod被删，或者一直没人管'
    else:
        deleted = recreated(1, owned=True)
        if policy == 'Foreground':
            deleted['foregroundDeletions'] = 1
        expect = '同名的ServingGroup对象回来（新UID）；旧Pod全部消失之前没有该组的新Pod；其余组不动'
        reject = '新旧两批Pod并存；其余组被替换'
    steps = [
        normal.step('user-deletes-group-1-' + policy.lower(), action='delete-serving-group',
                    groupDelete={'group': 1, 'propagation': policy}, stableSeconds=30,
                    expect={'noNewRevision': True, 'targets': [{'scope': 'SG', 'versions': {'A': 3}}],
                            'servingGroups': [deleted, same(0), same(2)]}),
    ]
    return ('NT4', c, a, steps, '用户以%s方式删除ServingGroup model-1（N=3，每组entry+worker）' % policy, expect, reject)


def nt6():
    c = config(3)
    a = normal.initial(c, 'controlled')
    four = copy.deepcopy(a)
    four['replicas'] = 4
    ready = {'kind': 'unit', 'version': 'A', 'ready': True, 'count': 3}
    occupy = 'foreign-object-takes-the-name-model-3'
    foreign = group(3, objects=1, exists=True, owned=False, pods=0)
    steps = [
        normal.step(occupy, action='create-foreign-group', foreignGroup=3, release='none', stableSeconds=10,
                    expect={'noReplacement': True, 'noNewRevision': True, 'servingGroups': [foreign]}),
        normal.step('scale-out-to-4-while-ordinal-3-is-occupied', four, until='conditions', conditions=[ready],
                    holdSeconds=30, stableSeconds=0,
                    expect={'noNewRevision': True,
                            'servingGroups': [dict(foreign, uidSameAs=occupy), group(4, objects=0, exists=False, pods=0),
                                              same(0), same(1), same(2)],
                            'modelServingConditions': {'RolloutBlocked': 'True/OrdinalOccupied'}}),
        normal.step('foreign-object-removed-and-ordinal-3-is-filled', action='delete-serving-group',
                    groupDelete={'group': 3, 'propagation': 'Background'}, stableSeconds=30,
                    expect={'noNewRevision': True, 'targets': [{'scope': 'SG', 'versions': {'A': 4}}],
                            'servingGroups': [group(3, objects=2, exists=True, owned=True, terminating=False, uidDiffersFrom=occupy),
                                              group(4, objects=0, exists=False), same(0), same(1), same(2)]}),
    ]
    return ('NT6', c, a, steps,
            '有人占了名字：事先建一个不属于该ModelServing的ServingGroup model-3，再把replicas从3扩到4',
            '不收为己有，不删它；这个序号建不出来，也不换别的序号顶替；RolloutBlocked的reason是OrdinalOccupied；外来对象被删除之后补上序号3',
            '把别人的对象当成自己的；静默地少一个组')


def nt9():
    c = config(3, u=0, s=1)
    a = normal.initial(c, 'controlled')
    b = normal.version(copy.deepcopy(a))
    surge = {'kind': 'unit', 'version': 'B', 'ordinal': 3, 'ready': False, 'count': 1}
    formal = [group(i, objects=1, exists=True, surge=False, uidSameAs='baseline') for i in range(3)]
    steps = [
        normal.step('surge-group-is-created-with-its-mark', b, until='conditions', conditions=[surge], release='none',
                    holdSeconds=5, stableSeconds=0,
                    expect={'servingGroups': [group(3, objects=1, exists=True, surge=True, surgeAtCreation=True)] + formal}),
        normal.step('finish-B-and-reclaim-the-surge', stableSeconds=30,
                    expect={'targets': [{'scope': 'SG', 'versions': {'B': 3}}],
                            'servingGroups': [group(3, objects=1, exists=False, pods=0)] +
                            [group(i, objects=2, exists=True, surge=False, surgeAtCreation=False) for i in range(3)]}),
    ]
    return ('NT9', c, a, steps,
            'surge标记的生命周期：N=3、U=0、S=1，A→B走到完成',
            'model-3的ServingGroup创建时就带注解，它的Pod带同名同值的副本；序号0–2的组和它们的Pod从来不带；结束时没有带注解的ServingGroup',
            '标记是后加的；正式的组带过标记；结束时还有带标记的组；对象和Pod上的不同步')


def grace_across_process(design, change):
    c = config(2, workers=1, recovery='RoleRecreate')
    c['restartGracePeriodSeconds'] = 60
    a = normal.initial({k: v for k, v in c.items() if k != 'restartGracePeriodSeconds'}, 'controlled')
    a['template']['restartGracePeriodSeconds'] = 60
    for key in ('entryTemplate', 'workerTemplate'):
        spec = normal.role(a)[key]['spec']
        spec['restartPolicy'] = 'Always'
        spec['containers'][0]['command'] = ['sh', '-c', "trap 'exit 42' USR1; while :; do sleep 1 & wait $!; done"]
    # Only the Role instance is recreated: the group object stays, its record
    # shows the instance drained once and established again one generation later.
    instance = group(0, objects=1, exists=True, terminating=False, uidSameAs='baseline', podNamesSameAs='baseline',
                     podUIDsDisjointFrom='baseline', seen=['frontend/0:Draining(0)', 'frontend/0:Established(1)'],
                     neverSeen=['Failed=True'])
    steps = [
        normal.step('entry-restarts-and-the-controller-process-changes-inside-the-grace', action='fault-member-across-process',
                    containerRestart={'group': 0, 'role': 'frontend', 'ordinal': 0, 'member': 'entry'}, processChange=change,
                    stableSeconds=30, expect={'noNewRevision': True, 'targets': [{'scope': 'SG', 'versions': {'A': 2}}],
                                              'servingGroups': [instance, same(1)]}),
    ]
    how = '重启controller（同镜像，Deployment重建Pod）' if change == 'restart-controller' else '删除leader，由另一个副本接管（两副本、开启选主）'
    return (design, c, a, steps,
            '重启故障的grace跨进程重新计：RoleRecreate、grace=60。t=0令组0的entry容器重启且不Ready；t=20%s；开启选主时新进程约需一个租约期接管，须在t=60之前看到故障' % how,
            '新进程从它完成初始同步起重新计满60秒：范围（entry和worker）在新进程同步后约60秒被重建，此前UID不变；不在t≈60重建',
            '新进程一看到就重建；在t≈60重建（把起点带过了进程）；一直不重建')


def group_informer_unsynced():
    c = config(3, u=0, s=1)
    a = normal.initial(c, 'controlled')
    b = normal.version(copy.deepcopy(a))
    steps = [
        normal.step('hold-servinggroup-informer-initial-sync-then-automatically-finish-B', b, action='hold-initial-sync',
                    stableSeconds=30,
                    expect={'targets': [{'scope': 'SG', 'versions': {'B': 3}}],
                            'servingGroups': [group(3, objects=1, exists=False, pods=0), group(4, objects=0, exists=False)] +
                            [recreated(i) for i in range(3)]}),
    ]
    return ('NT45', c, a, steps,
            'ServingGroup的informer没同步：终止controller，挂住新进程对servinggroups的初始LIST，期间提交B，再放开',
            '挂住期间不把“没有组”当事实：A的组和Pod保持原UID，不新建组；放开之后按预算滚到B，正式组各替换一次、surge只建一个并被回收',
            '多建组；把撞名字当错误长时间退避；挂住期间删除或重建健康的组')


def group_api_retry(request):
    c = config(3, u=0, s=1)
    a = normal.initial(c, 'controlled')
    b = normal.version(copy.deepcopy(a))
    blocked = {'kind': 'running-not-ready', 'role': 'frontend', 'version': 'B', 'count': 1}
    steps = [
        normal.step('B-surge-blocked-with-all-old-A-healthy', b, until='conditions', conditions=[blocked], release='none',
                    holdSeconds=5, stableSeconds=0),
        normal.step('one-real-ServingGroup-API-error-and-autonomous-B-recovery', action='retry-api-error', stableSeconds=30,
                    expect={'targets': [{'scope': 'SG', 'versions': {'B': 3}}],
                            'servingGroups': [group(3, objects=1, exists=False, pods=0), group(4, objects=0, exists=False)] +
                            [recreated(i) for i in range(3)]}),
    ]
    return ('8.6#10', c, a, steps,
            '提交B并停在surge未就绪；令controller对ServingGroup的%s失败一次（503），随后恢复' % request,
            '错误不视为空对象集或成功完成；重试幂等：surge组只建一个并被回收，正式组各替换一次；原健康容量不被无预算清空；恢复后自动全B',
            '失败之后重复创建或重复删除ServingGroup；把读失败当成没有组；停住不再推进')


def main():
    plans = [(653, nt1()), (654, nt2()), (655, nt4('Foreground')), (656, nt4('Background')), (657, nt4('Orphan')),
             (658, nt6()), (659, nt9()), (660, grace_across_process('NT18', 'restart-controller')),
             (661, grace_across_process('NT19', 'terminate-leader')), (662, group_informer_unsynced()),
             (663, group_api_retry('创建（POST）')), (664, group_api_retry('status写入（PUT status）')),
             (665, group_api_retry('直读（命名空间内的LIST）'))]
    out = ROOT/'cases/servinggroup-objects'
    out.mkdir(exist_ok=True)
    rows = []
    for number, (design, cfg, initial, steps, action, expect, reject) in plans:
        case_id = 'RUN-%03d' % number
        row = {'id': case_id, 'designID': design, 'group': 'ServingGroup对象', 'kind': '运行', 'contract': CONTRACT,
               'reference': SOURCE, 'config': cfg, 'initial': 'A全Ready', 'action': action, 'expect': expect,
               'mustReject': reject}
        for step in steps:
            step['timeoutSeconds'] = 420
        if design == 'NT45':
            row['environment'] = '单个controller副本；本例会终止并重建controller进程，不能与其他用例并行'
        if 'restartGracePeriodSeconds' in cfg:
            row['environment'] = ('两个controller副本并显式 --leader-elect=true' if design == 'NT19'
                                  else '单个controller副本；本例会终止并重建controller进程，不能与其他用例并行')
        case = {'format': 'rollout-runner/v3', 'id': case_id, 'baseline': normal.COMMIT,
                'scenario': {'requires': 'servingGroupObjects', 'source': row, 'profile': 'controlled',
                             'initialSpec': initial, 'steps': steps}}
        (out/(case_id+'.yaml')).write_text(normal.yaml(case)+'\n')
        rows.append(row)
    manifest = {'format': 'rollout-runner/suite-v1', 'controllerCommit': normal.COMMIT, 'contractVersion': CONTRACT,
                'requires': 'servingGroupObjects', 'reference': SOURCE,
                'developmentScope': 'RUN-653..665; ServingGroup objects: same-name recreation, user deletion, occupied ordinal, surge mark, finite grace across a controller process change, unsynced ServingGroup informer, one failing ServingGroup create/status write/direct read',
                'relatedCoverage': {
                    'NT3': 'cases/restart-convergence RC-01 (scripts/run-restart-convergence.py selects the ServingGroup DELETE cut point)',
                    'NT10': 'process invariants SURGE_PROMOTION_* and the settled surge-mark check on servinggroup-compound-v2 RUN-627 (SG-C03), RUN-648 (SG-P08), RUN-650 (SG-P10)',
                    'NT11': 'process invariant SURGE_PROMOTED_OUTSIDE_RANGE on servinggroup-compound-v2 RUN-621 (SG-S04)'},
                'cases': rows, 'inputs': {row['id']: hashlib.sha256((out/(row['id']+'.yaml')).read_bytes()).hexdigest() for row in rows}}
    (out/'suite.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
    print('Generated %d ServingGroup object cases.' % len(rows))


if __name__ == '__main__':
    main()
