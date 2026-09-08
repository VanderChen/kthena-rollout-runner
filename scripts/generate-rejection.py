#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Render all 97 admission cases with literal invalid fields and real update triggers."""
import copy
import hashlib
import importlib.util
import json
import pathlib

ROOT=pathlib.Path(__file__).resolve().parents[1]
loader=importlib.util.spec_from_file_location('recovery_generator',ROOT/'scripts/generate-recovery.py')
recovery=importlib.util.module_from_spec(loader);loader.loader.exec_module(recovery);normal=recovery.normal
cp=copy.deepcopy
name=lambda x:normal.NAMES.get(x,x)


def literal(config):
    # Preserve missing fields, nulls, invalid numeric types and graph order.
    sanitized=cp(config)
    for key in ('coordination','eviction','networkTopology','typeOverride','omitWorkerTemplateFor'):sanitized.pop(key,None)
    spec=normal.initial(sanitized,'controlled');strategy=spec.setdefault('rolloutStrategy',{})
    coord=config.get('coordination')
    if isinstance(coord,dict):
        co=cp(coord)
        if 'roles' in co:co['roles']=[name(x) for x in co['roles']]
        if 'dependencies' in co:
            deps=co['dependencies']
            if isinstance(deps,dict):deps=[{'role':k,'dependsOn':v} for k,v in deps.items()]
            co['dependencies']=[{'role':name(d['role']),'dependsOn':[name(x) for x in d['dependsOn']]} for d in deps]
        strategy['roleCoordination']=co
    if 'eviction' in config:
        ev=config['eviction'];value={'protectionLevel':'ServingGroup' if ev['level']=='SG' else 'Role'}
        if 'min' in ev:value['minAvailable' if ev['level']=='SG' else 'roleMinAvailable']=ev['min'] if ev['level']=='SG' else {name(ev.get('role','f')):ev['min']}
        strategy['evictionStrategy']=value
    if 'networkTopology' in config:spec['template']['networkTopology']=cp(config['networkTopology'])
    if 'typeOverride' in config:strategy['type']=config['typeOverride']
    for r in config.get('omitWorkerTemplateFor',[]):normal.role(spec,r).pop('workerTemplate',None)
    return spec


def targets(spec,version='A'):
    if spec['rolloutStrategy']['type']=='ServingGroupRollingUpdate':return [{'scope':'SG','versions':{version:spec['replicas']}}]
    return [{'role':r['name'],'versions':{version:r.get('replicas',1)}} for r in spec['template']['roles']]


def unit(role,version,count=1,ordinal=None,ready=True):
    value={'kind':'unit','group':0,'role':name(role),'version':version,'ready':ready,'count':count}
    if ordinal is not None:value['ordinal']=ordinal
    return value


