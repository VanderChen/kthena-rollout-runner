#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Complete category 3 only from pinned reviews and restored real Kind batches."""
import collections
import datetime
import hashlib
import importlib.util
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.util.spec_from_file_location('category2_summary', ROOT / 'scripts/summarize-category2.py')
category2 = importlib.util.module_from_spec(loader)
loader.loader.exec_module(category2)
BASELINE = '538b2825c06bc1e8c5392d18f18f84faee9fca95'
SOURCE_SHA = '757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
EXPECTED = {'RUN-%03d' % n for n in range(540, 612)} | {'DENY-%03d' % n for n in range(1, 98)}


def read_json(path):
    return json.loads(path.read_bytes())


def read_review(root, item):
    base = root / 'artifacts' / item['run']
    path = base / item['audit']
    blob = path.read_bytes()
    assert hashlib.sha256(blob).hexdigest() == item['sha256'], 'independent audit digest mismatch'
    report = json.loads(blob)
    assert report['status'] == 'VERIFIED', 'audit still needs review'
    control = root / 'artifacts/environment-022' / (item['run'] + '-control')
    done = read_json(control / 'completion.json')
    assert done['status'] == 'COLLECTED_PENDING_CASE_REVIEW' and done['runID'] == item['run']
    assert done['controllerSpecRestored'] and done['historicalModelServingUIDsPreserved'] == 9
    assert not done.get('cleanupErrors') and not done.get('proxyErrors'), 'environment restoration failed'
    before = read_json(control / 'controller-before.json')
    after = read_json(control / 'controller-restored.json')
    assert before['metadata']['uid'] == after['metadata']['uid'] and before['spec'] == after['spec']
    environment = read_json(base / 'environment.json')
    assert environment['baseline'] == BASELINE, 'different Kthena baseline'
    build = read_json(control / 'build.json')
    assert environment['runner']['binarySHA256'] == build['binarySHA256'], 'different runner binary'
    immutable = read_json(root / build['immutableBuildEvidence'])
    assert immutable['workingTree'] == '', 'image built from an unrecorded working tree'
    for key in ('runnerCommit', 'binarySHA256', 'image', 'imageID'):
        assert immutable[key] == build[key], 'immutable build proof mismatch'
    # build.workingTree records the later manifest dispatch, when an unrelated
    # auditor may be under development; the executed binary must match its
    # original clean build, independently of those later files.
    rows = report['cases']
    assert len({r['id'] for r in rows}) == len(rows), 'duplicate case in audit'
    assert {r['id'] for r in rows} == set(done['selected']), 'audit does not account for every selected case'
    assert all(r['classification'] in ('PASS', 'KTHENA_BEHAVIOR_FAILURE', 'RUNNER_INJECTION_MISS',
                                      'RUNNER_OVERRESTRICTIVE_LEADER_ORACLE', 'RUNNER_ORACLE',
                                      'KTHENA_PRECONDITION_CLEANUP_FAILURE') for r in rows), 'unreviewed classification'
    return str(path.relative_to(root)), rows


def main():
    manifest_path = ROOT / (sys.argv[1] if len(sys.argv) > 1 else 'cases/category3-verification-manifest.json')
    manifest = read_json(manifest_path)
    assert manifest['category'] == '3' and manifest['controllerCommit'] == BASELINE and manifest['sourceSHA256'] == SOURCE_SHA
    raw = (ROOT.parent / 'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest() == SOURCE_SHA
    source = {r['id']: r for r in json.loads(raw)['cases'] if r['categoryPath'][0] == '3'}
    assert set(source) == EXPECTED and len(source) == 169
    audits = [read_review(ROOT, item) for item in manifest['audits']]
    accepted, preserved = category2.assemble(audits, source)
    for row in accepted:
        row['source'] = source[row['id']]
    counts = dict(collections.Counter(r['status'] for r in accepted))
    summary = {
        'status': 'COMPLETE_CATEGORY_3', 'category': '3',
        'createdAt': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'controllerCommit': BASELINE, 'sourceSHA256': SOURCE_SHA,
        'uniqueCatalogueCases': len(accepted), 'counts': counts,
        'manifestSHA256': hashlib.sha256(manifest_path.read_bytes()).hexdigest(),
        'independentAuditRecords': sum(len(rows) for _, rows in audits),
        'preservedNoncreditedFindings': preserved,
        'preservedNoncreditedCounts': dict(collections.Counter(r['classification'] for r in preserved)),
        'watchRowsInAcceptedReviews': sum(r['evidence'].get('watchRows', 0) for r in accepted),
        'audits': manifest['audits'], 'results': accepted,
        'limitations': [
            'This aggregates independently reviewed native Kind evidence; it is not a fresh rerun.',
            'Product failures remain valid verdicts; their later unexecuted phases receive no credit.',
            'Runner and preparation findings remain separately preserved when a valid supplement exists.',
            'Deliberately held preparation finalizers are not product cleanup failures; identity and capacity remain semantic requirements.',
            'Kthena production source was not modified.'
        ]
    }
    out = ROOT / 'artifacts' / (sys.argv[2] if len(sys.argv) > 2 else 'category3-final-verification')
    out.mkdir()
    (out / 'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
    lines = ['# Category 3: independent Kind verification complete', '',
             '169 unique cases: %d PASS, %d Kthena behavior failures.' % (counts.get('PASS', 0), counts.get('FAIL', 0)), '',
             'Production ' + BASELINE + '. Later unexecuted phases receive no credit. Earlier runner and preparation findings are retained.', '',
             '| Case | Verdict | Independent audit | Limitation |', '| --- | --- | --- | --- |']
    for row in accepted:
        lines.append('| %s | %s | %s | %s |' % (row['id'], row['status'], row['audit'], str(row['evidence'].get('limitation', '')).replace('|', '/').replace('\n', ' ')))
    (out / 'REPORT.md').write_text('\n'.join(lines) + '\n')
    print(json.dumps({'cases': len(accepted), 'counts': counts, 'watchRows': summary['watchRowsInAcceptedReviews']}))


if __name__ == '__main__':
    main()
