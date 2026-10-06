# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env python3
"""Exercise RecoveryPolicy with real kubelet restarts in the local Kind cluster."""
import argparse
import hashlib
import copy
import json
from pathlib import Path
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--kubeconfig', required=True)
parser.add_argument('--artifacts', type=Path, required=True)
parser.add_argument('--namespace', default='runner-recovery-contract')
parser.add_argument('--controller-image', required=True)
parser.add_argument('--controller-commit', required=True)
parser.add_argument('--controller-namespace', default='kthena-system')
parser.add_argument('--controller-deployment', default='kthena-controller-manager')
args = parser.parse_args()
OUT = args.artifacts
OUT.mkdir(parents=True, exist_ok=False)
NS = args.namespace
CTX = ['kubectl', '--kubeconfig', args.kubeconfig, '--request-timeout=30s']
checks = []
commands = OUT / 'commands.jsonl'

def call(args, obj=None, check=True):
    cmd = CTX + args
    with commands.open('a') as f:
        f.write(json.dumps({'at': time.time(), 'command': cmd, 'input': obj}) + '\n')
    p = subprocess.run(cmd, input=json.dumps(obj) if obj is not None else None,
                       text=True, capture_output=True, timeout=70)
    if check and p.returncode:
        raise RuntimeError(f'{cmd}: {p.stderr}')
    return p

def get(resource, name=None):
    return json.loads(call(['-n', NS, 'get', resource] + ([name] if name else []) + ['-o', 'json']).stdout)

def save(name, obj):
    (OUT / (name + '.json')).write_text(json.dumps(obj, indent=2))

def record(name, details):
    checks.append({'check': name, 'details': details})
    save('summary', {'passed': checks})
    print('PASS', name, flush=True)

def poll(label, fn, timeout=150):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        value = fn()
        if value:
            return value
        time.sleep(1)
    raise AssertionError('timeout: ' + label)

def workload(name, policy, grace, restart='Always'):
    pod = {'spec': {
        'restartPolicy': restart, 'terminationGracePeriodSeconds': 1,
        'containers': [{'name': 'main', 'image': 'busybox:1.36', 'imagePullPolicy': 'IfNotPresent',
            'command': ['sh', '-c', 'while :; do if [ -e /state/restart ]; then rm /state/restart; exit 1; fi; sleep 1; done'],
            'resources': {'requests': {'cpu': '5m', 'memory': '8Mi'}, 'limits': {'memory': '32Mi'}},
            'readinessProbe': {'exec': {'command': ['sh', '-c', 'test ! -e /state/unready']}, 'periodSeconds': 1, 'failureThreshold': 1},
            'volumeMounts': [{'name': 'state', 'mountPath': '/state'}]}],
        'volumes': [{'name': 'state', 'emptyDir': {}}]}}
    return {'apiVersion': 'workload.serving.volcano.sh/v1alpha1', 'kind': 'ModelServing',
        'metadata': {'name': name, 'namespace': NS},
        'spec': {'schedulerName': 'volcano', 'replicas': 2, 'recoveryPolicy': policy,
            'template': {'restartGracePeriodSeconds': grace, 'roles': [
                {'name': 'server', 'replicas': 2, 'workerReplicas': 1, 'entryTemplate': pod, 'workerTemplate': copy.deepcopy(pod)}]}}}

def apply_workloads(items, filename):
    obj = {'apiVersion': 'v1', 'kind': 'List', 'items': items}
    save(filename, obj)
    call(['apply', '-f', '-'], obj)

def pods_for(name, allpods=None):
    allpods = allpods or get('pods')
    return {p['metadata']['name']: p for p in allpods['items'] if p['metadata'].get('labels', {}).get('modelserving.volcano.sh/name') == name}

def uids(pods):
    return {n: p['metadata']['uid'] for n, p in pods.items()}

def ready(p):
    return not p['metadata'].get('deletionTimestamp') and any(c['type'] == 'Ready' and c['status'] == 'True' for c in p.get('status', {}).get('conditions', []))

def wait_ready(name):
    def check():
        pods = pods_for(name)
        return pods if len(pods) == 8 and all(ready(p) for p in pods.values()) and get('modelserving', name).get('status', {}).get('availableReplicas') == 2 else None
    return poll(name + ' ready', check, 240)

def target(name):
    return name + '-0-server-0-1'

