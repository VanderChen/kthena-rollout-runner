#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Aggregate immutable independent reviews without hiding earlier noncredited findings."""
import collections,datetime,hashlib,json,pathlib,sys
ROOT=pathlib.Path(__file__).resolve().parents[1]

def assemble(audits,expected):
    accepted={};preserved=[]
    for origin,rows in audits:
        for row in rows:
            case=row['id'];assert case in expected,('unexpected category case',case)
            classification=row['classification'];record={'id':case,'audit':origin,'classification':classification,'evidence':row}
            if classification.startswith('PASS') or classification=='KTHENA_BEHAVIOR_FAILURE':
                assert case not in accepted,('duplicate valid verdict would conceal an earlier outcome',case)
                record['status']='PASS' if classification.startswith('PASS') else 'FAIL';accepted[case]=record
            else:
                assert classification in ('RUNNER_INJECTION_MISS','RUNNER_OVERRESTRICTIVE_LEADER_ORACLE','RUNNER_ORACLE','KTHENA_PRECONDITION_CLEANUP_FAILURE'),('unreviewed outcome',case,classification)
                preserved.append(record)
    assert set(accepted)==set(expected),('missing verified cases',sorted(set(expected)-set(accepted)))
    assert all(r['id'] in accepted for r in preserved)
    return [accepted[k] for k in sorted(accepted)],preserved


def main():
    manifest_path=ROOT/'cases/category2-verification-manifest.json';manifest=json.loads(manifest_path.read_text())
    source_path=ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json';raw=source_path.read_bytes();assert hashlib.sha256(raw).hexdigest()==manifest['sourceSHA256']
    source={r['id']:r for r in json.loads(raw)['cases'] if r['categoryPath'][0]=='2'};assert set(source)=={'RUN-%03d'%n for n in range(304,540)}
    audits=[]
    for item in manifest['audits']:
        path=ROOT/'artifacts'/item['run']/item['audit'];blob=path.read_bytes();assert hashlib.sha256(blob).hexdigest()==item['sha256']
        report=json.loads(blob);assert report['status'] in ('VERIFIED','VERIFIED_84_CASES_ONLY')
        if report.get('controllerCommit'):assert report['controllerCommit']==manifest['controllerCommit']
        if report.get('sourceSHA256'):assert report['sourceSHA256']==manifest['sourceSHA256']
        audits.append((str(path.relative_to(ROOT)),report.get('cases',report.get('results'))))
    accepted,preserved=assemble(audits,source)
    for row in accepted:row['source']=source[row['id']]
    counts=dict(collections.Counter(r['status'] for r in accepted));assert counts=={'PASS':128,'FAIL':108}
    summary={'status':'COMPLETE_CATEGORY_2','createdAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'category':'2','controllerCommit':manifest['controllerCommit'],'sourceSHA256':manifest['sourceSHA256'],'uniqueCatalogueCases':len(accepted),'counts':counts,'independentAuditRecords':sum(len(rows) for _,rows in audits),'preservedNoncreditedCounts':dict(collections.Counter(r['classification'] for r in preserved)),'preservedNoncreditedFindings':preserved,'watchRowsInAcceptedReviews':sum(r['evidence'].get('watchRows',0) for r in accepted),'manifestSHA256':hashlib.sha256(manifest_path.read_bytes()).hexdigest(),'audits':manifest['audits'],'results':accepted,'limitations':['This aggregates independently reviewed native Kind evidence; it does not represent a fresh rerun.','Product failures are valid verdicts, and their later unexecuted phases are not credited. Per-case evidence retains that scope.','Five prior precondition cleanup findings remain product findings with zero historical fault coverage; later prepared-source PASS does not erase them.','Four runner injection/oracle outcomes are retained without product verdict credit and have independent valid supplements.','Other facility setup attempts are preserved in task documentation; independentAuditRecords is not a claim of all execution attempts.','Kthena production source was not modified; category3 and the full708-case goal remain in progress.']}
    out=ROOT/'artifacts'/(sys.argv[1] if len(sys.argv)>1 else 'category2-final-verification');out.mkdir();(out/'summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n')
    lines=['# Category 2: independent Kind verification complete','','236 unique cases:128 PASS,108 Kthena behavior failures. Production '+manifest['controllerCommit']+'.','','Five earlier precondition cleanup findings and four runner outcomes are preserved separately. Case failures do not imply that later phases executed. No Kthena source modifications.','','| Case | Verdict | Independent audit | Limitation |','| --- | --- | --- | --- |']
    for row in accepted:lines.append('| '+row['id']+' | '+row['status']+' | '+row['audit']+' | '+str(row['evidence'].get('limitation','')).replace('|','/').replace('\n',' ')+' |')
    (out/'REPORT.md').write_text('\n'.join(lines)+'\n');print(json.dumps({'cases':len(accepted),'counts':counts,'preserved':summary['preservedNoncreditedCounts'],'watchRows':summary['watchRowsInAcceptedReviews']}))

if __name__=='__main__':main()
