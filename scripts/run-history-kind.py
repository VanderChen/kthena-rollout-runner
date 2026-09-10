#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Run a production history/boundary batch on a verified proxy with immutable build evidence.
Usage: run-history-kind.py MANIFEST BUILD_JSON BINARY
"""
import base64,calendar,copy,datetime,hashlib,json,os,pathlib,subprocess,sys,time,traceback,urllib.request

root=pathlib.Path(__file__).resolve().parents[1]
manifest=root/sys.argv[1]
jobRequest=json.loads(subprocess.check_output(['/private/tmp/runner022-yaml-json',str(manifest)]))[manifest.name]
args=jobRequest['spec']['template']['spec']['containers'][0]['args']
runid=next(a.split('=',1)[1] for a in args if a.startswith('--run-id='))
selected=next(a.split('=',1)[1].split(',') for a in args if a.startswith('--select='))
jobname=jobRequest['metadata']['name']
proxyPod=os.environ.get('RUNNER_PROXY_POD','recovery-proxy-022-r6')
proxyControl=os.environ.get('RUNNER_PROXY_CONTROL_URL','http://127.0.0.1:18033')
proxyCA=os.environ.get('RUNNER_PROXY_CA_FILE','/private/tmp/recovery-proxy-022-r6-tls/tls.crt')
proxyAPI=next(a.split('=',1)[1] for a in args if a.startswith('--fault-proxy-api='))
out=root/'artifacts/environment-022'/(runid+'-control');out.mkdir()
target=root/'artifacts'/runid
assert not target.exists()
k=['kubectl','--kubeconfig','/private/tmp/runner-normal-022.kubeconfig']
token=pathlib.Path('/private/tmp/recovery-proxy-022-control-token').read_text().strip()
proof={'runID':runid,'started':datetime.datetime.now(datetime.timezone.utc).isoformat(),'status':'SETUP','selected':selected}
original=None;connected=None;config=None;job=None;jobTerminal=False;logFollower=None
# Never reuse immutable ConfigMap names across proxy endpoints: node volume
# caches can retain the previous mounted contents while old Pods terminate.
cmname='runner-proxy-'+runid
assert len(cmname)<=63

def run(args,obj=None,timeout=30):
    p=subprocess.run(k+args,input=None if obj is None else json.dumps(obj).encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
    if p.returncode:raise RuntimeError('kubectl '+ ' '.join(args[:5])+' failed: '+p.stderr.decode())
    return p.stdout
def api(args,obj=None):return json.loads(run(args,obj))
def save(name,value):
    with (out/name).open('x') as f:json.dump(value,f,indent=2);f.write('\n')
def state():
    req=urllib.request.Request(proxyControl+'/v1/state',headers={'Authorization':'Bearer '+token})
    with urllib.request.urlopen(req,timeout=10) as response:return json.load(response)
def nanos(value):
    seconds=calendar.timegm(time.strptime(value[:19],"%Y-%m-%dT%H:%M:%S"))
    fraction=value[19:-1]
    return seconds*1_000_000_000+(int(fraction[1:].ljust(9,"0")) if fraction.startswith(".") else 0)
def until(check,seconds=90):
    deadline=time.monotonic()+seconds
    while True:
        value=check()
        if value:return value
        if time.monotonic()>deadline:raise RuntimeError('bounded setup condition timed out')
        time.sleep(.25)
def patchTemplate(current,template,label):
    patch=[{'op':'test','path':'/metadata/uid','value':current['metadata']['uid']},{'op':'test','path':'/metadata/resourceVersion','value':current['metadata']['resourceVersion']},{'op':'replace','path':'/spec/template','value':template}]
    save(label+'-patch.json',patch)
    return api(['-n','kthena-system','patch','deployment','kthena-controller-manager','--type=json','--patch-file',str(out/(label+'-patch.json')),'-o','json'])
def ids(items):return {(i['metadata']['namespace'],i['metadata']['name']):i['metadata']['uid'] for i in items['items']}

try:
    initial=state();assert not initial['errors'] and not any(r['active'] for r in initial.get('rules') or [])
    save('proxy-before.json',initial)
    proxyActual=api(['-n','rollout-runner','get','pod',proxyPod,'-o','json'])
    assert len(proxyActual['status']['containerStatuses'])==1 and proxyActual['status']['containerStatuses'][0]['ready'] and proxyActual['status']['containerStatuses'][0]['restartCount']==0
    save('proxy-active-pod.json',proxyActual)
    if os.environ.get('RUNNER_PROXY_BUILD_EVIDENCE'):
        proxyBuild=json.loads((root/os.environ['RUNNER_PROXY_BUILD_EVIDENCE']).read_text())
        assert proxyBuild['workingTree']=='' and proxyActual['status']['containerStatuses'][0]['imageID']==proxyBuild['imageID']
        save('proxy-build.json',proxyBuild)
    history=api(['get','modelservings','-A','-o','json']);assert len(history['items'])==9;save('history-before.json',history)
    original=api(['-n','kthena-system','get','deployment','kthena-controller-manager','-o','json'])
    baseline=json.loads((root/'artifacts/proxy-controller-pause-r5/controller-restored.json').read_text())
    assert original['metadata']['uid']==baseline['metadata']['uid'] and original['spec']==baseline['spec']
    save('controller-before.json',original)
    cert=pathlib.Path(proxyCA).read_bytes()
    kc={'apiVersion':'v1','kind':'Config','clusters':[{'name':'proxy','cluster':{'server':proxyAPI,'certificate-authority-data':base64.b64encode(cert).decode()}}],'users':[{'name':'controller','user':{'tokenFile':'/var/run/secrets/kubernetes.io/serviceaccount/token'}}],'contexts':[{'name':'proxy','context':{'cluster':'proxy','user':'controller'}}],'current-context':'proxy'}
    config=api(['create','-f','-','-o','json'],{'apiVersion':'v1','kind':'ConfigMap','metadata':{'name':cmname,'namespace':'kthena-system','labels':{'rollout-runner/run':runid}},'immutable':True,'data':{'kubeconfig':json.dumps(kc)}})
    save('proxy-config.json',config)
    template=copy.deepcopy(original['spec']['template'])
    container=template['spec']['containers'][0]
    container['args'].append('--kubeconfig=/runner-fault-proxy/kubeconfig')
    container['volumeMounts'].append({'name':'runner-fault-proxy','mountPath':'/runner-fault-proxy','readOnly':True})
    template['spec']['volumes'].append({'name':'runner-fault-proxy','configMap':{'name':cmname}})
    connected=patchTemplate(original,template,'connect');save('controller-connected.json',connected)
    run(['-n','kthena-system','rollout','status','deployment/kthena-controller-manager','--timeout=90s'],timeout=100)
    pods=api(['-n','kthena-system','get','pods','-l','app.kubernetes.io/component=kthena-controller-manager','-o','json'])
    active=[p for p in pods['items'] if not p['metadata'].get('deletionTimestamp')]
    assert len(active)==1
    controller=active[0]
    assert controller['status']['containerStatuses'][0]['imageID']=='sha256:7c6ed6c78b37d7afaf104381351554ad75aed56c64d0aca2ec941ce652756265'
    save('controller-active-pod.json',controller)
    mounted=json.loads(run(['-n','kthena-system','exec',controller['metadata']['name'],'--','cat','/runner-fault-proxy/kubeconfig']))
    assert mounted==kc, 'actual mounted kubeconfig does not match this run proxy'
    save('mounted-proxy-config.json',{'controllerUID':controller['metadata']['uid'],'configMapUID':config['metadata']['uid'],'configMapName':cmname,'server':mounted['clusters'][0]['cluster']['server'],'sha256':hashlib.sha256(json.dumps(mounted,sort_keys=True).encode()).hexdigest()})
    def actualStartupRequests():
        startupTrace=run(['-n','rollout-runner','exec',proxyPod,'--','tail','-n','20000','/evidence/trace.jsonl'])
        assert token.encode() not in startupTrace and b'Bearer ' not in startupTrace
        startupRequests=[json.loads(line) for line in startupTrace.splitlines() if line]
        return [row for row in startupRequests if row.get('action')=='request' and nanos(row['at'])>=nanos(controller['status']['startTime']) and row.get('resource')=='modelservings']
    startupRequests=until(actualStartupRequests,30)
    save('proxy-controller-startup-requests.json',startupRequests)
    lf=(out/'controller.log').open('xb')
    logFollower=subprocess.Popen(k+['-n','kthena-system','logs',controller['metadata']['name'],'-f','--timestamps'],stdout=lf,stderr=subprocess.STDOUT)
    warmup=json.loads((root/'artifacts/proxy-controller-pause-r5/initial-request.json').read_text())
    warmup['metadata']={'namespace':'rollout-runner','name':'recovery-admission-check'}
    attempts=[]
    def admissionReady():
        started=time.monotonic()
        try:
            value=api(['--request-timeout=10s','create','--raw','/apis/workload.serving.volcano.sh/v1alpha1/namespaces/rollout-runner/modelservings?dryRun=All','-f','-'],warmup)
            attempts.append({'status':'accepted','seconds':time.monotonic()-started,'object':value});return True
        except Exception as e:attempts.append({'status':'error','seconds':time.monotonic()-started,'error':str(e)});return False
    try:until(admissionReady,60)
    finally:save('admission-warmup.json',attempts)
    image=jobRequest['spec']['template']['spec']['containers'][0]['image']
    build={'runnerCommit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=str(root)).decode().strip(),'workingTree':subprocess.check_output(['git','status','--porcelain'],cwd=str(root)).decode(),'image':image,'imageID':json.loads(subprocess.check_output(['docker','image','inspect',image]))[0]['Id'],'binarySHA256':hashlib.sha256((root/sys.argv[3]).read_bytes()).hexdigest(),'manifestSHA256':hashlib.sha256(manifest.read_bytes()).hexdigest()}
    immutable=json.loads((root/sys.argv[2]).read_text())
    assert build['image']==immutable['image'] and build['imageID']==immutable['imageID'] and build['binarySHA256']==immutable['binarySHA256']
    build['manifestCommit']=build['runnerCommit']
    build['runnerCommit']=immutable['runnerCommit']
    build['immutableBuildEvidence']=sys.argv[2]
    save('build.json',build)
    job=api(['create','-f',str(manifest),'-o','json']);save('job-created.json',job)
    proof.update(status='RUNNING',job=jobname,jobUID=job['metadata']['uid'])
    save('start.json',proof)
    print('Started '+jobname+' UID='+job['metadata']['uid']+' selected='+','.join(selected),flush=True)
    previous=None
    while True:
        current=api(['-n','rollout-runner','get','job',jobname,'-o','json'])
        assert current['metadata']['uid']==job['metadata']['uid']
        terminal=any(c['type'] in ('Complete','Failed') and c['status']=='True' for c in current.get('status',{}).get('conditions',[]))
        p=subprocess.run(k+['-n','rollout-runner','exec','rollout-results-reader','--','cat','/artifacts/'+runid+'/summary.json'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        if p.returncode==0:
            try:
                summary=json.loads(p.stdout)
                compact=(len(summary['results']),summary['passed'])
                if compact!=previous:
                    print('Observed '+str(compact[0])+' completed, '+str(compact[1])+' rawPASS; '+summary['results'][-1]['id']+' '+summary['results'][-1]['status'],flush=True)
                    previous=compact
            except (ValueError,KeyError):pass
        if terminal:
            jobTerminal=True;save('job-terminal.json',current);break
        time.sleep(5)
    run(['-n','rollout-runner','cp','rollout-results-reader:/artifacts/'+runid,str(target)],timeout=90)
    runnerPods=api(['-n','rollout-runner','get','pods','-l','job-name='+jobname,'-o','json']);save('runner-pods.json',runnerPods)
    assert len(runnerPods['items'])==1
    rp=runnerPods['items'][0]
    with (out/'runner.log').open('xb') as f:f.write(run(['-n','rollout-runner','logs',rp['metadata']['name'],'--timestamps']))
    environment=json.loads((target/'environment.json').read_text())
    assert environment['runner']['binarySHA256']==build['binarySHA256']
    assert rp['status']['containerStatuses'][0]['imageID']==build['imageID'] and rp['status']['containerStatuses'][0]['restartCount']==0
    summary=json.loads((target/'summary.json').read_text())
    assert summary['selected']==len(selected) and [r['id'] for r in summary['results']]==selected
    trace=run(['-n','rollout-runner','exec',proxyPod,'--','cat','/evidence/trace.jsonl'])
    assert token.encode() not in trace and b'Bearer ' not in trace
    with (out/'proxy-trace.jsonl').open('xb') as f:f.write(trace)
    proxyState=state();save('proxy-after.json',proxyState)
    proxyAfter=api(['-n','rollout-runner','get','pod',proxyPod,'-o','json']);save('proxy-after-pod.json',proxyAfter)
    assert proxyAfter['metadata']['uid']==proxyActual['metadata']['uid'] and proxyAfter['status']['containerStatuses'][0]['restartCount']==0 and proxyAfter['status']['containerStatuses'][0]['imageID']==proxyActual['status']['containerStatuses'][0]['imageID']
    proof.update(status='COLLECTED_PENDING_CASE_REVIEW',completed=len(summary['results']),rawPassed=summary['passed'],rawFailures=[r for r in summary['results'] if r['status']!='PASS'],proxyErrors=proxyState['errors'])
except Exception as e:
    proof.update(status='EXECUTION_ERROR',error=str(e),traceback=traceback.format_exc())
    print('Execution error: '+str(e),flush=True)
finally:
    cleanup=[]
    if logFollower is not None:
        logFollower.terminate();logFollower.wait(timeout=10);lf.close()
    # Never detach the proxy while the authoritative Job is still running.
    if connected is not None and (job is None or jobTerminal):
        try:
            active=state()
            assert not any(r['active'] for r in active.get('rules') or [])
            current=api(['-n','kthena-system','get','deployment','kthena-controller-manager','-o','json'])
            assert current['metadata']['uid']==original['metadata']['uid'] and current['spec']['template']==connected['spec']['template']
            restored=patchTemplate(current,original['spec']['template'],'restore')
            run(['-n','kthena-system','rollout','status','deployment/kthena-controller-manager','--timeout=90s'],timeout=100)
            restored=api(['-n','kthena-system','get','deployment','kthena-controller-manager','-o','json'])
            assert restored['spec']==original['spec'];save('controller-restored.json',restored)
            proof['controllerSpecRestored']=True
            actual=api(['-n','kthena-system','get','configmap',cmname,'-o','json'])
            assert actual['metadata']['uid']==config['metadata']['uid']
            run(['-n','kthena-system','delete','configmap',cmname])
            after=api(['get','modelservings','-A','-o','json']);save('history-after.json',after)
            assert ids(history)==ids(after) and len(after['items'])==9
            proof['historicalModelServingUIDsPreserved']=9
        except Exception as e:cleanup.append(str(e))
    if job is not None and not jobTerminal:proof['jobMayStillBeActive']=True
    if cleanup:proof['cleanupErrors']=cleanup
    proof['finished']=datetime.datetime.now(datetime.timezone.utc).isoformat();save('completion.json',proof)
    print(json.dumps(proof,indent=2),flush=True)
    if proof['status']=='EXECUTION_ERROR' or cleanup:raise SystemExit(1)
