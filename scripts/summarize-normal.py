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
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]


def read(path):
    return json.loads(path.read_text())


def require(condition, message):
    if not condition:
        raise ValueError(message)


def container_status(pod):
    return next(c for c in pod['status']['containerStatuses'] if c['name'] == 'runner')


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


def markdown(report):
    counts = ', '.join(f'{k}={v}' for k, v in report['counts'].items())
    lines = ['# 第一大类 Kind 验证', '', f"实际执行 {report['total']} 项；{counts}。所有判定保留 runner 原始结果。", '', f"Kthena production: `{report['controllerCommit']}`。", f"Runner imageID: `{report['runnerImageID']}`。", '', '| 子类 | 执行 | PASS | 其他结果 |', '| --- | ---: | ---: | --- |']
    if 'assertionAddendum' in report:
        a = report['assertionAddendum']
        lines[3:3] = ['', f"共 {report['executionCount']} 次实际执行、303 个独立用例。RUN-301 补齐“不新增模板版本”断言后，以同一镜像/二进制重新执行；本表使用补测原始结果 {a['acceptedResult']['status']}。首次执行结果 {a['originalResult']['status']} 及证据仍保留于 JSON 的 assertionAddendum.originalResult，其余 302 项输入未变。", '']
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
    parser.add_argument('runs', nargs='+', type=pathlib.Path)
    args = parser.parse_args()
    try:
        require(bool(args.original_suite) == bool(args.addendum_301), 'both --original-suite and --addendum-301 are required together')
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
