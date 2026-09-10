#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Opt-in real API proof for exact List omission; no controller changes.

Usage: check-list-omission-kind.py OUTPUT PROXY_URL CONTROL_URL CA_FILE
Requires RUNNER_PROXY_KUBECONFIG and RUNNER_PROXY_TOKEN_FILE.
"""
import datetime
import json
import os
import pathlib
import ssl
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

out=pathlib.Path(sys.argv[1]);out.mkdir()
proxy,control,ca=sys.argv[2:]
k=['kubectl','--kubeconfig',os.environ['RUNNER_PROXY_KUBECONFIG']]
secret=pathlib.Path(os.environ['RUNNER_PROXY_TOKEN_FILE']).read_text().strip()
token=subprocess.check_output(k+['-n','rollout-runner','create','token','rollout-runner','--duration=10m']).decode().strip()
tls=ssl.create_default_context(cafile=ca)
ns='runner-omit-'+datetime.datetime.now(datetime.timezone.utc).strftime('%m%d%H%M%S')
ruleid=ns+'-history'
proof={'namespace':ns,'scope':'isolated namespace and owned ControllerRevisions; no ModelServing or controller mutations','started':datetime.datetime.now(datetime.timezone.utc).isoformat()}

def save(name,value):
    with (out/name).open('x') as f:json.dump(value,f,indent=2);f.write('\n')

def kube(args,value=None):
    p=subprocess.run(k+args,input=None if value is None else json.dumps(value).encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,check=True)
    return json.loads(p.stdout)

def http(origin,path,method='GET',value=None,accept='application/json'):
    request=urllib.request.Request(origin+path,data=None if value is None else json.dumps(value).encode(),method=method,headers={'Authorization':'Bearer '+(secret if origin==control else token),'Accept':accept,'Content-Type':'application/json'})
    try:
        with urllib.request.urlopen(request,context=tls if origin==proxy else None,timeout=15) as response:
            raw=response.read();return response.status,json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:return error.code,json.load(error)

created=None
try:
    status,initial=http(control,'/v1/state');assert status==200 and not initial['errors'] and not any(r['active'] for r in initial.get('rules') or [])
    created=kube(['create','-f','-','-o','json'],{'apiVersion':'v1','kind':'Namespace','metadata':{'name':ns}});save('namespace.json',created)
    owner=kube(['create','-f','-','-o','json'],{'apiVersion':'v1','kind':'ConfigMap','metadata':{'name':'owner','namespace':ns}})
    histories=[]
    for name in ('a','b'):
        histories.append(kube(['create','-f','-','-o','json'],{'apiVersion':'apps/v1','kind':'ControllerRevision','metadata':{'name':'model-'+name,'namespace':ns,'labels':{'modelserving.volcano.sh/name':'model'},'ownerReferences':[{'apiVersion':'v1','kind':'ConfigMap','name':'owner','uid':owner['metadata']['uid'],'controller':True}]},'revision':1,'data':{'version':name}}))
    save('histories-before.json',histories)
    selected=histories[1];uid=selected['metadata']['uid'];name=selected['metadata']['name']
    rule={'id':ruleid,'namespace':ns,'resource':'controllerrevisions','methods':['GET'],'mode':'omit-list-object','count':-1,'durationSeconds':60,'collectionOnly':True,'labelSelector':'modelserving.volcano.sh/name=model','listLimit':0,'omitObject':{'name':name,'uid':uid,'ownerUID':owner['metadata']['uid']}}
    status,installed=http(control,'/v1/rules','POST',rule);assert status==201;save('installed-rule.json',installed)
    path='/apis/apps/v1/namespaces/'+ns+'/controllerrevisions'
    query='?'+urllib.parse.urlencode({'labelSelector':rule['labelSelector']})
    status,filtered=http(proxy,path+query,accept='application/vnd.kubernetes.protobuf,application/json');assert status==200 and [r['metadata']['uid'] for r in filtered['items']]==[histories[0]['metadata']['uid']];save('omitted-list.json',filtered)
    direct=kube(['-n',ns,'get','controllerrevisions','-o','json']);assert {r['metadata']['uid'] for r in direct['items']}=={r['metadata']['uid'] for r in histories};save('direct-list.json',direct)
    status,paged=http(proxy,path+query+'&limit=500');assert status==200 and {r['metadata']['uid'] for r in paged['items']}=={r['metadata']['uid'] for r in histories};save('unrelated-paged-list.json',paged)
    status,read=http(proxy,path+'/'+name);assert status==200 and read['metadata']['uid']==uid and read['data']==selected['data'];save('direct-name-read.json',read)
    request={'apiVersion':'apps/v1','kind':'ControllerRevision','metadata':{'namespace':ns,'name':name,'ownerReferences':selected['metadata']['ownerReferences']},'revision':1,'data':selected['data']}
    status,collision=http(proxy,path,'POST',request);assert status==409 and collision['reason']=='AlreadyExists';save('native-collision.json',collision)
    status,_=http(control,'/v1/rules/'+ruleid,'DELETE');assert status==204
    status,restored=http(proxy,path+query);assert status==200 and {r['metadata']['uid'] for r in restored['items']}=={r['metadata']['uid'] for r in histories};save('restored-list.json',restored)
    for value in restored['items']:
        original=next(o for o in histories if o['metadata']['uid']==value['metadata']['uid'])
        assert original['metadata']['ownerReferences']==value['metadata']['ownerReferences'] and original['data']==value['data']
    status,final=http(control,'/v1/state');assert status==200 and not final['errors']
    installed=next(r for r in final['rules'] if r['id']==ruleid);assert installed['hits']==1 and not installed['active'];save('final-state.json',final)
    proof.update(status='PASS',historyUID=uid,omittedLists=1,nativeCreateStatus=409)
finally:
    http(control,'/v1/rules/'+ruleid,'DELETE')
    if created is not None:
        actual=kube(['get','namespace',ns,'-o','json']);assert actual['metadata']['uid']==created['metadata']['uid']
        subprocess.run(k+['delete','namespace',ns,'--wait=true','--timeout=60s'],check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    proof['finished']=datetime.datetime.now(datetime.timezone.utc).isoformat();save('result.json',proof)
print(json.dumps(proof,indent=2))
