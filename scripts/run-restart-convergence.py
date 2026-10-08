#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Run contract 2.6 partial recovery/restart cases against an isolated Kind controller.

Uses the existing runner fault proxy. Observation/admission bypass the proxy.
No controller deployment, RBAC, credentials or cluster is installed by this script.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import threading
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
SUITE = ROOT / 'cases/restart-convergence/suite.json'
LABEL = 'modelserving.volcano.sh/name'
ROLE = 'modelserving.volcano.sh/role'


class Inconclusive(RuntimeError):
    """A required environment or injection boundary was not established."""


def ready(p):
    return not p['metadata'].get('deletionTimestamp') and any(
        c['type'] == 'Ready' and c['status'] == 'True'
        for c in p.get('status', {}).get('conditions', []))


def version(p):
    return next((v['value'] for v in p['spec']['containers'][0].get('env', [])
                 if v['name'] == 'VERSION'), '?')


def uidmap(pods):
    return {p['metadata']['name']: p['metadata']['uid'] for p in pods}


def assert_retained(pods, expected):
    actual = {p['metadata']['name']: p for p in pods}
    for name, uid in expected.items():
        assert name in actual and actual[name]['metadata']['uid'] == uid, ('unexpected replacement', name)
        assert not actual[name]['metadata'].get('deletionTimestamp'), ('unexpected deletion', name)


def assert_refill(pods, failed, old_uid, retained, expected_names):
    assert_retained(pods, retained)
    unit = [p for p in pods if p['metadata']['name'] in expected_names]
    assert len(unit) == len(expected_names), 'incomplete historical layout'
    assert all(ready(p) and version(p) == 'A' for p in unit), 'first refill must be complete historical A'
    new = next(p for p in unit if p['metadata']['name'] == failed)
    assert new['metadata']['uid'] != old_uid, 'missing member was not replaced'


def audit_events(events, case, initial, failed, protected, low):
    """Check intermediate identities/Ready credit, not just the final snapshot."""
    live, first_refill = {}, None
    saw_failed = False
    held = {'restart-budget-held', 'historical-refill', 'legal-high-rollout'}
    started = False
    for item in events:
        phase, event = item['phase'], item['event']
        if event.get('type') not in ('ADDED', 'MODIFIED', 'DELETED'):
            continue
        p = event['object']
        if p.get('kind') != 'Pod':
            continue
        name, uid = p['metadata']['name'], p['metadata']['uid']
        if name == failed and uid == initial[failed] and p.get('status', {}).get('phase') == 'Failed':
            saw_failed = True
        # A correct held interval can have zero Pod events. The executor proves
        # initialSync and the hold via direct snapshots; the first post-hold
        # event may therefore already be the historical refill.
        if phase in held:
            started = True
        if phase in held and name in protected:
            assert uid == protected[name] and event['type'] != 'DELETED' and not p['metadata'].get('deletionTimestamp'), ('replayed survivor deletion', name)
        if phase == 'restart-budget-held' and name != failed:
            assert uid == initial[name] and event['type'] != 'DELETED' and not p['metadata'].get('deletionTimestamp'), ('extra budget after restart', name)
        if started and name == failed and uid != initial[failed] and first_refill is None:
            assert version(p) == 'A', 'first observed refill skipped historical A'
            first_refill = uid
        if phase in held and first_refill and name == failed:
            assert uid == first_refill and event['type'] != 'DELETED' and not p['metadata'].get('deletionTimestamp'), 'replacement deleted before partition release'
        if event['type'] == 'DELETED':
            if uidmap(live.values()).get(name) == uid:
                live.pop(name, None)
        else:
            live[name] = p
        # maxSurge=0 and fixed layout: no unplanned ordinal/worker may appear.
        assert set(live) <= set(initial), 'unexpected ordinal or worker layout'
        if phase in held | {'full-rollout', 'warm-recovery-control'}:
            units = [low, set(initial) - low] if case['mode'] == 'SG' else [
                {n for n in initial if n.startswith('current-0-prefill-' + str(i) + '-')}
                for i in range(2)]
            available = sum(all(n in live and ready(live[n]) for n in names) for names in units)
            assert available >= 1, 'complete Ready units fell below desiredReplicas-maxUnavailable=1'
    if not saw_failed or not started or not first_refill:
        raise Inconclusive('watch does not establish Failed, restart and first-refill boundaries')
    return {'firstRefillUID': first_refill, 'minAvailable': 1, 'events': len(events)}


