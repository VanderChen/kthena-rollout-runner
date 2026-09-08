#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Strict category1 report with the independently verified r11 write correction."""
import argparse
import collections
import copy
import hashlib
import importlib.util
import json
import pathlib
import subprocess

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('normal_report', ROOT / 'scripts/summarize-normal.py')
BASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BASE)
read, require, stamp = BASE.read, BASE.require, BASE.observation_time
FIX_IDS = {'RUN-247', 'RUN-255', 'RUN-257'}
FIX_COMMIT = '2fcbd29817c1289655142c6133c4d1d7c6bb4038'
FIX_IMAGE = 'sha256:9c9ecd55d1ed03c1f3c81e06b8b36d76546a5327a5facb836ac60bd3c53857a9'
FIX_BINARY = 'ea3b12b9f98cd4a12ed0f6a7a747571e49fe3b1ab86345068000303feda0791d'
ROLE = 'modelserving.volcano.sh/role'
GROUP = 'modelserving.volcano.sh/group-name'
ROLE_ID = 'modelserving.volcano.sh/role-id'


def yaml_files(paths):
    """Use the same YAML dependency as the runner, rejecting duplicate keys."""
    return json.loads(subprocess.check_output(['go', 'run', './tools/artifactjson', *[str(p.resolve()) for p in paths]], cwd=ROOT))


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def role_map(spec):
    return {r['name']: r for r in spec['template']['roles']}


def group(obj):
    return int(obj['metadata']['labels'][GROUP].rsplit('-', 1)[1])


def ordinal(obj):
    return int(obj['metadata']['labels'][ROLE_ID].rsplit('-', 1)[1])


def equivalent_defaults(left, right):
    """Allow only known omitted API defaults in a rejected pre-admission request."""
    a, b = copy.deepcopy(left), copy.deepcopy(right)
    for current, other in ((a, b), (b, a)):
        for key, value in [('recoveryPolicy', 'RoleRecreate'), ('revisionHistoryLimit', 10)]:
            if key in current and key not in other:
                require(current.pop(key) == value, 'write fix: unexpected API default')
        if 'restartGracePeriodSeconds' in current['template'] and 'restartGracePeriodSeconds' not in other['template']:
            require(current['template'].pop('restartGracePeriodSeconds') == 0, 'write fix: unexpected grace default')
        for name, role in role_map(current).items():
            target = role_map(other)[name]
            if 'maxUnavailable' in role and 'maxUnavailable' not in target:
                require(role.pop('maxUnavailable') == 1, 'write fix: unexpected unavailable default')
    return a == b


def case_evidence(root):
    result, attempts = read(root / 'result.json'), read(root / 'attempts.json')
    require(result['id'] in FIX_IDS, 'write fix: unsupported case')
    require(1 <= len(attempts) <= 3, 'write fix: invalid attempts')
    require(all(a['status'] == 'TRIGGER_MISSED' and not a.get('violations') and not a.get('cleanupError') for a in attempts[:-1]),
            'write fix: earlier failure was retried')
    directory = root / f'attempt-{len(attempts)}'
    raw = read(directory / 'result.json')
    require(raw == attempts[-1] == dict(result, durationSeconds=raw['durationSeconds']), 'write fix: attempt differs from result')
    require(not raw.get('cleanupError'), 'write fix: cleanup failed')
    events = [json.loads(line) for line in (directory / 'observations.jsonl').read_text().splitlines()]
    require(bool(events) and all(e['sequence'] == i and e['event'] not in ('GAP', 'ERROR') for i, e in enumerate(events, 1)),
            'write fix: incomplete Watch')
    models = [e['object'] for e in events if e['kind'] == 'modelservings']
    require(models and len({o['metadata']['uid'] for o in models}) == 1, 'write fix: ambiguous ModelServing UID')
    owner = models[0]['metadata']['uid']
    require(all(o['metadata']['name'] == 'model' and o['metadata']['namespace'] == raw['namespace'] for o in models), 'write fix: wrong namespace')
    def owned(obj):
        return obj['metadata'].get('namespace') == raw['namespace'] and any(
            r.get('uid') == owner and r.get('controller') is True for r in obj['metadata'].get('ownerReferences', []))
    return dict(root=root, directory=directory, result=result, raw=raw, events=events, models=models, owner=owner, owned=owned)


