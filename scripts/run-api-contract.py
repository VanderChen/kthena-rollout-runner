# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Submit the v2.1 admission contract to a real API server and fail on disagreement."""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import re


def is_rejection(result):
    """Do not count transport, auth, webhook outage, conflict or setup errors as denial."""
    error = result['stderr']
    return result['exitCode'] != 0 and (
        re.search(r'admission webhook "[^"\n]+" denied the request:', error) is not None
        or re.search(r'^The ModelServing "[^"\n]+" is invalid:', error) is not None
        or re.search(r'Error from server \((Invalid|BadRequest)\): .* is invalid:', error) is not None)


def prepare(obj, name, namespace):
    obj = copy.deepcopy(obj)
    obj['metadata'] = {'name': name, 'namespace': namespace}
    for role in obj['spec']['template']['roles']:
        for key in ('entryTemplate', 'workerTemplate'):
            if role.get(key):
                role[key].setdefault('spec', {}).setdefault('nodeSelector', {})['runner.kthena.io/admission-only'] = 'true'
    return obj


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--kubeconfig', required=True)
    parser.add_argument('--artifacts', required=True, type=Path)
    parser.add_argument('--cases', type=Path, default=Path(__file__).resolve().parents[1]/'cases/api-contract/cases.json')
    parser.add_argument('--namespace', default='runner-api-contract')
    parser.add_argument('--controller-image', required=True)
    parser.add_argument('--controller-commit', required=True)
    parser.add_argument('--controller-namespace', default='kthena-system')
    parser.add_argument('--controller-deployment', default='kthena-controller-manager')
    args = parser.parse_args()
    args.artifacts.mkdir(parents=True, exist_ok=False)
    def save(name, data):
        (args.artifacts/(name+'.json')).write_text(json.dumps(data, indent=2)+'\n')
    def run(argv, obj=None):
        cmd=['kubectl', '--kubeconfig', args.kubeconfig, '--request-timeout=25s']+argv
        try:
            p=subprocess.run(cmd, input=json.dumps(obj) if obj is not None else None, text=True, capture_output=True, timeout=35)
            result={'command':cmd,'input':obj,'exitCode':p.returncode,'stdout':p.stdout,'stderr':p.stderr}
        except subprocess.TimeoutExpired:
            result={'command':cmd,'input':obj,'exitCode':124,'stdout':'','stderr':'transport timeout'}
        with (args.artifacts/'requests.jsonl').open('a') as f: f.write(json.dumps(result)+'\n')
        return result
    controller=run(['-n',args.controller_namespace,'get','deployment',args.controller_deployment,'-o','json'])
    save('controller',controller)
    if controller['exitCode']: raise RuntimeError('Cannot observe tested controller')
    images=[c['image'] for c in json.loads(controller['stdout'])['spec']['template']['spec']['containers']]
    if args.controller_image not in images: raise RuntimeError('Tested controller image mismatch')
    setup=run(['create','namespace',args.namespace])
    if setup['exitCode']: raise RuntimeError('Namespace must be new: '+setup['stderr'])
    raw=args.cases.read_bytes(); cases=json.loads(raw); results=[]
    save('provenance',{'controllerCommit':args.controller_commit,'controllerImage':args.controller_image,'casesSHA256':hashlib.sha256(raw).hexdigest(),'runnerSHA256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'contractVersion':'2.1'})
    (args.artifacts/'cases.json').write_bytes(raw)
    for i,c in enumerate(cases):
        name=f'contract-{i:03}'; obj=prepare(c['object'],name,args.namespace)
        record={'case':c['name'],'expectedAllowed':c['expectedAllowed'],'passed':False}
        stored=None
        if c.get('old') is not None:
            created=run(['create','--validate=false','-f','-','-o','json'],prepare(c['old'],name,args.namespace))
            record['setup']=created
            if created['exitCode']:
                record['status']='INCONCLUSIVE';results.append(record);continue
            stored=json.loads(created['stdout'])
            # Controller status updates can change resourceVersion; refresh without changing the request.
            for attempt in range(5):
                latest=run(['-n',args.namespace,'get','modelserving',name,'-o','json'])
                if latest['exitCode']: result=latest;break
                current=json.loads(latest['stdout']);obj['metadata']['resourceVersion']=current['metadata']['resourceVersion']
                result=run(['replace','--validate=false','-f','-','-o','json'],obj)
                if '(Conflict)' not in result['stderr']: break
        else:
            result=run(['create','--dry-run=server','--validate=false','-f','-','-o','json'],obj)
        record['result']=result
        allowed=result['exitCode']==0; denied=is_rejection(result)
        record['status']='PASS' if allowed==c['expectedAllowed'] and (allowed or denied) else 'FAIL' if allowed or denied else 'INCONCLUSIVE'
        if stored is not None:
            actual=run(['-n',args.namespace,'get','modelserving',name,'-o','json'])
            record['after']=actual
            if actual['exitCode']: record['status']='INCONCLUSIVE'
            elif denied:
                after=json.loads(actual['stdout'])
                if (after['spec'],after['metadata']['uid'],after['metadata']['generation']) != (stored['spec'],stored['metadata']['uid'],stored['metadata']['generation']): record['status']='FAIL'
            record['cleanup']=run(['-n',args.namespace,'delete','modelserving',name,'--wait=false'])
            if record['cleanup']['exitCode']: record['status']='INCONCLUSIVE'
        record['passed']=record['status']=='PASS';results.append(record)
        save('results',results)
        print(record['status'],c['name'],flush=True)
    save('results',results)
    summary={'total':len(cases),'passed':sum(r['passed'] for r in results),'failed':[r['case'] for r in results if r['status']=='FAIL'],'inconclusive':[r['case'] for r in results if r['status']=='INCONCLUSIVE']}
    save('summary',summary); print(json.dumps(summary),flush=True)
    return 0 if summary['passed']==len(cases) else 1


if __name__=='__main__': raise SystemExit(main())