def template(ver='A'):
    return {'spec': {'restartPolicy': 'Never', 'terminationGracePeriodSeconds': 1,
        'containers': [{'name': 'workload', 'image': 'busybox:1.36', 'imagePullPolicy': 'IfNotPresent',
            'command': ['sh', '-c', 'touch /tmp/ready; while ! test -f /tmp/fail; do sleep 1; done; rm -f /tmp/ready; exit 1'],
            'env': [{'name': 'VERSION', 'value': ver}],
            'resources': {'requests': {'cpu': '5m', 'memory': '4Mi'}},
            'readinessProbe': {'exec': {'command': ['test', '-f', '/tmp/ready']},
                               'periodSeconds': 1, 'failureThreshold': 1}}]}}


def workload(case, ns):
    role_mode = case['mode'] == 'Role'
    roles = []
    for name in ['prefill', 'decode']:
        r = {'name': name, 'replicas': 2 if role_mode else 1,
             'workerReplicas': case['workers'] if name == 'prefill' else 0,
             'entryTemplate': template()}
        if r['workerReplicas']:
            r['workerTemplate'] = template()
        if role_mode:
            r.update(maxUnavailable=1, maxSurge=0, partition=2)
        roles.append(r)
    strategy = {'type': 'RoleRollingUpdate' if role_mode else 'ServingGroupRollingUpdate'}
    if role_mode and case['coordination']:
        strategy['roleCoordination'] = {'roles': ['prefill', 'decode'], 'maxSkew': '50%'}
    if not role_mode:
        strategy['rollingUpdateConfiguration'] = {'maxUnavailable': 1, 'maxSurge': 0, 'partition': 2}
    return {'apiVersion': 'workload.serving.volcano.sh/v1alpha1', 'kind': 'ModelServing',
            'metadata': {'name': 'current', 'namespace': ns},
            'spec': {'replicas': 1 if role_mode else 2, 'schedulerName': 'volcano',
                     'recoveryPolicy': case['policy'], 'rolloutStrategy': strategy,
                     'template': {'restartGracePeriodSeconds': 0, 'roles': roles}}}