def accepted(evidence, phase, data):
    request = read(evidence['directory'] / f'step-{phase:02d}-request-time.json')
    server = data[f'step-{phase:02d}-server.yaml']
    require(stamp(request['sent']) < stamp(request['received']), 'write fix: invalid request interval')
    require(server['metadata']['uid'] == evidence['owner'] and server['metadata']['resourceVersion'] == request['resourceVersion']
            and server['metadata']['generation'] == request['generation'], 'write fix: response identity differs')
    require(any(o['metadata']['resourceVersion'] == request['resourceVersion'] and o['spec'] == server['spec'] for o in evidence['models']),
            'write fix: accepted response absent from Watch')
    return request, server


def check_layout_change(cid, first, second):
    expected_n = 1 if cid == 'RUN-247' else 2
    roles = role_map(first)
    require(first['replicas'] == expected_n and set(roles) == {'frontend', 'backend'} and roles['frontend']['replicas'] == 6
            and roles['backend']['replicas'] == 1 and all(r.get('workerReplicas', 0) == 0 for r in roles.values()), 'write fix: unexpected original layout')
    require(first['rolloutStrategy']['type'] == 'RoleRollingUpdate', 'write fix: wrong rollout mode')
    require(BASE.pod_version({'spec': roles['frontend']['entryTemplate']['spec']}) == 'B' and
            BASE.pod_version({'spec': roles['backend']['entryTemplate']['spec']}) == 'A', 'write fix: wrong requested template versions')
    policy = roles['frontend']
    require((policy.get('maxUnavailable', 1), policy.get('maxSurge', 0), policy.get('partition', 0)) == (1, 0, int(cid == 'RUN-257')),
            'write fix: original budget/partition changed')
    target = copy.deepcopy(first)
    if cid == 'RUN-247':
        role_map(target)['frontend']['replicas'] = 8
    else:
        target['replicas'] = 3
    require(equivalent_defaults(target, second), 'write fix: request changed more than source scale')


def verify_original_conflict(root):
    e = case_evidence(root)
    r, p = e['result'], e['directory']
    require(r['status'] == 'FAIL' and not r.get('violations') and r.get('checkpoints') == 2 and
            r.get('error', '').startswith('step 02 separate-scale-during-natural-rollout: Operation cannot be fulfilled on modelservings.') and
            'the object has been modified; please apply your changes to the latest version and try again' in r['error'],
            'write fix: original is not the supported write conflict')
    require(all(not (p / name).exists() for name in ['step-02-server.yaml', 'step-02-request-time.json', 'step-02-trigger-after-proof.json']),
            'write fix: original second request was accepted')
    names = ['case.yaml', 'step-01-server.yaml', 'step-02-request.yaml', 'final-resources.yaml', 'step-02-trigger-before.yaml']
    data = yaml_files([p / name for name in names])
    require(data['case.yaml']['scenario']['source']['executionProfile'] == 'AUTO_READY_INTERLEAVE', 'write fix: wrong execution profile')
    _, first = accepted(e, 1, data)
    final = data['final-resources.yaml']['modelservings']
    require(set(final) == {e['owner']} and final[e['owner']]['spec'] == first['spec'] and final[e['owner']]['metadata']['generation'] == 2,
            'write fix: original spec progressed beyond first request')
    check_layout_change(r['id'], first['spec'], data['step-02-request.yaml']['spec'])
    require(data['step-02-request.yaml']['metadata']['uid'] == e['owner'], 'write fix: rejected request has wrong UID')
    proof = read(p / 'step-02-trigger-before-proof.json')
    require(proof['eligibleOldMemberPresent'] and data['step-02-trigger-before.yaml']['metadata']['resourceVersion'] == proof['resourceVersion'],
            'write fix: original before List identity differs')
    paths = [p / n for n in names] + [p / 'observations.jsonl', p / 'result.json', p / 'step-01-request-time.json', p / 'step-02-trigger-before-proof.json']
    return dict(classification='RUNNER_IMPLEMENTATION_FAILURE', subtype='UNRETRIED_OPTIMISTIC_CONCURRENCY_CONFLICT',
                rawStatus='FAIL', rawWatchRows=len(e['events']), evidenceSHA256={str(x.relative_to(root)): digest(x) for x in paths})


