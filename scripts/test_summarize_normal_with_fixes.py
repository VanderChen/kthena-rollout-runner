#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Synthetic correction-report counterexamples; never written into Kind results."""
import copy
import datetime
import importlib.util
import json
import pathlib
import tempfile
import unittest

import test_summarize_normal as fixtures

SPEC = importlib.util.spec_from_file_location('fix_report', pathlib.Path(__file__).with_name('summarize-normal-with-fixes.py'))
REPORT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(REPORT)
CATALOGUE = {r['id']: r for r in REPORT.read(REPORT.ROOT / 'cases/normal/suite.json')['cases']}


def at(seconds):
    return (datetime.datetime(2026, 9, 8, tzinfo=datetime.timezone.utc) + datetime.timedelta(seconds=seconds)).isoformat(timespec='microseconds').replace('+00:00', 'Z')


def save(path, value):
    path.write_text(json.dumps(value))


def fixture(root, cid='RUN-247', fixed=False):
    root.mkdir(exist_ok=True)
    p = root / 'attempt-1'
    p.mkdir()
    ns, uid = 'synthetic-' + cid.lower(), cid + '-model'
    initial_n = 1 if cid == 'RUN-247' else 2
    partition = int(cid == 'RUN-257')
    final_n, final_r = (1, 8) if cid == 'RUN-247' else (3, 6)
    def container(ver):
        return dict(name='workload', command=['sh', '-c', 'touch /tmp/ready; sleep 3600'], env=[dict(name='ROLLOUT_VERSION', value=ver)])
    def model(rv, generation, n, replicas, version):
        roles = [dict(name='backend', replicas=1, workerReplicas=0, maxUnavailable=1, entryTemplate=dict(spec=dict(containers=[container('A')]))),
                 dict(name='frontend', replicas=replicas, workerReplicas=0, maxUnavailable=1, maxSurge=0, partition=partition,
                      entryTemplate=dict(spec=dict(containers=[container(version)])))]
        return dict(apiVersion='workload.serving.volcano.sh/v1alpha1', kind='ModelServing',
                    metadata=dict(name='model', namespace=ns, uid=uid, resourceVersion=rv, generation=generation),
                    spec=dict(replicas=n, rolloutStrategy=dict(type='RoleRollingUpdate'), template=dict(roles=roles)))
    def pod(g, role, i, ver, identity):
        return dict(apiVersion='v1', kind='Pod', metadata=dict(name=f'model-{g}-{role}-{i}-0', namespace=ns, uid=identity,
                    ownerReferences=[dict(uid=uid, controller=True)], labels={REPORT.GROUP:f'model-{g}', REPORT.ROLE:role, REPORT.ROLE_ID:f'{role}-{i}'}),
                    spec=dict(containers=[container(ver)]), status=dict(conditions=[dict(type='Ready', status='True')]))
    before = model('10', 1, initial_n, 6, 'A')
    first = model('20', 2, initial_n, 6, 'B')
    current = model('21', 2, initial_n, 6, 'B')
    refreshed = model('22', 2, initial_n, 6, 'B')
    desired = model('21', 2, final_n, final_r, 'B')
    accepted = model('30', 3, final_n, final_r, 'B')
    baseline = {}
    for g in range(initial_n):
        for role, count in [('frontend',6), ('backend',1)]:
            for i in range(count):
                identity = f'old-{g}-{role}-{i}'
                baseline[identity] = pod(g, role, i, 'A', identity)
    old5 = copy.deepcopy(baseline['old-0-frontend-5'])
    old5['metadata']['deletionTimestamp'] = at(5.5)
    trigger_pods = copy.deepcopy(baseline)
    trigger_pods[old5['metadata']['uid']] = old5
    events = []
    def event(kind, action, time, obj):
        events.append(dict(sequence=len(events)+1, received=at(time), kind=kind, event=action, object=copy.deepcopy(obj)))
    event('modelservings','ADDED',1,before)
    for obj in baseline.values():
        event('pods','ADDED',2,obj)
    event('modelservings','MODIFIED',5,first)
    event('pods','MODIFIED',5.5,old5)
    event('modelservings','MODIFIED',6,current)
    event('modelservings','MODIFIED',8.2,refreshed)
    source = dict(profile='auto', source=CATALOGUE[cid], steps=[dict(action='update', release='none', holdSeconds=0), dict(action='update', release='none', holdSeconds=0)])
    save(p/'case.yaml', dict(id=cid, scenario=source))
    save(p/'before-server.yaml', before)
    save(p/'baseline-resources.yaml', dict(pods=baseline, modelservings={uid:before}))
    save(p/'step-01-server.yaml',first)
    first_request = copy.deepcopy(first)
    first_request['metadata'].update(resourceVersion='10', generation=1)
    save(p/'step-01-request.yaml',first_request)
    first_time = dict(sent=at(4.1), received=at(4.2), generation=2, resourceVersion='20')
    save(p/'step-01-request-time.json',first_time)
    cp = [dict(phase='baseline', completed=at(3), stableSince=at(2), stableSeconds=1, holdSeconds=0),
          dict(phase='observe-natural-rollout-start', completed=at(6), stableSince=at(6), stableSeconds=0, holdSeconds=0)]
    def trigger(prefix, time, rv):
        proof = dict(at=at(time), resourceVersion=rv, eligibleOldMemberPresent=True)
        lst = dict(apiVersion='v1', kind='PodList', metadata=dict(resourceVersion=rv), items=list(trigger_pods.values()))
        save(p/(prefix+'.yaml'),lst)
        save(p/(prefix+'-proof.json'),proof)
    trigger('step-02-trigger-before',7,'21')
    save(p/'step-02-request.yaml',desired)
    final = trigger_pods
    result = dict(id=cid, namespace=ns, status='FAIL', durationSeconds=9, checkpoints=2, releases=0,
                  error='step 02 separate-scale-during-natural-rollout: Operation cannot be fulfilled on modelservings.workload.serving.volcano.sh "model": the object has been modified; please apply your changes to the latest version and try again')
    final_model = refreshed
    if fixed:
        result.update(status='PASS', durationSeconds=51, checkpoints=3)
        result.pop('error')
        event('modelservings','MODIFIED',11,accepted)
        final = {}
        for g in range(final_n):
            for role, count in [('frontend',final_r), ('backend',1)]:
                for i in range(count):
                    kept = g < initial_n and (role == 'backend' or (partition and i == 0))
                    identity = f'old-{g}-{role}-{i}' if kept else f'new-{g}-{role}-{i}'
                    ver = 'A' if role=='backend' or kept else 'B'
                    final[identity] = copy.deepcopy(baseline[identity]) if kept else pod(g,role,i,ver,identity)
        for identity,obj in baseline.items():
            if identity not in final:
                event('pods','DELETED',18,obj)
        for identity,obj in final.items():
            if identity not in baseline:
                event('pods','ADDED',19,obj)
        final_model = accepted
        second_time = dict(sent=at(10), received=at(10.1), generation=3, resourceVersion='30')
        save(p/'step-02-request-time.json',second_time)
        save(p/'step-02-server.yaml',accepted)
        desired['metadata']['resourceVersion']='22'
        save(p/'step-02-request.yaml',desired)
        trigger('step-02-trigger-before',9,'22')
        trigger('step-02-trigger-after',10.5,'30')
        cp.append(dict(phase='separate-scale-during-natural-rollout', stableSince=at(20), completed=at(50), stableSeconds=30, holdSeconds=0))
        for phase, attempt, cur, submitted, response, times in [
            (1,1,before,first_request,first,first_time),
            (2,1,current,model('21',2,final_n,final_r,'B'),None,dict(sent=at(8),received=at(8.1))),
            (2,2,refreshed,desired,accepted,second_time),
        ]:
            prefix=f'step-{phase:02d}-write-{attempt:02d}'
            save(p/(prefix+'-current.yaml'),cur)
            save(p/(prefix+'-request.yaml'),submitted)
            receipt=dict(attempt=attempt,uid=uid,requestResourceVersion=cur['metadata']['resourceVersion'],sent=times['sent'],received=times['received'])
            if response is None:
                receipt.update(error='conflict',status=dict(code=409,reason='Conflict'))
            else:
                receipt.update(generation=response['metadata']['generation'],resourceVersion=response['metadata']['resourceVersion'])
                save(p/(prefix+'-server.yaml'),response)
            save(p/(prefix+'-receipt.json'),receipt)
            if phase==2:
                trigger(prefix+'-trigger-before',7 if attempt==1 else 9,'21' if attempt==1 else '22')
    for i,c in enumerate(cp,1):
        save(p/f'checkpoint-{i:03d}.json',c)
    save(p/'final-resources.yaml',dict(pods=final,modelservings={uid:final_model}))
    (p/'observations.jsonl').write_text(''.join(json.dumps(e)+'\n'for e in events))
    save(p/'result.json',result)
    save(root/'result.json',result)
    save(root/'attempts.json',[result])
    return result


