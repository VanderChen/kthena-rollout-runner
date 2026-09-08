#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Verify completed, disjoint Kind shards and report all 303 original verdicts.

Input directories must include the runner artifacts plus job.json and
runner-pod.json exported after the Job terminates. Historical smoke directories
must not be included. No failure is converted into an expected pass.

The explicit RUN-301 addendum supports the documented missing noNewRevision
assertion only. Both complete executions and their original verdicts are kept.
"""
import argparse
import collections
import copy
import datetime
import hashlib
import importlib.util
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]


def read(path):
    return json.loads(path.read_text())


def require(condition, message):
    if not condition:
        raise ValueError(message)


def container_status(pod):
    return next(c for c in pod['status']['containerStatuses'] if c['name'] == 'runner')


def observation_time(value):
    # Go RFC3339Nano permits fractional lengths unsupported by Python3.9.
    value = re.sub(r'\.(\d+)', lambda m: '.' + m[1][:6].ljust(6, '0'), value)
    return datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))


def verify_153_replica_baseline(directory, result):
    """Require the mutable replica baseline while preserving immutable history.

    The existing Kind binary captures this in raw Watch/checkpoint JSON. Keep
    the semantic check here so old complete evidence can be validated without
    changing workload inputs or rebuilding the already running candidate.
    """
    prefix = 'RUN-153 replica baseline: '
    phases = ['baseline', 'scale-equivalent-A', 'change-only-budgets', 'restart-and-compare']
    paths = [directory / f'checkpoint-{i:03d}.json' for i in range(1, 5)]
    require(all(p.is_file() for p in paths), prefix + 'missing checkpoint')
    checkpoints = [read(p) for p in paths]
    require([c['phase'] for c in checkpoints] == phases, prefix + 'unexpected checkpoint phases')
    ends = [observation_time(c['completed']) for c in checkpoints]
    require(all(a < b for a, b in zip(ends, ends[1:])), prefix + 'invalid checkpoint order')
    journal = directory / 'observations.jsonl'
    require(journal.is_file(), prefix + 'missing raw Watch')
    events = [json.loads(s) for s in journal.read_text().splitlines()]
    require(bool(events), prefix + 'empty raw Watch')
    for i, e in enumerate(events, 1):
        require(e['sequence'] == i and e['event'] not in ('GAP', 'ERROR'), prefix + 'incomplete raw Watch')
    models = {e['object']['metadata']['uid'] for e in events if e['kind'] == 'modelservings'
              and e.get('object', {}).get('metadata', {}).get('name') == 'model'
              and e['object']['metadata'].get('namespace') == result.get('namespace')}
    require(len(models) == 1, prefix + 'missing or ambiguous ModelServing identity')
    owner = models.pop()

    def owned(obj):
        return any(r.get('uid') == owner and r.get('controller') is True
                   for r in obj.get('metadata', {}).get('ownerReferences', []))

    def state_at(end):
        live = {}
        for e in events:
            if e['kind'] != 'controllerrevisions' or observation_time(e['received']) > end:
                continue
            obj = e['object']
            uid = obj['metadata']['uid']
            if e['event'] == 'DELETED':
                live.pop(uid, None)
            else:
                live[uid] = obj
        return {u: o for u, o in live.items() if owned(o)}

    initial = state_at(ends[0])
    require(len(initial) == 1, prefix + 'expected one initial A history')
    uid, history = next(iter(initial.items()))
    immutable_data = history.get('data')
    require(bool(immutable_data), prefix + 'missing initial Data')
    annotation = 'modelserving.volcano.sh/coordinated-role-replica-baseline'

    def replica_baseline(obj, phase):
        encoded = obj['metadata'].get('annotations', {}).get(annotation)
        require(isinstance(encoded, str), prefix + phase + ' annotation missing')
        try:
            return json.loads(encoded)
        except (ValueError, TypeError) as exc:
            raise ValueError(prefix + 'invalid annotation JSON') from exc

    for e in events:
        if e['kind'] != 'controllerrevisions' or observation_time(e['received']) < ends[0]:
            continue
        obj = e['object']
        if owned(obj) or obj['metadata']['uid'] == uid:
            require(obj['metadata']['uid'] == uid and e['event'] != 'DELETED' and owned(obj),
                    prefix + 'history identity or owner changed')
            require(obj.get('data') == immutable_data, prefix + 'immutable Data changed')
            if observation_time(e['received']) >= ends[1]:
                require(replica_baseline(obj, 'after completed scaling') == {'backend': 4, 'frontend': 4},
                        prefix + 'regressed after completed scaling')
    rows = []
    for i, (checkpoint, end) in enumerate(zip(checkpoints, ends)):
        live = state_at(end)
        require(set(live) == {uid}, prefix + checkpoint['phase'] + ' history identity differs')
        obj = live[uid]
        require(obj.get('data') == immutable_data, prefix + 'immutable Data changed')
        replicas = replica_baseline(obj, checkpoint['phase'])
        expected = {'backend': 3 if i == 0 else 4, 'frontend': 3 if i == 0 else 4}
        require(replicas == expected, prefix + checkpoint['phase'] + f' expected {expected}, got {replicas}')
        rows.append(dict(phase=checkpoint['phase'], completed=checkpoint['completed'], replicas=replicas))
    return dict(controllerRevisionUID=uid, controllerRevisionName=history['metadata']['name'],
        dataSHA256=hashlib.sha256(json.dumps(immutable_data, sort_keys=True).encode()).hexdigest(),
        observationsSHA256=hashlib.sha256(journal.read_bytes()).hexdigest(),
        checkpointSHA256={p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in paths},
        checkpoints=rows,
        scope='Stored replica baseline is3 before scaling and4 after scaling, policy update and restart; '
              'history UID/owner/Data unchanged throughout the observed trace. No later B rollout is claimed.')


def verify(directories, suite_path=None, required_ids=None):
    suite_path = suite_path or ROOT / 'cases/normal/suite.json'
    suite = read(suite_path)
    catalogue = {c['id']: c for c in suite['cases']}
    require(set(catalogue) == {f'RUN-{i:03}' for i in range(1, 304)}, 'catalogue is not 303 unique cases')
    required_ids = set(catalogue) if required_ids is None else set(required_ids)
    require(bool(required_ids) and required_ids <= set(catalogue), 'invalid required case IDs')
    results, shards = {}, []
    identity = None
    statuses = {'PASS', 'FAIL', 'ERROR', 'INCONCLUSIVE', 'TRIGGER_MISSED', 'NOT_RUN'}
    for directory in directories:
        summary = read(directory / 'summary.json')
        environment = read(directory / 'environment.json')
        job = read(directory / 'job.json')
        pod = read(directory / 'runner-pod.json')
        run = summary['runID']
        require(summary['kthenaBaseline'] == suite['controllerCommit'] == environment['baseline'], f'{run}: wrong controller baseline')
        require(summary['selected'] == len(summary['results']), f'{run}: unfinished summary')
        require(summary['passed'] == sum(r['status'] == 'PASS' for r in summary['results']), f'{run}: inconsistent pass count')
        require(environment['options']['RunID'] == run, f'{run}: mismatched environment')
        provenance = environment['runner']
        expected_inputs = {f'{k}.yaml': v for k, v in suite['inputs'].items()}
        require(provenance['caseSHA256'] == expected_inputs, f'{run}: case inputs differ from reviewed suite')
        actual = container_status(pod)
        initial = provenance['runnerPod']
        require(pod['metadata']['uid'] == initial['metadata']['uid'], f'{run}: wrong runner Pod')
        require(any(o['uid'] == job['metadata']['uid'] for o in pod['metadata']['ownerReferences']), f'{run}: wrong Job owner')
        # The process can start before kubelet publishes its imageID. The final
        # export is authoritative, tied to the exact preflight Pod UID.
        initial_image = container_status(initial).get('imageID')
        require(bool(actual.get('imageID')), f'{run}: final runner imageID missing')
        require(not initial_image or actual['imageID'] == initial_image, f'{run}: runner image changed')
        controller_ids = sorted({c['imageID'] for p in environment['controllerPods'] for c in p['status'].get('containerStatuses', []) if c.get('ready') and c.get('imageID')})
        require(len(controller_ids) == 1, f'{run}: controller digest is absent or ambiguous')
        current_identity = (actual['imageID'], provenance['binarySHA256'], environment['options']['ControllerImage'], controller_ids[0])
        require(identity is None or identity == current_identity, f'{run}: shards use different candidates')
        identity = current_identity
        termination = actual.get('state', {}).get('terminated', {})
        expected_exit = 0 if summary['passed'] == summary['selected'] else 1
        require(termination.get('exitCode') == expected_exit, f'{run}: unexpected Pod exit status')
        expected_condition = 'Complete' if expected_exit == 0 else 'Failed'
        require(any(c['type'] == expected_condition and c['status'] == 'True' for c in job['status'].get('conditions', [])), f'{run}: Job is not {expected_condition}')
        if expected_exit:
            require(any(c.get('reason') == 'BackoffLimitExceeded' and c['type'] == 'Failed' for c in job['status']['conditions']), f'{run}: Job failed for infrastructure/deadline reason')
        require(actual.get('restartCount', 0) == 0, f'{run}: runner restarted')
        for result in summary['results']:
            case_id = result['id']
            require(case_id in required_ids and case_id not in results, f'{run}: unexpected/duplicate {case_id}')
            require(result['status'] in statuses, f'{run}: unknown status for {case_id}')
            require(result == read(directory / case_id / 'result.json'), f'{run}: {case_id} result differs from summary')
            require(result['status'] != 'NOT_RUN', f'{run}: {case_id} did not run')
            require(not result.get('cleanupError'), f'{run}: {case_id} cleanup failed')
            if result['status'] == 'PASS':
                require(not result.get('violations') and not result.get('error'), f'{run}: {case_id} PASS contains failures')
            results[case_id] = dict(result, runID=run, section=catalogue[case_id]['section'], artifacts=str((directory / case_id).resolve()))
            if case_id == 'RUN-153' and result['status'] == 'PASS':
                results[case_id]['replicaBaselineEvidence'] = verify_153_replica_baseline(directory / case_id, result)
        shards.append(dict(runID=run, selected=summary['selected'], passed=summary['passed'], job=job['metadata']['name'], pod=pod['metadata']['name'], exitCode=termination['exitCode']))
    missing = sorted(required_ids - set(results))
    require(not missing, 'missing cases: ' + ', '.join(missing))
    ordered = [results[k] for k in sorted(results)]
    return dict(controllerCommit=suite['controllerCommit'], runnerImageID=identity[0], binarySHA256=identity[1], controllerImage=identity[2], controllerImageID=identity[3], suiteSHA256=hashlib.sha256(suite_path.read_bytes()).hexdigest(), total=len(ordered), counts=dict(collections.Counter(r['status'] for r in ordered)), shards=shards, results=ordered)


def verify_301_input_correction(original, current):
    """Allow one stronger assertion, with identical catalogue and API actions."""
    for key in ('format', 'controllerCommit', 'sourceSHA256', 'cases'):
        require(original[key] == current[key], f'RUN-301 addendum changes catalogue {key}')
    require(set(original['inputs']) == set(current['inputs']), 'addendum changes input IDs')
    changed = {cid for cid in current['inputs'] if current['inputs'][cid] != original['inputs'][cid]}
    require(changed == {'RUN-301'}, 'addendum must change only RUN-301 input')
    module_spec = importlib.util.spec_from_file_location('normal_generator', ROOT / 'scripts/generate-normal.py')
    generator = importlib.util.module_from_spec(module_spec)
    module_spec.loader.exec_module(generator)
    row = next(c for c in current['cases'] if c['id'] == 'RUN-301')
    corrected = generator.plan(row)
    steps = corrected['scenario']['steps']
    require(len(steps) == 2 and steps[1]['expect'].get('noNewRevision') is True,
            'RUN-301 must explicitly reject a new policy-only template revision')
    before = copy.deepcopy(corrected)
    del before['scenario']['steps'][1]['expect']['noNewRevision']
    for label, value, manifest in [('original', before, original), ('corrected', corrected, current)]:
        encoded = (generator.yaml(value) + '\n').encode()
        require(hashlib.sha256(encoded).hexdigest() == manifest['inputs']['RUN-301'],
                f'RUN-301 {label} input differs by more than the documented assertion')


def verify_with_301_addendum(directories, original_suite_path, addendum_directory):
    current = read(ROOT / 'cases/normal/suite.json')
    original = read(original_suite_path)
    verify_301_input_correction(original, current)
    baseline = verify(directories, original_suite_path)
    addendum = verify([addendum_directory], required_ids={'RUN-301'})
    for key in ('controllerCommit', 'runnerImageID', 'binarySHA256', 'controllerImage', 'controllerImageID'):
        require(baseline[key] == addendum[key], f'RUN-301 addendum changes candidate {key}')
    require(addendum['shards'][0]['runID'] not in {s['runID'] for s in baseline['shards']},
            'RUN-301 addendum must be a separate execution')
    report = copy.deepcopy(baseline)
    old_result = next(r for r in report['results'] if r['id'] == 'RUN-301')
    replacement = addendum['results'][0]
    if replacement['status'] == 'PASS':
        ledger = read(addendum_directory / 'RUN-301/ledger.json')
        require(ledger.get('noNewRevision') is True and bool(ledger.get('revisions')),
                'RUN-301 PASS does not prove the history assertion was armed')
    report['results'] = [replacement if r['id'] == 'RUN-301' else r for r in report['results']]
    report['counts'] = dict(collections.Counter(r['status'] for r in report['results']))
    report['baselineSuiteSHA256'] = baseline['suiteSHA256']
    report['suiteSHA256'] = addendum['suiteSHA256']
    report['shards'] += addendum['shards']
    report['executionCount'] = sum(s['selected'] for s in report['shards'])
    report['assertionAddendum'] = dict(
        caseID='RUN-301', reason='Explicit noNewRevision assertion on coordination removal',
        originalResult=old_result, acceptedResult=replacement,
        originalInputSHA256=original['inputs']['RUN-301'],
        correctedInputSHA256=current['inputs']['RUN-301'],
        unchangedOtherInputs=302,
    )
    return report


def rapid_restore_evidence(root):
    """Read original JSON artifacts, never a manually supplied classification."""
    result = read(root / 'result.json')
    attempts = read(root / 'attempts.json')
    require(bool(attempts), 'restore: missing attempts')
    require(all(a['status'] == 'TRIGGER_MISSED' and not a.get('cleanupError') for a in attempts[:-1]),
            'restore: previous failed execution cannot be replaced')
    directory = root / f'attempt-{len(attempts)}'
    raw = read(directory / 'result.json')
    comparable = dict(result, durationSeconds=raw['durationSeconds'])
    require(raw == attempts[-1] == comparable and not raw.get('cleanupError'), 'restore: attempt result mismatch')
    paths = [root / 'result.json', root / 'attempts.json', directory / 'result.json',
             directory / 'observations.jsonl']
    events = [json.loads(s) for s in paths[-1].read_text().splitlines()]
    require(bool(events), 'restore: empty Watch')
    for i, event in enumerate(events, 1):
        require(event['sequence'] == i and event['event'] not in ('GAP', 'ERROR'), 'restore: incomplete Watch')
    models = [e['object'] for e in events if e['kind'] == 'modelservings']
    require(bool(models) and len({o['metadata']['uid'] for o in models}) == 1, 'restore: ambiguous owner')
    require(all(o['metadata']['name'] == 'model' and o['metadata']['namespace'] == raw['namespace']
                for o in models), 'restore: wrong ModelServing owner')
    owner = models[0]['metadata']['uid']

    def owned(obj):
        return obj['metadata'].get('namespace') == raw['namespace'] and any(
            r.get('uid') == owner and r.get('controller') is True
            for r in obj['metadata'].get('ownerReferences', []))

    requests, accepted = [], []
    for i in range(1, 4):
        path = directory / f'step-{i:02d}-request-time.json'
        paths.append(path)
        request = read(path)
        require(observation_time(request['sent']) < observation_time(request['received']), 'restore: invalid request interval')
        candidates = [o for o in models if o['metadata']['resourceVersion'] == request['resourceVersion']
                      and o['metadata']['generation'] == request['generation']]
        require(bool(candidates), 'restore: accepted API response absent from Watch')
        requests.append(request)
        accepted.append(candidates[0])
    require(all(observation_time(a['received']) < observation_time(b['sent'])
                for a, b in zip(requests, requests[1:])), 'restore: requests out of order')
    require(accepted[0]['spec'] == accepted[2]['spec'], 'restore: final desired differs from B before shrink')
    for obj, expected in zip(accepted, (3, 1, 3)):
        roles = {r['name']: r for r in obj['spec']['template']['roles']}
        require(obj['spec']['replicas'] == 1 and set(roles) == {'frontend', 'backend'} and
                roles['frontend']['replicas'] == expected and roles['backend']['replicas'] == 3 and
                all(r.get('workerReplicas', 0) == 0 for r in roles.values()), 'restore: wrong R3/1/3 W0 layout')
    proofs = []
    for label in ('before', 'after'):
        path = directory / f'step-03-terminating-{label}-proof.json'
        paths.append(path)
        proofs.append(read(path))
    before, after = proofs
    request = requests[2]
    require(observation_time(before['at']) < observation_time(request['sent']) <
            observation_time(request['received']) < observation_time(after['at']), 'restore: missing live trigger interval')
    common = {u for u, yes in before['terminatingUIDs'].items() if yes and after['terminatingUIDs'].get(u)}
    require(bool(common), 'restore: no same terminating UID across accepted restore')
    for uid in common:
        rows = [e for e in events if e['kind'] == 'pods' and e['object']['metadata']['uid'] == uid]
        require(bool(rows) and all(owned(e['object']) for e in rows), 'restore: terminating UID has wrong owner')
        require(any(e['object']['metadata'].get('deletionTimestamp') and
                    observation_time(e['received']) < observation_time(before['at']) for e in rows),
                'restore: trigger UID deletion missing before request')
        require(not any(e['event'] == 'DELETED' and observation_time(e['received']) < observation_time(after['at'])
                        for e in rows), 'restore: trigger UID already disappeared')
    return dict(result=result, directory=directory, events=events, owner=owner, owned=owned,
                request=request, paths=paths, common=sorted(common), accepted=accepted)


def pod_ready(obj):
    return not obj['metadata'].get('deletionTimestamp') and any(
        c['type'] == 'Ready' and c['status'] == 'True' for c in obj.get('status', {}).get('conditions', []))


def pod_version(obj):
    return next(e['value'] for c in obj['spec']['containers'] if c['name'] == 'workload'
                for e in c.get('env', []) if e['name'] == 'ROLLOUT_VERSION')


def verify_restore_boundary(root):
    """Diagnose one ambiguous intent, never turn a raw failure into PASS.

    Deliberately independent of case ID: RUN-193's real trace must fail this
    temporal check even though it reports the same availability error.
    """
    evidence = rapid_restore_evidence(root)
    result, events, request = (evidence[k] for k in ('result', 'events', 'request'))
    violation = 'restore-before-deletion-finishes: BUDGET_VIOLATION: model-0/frontend ready=1 delete=model-0/frontend/frontend-0 minimum=2'
    require(result['status'] == 'FAIL' and result.get('violations') == [violation] and
            result.get('error') == 'step 03 restore-before-deletion-finishes: ' + violation + '; process violations: ' + violation,
            'boundary: not the sole supported budget error')
    target = [e for e in events if e['kind'] == 'pods' and
              e['object']['metadata'].get('name') == 'model-0-frontend-0-0']
    require(bool(target) and len({e['object']['metadata']['uid'] for e in target}) == 1,
            'boundary: missing or recreated target UID')
    require(all(evidence['owned'](e['object']) and pod_version(e['object']) == 'A' for e in target),
            'boundary: wrong target owner or version')
    uid = target[0]['object']['metadata']['uid']
    deletion = next((e for e in target if e['event'] == 'DELETED' or e['object']['metadata'].get('deletionTimestamp')), None)
    require(deletion is not None, 'boundary: no target deletion')
    earlier = [e for e in target if e['sequence'] < deletion['sequence']]
    require(bool(earlier) and pod_ready(earlier[-1]['object']), 'boundary: target was not Ready')
    require(observation_time(deletion['received']) > observation_time(request['received']), 'boundary: deletion not after response')
    starts = [s for s in result.get('normalStarts', []) if uid in s.get('uids', [])]
    require(len(starts) == 1 and starts[0]['reason'] == 'rollout' and starts[0]['readyBefore'] == 1 and
            starts[0]['minimum'] == 2 and starts[0]['phase'] == 'restore-before-deletion-finishes',
            'boundary: target belongs to another action')
    live = {}
    for e in events[:deletion['sequence'] - 1]:
        if e['kind'] == 'pods':
            o = e['object']
            if e['event'] == 'DELETED':
                live.pop(o['metadata']['uid'], None)
            else:
                live[o['metadata']['uid']] = o
    healthy = {u for u, o in live.items() if evidence['owned'](o) and pod_ready(o) and
               o['metadata']['labels'].get('modelserving.volcano.sh/role') == 'frontend'}
    require(healthy == {uid}, 'boundary: target not sole healthy frontend')
    log_path = root / 'controller.log'
    lines = [line for line in log_path.read_text().splitlines() if
             f'object="{result["namespace"]}/model"' in line and 'reason="RoleDeleting"' in line and
             'message="Role frontend/frontend-0 in ServingGroup model-0 is now Deleting"' in line]
    require(len(lines) == 1, 'boundary: missing or ambiguous RoleDeleting log')
    intent = lines[0].split()[0]
    require(observation_time(request['sent']) < observation_time(intent) < observation_time(request['received']),
            'boundary: deletion intent outside restore request interval')
    evidence['paths'].append(log_path)
    return dict(classification='OBSERVATION_BOUNDARY_UNRESOLVED', rawStatus='FAIL', targetUID=uid,
                ownerUID=evidence['owner'], request=request, intentLogTime=intent,
                firstPodDeletionTime=deletion['received'], terminatingUIDs=evidence['common'],
                artifactSHA256={str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in evidence['paths']},
                limitation='The API request overlaps an observed deletion intent. Causality is unresolved; this is not PASS or proof that the deletion was correct.')


def verify_restore_completion(root):
    evidence = rapid_restore_evidence(root)
    result, events, directory = (evidence[k] for k in ('result', 'events', 'directory'))
    require(result['status'] == 'PASS' and not result.get('error') and not result.get('violations'), 'restore: completion is not raw PASS')
    require(result.get('checkpoints') == 4, 'restore: missing final checkpoint count')
    paths = [directory / f'checkpoint-{i:03d}.json' for i in range(1, 5)]
    checkpoints = [read(p) for p in paths]
    require([c['phase'] for c in checkpoints] == ['baseline', 'enter-mixed-rollout', 'shrink-while-B-in-flight',
            'restore-before-deletion-finishes'], 'restore: incomplete phases')
    ends = [observation_time(c['completed']) for c in checkpoints]
    require(all(a < b for a, b in zip(ends, ends[1:])), 'restore: checkpoint order')
    final = checkpoints[-1]
    stable = observation_time(final['stableSince'])
    require(final['stableSeconds'] == 30 and (ends[-1] - stable).total_seconds() >= 30 and stable > ends[-2],
            'restore: missing 30 second stable window')
    live, baseline, states = {}, None, []
    for e in events:
        at = observation_time(e['received'])
        require(at <= ends[-1], 'restore: Watch extends beyond final checkpoint')
        if baseline is None and at > ends[0]:
            baseline = copy.deepcopy(live)
        if at >= stable and not states:
            states.append(copy.deepcopy(live))
        if e['kind'] == 'pods':
            o = e['object']
            require(evidence['owned'](o), 'restore: unexpected Pod owner')
            uid = o['metadata']['uid']
            if e['event'] == 'DELETED':
                live.pop(uid, None)
            else:
                live[uid] = o
            if at >= stable:
                states.append(copy.deepcopy(live))
    if not states:
        states.append(copy.deepcopy(live))
    require(baseline is not None and len(baseline) == 6 and all(pod_ready(o) and pod_version(o) == 'A'
            for o in baseline.values()), 'restore: missing healthy A baseline')
    role_key = 'modelserving.volcano.sh/role'
    backend = {u for u, o in baseline.items() if o['metadata']['labels'][role_key] == 'backend'}
    require(len(backend) == 3, 'restore: baseline backend count')
    for state in states:
        require(len(state) == 6 and all(pod_ready(o) for o in state.values()), 'restore: final layout is not six Ready Pods')
        require({u for u, o in state.items() if o['metadata']['labels'][role_key] == 'backend'} == backend,
                'restore: original backend UID changed')
        require(all(pod_version(o) == ('B' if o['metadata']['labels'][role_key] == 'frontend' else 'A')
                    for o in state.values()), 'restore: final template version differs')
        for role in ('frontend', 'backend'):
            members = [o for o in state.values() if o['metadata']['labels'][role_key] == role]
            require(len(members) == 3 and len({o['metadata']['labels'].get('modelserving.volcano.sh/role-id')
                    for o in members}) == 3 and all(o['metadata']['labels'].get('modelserving.volcano.sh/group-name') == 'model-0'
                    and o['metadata']['labels'].get('modelserving.volcano.sh/entry') == 'true' for o in members),
                    'restore: final role membership differs')
    evidence['paths'] += paths
    return dict(rawStatus='PASS', attempts=len(read(root / 'attempts.json')), ownerUID=evidence['owner'],
                terminatingUIDs=evidence['common'], backendUIDs=sorted(backend), finalReadyPods=6,
                stableSince=final['stableSince'], completed=final['completed'],
                artifactSHA256={str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in evidence['paths']})


def verify_with_addenda(directories, original_suite_path, addendum_301, addendum_183):
    report = verify_with_301_addendum(directories, original_suite_path, addendum_301)
    old = next(r for r in report['results'] if r['id'] == 'RUN-183')
    review = verify_restore_boundary(pathlib.Path(old['artifacts']))
    additional = verify([addendum_183], original_suite_path, {'RUN-183'})
    for key in ('controllerCommit', 'runnerImageID', 'binarySHA256', 'controllerImage', 'controllerImageID'):
        require(report[key] == additional[key], f'RUN-183 addendum changes candidate {key}')
    require(additional['shards'][0]['runID'] not in {s['runID'] for s in report['shards']}, 'RUN-183 must be a separate execution')
    replacement = additional['results'][0]
    require(replacement['status'] in ('PASS', 'FAIL'), 'RUN-183 addendum remains inconclusive')
    if replacement['status'] == 'PASS':
        replacement['restoreCompletionEvidence'] = verify_restore_completion(addendum_183 / 'RUN-183')
    elif 'BUDGET_VIOLATION: model-0/frontend ready=1 delete=model-0/frontend/frontend-0 minimum=2' in replacement.get('error', ''):
        # A second ambiguous result is not acceptance and never triggers a blind
        # retry-until-green loop. Other failures retain their raw verdict.
        try:
            verify_restore_boundary(addendum_183 / 'RUN-183')
        except ValueError as exc:
            require(str(exc) == 'boundary: deletion intent outside restore request interval',
                    'RUN-183 failed addendum needs manual evidence review: ' + str(exc))
        else:
            raise ValueError('RUN-183 addendum still has an unresolved observation boundary')
    report['results'] = [replacement if r['id'] == 'RUN-183' else r for r in report['results']]
    report['counts'] = dict(collections.Counter(r['status'] for r in report['results']))
    report['shards'] += additional['shards']
    report['executionCount'] = sum(s['selected'] for s in report['shards'])
    report['boundaryAddendum'] = dict(caseID='RUN-183', originalResult=old, boundaryReview=review,
                                    acceptedResult=replacement, unchangedInputSHA256=read(original_suite_path)['inputs']['RUN-183'])
    return report


def markdown(report):
    counts = ', '.join(f'{k}={v}' for k, v in report['counts'].items())
    lines = ['# 第一大类 Kind 验证', '', f"实际执行 {report['total']} 项；{counts}。所有判定保留 runner 原始结果。", '', f"Kthena production: `{report['controllerCommit']}`。", f"Runner imageID: `{report['runnerImageID']}`。", '', '| 子类 | 执行 | PASS | 其他结果 |', '| --- | ---: | ---: | --- |']
    if 'assertionAddendum' in report:
        a = report['assertionAddendum']
        lines[3:3] = ['', f"共 {report['executionCount']} 次实际执行、303 个独立用例。RUN-301 补齐“不新增模板版本”断言后，以同一镜像/二进制重新执行；本表使用补测原始结果 {a['acceptedResult']['status']}。首次执行结果 {a['originalResult']['status']} 及证据仍保留于 JSON 的 assertionAddendum.originalResult，其余 302 项输入未变。", '']
    if 'boundaryAddendum' in report:
        a = report['boundaryAddendum']
        lines[3:3] = ['', f"RUN-183 原始 FAIL 的删除意图与恢复请求区间重叠，因果证据不足。原始 FAIL、证据哈希和退出码保留；同一镜像、二进制及原输入的独立补跑结果为 {a['acceptedResult']['status']}，表中采用此新执行结果。RUN-193 等其余结果未因这项边界核查改变。", '']
    sections = collections.defaultdict(list)
    for result in report['results']:
        sections[result['section']].append(result)
    for section, results in sections.items():
        counts = collections.Counter(r['status'] for r in results)
        other = ', '.join(f'{k}={v}' for k, v in counts.items() if k != 'PASS') or '—'
        lines.append(f"| {section} | {len(results)} | {counts['PASS']} | {other} |")
    lines += ['', '| ID | 结果 | 秒 | 观察/失败原因 |', '| --- | --- | ---: | --- |']
    for r in report['results']:
        error = r.get('error', '').replace('|', '\\|').replace('\n', ' ')
        lines.append(f"| {r['id']} | {r['status']} | {r['durationSeconds']:.1f} | {error or '—'} |")
    return '\n'.join(lines) + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', required=True, type=pathlib.Path, help='new report directory; never overwrite')
    parser.add_argument('--original-suite', type=pathlib.Path, help='frozen original suite; requires --addendum-301')
    parser.add_argument('--addendum-301', type=pathlib.Path, help='completed same-image RUN-301 assertion addendum')
    parser.add_argument('--addendum-183', type=pathlib.Path, help='completed same-image RUN-183 boundary retest; requires the301 addendum')
    parser.add_argument('runs', nargs='+', type=pathlib.Path)
    args = parser.parse_args()
    try:
        require(bool(args.original_suite) == bool(args.addendum_301), 'both --original-suite and --addendum-301 are required together')
        require(not args.addendum_183 or args.addendum_301, '--addendum-183 requires --addendum-301')
        if args.addendum_183:
            report = verify_with_addenda(args.runs, args.original_suite, args.addendum_301, args.addendum_183)
        else:
            report = verify_with_301_addendum(args.runs, args.original_suite, args.addendum_301) if args.addendum_301 else verify(args.runs)
        args.out.mkdir(parents=True, exist_ok=False)
        (args.out / 'summary.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        (args.out / 'RESULTS.md').write_text(markdown(report))
    except (ValueError, KeyError, OSError, StopIteration) as exc:
        print(f'Verification report rejected: {exc}', file=sys.stderr)
        return 2
    print(f"Verified {report['total']} cases: {report['counts']}; report: {args.out}")
    return 0


if __name__ == '__main__':
    sys.exit(main())