def verify_new_execution(root):
    e = case_evidence(root)
    r, p = e['result'], e['directory']
    require(r['status'] in {'PASS', 'FAIL'}, 'write fix: rerun remains inconclusive')
    names = ['case.yaml', 'baseline-resources.yaml', 'before-server.yaml', 'step-01-server.yaml', 'step-02-server.yaml',
             'step-02-trigger-before.yaml', 'step-02-trigger-after.yaml', 'final-resources.yaml', 'step-01-request.yaml', 'step-02-request.yaml']
    data = yaml_files([p / name for name in names])
    source = data['case.yaml']['scenario']
    require(source['profile'] == 'auto' and source['source']['executionProfile'] == 'AUTO_READY_INTERLEAVE' and not r.get('releases'),
            'write fix: automatic readiness was replaced')
    require(all(s['release'] == 'none' and s['holdSeconds'] == 0 for s in source['steps']), 'write fix: held readiness differs from source')
    requests = []
    servers = [data['before-server.yaml']]
    for phase in (1, 2):
        request, server = accepted(e, phase, data)
        requests.append(request)
        servers.append(server)
    require(stamp(requests[0]['received']) < stamp(requests[1]['sent']), 'write fix: requests not separate')
    check_layout_change(r['id'], servers[1]['spec'], servers[2]['spec'])
    baseline = data['baseline-resources.yaml']['pods']
    require(baseline and all(e['owned'](o) and BASE.pod_ready(o) and BASE.pod_version(o) == 'A' for o in baseline.values()), 'write fix: invalid Ready A baseline')
    initial_n = 1 if r['id'] == 'RUN-247' else 2
    require(len(baseline) == initial_n * 7, 'write fix: incomplete baseline')
    def eligible_old(lst):
        return {o['metadata']['uid'] for o in lst['items'] if e['owned'](o) and o['metadata']['labels'][ROLE] == 'frontend'
                and ordinal(o) >= int(r['id'] == 'RUN-257') and BASE.pod_version(o) == 'A'}
    proof_rows, old_sets, paths = [], [], [p / n for n in names]
    for label in ('before', 'after'):
        path = p / f'step-02-trigger-{label}-proof.json'
        proof = read(path)
        lst = data[f'step-02-trigger-{label}.yaml']
        require(proof['eligibleOldMemberPresent'] and lst['metadata']['resourceVersion'] == proof['resourceVersion'], 'write fix: actual trigger List differs')
        old = eligible_old(lst)
        require(old and old <= set(baseline), 'write fix: no original eligible A member at trigger')
        proof_rows.append(proof)
        old_sets.append(old)
        paths.append(path)
    before, after = proof_rows
    require(stamp(before['at']) < stamp(requests[1]['sent']) < stamp(requests[1]['received']) < stamp(after['at']) and bool(old_sets[0] & old_sets[1]),
            'write fix: scale did not overlap original rollout')
    write_rows = []
    for phase in (1, 2):
        receipts = sorted(p.glob(f'step-{phase:02d}-write-*-receipt.json'))
        require(1 <= len(receipts) <= 5, 'write fix: missing or unbounded write attempts')
        for i, receipt_path in enumerate(receipts, 1):
            prefix = f'step-{phase:02d}-write-{i:02d}'
            require(receipt_path.name == prefix + '-receipt.json', 'write fix: noncontiguous write attempts')
            receipt = read(receipt_path)
            saved = yaml_files([p / (prefix + '-current.yaml'), p / (prefix + '-request.yaml')])
            current, submitted = saved[prefix + '-current.yaml'], saved[prefix + '-request.yaml']
            require(receipt['attempt'] == i and receipt['uid'] == e['owner'] == current['metadata']['uid'] == submitted['metadata']['uid'] and
                    receipt['requestResourceVersion'] == current['metadata']['resourceVersion'] == submitted['metadata']['resourceVersion'],
                    'write fix: attempt UID/resourceVersion differs')
            require(current['spec'] == servers[phase - 1]['spec'] and equivalent_defaults(submitted['spec'], servers[phase]['spec']),
                    'write fix: retry overwrote changed spec')
            require(stamp(receipt['sent']) < stamp(receipt['received']), 'write fix: invalid write receipt times')
            paths.extend([receipt_path, p / (prefix + '-current.yaml'), p / (prefix + '-request.yaml')])
            if phase == 2:
                proof_path = p / (prefix + '-trigger-before-proof.json')
                proof = read(proof_path)
                lst_path = p / (prefix + '-trigger-before.yaml')
                lst = yaml_files([lst_path])[lst_path.name]
                old = eligible_old(lst)
                require(proof['eligibleOldMemberPresent'] and lst['metadata']['resourceVersion'] == proof['resourceVersion'] and old and
                        old <= set(baseline) and stamp(proof['at']) < stamp(receipt['sent']), 'write fix: retry trigger not rechecked')
                paths.extend([proof_path, lst_path])
                if i == len(receipts):
                    require(proof == before and lst == data['step-02-trigger-before.yaml'], 'write fix: canonical proof describes another attempt')
            if i < len(receipts):
                require(receipt.get('status', {}).get('code') == 409 and receipt['status'].get('reason') == 'Conflict' and receipt.get('error'),
                        'write fix: non-conflict failure was retried')
            else:
                require(submitted == data[f'step-{phase:02d}-request.yaml'], 'write fix: canonical submitted request differs')
                success_path = p / (prefix + '-server.yaml')
                success = yaml_files([success_path])[success_path.name]
                require(success == servers[phase], 'write fix: canonical response differs from successful write')
                paths.append(success_path)
                require(not receipt.get('error') and receipt['resourceVersion'] == requests[phase - 1]['resourceVersion'] and
                        receipt['generation'] == requests[phase - 1]['generation'] and receipt['sent'] == requests[phase - 1]['sent'] and
                        receipt['received'] == requests[phase - 1]['received'], 'write fix: canonical request differs from successful write')
            write_rows.append(receipt)
    # An actual new product failure remains a failure. It must have reached the
    # accepted scale/trigger; write/preflight/inconclusive failures are not completion.
    if r['status'] == 'FAIL':
        require(r.get('error') and ('process violations:' in r['error'] or 'TIMEOUT:' in r['error'] or 'STABILITY_VIOLATION:' in r['error']),
                'write fix: new failure needs manual runner/infrastructure review')
        completion = dict(rawStatus='FAIL', reason=r['error'])
    else:
        require(not r.get('violations') and not r.get('error') and r.get('checkpoints') == 3, 'write fix: incomplete PASS')
        checkpoints = [read(p / f'checkpoint-{i:03d}.json') for i in (1, 2, 3)]
        require([c['phase'] for c in checkpoints] == ['baseline', 'observe-natural-rollout-start', 'separate-scale-during-natural-rollout'],
                'write fix: missing scenario stages')
        final_cp = checkpoints[-1]
        require(final_cp['stableSeconds'] == 30 and (stamp(final_cp['completed']) - stamp(final_cp['stableSince'])).total_seconds() >= 30
                and stamp(final_cp['stableSince']) > stamp(after['at']), 'write fix: final 30 second stability missing')
        final_n, final_r = (1, 8) if r['id'] == 'RUN-247' else (3, 6)
        protected = {u for u, o in baseline.items() if o['metadata']['labels'][ROLE] == 'backend' or (r['id'] == 'RUN-257' and ordinal(o) == 0)}
        def check_final(state):
            require(len(state) == final_n * (final_r + 1) and all(e['owned'](o) and BASE.pod_ready(o) for o in state.values()), 'write fix: final members not complete/Ready')
            index = {(group(o), o['metadata']['labels'][ROLE], ordinal(o)): o for o in state.values()}
            wanted = {(g, role, i) for g in range(final_n) for role, count in [('frontend', final_r), ('backend', 1)] for i in range(count)}
            require(set(index) == wanted, 'write fix: wrong final member layout')
            require(protected <= set(state), 'write fix: protected/backend UID replaced')
            for (g, role, i), obj in index.items():
                expected = 'A' if role == 'backend' or (r['id'] == 'RUN-257' and g < initial_n and i == 0) else 'B'
                require(BASE.pod_version(obj) == expected, 'write fix: wrong final template version')
        final_models = data['final-resources.yaml']['modelservings']
        require(set(final_models) == {e['owner']} and final_models[e['owner']]['spec'] == servers[2]['spec'], 'write fix: final desired spec differs')
        old5 = {u for u, o in baseline.items() if o['metadata']['labels'][ROLE] == 'frontend' and ordinal(o) == 5}
        require(any(ev['kind'] == 'pods' and ev['object']['metadata']['uid'] in old5 and
                    (ev['event'] == 'DELETED' or ev['object']['metadata'].get('deletionTimestamp')) and
                    stamp(ev['received']) <= stamp(checkpoints[1]['completed']) for ev in e['events']), 'write fix: initial rollout trigger missing')
        final = data['final-resources.yaml']['pods']
        check_final(final)
        live = {}
        at_start = None
        for event in e['events']:
            at = stamp(event['received'])
            require(at <= stamp(final_cp['completed']), 'write fix: Watch exceeds final checkpoint')
            if at > stamp(final_cp['stableSince']) and at_start is None:
                at_start = dict(live)
            if event['kind'] == 'pods':
                obj, uid = event['object'], event['object']['metadata']['uid']
                require(e['owned'](obj), 'write fix: wrong Pod owner')
                container = next(c for c in obj['spec']['containers'] if c['name'] == 'workload')
                require(container['command'] == ['sh', '-c', 'touch /tmp/ready; sleep 3600'], 'write fix: readiness was manually gated')
                if uid in protected:
                    require(event['event'] != 'DELETED' and not obj['metadata'].get('deletionTimestamp') and BASE.pod_version(obj) == 'A',
                            'write fix: original protected/backend UID changed')
                if event['event'] == 'DELETED':
                    live.pop(uid, None)
                else:
                    live[uid] = obj
            if at >= stamp(final_cp['stableSince']):
                check_final(live)
        check_final(at_start if at_start is not None else live)
        require(live == final, 'write fix: final snapshot differs from Watch')
        completion = dict(rawStatus='PASS', finalReadyPods=len(final), protectedUIDs=sorted(protected), finalStable=final_cp)
        paths.extend(p / f'checkpoint-{i:03d}.json' for i in (1, 2, 3))
    paths.extend([p / 'result.json', p / 'observations.jsonl', p / 'step-01-request-time.json', p / 'step-02-request-time.json'])
    return dict(rawWatchRows=len(e['events']), commonEligibleOriginalAUIDs=sorted(old_sets[0] & old_sets[1]),
                beforeProof=before, acceptedScale=requests[1], afterProof=after, writes=write_rows, completion=completion,
                evidenceSHA256={str(path.relative_to(root)): digest(path) for path in paths})