def plan(row):
    n=int(row['id'][5:]);bad=literal(row['config']);a=cp(bad);steps=[];patch=None;conditions=[]
    if n<=43:
        budget=a['rolloutStrategy']['rollingUpdateConfiguration'] if row['config']['mode']=='SG' else normal.role(a)
        for key in ('maxUnavailable','maxSurge','partition'):budget.pop(key,None)
        budget.update(maxUnavailable=1,maxSurge=0 if n<=13 else 1,partition=0)
        if n<=13:normal.version(bad)
    elif n==44:a['rolloutStrategy'].pop('rollingUpdateConfiguration')
    elif n in (45,46):normal.role(a).pop('maxSurge' if n==45 else 'partition')
    elif n in (47,48):a['rolloutStrategy'].pop('roleCoordination');a['rolloutStrategy']['type']='ServingGroupRollingUpdate'
    elif n==49:a.pop('recoveryPolicy')
    elif n==50:normal.role(a)['maxUnavailable']=1
    elif n==51:normal.role(a).update(replicas=3,maxUnavailable=1)
    elif n in (52,53):a['rolloutStrategy']['type']='ServingGroupRollingUpdate'
    elif n in (54,55):
        b=normal.version(cp(a));bad=cp(b)
        if n==54:bad['rolloutStrategy'].pop('type')
        else:normal.role(bad)['maxSurge']=0
        conditions=[unit('f','B',ordinal=3,ready=False),unit('f','A',3)]
        steps=[normal.step('establish-actual-inflight-A-B',b,until='conditions',release='none',conditions=conditions,stableSeconds=10,timeoutSeconds=180)]
    elif 56<=n<=75:a['rolloutStrategy'].pop('roleCoordination')
    elif n==76:normal.version(bad,('f','b'))
    elif n==77:normal.role(a,'b')['partition']=0;normal.version(bad,('f','b'))
    elif n in (78,79):
        b=normal.version(cp(a),('f','b'));bad=cp(b);normal.role(bad,'b')['replicas' if n==78 else 'partition']=1 if n==78 else 2
        conditions=[unit('b','B',ordinal=2,ready=False),unit('b','A',2),unit('f','A',3)]
        steps=[normal.step('establish-actual-dependency-update',b,until='conditions',release='none',conditions=conditions,stableSeconds=10,timeoutSeconds=180)]
    elif n in (80,87):normal.role(a)['workerReplicas']=0
    elif n in (81,88):normal.role(a)['workerReplicas']=0;normal.role(a).pop('workerTemplate',None)
    elif n in (82,83,89,90):a['rolloutStrategy'].pop('evictionStrategy')
    elif n in (84,91):bad['template']['gangPolicy']['minRoleReplicas']['frontend']=0
    elif n in (85,92):patch={'spec':{'template':{'gangPolicy':None}}}
    elif n in (86,93):patch={'spec':{'template':{'networkTopology':None}}}
    elif n in (94,95):
        b=normal.version(cp(a));bad=cp(b);normal.set_desired(bad,4)
        steps=[normal.step('establish-all-protected-B-stop',b,release='none',stableSeconds=30,expect={'noReplacement':True,'targets':targets(a)})]
    elif n in (96,97):
        b=normal.version(cp(a));bad=cp(b);normal.role(bad)['replicas']=1 if n==96 else 3
        conditions=[unit('f','B',ordinal=o,ready=False) for o in ([2,1] if n==96 else [4])]
        conditions += [unit('f','A',1 if n==96 else 4),unit('b','A')]
        steps=[normal.step('establish-actual-unready-replacements',b,until='conditions',release='none',conditions=conditions,stableSeconds=10,timeoutSeconds=180)]
    else:raise AssertionError(row['id'])
    active=n in (54,55,78,79,96,97)
    ex={'noReplacement':not active,'noNewRevision':True}
    if not active:ex['targets']=targets(a)
    rejection=normal.step('reject-invalid-request-and-preserve-accepted-state',action='reject-merge-patch' if patch is not None else 'reject-update',release='none',stableSeconds=10 if active else 30,timeoutSeconds=180,expect=ex)
    if patch is not None:rejection['patch']=patch
    else:rejection['spec']=bad
    if active:rejection.update(until='conditions',conditions=conditions)
    steps.append(rejection)
    if active:
        final=targets(a);front=next(r for r in final if r['role']=='frontend');r=normal.role(a);p=r.get('partition',0);desired=r['replicas'];front['versions']={}
        if p:front['versions']['A']=p;front['ordinals']={str(i):'A' for i in range(p)}
        if desired>p:front['versions']['B']=desired-p
        if n in (78,79):next(r for r in final if r['role']=='backend')['versions']={'B':2}
        steps.append(normal.step('continue-under-last-accepted-spec',release='one',stableSeconds=30,timeoutSeconds=420,expect={'noNewRevision':True,'targets':final}))
    return a,steps


def main():
    raw=normal.SOURCE.read_bytes();assert hashlib.sha256(raw).hexdigest()==recovery.SOURCE_SHA
    rows=[r for r in json.loads(raw)['cases'] if r['id'].startswith('DENY-')];assert len(rows)==97
    out=ROOT/'cases/rejection';out.mkdir(exist_ok=True)
    for row in rows:
        a,steps=plan(row);case={'format':'rollout-runner/v4','id':row['id'],'baseline':normal.COMMIT,'scenario':{'source':row,'profile':'controlled','initialSpec':a,'steps':steps}}
        (out/(row['id']+'.yaml')).write_text(normal.yaml(case)+'\n')
    (out/'suite.json').write_text(json.dumps({'format':'rollout-runner/suite-v1','controllerCommit':normal.COMMIT,'sourceSHA256':recovery.SOURCE_SHA,'developmentScope':'97 real admission requests, atomic stored spec/generation/UID and retained histories; six actual in-flight sources continue under their last accepted spec; explicit invalid types, missing fields, duplicate graphs and null merge-patches retained','cases':rows,'inputs':{r['id']:hashlib.sha256((out/(r['id']+'.yaml')).read_bytes()).hexdigest() for r in rows}},ensure_ascii=False,indent=2)+'\n')
    print('Generated 97 literal admission rejection cases.')

if __name__=='__main__':main()
