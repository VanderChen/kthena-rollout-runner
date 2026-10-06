# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Check floor(U%) at runtime: keep old Pods while a surge Pod is unready."""
import argparse
import hashlib
import copy
import json
import subprocess
import time
from pathlib import Path
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--kubeconfig',required=True)
parser.add_argument('--artifacts',required=True,type=Path)
parser.add_argument('--namespace',default='runner-budget-contract')
parser.add_argument('--controller-image',required=True)
parser.add_argument('--controller-commit',required=True)
args=parser.parse_args()
out=args.artifacts
out.mkdir(parents=True,exist_ok=False)
cmd=['kubectl','--kubeconfig',args.kubeconfig,'--request-timeout=20s']
namespace=args.namespace
records, summary = [], []

def run(args, obj=None):
    result = subprocess.run(cmd + args, input=json.dumps(obj) if obj is not None else None, capture_output=True, text=True, timeout=30)
    records.append({'command': cmd + args, 'input': obj, 'exitCode': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr})
    if result.returncode:
        raise RuntimeError(result.stderr)
    return json.loads(result.stdout) if result.stdout.startswith('{') else result.stdout

def pods(ms):
    return [p for p in run(['get', 'pods', '-n', namespace, '-o', 'json'])['items'] if any(o['uid'] == ms['metadata']['uid'] for o in p['metadata'].get('ownerReferences', []))]

def ready(p):
    return not p['metadata'].get('deletionTimestamp') and any(c['type'] == 'Ready' and c['status'] == 'True' for c in p.get('status', {}).get('conditions', []))

def version(p):
    return next(e['value'] for e in p['spec']['containers'][0]['env'] if e['name'] == 'VERSION')

def wait(ms, predicate, message):
    deadline = time.monotonic() + 150
    while time.monotonic() < deadline:
        observed = pods(ms)
        if predicate(observed):
            return observed
        time.sleep(1)
    raise AssertionError(message)

try:
    controller=run(['-n','kthena-system','get','deployment','kthena-controller-manager','-o','json'])
    assert args.controller_image in [c['image'] for c in controller['spec']['template']['spec']['containers']]
    (out/'provenance.json').write_text(json.dumps({'controllerCommit':args.controller_commit,'controllerImage':args.controller_image,'runnerSHA256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'contractVersion':'2.1','controller':controller},indent=2))
    run(['create', 'namespace', namespace])
    for mode in ['ServingGroupRollingUpdate', 'RoleRollingUpdate']:
        sg = mode.startswith('Serving')
        budget = {'maxUnavailable': '25%', 'maxSurge': 1}
        role = {'name': 'inference', 'replicas': 1 if sg else 3, 'workerReplicas': 0,
                'entryTemplate': {'spec': {'terminationGracePeriodSeconds': 1, 'containers': [{
                    'name': 'server', 'image': 'busybox:1.36', 'imagePullPolicy': 'IfNotPresent',
                    'command': ['sh', '-c', 'sleep 3600'], 'env': [{'name': 'VERSION', 'value': 'v1'}],
                    'readinessProbe': {'exec': {'command': ['sh', '-c', 'test "$VERSION" = v1 || test -f /tmp/ready']}, 'periodSeconds': 1, 'failureThreshold': 1}
                }]}}}
        strategy = {'type': mode}
        if sg:
            strategy['rollingUpdateConfiguration'] = budget
            role.update(maxUnavailable=100,maxSurge=100,partition=100)
        else:
            role.update(budget)
            strategy['rollingUpdateConfiguration']={'maxUnavailable':100,'maxSurge':100,'partition':100}
        obj = {'apiVersion': 'workload.serving.volcano.sh/v1alpha1', 'kind': 'ModelServing',
               'metadata': {'name': 'sg-floor' if sg else 'role-floor', 'namespace': namespace},
               'spec': {'replicas': 3 if sg else 1, 'schedulerName': 'volcano', 'recoveryPolicy': 'RoleRecreate',
                        'rolloutStrategy': strategy, 'template': {'roles': [role]}}}
        ms = run(['create', '-f', '-', '-o', 'json'], obj)
        before = wait(ms, lambda ps: len(ps) == 3 and all(ready(p) for p in ps), 'initial Pods did not become ready')
        old_uids = {p['metadata']['uid'] for p in before}
        run(['patch', 'modelserving', ms['metadata']['name'], '-n', namespace, '--type=json', '-p', json.dumps([{'op': 'replace', 'path': '/spec/template/roles/0/entryTemplate/spec/containers/0/env/0/value', 'value': 'v2'}]), '-o', 'json'])
        wait(ms, lambda ps: any(version(p) == 'v2' and p.get('status', {}).get('phase') == 'Running' for p in ps), 'surge Pod did not start')
        samples = []
        for _ in range(10):
            observed = pods(ms)
            old = [p for p in observed if p['metadata']['uid'] in old_uids]
            new = [p for p in observed if version(p) == 'v2']
            assert len(old) == 3 and all(ready(p) for p in old), 'old ready Pods were removed with effective U=0'
            assert len(observed) == 4 and len(new) == 1 and not ready(new[0]), 'surge should wait for readiness'
            samples.append({'oldReady': len(old), 'total': len(observed), 'newReady': sum(ready(p) for p in new)})
            time.sleep(1)
        print(mode, 'preserved all 3 ready old Pods while surge was unready', flush=True)
        signaled = set()
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            observed = pods(ms)
            for p in observed:
                name = p['metadata']['name']
                if version(p) == 'v2' and p.get('status', {}).get('phase') == 'Running' and p['metadata']['uid'] not in signaled:
                    run(['exec', '-n', namespace, name, '--', 'touch', '/tmp/ready'])
                    signaled.add(p['metadata']['uid'])
            if len(observed) == 3 and all(version(p) == 'v2' and ready(p) for p in observed):
                assert old_uids.isdisjoint(p['metadata']['uid'] for p in observed)
                break
            time.sleep(1)
        else:
            raise AssertionError('rollout did not finish after surge Pods became ready')
        summary.append({'mode': mode, 'replicas': 3, 'maxUnavailable': '25%', 'effectiveMaxUnavailable': 0, 'maxSurge': 1, 'unreadySurgeSamples': samples, 'finalReadyNewPods': 3})
        print(mode, 'completed with 3 ready new Pods', flush=True)
        run(['delete', 'modelserving', ms['metadata']['name'], '-n', namespace, '--wait=true'])
finally:
    (out / 'kind-surge-results.json').write_text(json.dumps(records, indent=2) + '\n')
    (out / 'kind-surge-summary.json').write_text(json.dumps(summary, indent=2) + '\n')