def verify(directories, original_suite, a301, a183, fixes, candidate):
    require(candidate['runnerCommit'] == FIX_COMMIT and candidate['dockerImageID'] == FIX_IMAGE and candidate['binarySHA256'] == FIX_BINARY,
            'write fix: candidate is not the reviewed correction')
    report = BASE.verify_with_addenda(directories, original_suite, a301, a183)
    seen_conflicts = {r['id'] for r in report['results'] if 'Operation cannot be fulfilled' in r.get('error', '') and 'the object has been modified' in r.get('error', '')}
    require(seen_conflicts == FIX_IDS, 'write fix: additional or missing original conflicts require review')
    extra = BASE.verify([fixes], required_ids=FIX_IDS)
    for key in ('controllerCommit', 'controllerImage', 'controllerImageID'):
        require(report[key] == extra[key], 'write fix: production controller changed: ' + key)
    require(extra['runnerImageID'] == FIX_IMAGE and extra['binarySHA256'] == FIX_BINARY and report['runnerImageID'] != FIX_IMAGE,
            'write fix: wrong runner identity')
    old_suite, current = read(original_suite), read(ROOT / 'cases/normal/suite.json')
    require(all(old_suite['inputs'][cid] == current['inputs'][cid] for cid in FIX_IDS), 'write fix: selected case input changed')
    require(extra['shards'][0]['runID'] not in {s['runID'] for s in report['shards']}, 'write fix: execution is not independent')
    identities = [{key: source[key] for key in ('runnerImageID', 'binarySHA256', 'controllerCommit', 'controllerImage', 'controllerImageID')}
                  for source in (report, extra)]
    original = {r['id']: r for r in report['results']}
    corrections = []
    for row in extra['results']:
        old = original[row['id']]
        review = verify_original_conflict(pathlib.Path(old['artifacts']))
        proof = verify_new_execution(pathlib.Path(row['artifacts']))
        row['provenance'] = identities[1]
        row['writeCorrectionEvidence'] = proof
        corrections.append(dict(caseID=row['id'], originalResult=dict(old, provenance=identities[0]), originalReview=review, acceptedResult=row,
                                unchangedInputSHA256=old_suite['inputs'][row['id']]))
    replacement = {r['id']: r for r in extra['results']}
    report['results'] = [replacement.get(r['id'], dict(r, provenance=identities[0])) for r in report['results']]
    report['shards'] += extra['shards']
    report['executionCount'] = sum(s['selected'] for s in report['shards'])
    report['counts'] = dict(collections.Counter(r['status'] for r in report['results']))
    report['runnerCandidates'] = identities
    report['runnerWriteCorrection'] = dict(implementationCommit=FIX_COMMIT, corrections=corrections,
        originalRunnerImageID=report.pop('runnerImageID'), originalBinarySHA256=report.pop('binarySHA256'))
    return report


