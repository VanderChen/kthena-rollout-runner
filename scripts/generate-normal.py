#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Expand reviewed ID-specific actions into independent, fully rendered YAML.

No action prose is interpreted at runtime. Generation fails for every unmapped
ID; --through permits explicitly incomplete development catalogues.
"""
import argparse, copy, hashlib, json, pathlib, shutil
ROOT = pathlib.Path(__file__).resolve().parents[1]
SOURCE = ROOT.parent/'issues/features/020-modelserving-rollingupdate-behavior-matrix-DONE/ROLLING_UPDATE_CASES.json'
COMMIT='538b2825c06bc1e8c5392d18f18f84faee9fca95'
NAMES={'f':'frontend','b':'backend','c':'role-c','d':'role-d','x':'extra'}
cp=copy.deepcopy

def yaml(v, indent=0):
    pad=' '*indent
    if isinstance(v,dict) and v:
        lines=[]
        for k,val in v.items():
            key=json.dumps(str(k),ensure_ascii=False)
            if isinstance(val,(dict,list)) and val: lines.append(pad+key+':\n'+yaml(val,indent+2))
            else: lines.append(pad+key+': '+json.dumps(val,ensure_ascii=False))
        return '\n'.join(lines)
    if isinstance(v,list) and v:
        return '\n'.join(pad+'-\n'+yaml(x,indent+2) if isinstance(x,(dict,list)) and x else pad+'- '+json.dumps(x,ensure_ascii=False) for x in v)
    return pad+json.dumps(v,ensure_ascii=False)

def template(name, member, version='A', profile='controlled'):
    command='touch /tmp/ready; sleep 3600' if profile=='auto' else 'sleep 3600'
    return {'metadata':{'annotations':{'production.kthena.io/ranktable':json.dumps({'pod_name':name+'-'+member,'server_id':name},separators=(',',':'))}},'spec':{
        'terminationGracePeriodSeconds':1,'containers':[{'name':'workload','image':'busybox:1.36','imagePullPolicy':'IfNotPresent','command':['sh','-c',command],
        'env':[{'name':'ROLLOUT_VERSION','value':version}], 'readinessProbe':{'exec':{'command':['test','-f','/tmp/ready']},'periodSeconds':1,'failureThreshold':1},'resources':{'requests':{'cpu':'5m','memory':'4Mi'}}}]}}

def role(spec,name='f'):
    return next(r for r in spec['template']['roles'] if r['name']==NAMES.get(name,name))
def version(spec, roles=('f',), v='B'):
    for name in roles:
        r=role(spec,name)
        for key in ('entryTemplate','workerTemplate'):
            if key in r: r[key]['spec']['containers'][0]['env'][0]['value']=v
    return spec

def initial(config,profile):
    spec={'template':{'roles':[]}}
    if config.get('revisionHistoryLimit', 'omitted') != 'omitted':
        spec['revisionHistoryLimit'] = config['revisionHistoryLimit']
    if 'n' in config: spec['replicas']=config['n']
    if config.get('schedulerName')!='omitted':spec['schedulerName']='volcano'
    if config.get('plugins')!='omitted':
        spec['plugins']=[]
        for name in config.get('plugins',['headless-service','ranktable']):
            p={'name':name,'type':'BuiltIn'}
            if name=='ranktable':p['config']={'template':'production020-ranktable-template'}
            spec['plugins'].append(p)
    if config.get('recovery','omitted')!='omitted':spec['recoveryPolicy']=config['recovery']
    for name,c in config['roles'].items():
        name=NAMES.get(name,name);r={'name':name}
        for a,b in [('r','replicas'),('w','workerReplicas'),('u','maxUnavailable'),('s','maxSurge'),('p','partition')]:
            if a in c:r[b]=c[a]
        r['entryTemplate']=template(name,'entry',profile=profile)
        if c.get('w',0)>0:r['workerTemplate']=template(name,'worker',profile=profile)
        spec['template']['roles'].append(r)
    strategy={'type':'ServingGroupRollingUpdate' if config['mode']=='SG' else 'RoleRollingUpdate'}
    if 'top' in config:strategy['rollingUpdateConfiguration']={dict(u='maxUnavailable',s='maxSurge',p='partition')[k]:v for k,v in config['top'].items()}
    coord=config.get('coordination','omitted')
    if isinstance(coord,dict):
        co={'maxSkew':coord['maxSkew']}
        if 'roles' in coord:co['roles']=[NAMES.get(n,n) for n in coord['roles']]
        if 'dependencies' in coord:
            deps=coord['dependencies'];co['dependencies']=[{'role':NAMES[a],'dependsOn':[NAMES[b] for b in bs]} for a,bs in deps.items()] if isinstance(deps,dict) else []
        strategy['roleCoordination']=co
    if 'eviction' in config:
        ev=config['eviction'];e={'protectionLevel':'ServingGroup' if ev['level']=='SG' else 'Role'}
        if ev['level']=='SG':e['minAvailable']=ev['min']
        else:e['roleMinAvailable']={NAMES.get(ev.get('role','f'),ev.get('role','f')):ev['min']}
        strategy['evictionStrategy']=e
    if 'gangPolicy' in config:
        spec['template']['gangPolicy']=cp(config['gangPolicy'])
        if 'minRoleReplicas' in config['gangPolicy']:spec['template']['gangPolicy']['minRoleReplicas']={NAMES.get(k,k):v for k,v in config['gangPolicy']['minRoleReplicas'].items()}
    form=config.get('strategyForm')
    if form=='omitted':pass
    elif form=='null':spec['rolloutStrategy']=None
    elif form in ('{}','empty-object'):spec['rolloutStrategy']={}
    else:
        if form in ('onlyType','type-only','仅type'):strategy.pop('rollingUpdateConfiguration',None)
        if form in ('omitType','type-omitted','configuration-only','省略type'):strategy.pop('type',None)
        if form in ('eviction-only','onlyEviction','仅eviction'):strategy.pop('type',None);strategy.pop('rollingUpdateConfiguration',None)
        spec['rolloutStrategy']=strategy
    return spec

def step(name,spec=None,**kw):
    p={'name':name,'action':'update' if spec is not None else 'observe','until':'settled','release':'one','holdSeconds':0,'stableSeconds':10,'expect':{}}
    if spec is not None:p['spec']=cp(spec)
    p.update(kw);return p

def plan(row):
    n=int(row['id'][4:]);profile='auto' if 243<=n<=274 else 'controlled'
    a=initial(row['config'],profile);s=cp(a);steps=[]
    if n==148:role(a)['replicas']=3;s=cp(a)
    if 61<=n<=68 or n==72:
        steps=[step('rollout-B',version(s))]
    elif 69<=n<=71:
        if n==69:a['rolloutStrategy']={'type':'ServingGroupRollingUpdate','rollingUpdateConfiguration':{'maxUnavailable':1,'maxSurge':0,'partition':0}}
        else:
            b=a['rolloutStrategy']['rollingUpdateConfiguration'] if n==70 else role(a)
            b.update(maxUnavailable=1,maxSurge=0,partition=0)
        steps=[step('submit-null-and-B',version(s))]
    elif 73<=n<=76:
        keys=['maxUnavailable'] if n in (73,75) else ['maxSurge','partition']
        if n<=74:
            patch={'spec':{'rolloutStrategy':{'rollingUpdateConfiguration':{k:None for k in keys}}}}
            for k in keys:s['rolloutStrategy']['rollingUpdateConfiguration'].pop(k,None)
        else:
            # merge-patch replaces arrays; preserve every Role and literal null.
            rr=cp(s['template']['roles'])
            next(r for r in rr if r['name']=='frontend').update({k:None for k in keys})
            patch={'spec':{'template':{'roles':rr}}}
            for k in keys:role(s).pop(k,None)
        steps=[step('restore-defaults',action='merge-patch',patch=patch,expect={'noReplacement':True}),step('rollout-B',version(s))]
    elif 77<=n<=106:
        names=list(row['config']['roles'])
        update=[x for x in names if x!='x']
        if n==102:update=['f']
        if n==103:update=['b']
        if n==104:update=['x']
        if n==106:steps=[step('create-only',expect={'noReplacement':True,'noNewRevision':True})]
        else:
            version(s,update);steps=[step('coordinated-B',s)]
            if n==99:
                steps[0].update(until='conditions',release='except',exclude=[{'kind':'unit','group':0,'role':'backend','count':1}],conditions=[{'kind':'unit','group':1,'role':'frontend','version':'B','ready':True,'count':3},{'kind':'unit','group':2,'role':'frontend','version':'B','ready':True,'count':3}],holdSeconds=10,stableSeconds=0)
                steps.append(step('release-group-zero'))
            if n==101:steps[-1]['expect']={'noFullPromotion':True,'targets':[{'role':'frontend','versions':{'A':1,'B':2},'ordinals':{'0':'A'}},{'role':'backend','minVersions':{'A':1,'B':1},'minCount':3,'maxCount':4}]}
    elif 107<=n<=122:steps=[step('rollout-with-configured-plugins-and-budgets',version(s))]
    elif 123<=n<=131:
        op=(n-123)%4
        if n==131:op=0;version(s)
        if op==0:s['template']['roles'].append({'name':'extra','replicas':1,'workerReplicas':0,'entryTemplate':template('extra','entry',profile=profile)})
        elif op==1:s['template']['roles']=[r for r in s['template']['roles'] if r['name']!='frontend']
        elif op==2:role(s)['name']='extra'
        elif op==3:role(s)['workerReplicas']=1;role(s)['workerTemplate']=template('frontend','worker','B',profile)
        steps=[step('change-member-layout',s)]
    elif 132<=n<=150:
        ex={'noReplacement':True}
        if n in (132,133,134,140,141,142):
            ex['noNewRevision']=True;steps=[step('repeat-identical-A',s,expect=ex)]
        elif n in (135,143):
            steps=[step('outer-metadata',action='merge-patch',patch={'metadata':{'labels':{'normal-case':'metadata'},'annotations':{'normal-case':'metadata'}}},expect=ex)]
        elif n in (136,144):
            s['template']['roles'].reverse();steps=[step('reverse-role-list',s,expect=ex)]
        elif n in (137,145):
            for r in s['template']['roles']:r['entryTemplate']['metadata']['labels']={}
            steps=[step('empty-template-labels',s,expect=ex)]
        elif n in (138,146):
            s['schedulerName']='default-scheduler';steps=[step('change-top-level-scheduler',s,expect=ex)]
        elif n in (139,147):
            s['recoveryPolicy']='None';s['template']['restartGracePeriodSeconds']=30;steps=[step('change-recovery-policy',s,expect=ex)]
        elif n==148:
            s['rolloutStrategy']={'type':'RoleRollingUpdate'};steps=[step('switch-SG-to-Role',s,expect=ex)]
        elif n==149:
            for r in s['template']['roles']:r.pop('maxSurge',None);r.pop('partition',None)
            s['rolloutStrategy']={'type':'ServingGroupRollingUpdate','rollingUpdateConfiguration':{'maxUnavailable':1,'maxSurge':0,'partition':0}}
            steps=[step('switch-Role-to-SG',s,expect=ex)]
        elif n==150:
            co=s['rolloutStrategy'].pop('roleCoordination');steps=[step('remove-coordination',s,expect=ex)]
            s['rolloutStrategy']['roleCoordination']=co;steps.append(step('restore-coordination',s,expect=ex))
    elif n in (151,152):
        s['plugins'].append({'name':'ranktable','type':'BuiltIn','config':{'template':'${CASE_TEMPLATE}'}})
        ex={'noReplacement':True}
        # ConfigMap-only changes need the unchanged production live-audit
        # cadence (5 minutes), plus its 30-second reconciliation deadline and
        # the full stable window. The generic 180 seconds can expire too early.
        steps=[step('enable-ranktable',s,expect=ex),step('update-isolated-template',action='configmap',expect=ex,timeoutSeconds=420)]
    elif n==153:
        for r in s['template']['roles']:r['replicas']=4
        steps=[step('scale-equivalent-A',s,expect={'noReplacement':True})]
        for r in s['template']['roles']:r['maxUnavailable']=0;r['maxSurge']=1
        s['rolloutStrategy']['roleCoordination']['maxSkew']='25%'
        steps.append(step('change-only-budgets',s,expect={'noReplacement':True}))
        steps.append(step('restart-and-compare',action='restart-controller',expect={'noReplacement':True},stableSeconds=30))
    elif 154<=n<=159:
        # Build the target through a real rollout, restore the captured old MS
        # status, and require the still-running controller to promote it.
        version(s,('f','b'))
        preparation=cp(s)
        for r in preparation['template']['roles']:r['maxUnavailable']=1;r['maxSurge']=0
        preparation['rolloutStrategy'].pop('roleCoordination',None)
        steps=[step('prepare-contiguous-B-fixture',preparation),step('install-catalogue-strategy-on-B',s,expect={'noReplacement':True,'noNewRevision':True}),step('restore-old-current-state',action='restore-status',expect={'noReplacement':True},stableSeconds=30),step('restart-after-promotion',action='restart-controller',expect={'noReplacement':True},stableSeconds=30)]
    elif n in (160,161,162):
        if n==161:
            for r in s['template']['roles']:r['replicas']=2
            steps.append(step('expand-equivalent-A-first',s,expect={'noReplacement':True}))
        if n in (160,161):
            for r in s['template']['roles']:r['replicas']=2
            version(s,('f','b'))
        else:
            role(s)['replicas']=2;role(s)['workerReplicas']=2;version(s)
        steps.append(step('atomic-layout-and-template',s,stableSeconds=30))
    elif 163<=n<=194:
        if n in (178,194):
            set_desired(s,10);version(s);steps=[step('atomic-percentage-scale',s,stableSeconds=30)]
        else:
            base=163 if n<178 else 179
            op=(n-base)%5
            if op in (0,1):
                if op==0:s['replicas']=s.get('replicas',1)+1
                else:role(s)['replicas']=role(s).get('replicas',1)+1
                steps=[step('expand-equivalent-A',s,expect={'noReplacement':True}),step('shrink-equivalent-A',a,expect={'noReplacement':True},stableSeconds=30)]
            elif op==2:
                s['replicas']=s.get('replicas',1)+1;role(s)['replicas']=role(s).get('replicas',1)+1;version(s)
                steps=[step('atomic-N-R-and-B',s,stableSeconds=30)]
            else:
                version(s)
                cond=blocked(s,desired(s)-1 if effective_budget(s,'maxUnavailable') else desired(s))
                steps=[step('enter-mixed-rollout',s,until='conditions',release='none',conditions=[cond],holdSeconds=10,stableSeconds=0)]
                if op==3:s['replicas']=1
                else:role(s)['replicas']=1
                steps.append(step('shrink-while-B-in-flight',s,until='conditions',release='none',conditions=[{'kind':'terminating','count':1}],stableSeconds=0))
                if op==3:s['replicas']=a.get('replicas',1)
                else:role(s)['replicas']=role(a).get('replicas',1)
                steps.append(step('restore-before-deletion-finishes',s,requireLiveTerminating=True,stableSeconds=30))
    elif 195<=n<=242:
        version(s)
        if n in (229,230):role(s)['workerReplicas']=2
        steady=207<=n<=218 or n in (229,230,233,234,238,242)
        if steady:
            steps=[step('reach-canary-before-scale',s,stableSeconds=30)]
        else:
            d=desired(s);u=effective_budget(s,'maxUnavailable')
            target=d-1 if u else d
            conds=[blocked(s,target)]
            if n in (199,200,205,206,221,225):conds.append(blocked(s,d-2))
            if n in (221,225):conds.append(blocked(s,d))
            if n in (222,226):conds.extend([blocked(s,d),blocked(s,d+1)])
            if n in (231,232):conds=[dict(blocked(s,2),group=g) for g in (0,1)]
            steps=[step('reach-blocked-target',s,until='conditions',release='none',conditions=conds,holdSeconds=10,stableSeconds=0)]
        destinations={195:5,196:2,197:5,198:2,199:6,200:3,201:5,202:2,203:5,204:2,205:6,206:3,
            207:5,208:3,209:2,210:6,211:5,212:3,213:5,214:3,215:2,216:6,217:5,218:3,
            219:5,220:5,221:6,222:3,223:5,224:5,225:6,226:3,235:5,236:3,239:5,240:3,238:5,242:5}
        if n in (237,241):
            cond=blocked(s,2)
            steps.append(step('pin-exact-target-UID',action='pin',until='conditions',release='none',conditions=[cond],stableSeconds=0))
            set_desired(s,2);steps.append(step('shrink-pinned-target',s,until='conditions',release='none',conditions=[{'kind':'terminating','version':'B','count':1}],stableSeconds=0))
            set_desired(s,3);steps.append(step('expand-while-target-terminates',s,requireLiveTerminating=True,until='conditions',release='none',conditions=[{'kind':'terminating','version':'B','count':1}],stableSeconds=0))
            steps.append(step('unpin-and-complete',action='unpin',stableSeconds=30))
        else:
            if n in (227,228,229,230):role(s)['replicas']=2 if n in (227,229) else 1
            elif n in (231,232,233,234):s['replicas']=3 if n in (231,233) else 1
            else:set_desired(s,destinations[n])
            steps.append(step('scale-with-continuous-ledger',s,stableSeconds=30))
            if n in (238,242):
                set_budget(s,'partition',0);steps.append(step('unlock-last-protected-ordinal',s,stableSeconds=30))
        if n in (211,217,219,223):
            steps[-1]['expect']['targets']=[{'role':'frontend','versions':{'A':2,'B':3}}] if mode(s)=='Role' else []
        if n in (231,233):
            steps[-1]['expect']['targets']=[{'group':g,'role':'frontend','versions':({'A':1,'B':2} if g<2 else {'B':3})} for g in range(3)]
    elif 243<=n<=262:
        version(s)
        old_d=desired(s)
        cond={'kind':'started','ordinal':old_d-1,'count':1}
        if mode(s)=='Role':cond['role']='frontend'
        steps=[step('observe-natural-rollout-start',s,until='conditions',release='none',conditions=[cond],stableSeconds=0)]
        if n in (251,252,253,254):role(s)['replicas']=3 if n%2 else 1
        elif n in (255,256,257,258):s['replicas']=3 if n%2 else 1
        else:set_desired(s,8 if n%2 else 4)
        steps.append(step('separate-scale-during-natural-rollout',s,stableSeconds=30))
    elif 263<=n<=274:
        version(s)
        if n<=270:set_desired(s,5 if n%2 else 3)
        elif n in (271,272):role(s)['replicas']=3 if n==271 else 1;role(s)['workerReplicas']=2
        else:s['replicas']=3 if n==273 else 1
        steps=[step('single-request-scale-and-PodSpec',s,stableSeconds=30)]
    elif 275<=n<=296:
        offset=n-275 if n<=285 else n-286
        version(s)
        if offset in (0,1,2):
            d=desired(s);cond=blocked(s,d if effective_budget(s,'maxUnavailable')==0 else d-1)
            steps=[step('B-with-A-still-present',s,until='conditions',release='none',conditions=[cond],holdSeconds=10,stableSeconds=0)]
            version(s,v='C');steps.append(step('replace-inflight-B-with-C',s))
            version(s,v='A');steps.append(step('rollback-to-A',s,stableSeconds=30))
        elif offset in (3,5):
            steps=[step('reach-P1-canary',s,stableSeconds=30)]
            if offset==3:
                set_budget(s,'partition',0);steps.append(step('lower-partition-to-zero',s,stableSeconds=30))
            else:
                if mode(s)=='SG':patch={'spec':{'rolloutStrategy':{'rollingUpdateConfiguration':{'partition':None}}}}
                else:
                    role(s)['partition']=None;patch={'spec':{'template':{'roles':cp(s['template']['roles'])}}}
                steps.append(step('remove-partition-with-merge-patch',action='merge-patch',patch=patch,stableSeconds=30))
        elif offset==10:
            steps=[step('full-partition-before-freeze',s,stableSeconds=10)]
            set_budget(s,'maxUnavailable',0);set_budget(s,'maxSurge',0)
            steps.append(step('freeze-with-zero-budgets',s,expect={'noReplacement':True},stableSeconds=30))
        elif offset==7:
            steps=[step('occupy-free-scheduling-capacity',action='block-resources',expect={'noReplacement':True})]
            cond={'kind':'unschedulable','version':'B','count':1}
            if mode(s)=='Role':cond['role']='frontend'
            steps.append(step('B-pending-on-real-resources',s,until='conditions',release='none',conditions=[cond],holdSeconds=10,stableSeconds=0))
            set_budget(s,'maxUnavailable',1)
            steps.append(step('allow-one-unavailable',s,until='conditions',release='none',conditions=[{'kind':'started','count':1}],stableSeconds=0))
            steps.append(step('restore-scheduling-capacity',action='restore-resources',stableSeconds=30))
        else:
            d=desired(s);cond=blocked(s,d if effective_budget(s,'maxUnavailable')==0 else d-1)
            conds=[cond]
            if offset==9:conds.extend([blocked(s,d),blocked(s,d+1)])
            steps=[step('enter-inflight-B',s,until='conditions',release='none',conditions=conds,holdSeconds=10,stableSeconds=0)]
            if offset==4:set_budget(s,'partition',3)
            elif offset==6:set_budget(s,'maxUnavailable',0)
            elif offset==8:set_budget(s,'maxSurge',2)
            elif offset==9:set_budget(s,'maxSurge',0)
            steps.append(step('change-only-inflight-budget',s,stableSeconds=30))
    elif n==297:
        version(s,('f','b'));steps=[step('independent-role-budgets',s,stableSeconds=30)]
        version(s,('b',),'A');steps.append(step('rollback-backend-only',s,stableSeconds=30))
    elif 298<=n<=303:
        version(s,tuple(row['config']['roles']))
        if n==303:
            conds=[{'kind':'unit','role':'backend','version':'B','ready':True,'ordinal':2,'count':1}]
            steps=[step('reach-stable-target-backend',s,until='conditions',release='except',exclude=[{'kind':'unit','role':'frontend','count':1}],conditions=conds,holdSeconds=10,stableSeconds=0)]
        else:
            steps=[step('enter-coordinated-mixed-state',s,until='conditions',release='none',conditions=[{'kind':'unit','role':'backend','version':'B','ready':False,'count':1}],holdSeconds=10,stableSeconds=0)]
        co=s['rolloutStrategy']['roleCoordination']
        if n==298:co['maxSkew']='100%'
        elif n==299:co['maxSkew']='1%'
        elif n==300:co.pop('dependencies',None)
        elif n==301:s['rolloutStrategy'].pop('roleCoordination')
        elif n==302:co['roles']=['frontend','extra']
        elif n==303:co['dependencies']=[{'role':'frontend','dependsOn':['backend']}]
        steps.append(step('change-coordination-without-resetting-progress',s,stableSeconds=30))
        if n==301:
            steps[-1]['expect']['noNewRevision']=True
    else:raise ValueError(f'{row["id"]}: no explicit executable mapping yet')
    # Explicit stage outcomes supplement the continuous UID/budget ledger.
    # Percentage expansion keeps an already-issued B below the new partition.
    if 195<=n<=226 or n in (235,236,237,238,239,240,241,242) or 263<=n<=270:
        current=cp(a)
        for idx,p in enumerate(steps):
            if 'spec' in p: current=cp(p['spec'])
            if p['until']!='settled':continue
            d=desired(current);old=min(d,effective_budget(current,'partition'))
            if n in (211,217,219,223) and idx>0:old=2
            ords={str(i):'A' if i<old else 'B' for i in range(d)}
            t={'versions':{v:list(ords.values()).count(v) for v in sorted(set(ords.values()))},'ordinals':ords}
            if mode(current)=='SG':t['scope']='SG'
            else:t['role']='frontend'
            # Surge may introduce extra ordinals in flight. Every settled
            # normal endpoint must restore the current desired 0..D-1 set.
            p['expect']['targets']=[t]
    if n==153:
        for p in steps:p['expect']['noNewRevision']=True
    if n in (162,227,228,229,230,253,254,271,272):
        current=cp(a)
        for p in steps:
            if 'spec' in p:current=cp(p['spec'])
            if p['until']!='settled':continue
            p['expect']['targets']=[{'group':g,'role':'frontend','versions':{'A' if g==0 else 'B':role(current)['replicas']},'workers':{'A':role(a)['workerReplicas'],'B':role(current)['workerReplicas']}} for g in range(current['replicas'])]
    if n in (231,233,257,273):
        final=steps[-1]['spec'];r=role(final)['replicas']
        steps[-1]['expect']['targets']=[{'group':g,'role':'frontend','versions':({'A':1,'B':r-1} if g<2 else {'B':r}),'ordinals':{'0':'A' if g<2 else 'B'}} for g in range(3)]
    if 154<=n<=159:
        for p in steps:p['expect']['targets']=[{'role':r,'versions':{'B':2},'ordinals':{'0':'B','1':'B'}} for r in ('frontend','backend')]
    if profile=='auto':
        for p in steps:p['release']='none'
    return {'format':'rollout-runner/v2','id':row['id'],'baseline':COMMIT,'scenario':{'source':row,'profile':profile,'initialSpec':a,'steps':steps}}

def mode(spec):return 'Role' if spec.get('rolloutStrategy',{}).get('type')=='RoleRollingUpdate' else 'SG'
def desired(spec):return role(spec).get('replicas',1) if mode(spec)=='Role' else spec.get('replicas',1)
def set_desired(spec,n):
    if mode(spec)=='Role':role(spec)['replicas']=n
    else:spec['replicas']=n
def set_budget(spec,key,value):
    if mode(spec)=='Role':role(spec)[key]=value
    else:spec.setdefault('rolloutStrategy',{}).setdefault('rollingUpdateConfiguration',{})[key]=value
def effective_budget(spec,key):
    import math
    b=role(spec) if mode(spec)=='Role' else spec.get('rolloutStrategy',{}).get('rollingUpdateConfiguration',{})
    value=b.get(key,1 if key=='maxUnavailable' else 0)
    if isinstance(value,str):
        x=int(value[:-1])*desired(spec)/100
        return max(int(x),1) if key=='maxUnavailable' and mode(spec)=='SG' and x>0 else int(x) if key=='maxUnavailable' else math.ceil(x)
    return value
def blocked(spec,ordinal):
    c={'kind':'unit','ordinal':ordinal,'version':'B','ready':False,'count':1}
    if mode(spec)=='Role':c['role']='frontend'
    return c

def main():
    ap=argparse.ArgumentParser();ap.add_argument('--through',type=int,default=303);args=ap.parse_args()
    rows=json.loads(SOURCE.read_text())['cases'];out=ROOT/'cases/normal';out.mkdir(exist_ok=True)
    for p in (ROOT/'cases/core').glob('RUN-*.yaml'):shutil.copyfile(p,out/p.name)
    count=60
    for row in rows:
        if not row['id'].startswith('RUN-'):continue
        n=int(row['id'][4:])
        if not 61<=n<=args.through:continue
        case=plan(row);(out/(row['id']+'.yaml')).write_text(yaml(case)+'\n');count+=1
    indexed=[row for row in rows if row['id'].startswith('RUN-') and 1<=int(row['id'][4:])<=args.through]
    manifest={'format':'rollout-runner/suite-v1','controllerCommit':COMMIT,'sourceSHA256':hashlib.sha256(SOURCE.read_bytes()).hexdigest(),'cases':indexed,'inputs':{row['id']:hashlib.sha256((out/(row['id']+'.yaml')).read_bytes()).hexdigest() for row in indexed}}
    (out/'suite.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
    print(f'Generated {count} explicit cases (development range through RUN-{args.through:03d}).')
if __name__=='__main__':main()
