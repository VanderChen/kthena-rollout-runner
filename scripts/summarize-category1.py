#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Verify all first-category executions, preserving every superseded raw verdict."""
import argparse
import collections
import copy
import hashlib
import importlib.util
import json
import pathlib

ROOT=pathlib.Path(__file__).resolve().parents[1]
SPEC=importlib.util.spec_from_file_location('write_report',ROOT/'scripts/summarize-normal-with-fixes.py')
WRITE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(WRITE)
BASE=WRITE.BASE
read,require,stamp,digest=WRITE.read,BASE.require,WRITE.stamp,WRITE.digest
R12=('d6e8c723a0efd57c3d4989d54aeafd4a1a137015','sha256:a2419ddf37da85d5fec0ce16c0c342e2614d2a2eae0f7dc38afd9aba7da74bf3','0642238b7718f42b7a2f39445a0b8025fe5f95067c0d193d2f4a6c463d21d70f')
R13=('8c216c5c0f8d875842ce2f1d6a39d0bc272d5a39','sha256:93f93fde0fbc5c2e5c0d2425c508da703ca5ad7909ee1ca166187088a7142fb1','a4feb696e5375d4a8194d70b3da90e39b2afc27b5433ceac4f6e8711597f5b95')
KEYS=('runnerImageID','binarySHA256','controllerCommit','controllerImage','controllerImageID')
def identity(report):return {k:report[k] for k in KEYS}
def candidate(report,build,pin):
    require((build['runnerCommit'],build['dockerImageID'],build['binarySHA256'])==pin,'unreviewed intent runner build')
    require((report['runnerImageID'],report['binarySHA256'])==pin[1:],'intent runner identity mismatch')
    require(build['caseInputs']==read(ROOT/'cases/normal/suite.json')['inputs'],'intent runner input mismatch')
def prior_review(root):
    e=BASE.restore_deletion_evidence(root)
    require(e['relation']=='BEFORE_REQUEST','prior execution is not a proved pre-request intent')
    original=read(root/'result.json');classification=read(root/'classification.json')
    require(classification['classification']=='RUNNER_OBSERVATION_FAILURE' and classification['originalStatus']==original['status'] and classification['originalError']==original['error'],'prior runner classification differs from raw result')
    paths=[root/'attempt-1'/f'step-{i:02d}-server.yaml' for i in (2,3)]
    docs=WRITE.yaml_files(paths);old=docs[paths[0].name]['spec'];new=docs[paths[1].name]['spec']
    changed=copy.deepcopy(old);roles={r['name']:r for r in changed['template']['roles']};r=roles['frontend']
    require(r['replicas']==1 and r['maxUnavailable']==1 and r.get('maxSurge',0)==r.get('partition',0)==r.get('workerReplicas',0)==0,'prior budget does not permit old deletion')
    r['replicas']=3;require(changed==new,'restore changed fields other than replicas')
    return dict(e,classification=classification,classificationSHA256=digest(root/'classification.json'))