def fault(name):
    call(['-n', NS, 'exec', target(name), '--', 'sh', '-c', 'touch /state/unready /state/restart'])

def patch(name, spec):
    call(['-n', NS, 'patch', 'modelserving', name, '--type=merge', '-p', json.dumps({'spec': spec})])

def changed(before, after):
    return sorted(n for n, uid in before.items() if after.get(n) != uid)

def expected_scope(name, policy):
    before = pods_for(name)
    if policy == 'None':
        return [target(name)]
    prefix = name + '-0-' + ('server-0-' if policy == 'RoleRecreate' else '')
    return sorted(n for n in before if n.startswith(prefix))

def snapshots(label):
    save(label + '-pods', get('pods'))
    save(label + '-modelservings', get('modelservings'))

def main():
    deployment = json.loads(call(['-n', args.controller_namespace, 'get', 'deployment', args.controller_deployment, '-o', 'json']).stdout)
    assert deployment['spec']['template']['spec']['containers'][0]['image'] == args.controller_image
    save('provenance', {'controllerCommit': args.controller_commit, 'controllerImage': args.controller_image, 'runnerSHA256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), 'contractVersion': '2.1'})
    save('controller-before', deployment)
    call(['create', 'namespace', NS])
    cases = {
        'role-infinite': ('RoleRecreate', -1), 'group-infinite': ('ServingGroupRecreate', -1),
        'none-default': ('None', 0), 'role-zero': ('RoleRecreate', 0),
        'group-zero': ('ServingGroupRecreate', 0), 'role-finite': ('RoleRecreate', 30),
        'group-finite': ('ServingGroupRecreate', 30),
    }
    apply_workloads([workload(n, *cfg) for n, cfg in cases.items()], 'workloads')
    before = {n: uids(wait_ready(n)) for n in cases}
    snapshots('initial')
    scopes = {n: expected_scope(n, cfg[0]) for n, cfg in cases.items()}
    faults = {}
    for n in cases:
        fault(n)
        faults[n] = time.monotonic()
    protected = ['role-infinite', 'group-infinite', 'none-default']
    restarts = set()
    def recovered():
        allpods = get('pods')
        done = True
        for n, (policy, grace) in cases.items():
            ps = pods_for(n, allpods)
            current = uids(ps)
            if n in protected:
                assert current == before[n] and not any(p['metadata'].get('deletionTimestamp') for p in ps.values()), (n, changed(before[n], current))
                status = ps[target(n)].get('status', {})
                if any(s.get('restartCount', 0) > 0 for s in status.get('containerStatuses', [])):
                    restarts.add(n)
            else:
                if grace > 0 and time.monotonic() - faults[n] < 15:
                    assert current == before[n], 'recovered before grace: ' + n
                if len(ps) != 8 or not all(ready(p) for p in ps.values()) or changed(before[n], current) != scopes[n]:
                    done = False
        return done and len(restarts) == len(protected)
    poll('restart recovery scopes', recovered, 210)
    snapshots('after-container-restarts')
    for n in cases:
        delta = changed(before[n], uids(pods_for(n)))
        if n in protected:
            assert get('modelserving', n)['status'].get('availableReplicas', 0) == 1
        record('container-restart/' + n, {'changed': delta, 'nativeRestartObserved': n in restarts})
    for n in protected:
        call(['-n', NS, 'exec', target(n), '--', 'rm', '-f', '/state/unready'])
        wait_ready(n)
        assert uids(pods_for(n)) == before[n]
        record('native-recovery/' + n, 'same Pod UIDs; availableReplicas returns to 2')
        base = uids(pods_for(n))
        call(['-n', NS, 'delete', 'pod', target(n), '--wait=false'])
        after = uids(wait_ready(n))
        assert changed(base, after) == scopes[n], (n, changed(base, after), scopes[n])
        record('manual-delete/' + n, {'changed': changed(base, after)})
    snapshots('after-manual-deletes')

    # A transient unhealthy restart within a finite grace period keeps the UID.
    n = 'role-finite'
    patch(n, {'template': {'restartGracePeriodSeconds': 60}})
    base = uids(wait_ready(n))
    fault(n)
    poll('transient restart', lambda: any(s.get('restartCount', 0) > 0 for s in get('pod', target(n))['status'].get('containerStatuses', [])))
    call(['-n', NS, 'exec', target(n), '--', 'rm', '-f', '/state/unready'])
    wait_ready(n)
    assert uids(pods_for(n)) == base
    record('finite-native-recovery', 'same Pod UIDs after native container restart')

    terminal = {'failed-none': ('None', 0), 'failed-role': ('RoleRecreate', -1), 'failed-group': ('ServingGroupRecreate', -1)}
    switching = {'switch-none': ('RoleRecreate', 25), 'switch-infinite': ('RoleRecreate', 25)}
    apply_workloads([workload(n, *cfg, restart='Never') for n, cfg in terminal.items()] + [workload(n, *cfg) for n, cfg in switching.items()], 'terminal-and-switch-workloads')
    bases = {n: uids(wait_ready(n)) for n in [*terminal, *switching]}
    for n in [*terminal, *switching]:
        fault(n)
    for n in terminal:
        poll('terminal ' + n, lambda n=n: get('pod', target(n))['status']['phase'] == 'Failed')
    for n in switching:
        poll('switching restart ' + n, lambda n=n: any(s.get('restartCount', 0) > 0 for s in get('pod', target(n))['status'].get('containerStatuses', [])))
    patch('switch-none', {'recoveryPolicy': 'None'})
    patch('switch-infinite', {'template': {'restartGracePeriodSeconds': -1}})
    until = time.monotonic() + 35
    while time.monotonic() < until:
        allpods = get('pods')
        for n, base in bases.items():
            assert uids(pods_for(n, allpods)) == base and not any(p['metadata'].get('deletionTimestamp') for p in pods_for(n, allpods).values()), 'unexpected replacement: ' + n
        time.sleep(2)
    snapshots('before-controller-restart')
    for n in bases:
        assert get('modelserving', n)['status'].get('availableReplicas', 0) == 1
        record('tolerated/' + n, 'UIDs unchanged across old grace deadline; unavailable state retained')
    call(['-n', args.controller_namespace, 'rollout', 'restart', 'deployment/' + args.controller_deployment])
    poll('controller ready', lambda: call(['-n', args.controller_namespace, 'rollout', 'status', 'deployment/' + args.controller_deployment, '--timeout=1s'], check=False).returncode == 0, 120)
    until = time.monotonic() + 15
    while time.monotonic() < until:
        allpods = get('pods')
        for n, base in bases.items():
            assert uids(pods_for(n, allpods)) == base and not any(p['metadata'].get('deletionTimestamp') for p in pods_for(n, allpods).values()), 'restart bypassed policy: ' + n
        time.sleep(2)
    for n in bases:
        record('controller-restart/' + n, 'same UIDs after controller startup observation')
    snapshots('after-controller-restart')
    verify_deleted_retained_pods()
    invalid = workload('invalid-grace', 'None', -2)
    response = call(['apply', '--dry-run=server', '-f', '-'], invalid, check=False)
    assert response.returncode != 0 and 'restartGracePeriodSeconds' in response.stderr, response.stderr
    (OUT / 'invalid-grace.txt').write_text(response.stderr)
    record('crd-validation', '-1 accepted and -2 rejected')
    save('events', get('events'))
    logs = call(['-n', args.controller_namespace, 'logs', 'deployment/' + args.controller_deployment, '--since=30m']).stdout
    (OUT / 'controller.log').write_text(logs)
    record('complete', {'namespace': NS, 'image': args.controller_image})

def verify_deleted_retained_pods():
    for name, policy in {
        'failed-none': 'None', 'failed-role': 'RoleRecreate',
        'failed-group': 'ServingGroupRecreate', 'switch-none': 'None',
        'switch-infinite': 'RoleRecreate',
    }.items():
        base = uids(pods_for(name))
        scope = expected_scope(name, policy)
        call(['-n', NS, 'delete', 'pod', target(name), '--wait=false'])
        after = uids(wait_ready(name))
        assert changed(base, after) == scope, (name, changed(base, after), scope)
        record('delete-retained/' + name, {'changed': changed(base, after)})
    snapshots('after-retained-deletes')

if __name__ == '__main__':
    try:
        main()
    except Exception as e:
        save('failure', {'error': str(e), 'passed': checks})
        try:
            snapshots('failure')
            save('failure-events', get('events'))
            (OUT / 'controller-failure.log').write_text(call(['-n', args.controller_namespace, 'logs', 'deployment/' + args.controller_deployment, '--since=30m']).stdout)
        except Exception:
            pass
        raise