class CorrectionTests(unittest.TestCase):
    setUp = fixtures.AddendumTests.setUp
    save = fixtures.AddendumTests.save
    run_artifacts = fixtures.AddendumTests.run_artifacts
    triple = fixtures.BoundaryTests.triple

    def case(self,cid='RUN-247',fixed=True):
        root=self.root/cid
        fixture(root,cid,fixed)
        return root

    def change(self,root,name,fn):
        path=root/'attempt-1'/name
        value=REPORT.read(path)
        fn(value)
        save(path,value)

    def test_original_three_conflicts_read_from_raw(self):
        for cid in sorted(REPORT.FIX_IDS):
            root=self.case(cid,False)
            evidence=REPORT.verify_original_conflict(root)
            self.assertEqual(evidence['classification'],'RUNNER_IMPLEMENTATION_FAILURE')
            self.assertEqual(REPORT.read(root/'result.json')['status'],'FAIL')

    def test_three_new_complete_layouts(self):
        for cid,want in [('RUN-247',9),('RUN-255',21),('RUN-257',21)]:
            proof=REPORT.verify_new_execution(self.case(cid))
            self.assertEqual(proof['completion']['finalReadyPods'],want)
            self.assertEqual(len(proof['writes']),3)

    def test_original_accepted_second_request_is_rejected(self):
        root=self.case(fixed=False)
        save(root/'attempt-1/step-02-server.yaml',{})
        with self.assertRaisesRegex(ValueError,'second request was accepted'):
            REPORT.verify_original_conflict(root)

    def test_missing_final_checkpoint_is_rejected(self):
        root=self.case();(root/'attempt-1/checkpoint-003.json').unlink()
        with self.assertRaises(FileNotFoundError):REPORT.verify_new_execution(root)

    def test_legacy_clock_difference_requires_verified_monotonic_binary(self):
        root=self.case();self.change(root,'checkpoint-003.json',lambda x:x.update(completed=at(49.999872)))
        with self.assertRaisesRegex(ValueError,'30 second stability'):
            REPORT.verify_new_execution(root)
        with self.assertRaisesRegex(ValueError,'30 second stability'):
            REPORT.verify_new_execution(root,verified_binary='unknown')
        proof=REPORT.verify_new_execution(root,verified_binary=REPORT.FIX_BINARY)
        self.assertEqual(proof['completion']['clockEvidence']['monotonicMinimumSeconds'],30)
        self.assertAlmostEqual(proof['completion']['clockEvidence']['wallSeconds'],29.999872)

    def test_larger_clock_gap_still_fails_closed(self):
        root=self.case();self.change(root,'checkpoint-003.json',lambda x:x.update(completed=at(49.99)))
        with self.assertRaisesRegex(ValueError,'30 second stability'):
            REPORT.verify_new_execution(root,verified_binary=REPORT.FIX_BINARY)

    def test_lost_actual_after_trigger_is_rejected(self):
        root=self.case();self.change(root,'step-02-trigger-after.yaml',lambda x:x.update(items=[]))
        with self.assertRaisesRegex(ValueError,'no original eligible A'):REPORT.verify_new_execution(root)

    def test_retry_trigger_must_be_rechecked(self):
        root=self.case();(root/'attempt-1/step-02-write-02-trigger-before-proof.json').unlink()
        with self.assertRaises(FileNotFoundError):REPORT.verify_new_execution(root)

    def test_non_conflict_failure_cannot_be_retried(self):
        root=self.case();self.change(root,'step-02-write-01-receipt.json',lambda x:x['status'].update(code=403,reason='Forbidden'))
        with self.assertRaisesRegex(ValueError,'non-conflict failure'):REPORT.verify_new_execution(root)

    def test_changed_spec_on_retry_is_rejected(self):
        root=self.case();self.change(root,'step-02-write-02-current.yaml',lambda x:x['spec'].update(replicas=2))
        with self.assertRaisesRegex(ValueError,'overwrote changed spec'):REPORT.verify_new_execution(root)

    def test_canonical_request_cannot_point_to_failed_attempt(self):
        root=self.case();self.change(root,'step-02-request.yaml',lambda x:x['metadata'].update(resourceVersion='21'))
        with self.assertRaisesRegex(ValueError,'canonical submitted request differs'):REPORT.verify_new_execution(root)

    def test_short_final_stability_is_rejected(self):
        root=self.case();self.change(root,'checkpoint-003.json',lambda x:x.update(stableSince=at(30)))
        with self.assertRaisesRegex(ValueError,'30 second stability'):REPORT.verify_new_execution(root)

    def test_new_group_frontend_zero_must_be_B(self):
        root=self.case('RUN-257')
        def change(x):x['pods']['new-2-frontend-0']['spec']['containers'][0]['env'][0]['value']='A'
        self.change(root,'final-resources.yaml',change)
        with self.assertRaisesRegex(ValueError,'wrong final template version'):REPORT.verify_new_execution(root)

    def test_replaced_original_backend_is_rejected(self):
        root=self.case('RUN-255')
        def change(x):
            pod=x['pods'].pop('old-0-backend-0');pod['metadata']['uid']='replacement';x['pods']['replacement']=pod
        self.change(root,'final-resources.yaml',change)
        with self.assertRaisesRegex(ValueError,'protected/backend UID replaced'):REPORT.verify_new_execution(root)

    def combined(self):
        base,a301,a183=self.triple()
        for cid in sorted(REPORT.FIX_IDS):fixture(base/cid,cid,False)
        summary=REPORT.read(base/'summary.json')
        summary['results']=[REPORT.read(base/r['id']/'result.json')for r in summary['results']]
        summary['passed']=sum(r['status']=='PASS'for r in summary['results']);save(base/'summary.json',summary)
        new=self.run_artifacts('fixed',sorted(REPORT.FIX_IDS),self.current)
        for cid in sorted(REPORT.FIX_IDS):fixture(new/cid,cid,True)
        summary=REPORT.read(new/'summary.json');summary['results']=[REPORT.read(new/cid/'result.json')for cid in sorted(REPORT.FIX_IDS)];save(new/'summary.json',summary)
        pod=REPORT.read(new/'runner-pod.json');pod['status']['containerStatuses'][0]['imageID']=REPORT.FIX_IMAGE;save(new/'runner-pod.json',pod)
        env=REPORT.read(new/'environment.json');env['runner']['runnerPod']=pod;env['runner']['binarySHA256']=REPORT.FIX_BINARY;save(new/'environment.json',env)
        return base,a301,a183,new

    def report(self,dirs):
        base,a301,a183,new=dirs
        return REPORT.verify([base],self.original_path,a301,a183,new,dict(runnerCommit=REPORT.FIX_COMMIT,dockerImageID=REPORT.FIX_IMAGE,binarySHA256=REPORT.FIX_BINARY))

    def test_full_aggregate_preserves_originals_and_other_failure(self):
        dirs=self.combined();report=self.report(dirs)
        self.assertEqual((report['total'],report['executionCount']),(303,308))
        self.assertEqual(report['counts'],{'PASS':302,'FAIL':1})
        self.assertEqual(next(r for r in report['results']if r['id']=='RUN-193')['status'],'FAIL')
        self.assertEqual(len(report['runnerCandidates']),2)
        self.assertNotIn('runnerImageID',report)
        for item in report['runnerWriteCorrection']['corrections']:
            self.assertEqual(item['originalResult']['status'],'FAIL')
            self.assertEqual(item['acceptedResult']['status'],'PASS')
            self.assertNotEqual(item['originalResult']['provenance']['runnerImageID'],item['acceptedResult']['provenance']['runnerImageID'])
        self.assertEqual(REPORT.read(dirs[0]/'RUN-247/result.json')['status'],'FAIL')

    def test_wrong_fixed_binary_is_rejected(self):
        dirs=self.combined();p=dirs[-1]/'environment.json';e=REPORT.read(p);e['runner']['binarySHA256']='unreviewed';save(p,e)
        with self.assertRaisesRegex(ValueError,'wrong runner identity'):self.report(dirs)

    def test_controller_change_is_rejected(self):
        dirs=self.combined();p=dirs[-1]/'environment.json';e=REPORT.read(p);e['controllerPods'][0]['status']['containerStatuses'][0]['imageID']='another-controller';save(p,e)
        with self.assertRaisesRegex(ValueError,'production controller changed'):self.report(dirs)

    def test_changed_input_is_rejected(self):
        dirs=self.combined();p=dirs[-1]/'environment.json';e=REPORT.read(p);e['runner']['caseSHA256']['RUN-255.yaml']='different';save(p,e)
        with self.assertRaisesRegex(ValueError,'case inputs differ'):self.report(dirs)

    def test_another_original_conflict_requires_review(self):
        dirs=self.combined();p=dirs[0]/'summary.json';s=REPORT.read(p)
        row=next(r for r in s['results']if r['id']=='RUN-193');row['error']=REPORT.read(dirs[0]/'RUN-247/result.json')['error'];save(p,s);save(dirs[0]/'RUN-193/result.json',row)
        with self.assertRaisesRegex(ValueError,'additional or missing original conflicts'):self.report(dirs)

    def test_unfinished_fix_job_is_rejected(self):
        dirs=self.combined();p=dirs[-1]/'runner-pod.json';o=REPORT.read(p);o['status']['containerStatuses'][0]['state']={'running':{}};save(p,o)
        with self.assertRaisesRegex(ValueError,'Pod exit status'):self.report(dirs)

    def test_new_product_failure_remains_fail(self):
        root=self.case();r=REPORT.read(root/'result.json');r.update(status='FAIL',error='step 02 separate-scale-during-natural-rollout: TIMEOUT: orphan ranktable',checkpoints=2)
        for path in(root/'result.json',root/'attempt-1/result.json'):save(path,r)
        save(root/'attempts.json',[r])
        self.assertEqual(REPORT.verify_new_execution(root)['completion']['rawStatus'],'FAIL')


if __name__=='__main__':unittest.main()