def latest_review(root):
    completion=BASE.verify_restore_completion(root)
    p=root/'attempt-1';receipt=read(p/'step-03-prior-intent-review.json');before=read(p/'step-03-prior-intent-before-ledger.json');after=read(p/'ledger.json')
    e=BASE.rapid_restore_evidence(root);uid=receipt['uid'];start=receipt['originalStart'];request=e['request']
    require(receipt['owner']==e['owner']==before['owner']==after['owner'] and start['uids']==[uid], 'correction owner/UID mismatch')
    require(receipt['request']=={'Sent':request['sent'],'Received':request['received']},'correction request differs from actual API receipt')
    require(stamp(receipt['intentTime'])<stamp(request['sent']) and stamp(receipt['intentTime'])<stamp(start['at']),'correction intent not before request and Pod observation')
    require(receipt['previousDesired']==1 and receipt['previousMinimum']==0 and before['violations']==[receipt['originalViolation']] and not after['violations'],'correction changes other violations')
    require(before['starts'][-1]==start and start['reason']=='rollout' and start['minimum']==2 and start['readyBefore']==1 and start['ordinal']==0,'correction original start differs')
    require(before['committedUIDs'].get(uid),'correction lacks finite old UID commitment')
    expected=copy.deepcopy(before['starts']);expected[-1]['reason']='rollout-in-flight';expected[-1]['minimum']=0
    require(after['starts'][:len(expected)]==expected,'correction changed another recorded action')
    pods=WRITE.yaml_files([p/'step-03-prior-intent-pods.yaml'])['step-03-prior-intent-pods.yaml']['pods']
    target=pods[uid];require(e['owned'](target) and BASE.pod_version(target)=='A','correction target has wrong owner/version')
    matching=[x for x in e['events'] if x['kind']=='pods' and x['object']['metadata']['uid']==uid]
    require(matching and any(x['object']['metadata'].get('deletionTimestamp') for x in matching),'corrected UID absent from real Watch')
    for log in [p/'step-03-prior-intent-controller.log',root/'controller.log']:
        lines=[x for x in log.read_text().splitlines() if 'object="'+e['result']['namespace']+'/model"' in x and 'reason="RoleDeleting"' in x and 'message="Role frontend/frontend-0 in ServingGroup model-0 is now Deleting"' in x and stamp(matching[0]['received']) < stamp(x.split()[0]) < stamp(start['at'])]
        require(len(lines)==1 and stamp(lines[0].split()[0])==stamp(receipt['intentTime']),'correction intent log differs')
    docs=WRITE.yaml_files([p/'step-02-server.yaml',p/'step-03-server.yaml']);old,new=docs['step-02-server.yaml'],docs['step-03-server.yaml']
    spec=copy.deepcopy(old['spec']);roles={r['name']:r for r in spec['template']['roles']};r=roles['frontend']
    require(r['replicas']==1 and r['maxUnavailable']==1 and r.get('maxSurge',0)==r.get('partition',0)==r.get('workerReplicas',0)==0,'correction old budget mismatch')
    r['replicas']=3;require(spec==new['spec'],'correction did not accompany only a replicas restore')
    cp=read(p/'checkpoint-004.json');elapsed=cp['elapsedStableNanos'];require(type(elapsed)is int and elapsed>=30_000_000_000,'monotonic final window missing')
    paths=list(p.glob('step-03-prior-intent-*'))+[p/'ledger.json',p/'checkpoint-004.json',root/'controller.log']
    return dict(completion=completion,receipt=receipt,monotonicStableNanos=elapsed,evidenceSHA256={str(x.relative_to(root)):digest(x) for x in paths})
