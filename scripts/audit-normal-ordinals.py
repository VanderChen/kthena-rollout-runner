#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Supplement old normal-flow verdicts with an independent endpoint ordinal audit.

Reads immutable saved Kind snapshots; never claims a new live execution, a
complete process audit, or completion of stages that previously failed.
"""
import argparse
import collections
import hashlib
import json
import pathlib
import re
import subprocess


def read(path):
    return json.loads(path.read_text())


def owned(obj, owner):
    return any(r.get('uid') == owner for r in obj.get('metadata', {}).get('ownerReferences', []))


def ordinal(name):
    match = re.fullmatch(r'.*?(?:^|-)([0-9]+)', name)
    return int(match[1]) if match else -1


def check(scope, desired, identities):
    actual = sorted(ordinal(i) for i in identities)
    wanted = list(range(desired))
    return {'scope': scope, 'desired': desired, 'actual': actual, 'expected': wanted,
            'identities': sorted(identities), 'pass': actual == wanted}


def audit_snapshot(objects):
    models = list(objects.get('modelservings', {}).values())
    if len(models) != 1:
        raise ValueError('snapshot must identify exactly one ModelServing')
    model = models[0]
    owner, spec = model['metadata']['uid'], model['spec']
    latest = {r['name']: r.get('replicas', 1) for r in spec['template']['roles']}
    histories = {r['metadata']['name']: r for r in objects.get('controllerrevisions', {}).values() if owned(r, owner)}
    groups = {}
    for pod in objects.get('pods', {}).values():
        if not owned(pod, owner):
            continue
        labels = pod['metadata']['labels']
        group, role, instance = (labels.get('modelserving.volcano.sh/' + k, '') for k in ('group-name', 'role', 'role-id'))
        groups.setdefault(group, {}).setdefault(role, {}).setdefault(instance, []).append(pod)
    checks = [check('SG', spec.get('replicas', 1), groups)]
    for group, roles in sorted(groups.items()):
        for role, instances in sorted(roles.items()):
            desired = latest.get(role)
            # Partitioned SGs can retain a Role removed from the new template.
            # Its own persisted history supplies its original replica count.
            if desired is None:
                historical_counts = set()
                for pods in instances.values():
                    for pod in pods:
                        revision = pod['metadata']['labels'].get('modelserving.volcano.sh/revision')
                        history = histories.get(model['metadata']['name'] + '-' + str(revision))
                        if history:
                            historical_counts.update(r.get('replicas', 1) for r in history['data']['data'] if r['name'] == role)
                if len(historical_counts) != 1:
                    raise ValueError('cannot resolve retained Role layout: ' + group + '/' + role)
                desired = historical_counts.pop()
            row = check(group + '/' + role, desired, instances)
            entries = {identity: sum(p['metadata']['labels'].get('modelserving.volcano.sh/entry') == 'true' for p in pods)
                       for identity, pods in instances.items()}
            row['entryCounts'] = entries
            row['pass'] = row['pass'] and all(n == 1 for n in entries.values())
            checks.append(row)
    return {'ownerUID': owner, 'generation': model['metadata'].get('generation'),
            'currentRevision': model.get('status', {}).get('currentRevision'),
            'updateRevision': model.get('status', {}).get('updateRevision'),
            'checks': checks, 'pass': all(c['pass'] for c in checks)}


def selected_attempt(result):
    root = pathlib.Path(result['artifacts'])
    candidates = []
    for final in root.glob('**/final-resources.yaml'):
        directory = final.parent
        if not (directory / 'result.json').exists():
            continue
        actual = read(directory / 'result.json')
        if actual.get('namespace') == result['namespace'] and actual['status'] == result['status']:
            candidates.append(directory)
    if len(candidates) != 1:
        raise ValueError('ambiguous/missing accepted attempt: ' + result['id'])
    return candidates[0]


def audit_case(result, parser):
    directory = selected_attempt(result)
    paths = [directory / name for name in ('case.yaml', 'baseline-resources.yaml', 'final-resources.yaml')]
    data = json.loads(subprocess.check_output([parser] + list(map(str, paths))))
    case = data['case.yaml']
    if case['id'] != result['id']:
        raise ValueError('case identity mismatch')
    baseline = audit_snapshot(data['baseline-resources.yaml'])
    last = case.get('scenario', {}).get('steps', [{}])[-1]
    stop = last.get('expect', {}).get('noFullPromotion') or last.get('expect', {}).get('blockedByBudget')
    final = None
    verdict = 'SKIPPED_PREVIOUS_FAILURE' if result['status'] != 'PASS' else 'SKIPPED_INCOMPLETE_STOP' if stop else 'PASS'
    if verdict == 'PASS':
        final = audit_snapshot(data['final-resources.yaml'])
        if final['ownerUID'] != baseline['ownerUID']:
            raise ValueError('ModelServing identity changed')
        if not final['pass']:
            verdict = 'FAIL_FINAL_ORDINAL'
    if not baseline['pass']:
        verdict = 'FAIL_BASELINE_ORDINAL'
    paths.append(directory / 'result.json')
    return {'id': result['id'], 'previousStatus': result['status'], 'verdict': verdict,
            'artifacts': str(directory), 'baseline': baseline, 'final': final,
            'sha256': {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--summary', type=pathlib.Path, required=True)
    ap.add_argument('--yaml-parser', required=True)
    ap.add_argument('--out', type=pathlib.Path, required=True)
    args = ap.parse_args()
    source = read(args.summary)
    expected = {'RUN-%03d' % n for n in range(1, 304)}
    if len(source['results']) != 303 or {r['id'] for r in source['results']} != expected:
        raise ValueError('need exactly 303 previously accepted case records')
    args.out.mkdir(exist_ok=False, parents=True)
    records = [audit_case(r, args.yaml_parser) for r in source['results']]
    counts = dict(collections.Counter(r['verdict'] for r in records))
    report = {'controllerCommit': source['controllerCommit'], 'inputSummary': str(args.summary.resolve()),
              'inputSHA256': hashlib.sha256(args.summary.read_bytes()).hexdigest(), 'total': len(records),
              'counts': counts, 'kindRerun': False,
              'limitation': 'Ordinal-only supplemental audit of saved baselines and final snapshots. Does not revalidate every intermediate settled checkpoint or process safety. Previously failed cases have no final-completion credit; the explicit incomplete dependency stop has no full-completion credit. Original verdicts remain unchanged.',
              'results': records}
    (args.out / 'summary.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    lines = ['# 正常流程起止序号补充复核', '', '既有 Kind 快照复核，不是全部 303 项重新执行。原结果保留。', '',
             '统计：`' + json.dumps(counts, ensure_ascii=False) + '`。', '',
             '| 用例 | 旧结果 | 新增检查发现 | 证据 |', '| --- | --- | --- | --- |']
    for record in records:
        if not record['verdict'].startswith('FAIL_'):
            continue
        facts = record['final'] or record['baseline']
        errors = ['%s：实际 %s，要求 %s' % (c['scope'], c['actual'], c['expected']) for c in facts['checks'] if not c['pass']]
        lines.append('| %s | %s | %s | %s |' % (record['id'], record['previousStatus'], '；'.join(errors), record['artifacts']))
    lines += ['', report['limitation'], '']
    (args.out / 'REPORT.md').write_text('\n'.join(lines))
    print(json.dumps({'total': len(records), 'counts': counts}, ensure_ascii=False))


if __name__ == '__main__':
    main()
