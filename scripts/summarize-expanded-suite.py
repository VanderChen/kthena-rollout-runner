#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Combine complete category reports without erasing failed or untested phases."""
import collections
import datetime
import hashlib
import json
import pathlib

ROOT=pathlib.Path(__file__).resolve().parents[1]
BASELINE='538b2825c06bc1e8c5392d18f18f84faee9fca95'
SOURCE_SHA='757de7f6de64ebfa2e8ce7a6e0be53809bd085d58191971ae552fc10141580c5'
EXPECTED={
    '1':{'RUN-%03d'%n for n in range(1,304)},
    '2':{'RUN-%03d'%n for n in range(304,540)},
    '3':{'RUN-%03d'%n for n in range(540,612)}|{'DENY-%03d'%n for n in range(1,98)},
}


def validate_category(category,report):
    assert str(report['category'])==category and report['controllerCommit']==BASELINE,'category or production baseline mismatch'
    if category=='1':
        assert report['validationComplete'] is True,'category 1 not complete'
        failures={r['id'] for r in report['confirmedFailures'] if r['classification']['classification']=='KTHENA_BEHAVIOR_FAILURE'}
    else:
        assert report['status']=='COMPLETE_CATEGORY_'+category and report['sourceSHA256']==SOURCE_SHA,'category report not complete'
        failures={r['id'] for r in report['results'] if r['classification']=='KTHENA_BEHAVIOR_FAILURE'}
        assert all(r['classification']=='PASS' or r['classification'].startswith('PASS_INDEPENDENT_') and r['classification'].endswith('_CHECK') or r['classification']=='KTHENA_BEHAVIOR_FAILURE' for r in report['results']),'runner interruption is not a catalogue verdict'
        assert all(r['source']['id']==r['id'] and r['source']['categoryPath'][0]==category for r in report['results']),'source mismatch'
    rows=report['results'];ids=[r['id'] for r in rows]
    assert len(ids)==len(set(ids)) and set(ids)==EXPECTED[category],'missing, duplicate or out-of-category case'
    assert all(r['status'] in ('PASS','FAIL') for r in rows),'unreviewed verdict'
    assert failures=={r['id'] for r in rows if r['status']=='FAIL'},'product failure evidence mismatch'
    counts=dict(collections.Counter(r['status'] for r in rows))
    assert counts==report['counts'],'reported counts differ from actual verdicts'
    return counts


def main():
    raw=(ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest()==SOURCE_SHA
    catalogue=json.loads(raw)['cases']
    for category,expected in EXPECTED.items():assert {r['id'] for r in catalogue if r['categoryPath'][0]==category}==expected
    rows=[];categories=[];preserved=[]
    for category in ('1','2','3'):
        path=ROOT/('artifacts/category'+category+'-final-verification/summary.json')
        blob=path.read_bytes();report=json.loads(blob);counts=validate_category(category,report)
        categories.append({'category':category,'cases':len(EXPECTED[category]),'counts':counts,'report':str(path.relative_to(ROOT)),'sha256':hashlib.sha256(blob).hexdigest()})
        for row in report['results']:
            rows.append({'id':row['id'],'category':category,'status':row['status'],'categoryReport':str(path.relative_to(ROOT)),'evidence':row.get('audit',row.get('artifacts')),'limitation':row.get('evidence',{}).get('limitation')})
        for finding in report.get('preservedNoncreditedFindings',[]):preserved.append({'category':category,'finding':finding})
    environment_path=ROOT/'artifacts/environment-022/expanded-suite-final-environment.json'
    environment= json.loads(environment_path.read_bytes())
    assert environment['status']=='VERIFIED' and environment['controllerCommit']==BASELINE and environment['kthenaWorkingTreeClean']
    assert environment['controllerSpecRestored'] and environment['historicalUIDsPreserved']==9 and environment['activeCaseJobs']==[] and environment['residualCaseNamespaces']==[]
    assert not environment['proxyErrors'] and not environment['activeFaultRules']
    summary={'status':'COMPLETE_EXPANDED_SUITE','createdAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'controllerCommit':BASELINE,'sourceSHA256':SOURCE_SHA,'uniqueCatalogueCases':len(rows),'counts':dict(collections.Counter(r['status'] for r in rows)),'categories':categories,'finalEnvironment':str(environment_path.relative_to(ROOT)),'finalEnvironmentSHA256':hashlib.sha256(environment_path.read_bytes()).hexdigest(),'results':rows,'preservedNoncreditedFindings':preserved,'limitations':['Completion means every requested case has an independently reviewed Kind verdict; product failures are retained.','Each category report retains original attempts, failed preparation, injection misses and unexecuted later phases; none are converted into PASS.','Kthena production source was not modified. Reversible runner corrections were separately built and verified.','These are local Kind results for the archived mock-serving workloads, not a claim of production GPU or load validation.']}
    assert len(rows)==708 and len({r['id'] for r in rows})==708
    out=ROOT/'artifacts/expanded-suite-final-verification';out.mkdir()
    (out/'summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n')
    lines=['# Three-category Kind verification complete','',f"708 unique cases: {summary['counts'].get('PASS',0)} PASS, {summary['counts'].get('FAIL',0)} product failures.",'','Production '+BASELINE+'. Kthena source unchanged.','','| Category | Cases | PASS | Product FAIL |','| --- | ---: | ---: | ---: |']
    for row in categories:lines.append('| %s | %d | %d | %d |'%(row['category'],row['cases'],row['counts'].get('PASS',0),row['counts'].get('FAIL',0)))
    lines+=['']+summary['limitations']
    (out/'REPORT.md').write_text('\n'.join(lines)+'\n')
    print(json.dumps({'status':summary['status'],'cases':len(rows),'counts':summary['counts']}))


if __name__=='__main__':main()