def verify(artifacts):
    env=artifacts/'environment-022';original=artifacts/'normal-r10-303';frozen=env/'r10-frozen-suite.json'
    report=BASE.verify_with_301_addendum([original],frozen,artifacts/'normal-r10-301-addendum')
    original183=copy.deepcopy(next(r for r in report['results'] if r['id']=='RUN-183'))
    original183review=BASE.verify_restore_boundary(original/'RUN-183')
    report=WRITE.apply_write_corrections(report,frozen,artifacts/'normal-r11-write-fix',read(env/'runner-r11-build.json'))
    preserved=[]
    for run,pin in [('normal-r10-183-addendum',None),('normal-r12-183-intent',R12)]:
        directory=artifacts/run;part=BASE.verify([directory],frozen if pin is None else None,{'RUN-183'})
        if pin:candidate(part,read(env/'runner-r12-build.json'),pin)
        else:require(identity(part)==report['runnerCandidates'][0],'old183 candidate changed')
        require(all(part[k]==report[k] for k in KEYS[2:]),'prior controller identity changed')
        preserved.append(dict(runID=run,result=part['results'][0],provenance=identity(part),review=prior_review(directory/'RUN-183')))
        report['shards']+=part['shards']
        if identity(part) not in report['runnerCandidates']:report['runnerCandidates'].append(identity(part))
    latest=artifacts/'normal-r13-183-intent';part=BASE.verify([latest],required_ids={'RUN-183'});candidate(part,read(env/'runner-r13-build.json'),R13)
    require(all(part[k]==report[k] for k in KEYS[2:]),'latest controller identity changed')
    proof=latest_review(latest/'RUN-183');row=dict(part['results'][0],provenance=identity(part),priorIntentCorrectionEvidence=proof)
    require(row['status']=='PASS','latest183 requires manual failure review')
    report['results']=[row if r['id']=='RUN-183' else r for r in report['results']]
    report['shards']+=part['shards'];report['runnerCandidates'].append(identity(part))
    report['priorIntentCorrection']=dict(originalResult=original183,originalReview=original183review,preservedExecutions=preserved,acceptedResult=row)
    failures=[]
    for result in report['results']:
        if result['status']=='PASS':continue
        require(result['status']=='FAIL','unresolved final result')
        path=pathlib.Path(result['artifacts'])/'classification.json';c=read(path)
        require(c['classification']=='KTHENA_BEHAVIOR_FAILURE' and c['originalStatus']==result['status'] and c['originalError']==result['error'],'unclassified final failure')
        failures.append(dict(id=result['id'],classification=c,classificationSHA256=digest(path)))
    startup=read(artifacts/'normal-r10-183-boundary-addendum/startup-failure.json');require(startup['caseExecutions']==0,'startup failure counted as scenario')
    require(all(digest(artifacts/'normal-r10-183-boundary-addendum'/name)==sha for name,sha in startup['evidenceSHA256'].items()),'startup raw evidence changed')
    environment=read(env/'category1-final-environment.json');require(environment['status']=='PASS' and environment['historicalUIDsPreserved']==9 and not environment['residualTestNamespaces'] and environment['kthenaWorkingTreeClean'],'final environment not restored')
    report.update(counts=dict(collections.Counter(r['status'] for r in report['results'])),executionCount=sum(s['selected'] for s in report['shards']),confirmedFailures=failures,startupFailures=[startup],finalEnvironment=environment,category='1',validationComplete=True,scopeLimitation='Only category1 completed. Categories2/3 and the708-case goal remain active.')
    require(report['total']==303 and report['executionCount']==310 and len({s['runID'] for s in report['shards']})==6,'execution inventory differs')
    return report
def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--artifacts',type=pathlib.Path,default=ROOT/'artifacts');parser.add_argument('--output',type=pathlib.Path,required=True);args=parser.parse_args()
    report=verify(args.artifacts.resolve());args.output.mkdir(exist_ok=False)
    (args.output/'summary.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    text=['# 第一类最终验证','','303个独立用例，310次场景执行；'+', '.join(k+'='+str(v) for k,v in report['counts'].items())+'。另有1次启动失败，未执行任何用例。','', 'Kthena production '+report['controllerCommit']+'；源码未修改。', '', '原始303项为262 PASS/41 FAIL。301断言补测、183观察修正和247/255/257写冲突修正的所有原始结果与新旧runner身份均在JSON中保留。最终37项产品失败单独记录，不修复Kthena源码。', '', '9个历史ModelServing UID保持，测试namespace已清理，控制器Ready。第2/3类尚待实现与验证，708项目标未完成。','','| ID | 结果 | 原始执行目录 |','| --- | --- | --- |']
    text += ['| '+r['id']+' | '+r['status']+' | '+str(r['artifacts'])+' |' for r in report['results']]
    (args.output/'RESULTS.md').write_text('\n'.join(text)+'\n');print(json.dumps(dict(counts=report['counts'],executions=report['executionCount'],distinct=report['total'])))
if __name__=='__main__':main()