class Execution:
    def __init__(self, args, case):
        self.args, self.case = args, case
        self.ns = args.namespace_prefix + '-' + case['id'].lower()
        self.out = args.artifacts / case['id']
        self.out.mkdir()
        self.k = ['kubectl', '--kubeconfig', args.kubeconfig, '--request-timeout=20s']
        self.rules = set()
        self.phase = 'prepare'
        self.watch = None
        self.stopped = False
        self.audit_input = None

    def save(self, name, obj):
        (self.out / (name + '.json')).write_text(json.dumps(obj, indent=2) + '\n')

    def call(self, *args, obj=None, check=True):
        proc = subprocess.run(self.k + list(args), input=json.dumps(obj) if obj else None,
                              text=True, capture_output=True, timeout=120)
        with (self.out / 'commands.jsonl').open('a') as f:
            f.write(json.dumps({'at': time.time(), 'args': args, 'exit': proc.returncode,
                                'stderr': proc.stderr[:2000]}) + '\n')
        if check and proc.returncode:
            raise Inconclusive(proc.stderr)
        return proc.stdout

    def get(self, kind, name=None, ns=None):
        return json.loads(self.call('-n', ns or self.ns, 'get', kind,
                                    *([name] if name else []), '-o', 'json'))

    def pods(self):
        return self.get('pods')['items']

    def poll(self, label, fn, timeout=150, fixture=False):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            value = fn()
            if value:
                return value
            time.sleep(.5)
        raise (Inconclusive if fixture else AssertionError)('timeout: ' + label)

    def control(self, method, path, data=None):
        token = self.args.proxy_token_file.read_text().strip()
        req = urllib.request.Request(self.args.proxy_url.rstrip('/') + path, method=method,
            headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'},
            data=json.dumps(data).encode() if data is not None else None)
        with urllib.request.urlopen(req, timeout=15) as response:
            raw = response.read()
            return json.loads(raw) if raw else None

    def block(self, method, name):
        rid = self.ns + '-' + method.lower()
        rule = {'id': rid, 'namespace': self.ns, 'resource': 'pods', 'name': name,
                'mode': 'error', 'methods': [method], 'statusCode': 503,
                'count': -1, 'durationSeconds': 600}
        self.save('rule-' + method, self.control('POST', '/v1/rules', rule))
        self.rules.add(rid)
        return rid

    def clear(self, rid):
        self.control('DELETE', '/v1/rules/' + rid)
        self.rules.remove(rid)

    def hits(self, rid):
        return next((r['hits'] for r in self.control('GET', '/v1/state')['rules'] if r['id'] == rid), 0)

    def facts(self):
        facts = {}
        for cm in self.get('configmaps')['items']:
            facts.update(json.loads(cm.get('data', {}).get('created.json', '{}')))
        return facts

    def update(self, partition, target=False):
        obj = self.get('modelserving', 'current')
        obj.pop('status', None)
        if self.case['mode'] == 'SG':
            obj['spec']['rolloutStrategy']['rollingUpdateConfiguration']['partition'] = partition
        for r in obj['spec']['template']['roles']:
            if self.case['mode'] == 'Role':
                if r['name'] != 'prefill':
                    continue
                r['partition'] = partition
            if target:
                for key in ['entryTemplate', 'workerTemplate']:
                    if key in r:
                        r[key] = template('B')
        self.save(self.phase + '-input', obj)
        self.call('replace', '-f', '-', obj=obj)

    def controller_pods(self):
        return json.loads(self.call('-n', self.args.controller_namespace, 'get', 'pods', '-l',
            'app.kubernetes.io/component=kthena-controller-manager', '-o', 'json'))['items']

    def scale(self, count):
        self.call('-n', self.args.controller_namespace, 'scale',
                  'deployment/' + self.args.controller_deployment, '--replicas=' + str(count))
        self.stopped = count == 0
        if count == 0:
            self.poll('controller stopped', lambda: not self.controller_pods(), fixture=True)
        else:
            self.call('-n', self.args.controller_namespace, 'rollout', 'status',
                      'deployment/' + self.args.controller_deployment, '--timeout=90s')

    def snapshot(self, stage):
        self.save(stage, {'at': time.time(), 'pods': self.pods(),
            'modelserving': self.get('modelserving', 'current'),
            'configmaps': self.get('configmaps'), 'proxy': self.control('GET', '/v1/state')})

    def start_watch(self):
        self.watch = subprocess.Popen(self.k + ['--request-timeout=0', '-n', self.ns, 'get', 'pods',
            '--watch', '--output-watch-events', '-o', 'json'], stdout=subprocess.PIPE,
            stderr=(self.out / 'watch.stderr').open('w'))
        self.watch_errors = []
        def read():
            decoder, buf = json.JSONDecoder(), ''
            with (self.out / 'watch.jsonl').open('w') as f:
                while chunk := self.watch.stdout.read1(65536):
                    buf += chunk.decode()
                    while buf.strip():
                        buf = buf.lstrip()
                        try:
                            event, pos = decoder.raw_decode(buf)
                        except ValueError:
                            break
                        buf = buf[pos:]
                        if event.get('type') == 'ERROR':
                            self.watch_errors.append(event)
                        f.write(json.dumps({'at': time.time(), 'phase': self.phase, 'event': event}) + '\n')
                        f.flush()
                if buf.strip():
                    self.watch_errors.append('truncated watch JSON')
        self.reader = threading.Thread(target=read, daemon=True)
        self.reader.start()

    def run(self):
        a, c = self.args, self.case
        dep = self.get('deployment', a.controller_deployment, a.controller_namespace)
        if dep['spec'].get('replicas') != 1 or dep['spec']['template']['spec']['containers'][0]['image'] != a.controller_image:
            raise Inconclusive('requires the declared isolated single-replica controller/image')
        self.save('controller-before', dep)
        controllers = self.controller_pods()
        if len(controllers) != 1 or not ready(controllers[0]):
            raise Inconclusive('one Ready controller required')
        old_controller = controllers[0]['metadata']['uid']
        self.save('controller-pod-before', controllers[0])
        self.control('GET', '/v1/state')
        self.call('create', 'namespace', self.ns)
        obj = workload(c, self.ns)
        self.save('initial-input', obj)
        self.call('apply', '-f', '-', obj=obj)
        self.start_watch()
        total = 4 + 2 * c['workers']
        before = self.poll('all A Ready', lambda: self.all_ready(total), fixture=True)
        self.poll('durable completion facts', lambda: sum(len(v) for v in self.facts().values()) == 4, fixture=True)
        if c['mode'] == 'SG':
            rev = before[0]['metadata']['labels']['modelserving.volcano.sh/revision']
            self.poll('completed A baseline', lambda: self.get('modelserving', 'current').get('status', {}).get('currentRevision') == rev, fixture=True)
        initial = uidmap(before)
        failed = 'current-0-decode-0-0' if c['policy'] == 'ServingGroupRecreate' else 'current-0-prefill-0-1'
        survivor = 'current-0-prefill-0-0'
        # The protected rollout unit includes all SG members or one full Role.
        low = {n for n in initial if n.startswith('current-0-' if c['mode'] == 'SG' else 'current-0-prefill-0-')}
        retained = {n: uid for n, uid in initial.items() if n != failed}
        protected = {n: uid for n, uid in retained.items() if n in low or (c['mode'] == 'Role' and '-decode-' in n)}
        self.audit_input = (c, initial, failed, protected, low)
        self.phase = 'desired-b-protected'
        self.update(2, target=True)
        self.observe_retained(initial, 3)
        deletion = self.block('DELETE', survivor)
        creation = self.block('POST', failed)
        self.phase = 'partial-recovery'
        self.call('-n', self.ns, 'exec', failed, '--', 'touch', '/tmp/fail')
        def partial():
            ps, facts = self.pods(), self.facts()
            cleared = ('current-0' not in facts if c['policy'] == 'ServingGroupRecreate'
                       else 'prefill-0' not in facts.get('current-0', {}))
            return ps if (initial[failed] not in uidmap(ps).values() and
                          uidmap(ps).get(survivor) == initial[survivor] and
                          cleared and self.hits(deletion) > 0) else None
        self.poll('established partial recovery', partial, fixture=True)
        self.snapshot('partial-recovery')
        self.scale(0)
        self.clear(deletion)
        self.phase = 'restart-budget-held'
        self.update(1)
        self.scale(1)
        def initialized():
            ps = self.controller_pods()
            if len(ps) != 1 or ps[0]['metadata']['uid'] == old_controller or not ready(ps[0]):
                return False
            logs = self.call('-n', a.controller_namespace, 'logs', ps[0]['metadata']['name'])
            return (ps[0], logs) if 'initial sync has been done' in logs else False
        controller, logs = self.poll('replacement initialSync', initialized, fixture=True)
        self.save('controller-pod-after', controller)
        (self.out / 'controller-after.log').write_text(logs)
        after_dep = self.get('deployment', a.controller_deployment, a.controller_namespace)
        assert dep['metadata']['uid'] == after_dep['metadata']['uid'] and dep['spec'] == after_dep['spec'], 'controller spec changed'
        self.observe_retained(retained, a.hold_seconds)
        self.poll('replacement POST blocked', lambda: self.hits(creation) > 0, fixture=True)
        self.snapshot('restart-budget-held')
        self.phase = 'historical-refill'
        self.clear(creation)
        def refilled():
            ps = self.pods()
            assert_retained(ps, protected)
            unit = [p for p in ps if p['metadata']['name'] in low]
            return ps if len(unit) == len(low) and all(ready(p) for p in unit) else None
        first = self.poll('historical unit refill', refilled)
        assert_refill(first, failed, initial[failed], protected, low)
        refill_uid = uidmap(first)[failed]
        self.snapshot('historical-refill')
        self.phase = 'legal-high-rollout'
        def mixed():
            ps = self.pods()
            assert_retained(ps, dict(protected, **{failed: refill_uid}))
            return ps if len(ps) == total and all(ready(p) and version(p) == (
                'A' if p['metadata']['name'] in low or (c['mode'] == 'Role' and '-decode-' in p['metadata']['name']) else 'B') for p in ps) else None
        self.poll('eligible high unit reaches B', mixed)
        self.observe_retained(dict(protected, **{failed: refill_uid}), 3)
        self.snapshot('legal-high-rollout')
        self.phase = 'full-rollout'
        self.update(0)
        def final():
            ps = self.all_ready(total)
            return ps if ps and all(version(p) == ('A' if c['mode'] == 'Role' and '-decode-' in p['metadata']['name'] else 'B') for p in ps) else None
        final_pods = self.poll('all eligible units B Ready', final)
        self.snapshot('final-b')
        # A new actual fault must still execute the configured full warm scope.
        self.phase = 'warm-recovery-control'
        base = uidmap(final_pods)
        scope = {n for n in base if n.startswith('current-0-' if c['policy'] == 'ServingGroupRecreate' else 'current-0-prefill-0-')}
        self.call('-n', self.ns, 'exec', failed, '--', 'touch', '/tmp/fail')
        def warm():
            ps = self.all_ready(total)
            if not ps:
                return None
            current = uidmap(ps)
            delta = {n for n, uid in base.items() if current.get(n) != uid}
            assert delta <= scope, ('warm recovery exceeded scope', delta - scope)
            return ps if delta == scope else None
        self.poll('warm recovery exact scope', warm)
        self.snapshot('warm-recovery-control')
        return {'result': 'PASS', 'id': c['id'], 'survivorUID': initial[survivor],
                'firstRefillUID': refill_uid, 'firstRefillVersion': 'A',
                'warmReplacedNames': sorted(scope), 'controllerUIDBefore': old_controller,
                'controllerUIDAfter': controller['metadata']['uid'], 'partialBoundaryEstablished': True}

    def all_ready(self, total):
        ps = self.pods()
        return ps if len(ps) == total and all(ready(p) for p in ps) else None

    def observe_retained(self, expected, seconds):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            assert_retained(self.pods(), expected)
            time.sleep(.5)

    def cleanup(self):
        if self.watch:
            exited_early = self.watch.poll() is not None
            self.watch.terminate()
            self.watch.wait(timeout=10)
            self.reader.join(timeout=5)
            if exited_early:
                self.watch_errors.append('watch ended before case completion')
        for rid in list(self.rules):
            self.clear(rid)
        if self.stopped:
            self.scale(1)

    def audit(self):
        if self.watch_errors or self.reader.is_alive():
            raise Inconclusive('incomplete watch: ' + str(self.watch_errors))
        events = [json.loads(line) for line in (self.out / 'watch.jsonl').read_text().splitlines()]
        result = audit_events(events, *self.audit_input)
        self.save('watch-audit', result)
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--kubeconfig', required=True)
    parser.add_argument('--artifacts', required=True, type=Path)
    parser.add_argument('--controller-image', required=True)
    parser.add_argument('--controller-commit', required=True)
    parser.add_argument('--controller-namespace', default='kthena-system')
    parser.add_argument('--controller-deployment', default='kthena-controller-manager')
    parser.add_argument('--proxy-url', required=True)
    parser.add_argument('--proxy-token-file', required=True, type=Path)
    parser.add_argument('--namespace-prefix', default='runner-restart')
    parser.add_argument('--hold-seconds', default=10, type=int)
    parser.add_argument('--case', action='append', dest='ids')
    args = parser.parse_args()
    suite = json.loads(SUITE.read_text())
    if args.hold_seconds < 5 or len(args.controller_commit) != 40:
        parser.error('hold-seconds >= 5 and full controller SHA required')
    cases = [c for c in suite['cases'] if not args.ids or c['id'] in args.ids]
    if not cases or (args.ids and set(args.ids) - {c['id'] for c in cases}):
        parser.error('unknown case ID')
    args.artifacts.mkdir(parents=True, exist_ok=False)
    provenance = {'contractVersion': '2.6', 'controllerCommit': args.controller_commit,
        'controllerImage': args.controller_image,
        'executorSHA256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'suiteSHA256': hashlib.sha256(SUITE.read_bytes()).hexdigest(), 'cases': [c['id'] for c in cases]}
    (args.artifacts / 'provenance.json').write_text(json.dumps(provenance, indent=2) + '\n')
    results = []
    for case in cases:
        print('RUN', case['id'], case['description'], flush=True)
        execution = Execution(args, case)
        try:
            result = execution.run()
        except Exception as exc:
            result = {'id': case['id'], 'result': 'FAIL' if isinstance(exc, AssertionError) else 'INCONCLUSIVE',
                      'phase': execution.phase, 'error': str(exc)}
        finally:
            try:
                execution.cleanup()
            except Exception as exc:
                result = {'id': case['id'], 'result': 'INCONCLUSIVE', 'error': 'cleanup: ' + str(exc)}
        if result['result'] == 'PASS':
            try:
                result['watchAudit'] = execution.audit()
            except Exception as exc:
                result = {'id': case['id'], 'result': 'FAIL' if isinstance(exc, AssertionError) else 'INCONCLUSIVE',
                          'error': str(exc)}
        execution.save('result', result)
        results.append(result)
        (args.artifacts / 'summary.json').write_text(json.dumps(results, indent=2) + '\n')
        print(json.dumps(result, ensure_ascii=False), flush=True)
        # Each case has an isolated namespace and its own bounded rules. Keep
        # collecting independent results after a product failure; the next
        # case still checks the controller/environment before touching it.
    return 0 if len(results) == len(cases) and all(r['result'] == 'PASS' for r in results) else 1


if __name__ == '__main__':
    raise SystemExit(main())