def markdown(report):
    lines = ['# 第一大类 Kind 验证', '', f"{report['total']}个独立用例，{report['executionCount']}次执行；" +
             '，'.join(f'{k}={v}' for k, v in report['counts'].items()) + '。', '',
             f"Kthena production `{report['controllerCommit']}`，实际控制器digest `{report['controllerImageID']}`。", '',
             '原303项、301断言补测及183边界补跑的证据保留。写入冲突修正仅替换247/255/257的汇总条目，原始FAIL及新旧身份见JSON的runnerWriteCorrection；其他条目未因此改变。', '',
             '| Runner候选 | 镜像ID | 二进制SHA256 |', '| --- | --- | --- |']
    for label, identity in zip(('原始r10及301/183补测', '写入修正r11复测'), report['runnerCandidates']):
        lines.append(f"| {label} | {identity['runnerImageID']} | {identity['binarySHA256']} |")
    lines += ['', '| 用例 | 结果 | 执行run ID | 失败原因 |', '| --- | --- | --- | --- |']
    for row in report['results']:
        error = row.get('error', '').replace('|', '\\|').replace('\n', ' ')
        lines.append(f"| {row['id']} | {row['status']} | {row['runID']} | {error or '—'} |")
    return '\n'.join(lines) + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('out', 'original-suite', 'addendum-301', 'addendum-183', 'write-fix-run', 'candidate'):
        parser.add_argument('--' + name, required=True, type=pathlib.Path)
    parser.add_argument('runs', nargs='+', type=pathlib.Path)
    args = parser.parse_args()
    report = verify(args.runs, args.original_suite, args.addendum_301, args.addendum_183, args.write_fix_run, read(args.candidate))
    args.out.mkdir(exist_ok=False)
    (args.out / 'summary.json').write_text(json.dumps(report, indent=2, ensure_ascii=False) + '\n')
    (args.out / 'RESULTS.md').write_text(markdown(report))
    print(json.dumps(dict(total=report['total'], executionCount=report['executionCount'], counts=report['counts'])))


if __name__ == '__main__':
    main()
