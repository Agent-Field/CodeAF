"""Real mini/Pi clients against a local fake provider, never paid inference."""
import http.server,json,os,subprocess,sys,tempfile,threading,time
from pathlib import Path
BASE=Path(sys.argv[1]).resolve();ROOT=BASE/'runner'
READY=BASE.parent/'scoring-ready-01'
PREPARATION=json.loads((READY/'SCORING-PREPARATION.json').read_text())
assert PREPARATION['status']=='ready_for_actual_client_preflight'
FIRST=PREPARATION['accepted'][0]['id']
MODEL='deepseek/deepseek-v4-flash-0731'
BAD={'max_tokens','max_completion_tokens','max_output_tokens','reasoning','reasoning_effort','thinking','thinking_budget','temperature','top_p','top_k','min_p','frequency_penalty','presence_penalty'}
seen=[]
class Stub(http.server.BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def do_POST(self):
        p=json.loads(self.rfile.read(int(self.headers['Content-Length'])));seen.append(p)
        usage={'prompt_tokens':10,'completion_tokens':3,'total_tokens':13,'cost':.00001}
        if p.get('stream'):
            frames=[{'id':'stub','object':'chat.completion.chunk','model':MODEL,'choices':[{'index':0,'delta':{'role':'assistant','content':'Stub complete.'},'finish_reason':None}]},{'id':'stub','choices':[{'index':0,'delta':{},'finish_reason':'stop'}],'usage':usage}]
            body=''.join('data: '+json.dumps(x)+'\n\n' for x in frames)+'data: [DONE]\n\n';typ='text/event-stream'
        else:
            count=sum(not x.get('stream') for x in seen);assert count < 6, 'Unexpected mini loop';command='printf mini-smoke > smoke.txt' if count==1 else 'echo COMPLETE_TASK_AND_SUBMIT_FINAL_OUTPUT'
            body=json.dumps({'id':f'stub-mini-{count}','model':MODEL,'choices':[{'index':0,'message':{'role':'assistant','content':'Checking.','tool_calls':[{'id':f'call{count}','type':'function','function':{'name':'bash','arguments':json.dumps({'command':command})}}]},'finish_reason':'tool_calls'}],'usage':usage});typ='application/json'
        self.send_response(200);self.send_header('Content-Type',typ);self.send_header('Content-Length',str(len(body.encode())));self.end_headers();self.wfile.write(body.encode())
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Stub);threading.Thread(target=server.serve_forever,daemon=True).start();port=server.server_address[1]
work=Path(sys.argv[2]).resolve();work.mkdir(exist_ok=False);out=work/'mini';out.mkdir();(out/'workspace').mkdir();(out/'home').mkdir();(out/'home/.gitconfig').write_text('[safe]\n directory = /testbed\n');(out/'prompt.txt').write_text('Write smoke.txt and finish.')
prepared=json.loads((READY/'prepared'/FIRST/'ready.json').read_text())
spec={'model':MODEL,'seed':1,'image':prepared['runtime_image_id'],'repo_dir':prepared['repo_dir'],'config':str(BASE/'mini-source/src/minisweagent/config/mini.yaml'),'endpoint':f'http://127.0.0.1:{port}/v1/chat/completions'};(out/'mini-input.json').write_text(json.dumps(spec))
env={'PATH':os.environ['PATH'],'LANG':'C.UTF-8','HOME':str(out/'home'),'OPENROUTER_API_KEY':'mini-bench-sentinel','MSWEA_GLOBAL_CONFIG_DIR':str(out/'config'),'MSWEA_SILENT_STARTUP':'1','PYTHONPATH':str(BASE/'mini-source/src')}
with (out/'client.log').open('w') as log:p=subprocess.run([str(BASE/'venv/bin/python'),str(ROOT/'mini_entry.py'),str(out)],env=env,stdout=log,stderr=subprocess.STDOUT,timeout=120)
assert p.returncode==0,(out/'client.log').read_text()[-2500:]
assert (out/'workspace/smoke.txt').read_text()=='mini-smoke'
assert len(seen)==2 and all(not BAD.intersection(x) and x['model']==MODEL and x['seed']==1 for x in seen)
mini_meta=[{'fields':sorted(x),'forbidden_fields':sorted(BAD.intersection(x)),'seed':x['seed'],'model':x['model']} for x in seen]
runtimes=json.loads((READY/'pi-runtimes.json').read_text());image=next(x['pi_runtime_image_id'] for x in runtimes if x['task']==FIRST)
pi_results={}
for hook in [False,True]:
    pi_home=work/('pi-hook' if hook else 'pi-original');pi_home.mkdir()
    model={'id':MODEL,'reasoning':True,'input':['text'],'contextWindow':1000000,'maxTokens':32768,'cost':{'input':.1,'output':.1,'cacheRead':.01,'cacheWrite':0},'compat':{'supportsReasoningEffort':False}}
    (pi_home/'models.json').write_text(json.dumps({'providers':{'guard':{'baseUrl':f'http://127.0.0.1:{port}/v1','apiKey':'pi-bench-sentinel','api':'openai-completions','models':[model]}}}))
    cmd=['docker','run','--rm','--platform','linux/amd64','--network','host','--mount',f'type=bind,src={pi_home},dst=/bench/pi','--mount',f'type=bind,src={ROOT}/pi-defaults.ts,dst=/bench/pi-defaults.ts,readonly','-e','PI_CODING_AGENT_DIR=/bench/pi','-e','HOME=/bench/pi','-e','PI_TELEMETRY=0','--entrypoint','pi',image,'-p','--mode','json','--provider','guard','--model',MODEL,'--no-skills','--no-extensions','--no-prompt-templates','--no-themes','--no-context-files','--approve','--offline']
    if hook:cmd+=['--extension','/bench/pi-defaults.ts']
    cmd+=['Reply briefly.']
    start=len(seen)
    with (pi_home/'client.log').open('w') as log:p=subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT,stdin=subprocess.DEVNULL,timeout=90)
    assert p.returncode==0,(pi_home/'client.log').read_text()[-2500:]
    payloads=seen[start:];assert payloads
    fields=[sorted(BAD.intersection(x)) for x in payloads]
    if hook:assert all(not x for x in fields),fields
    else:assert any('max_tokens' in x or 'max_completion_tokens' in x for x in fields),fields
    pi_results['hook' if hook else 'original']={'requests':len(payloads),'forbidden_fields':fields}
# Test the actual strict guard: bad fields refuse before any upstream request.
env=dict(os.environ,GUARD_TEST='1',GUARD_UPSTREAM_KEY='stub-key',BENCH_SEED='2',BENCH_GUARD_BIND='127.0.0.1')
with (work/'guard-port.txt').open('w') as log:
    guard=subprocess.Popen([sys.executable,str(ROOT/'vendor/guard.py'),'--allow',MODEL,'--sentinel','sentinel','--audit',str(work/'guard-audit.jsonl'),'--usage',str(work/'guard-usage.jsonl'),'--upstream',f'http://127.0.0.1:{port}/v1'],env=env,stdout=log,stderr=subprocess.DEVNULL)
try:
    for _ in range(100):
        lines=(work/'guard-port.txt').read_text().splitlines()
        if lines:break
        time.sleep(.05)
    gp=int(lines[0].split()[1]);import requests
    before=len(seen)
    for key in BAD:
        p=requests.post(f'http://127.0.0.1:{gp}/v1/chat/completions',headers={'Authorization':'Bearer sentinel'},json={'model':MODEL,'messages':[],key:1},timeout=5);assert p.status_code==422,(key,p.status_code)
    assert len(seen)==before
    p=requests.post(f'http://127.0.0.1:{gp}/v1/chat/completions',headers={'Authorization':'Bearer sentinel'},json={'model':MODEL,'messages':[]},timeout=5);assert p.status_code==200
    assert seen[-1]['seed']==2 and not BAD.intersection(seen[-1])
finally:guard.terminate();guard.wait(timeout=10)
receipt={'passed':True,'real_provider_queries':0,'mini':mini_meta,'pi':pi_results,'guard_forbidden_cases':len(BAD),'seed_enforced':2}


# Exercise both exact shipping binaries through their native protocol-13 driver.
# This preflight uses only the loopback fake provider and sentinel credentials.
import importlib.util
receipt['aforge']={}
for arm in ['base','candidate']:
    directory=work/arm;directory.mkdir()
    home=directory/'home';home.mkdir()
    repo=directory/'workspace';repo.mkdir()
    subprocess.run(['git','init','-q',str(repo)],check=True)
    (home/'.gitconfig').write_text('[safe]\n directory = /repo\n')
    binary=BASE/'binaries'/arm/'aforge'
    name='afholdout-client-'+arm+'-'+str(os.getpid())
    command=['docker','run','--rm','--init','-i','--name',name,'--platform','linux/amd64','--network','host',
             '--cpus','2','--memory','8g','--mount',f'type=bind,src={repo},dst=/repo',
             '--mount',f'type=bind,src={home},dst=/bench/home',
             '--mount',f'type=bind,src={binary},dst=/bench/aforge,readonly','-w','/repo',
             '-e','HOME=/bench/home','-e','AFORGE_HOME=/bench/home',
             '-e','OPENROUTER_API_KEY=aforge-bench-sentinel',
             '-e',f'AFORGE_BASE_URL=http://127.0.0.1:{port}/v1',
             '--entrypoint','/bench/aforge',prepared['runtime_image_id'],
             'engine','--no-host','--workspace','/repo']
    spec=importlib.util.spec_from_file_location('preflight_engine_'+arm,BASE/('af-'+arm)/'engine_driver.py')
    driver=importlib.util.module_from_spec(spec);spec.loader.exec_module(driver)
    start=len(seen)
    try:
        code=driver.run(command,'/repo',MODEL,'Reply briefly.',directory,120)
        stop=json.loads((directory/'engine-stop.json').read_text())
        assert stop['reason']=='settled',(arm,code,stop['reason'])
        payloads=seen[start:];assert payloads
        assert all(x['model']==MODEL and not BAD.intersection(x) for x in payloads)
        receipt['aforge'][arm]={'requests':len(payloads),'protocol':13,'stop':stop['reason'],
                               'forbidden_fields':[sorted(BAD.intersection(x)) for x in payloads]}
    finally:
        subprocess.run(['docker','rm','-f',name],capture_output=True)
server.shutdown()
(work/'receipt.json').write_text(json.dumps(receipt,indent=2))
print(json.dumps(receipt))
