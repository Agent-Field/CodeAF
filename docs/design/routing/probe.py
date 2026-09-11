import json,os,sys,time,collections,urllib.request
KEY=os.environ['OPENROUTER_API_KEY']
URL='https://openrouter.ai/api/v1/chat/completions'
MODELS=['z-ai/glm-5.3-flash','deepseek/deepseek-v4.1-flash','qwen/qwen3.8-27b','mistralai/mistral-nemo','moonshotai/kimi-k3']
# lanes per model = every provider that served it in the last 3 days of the log
recs=[]
for l in open(os.path.expanduser('~/.aforge/logs/calls.jsonl')):
    try: recs.append(json.loads(l))
    except: pass
lanes=collections.defaultdict(collections.Counter)
for r in recs:
    if r.get('ts','')>='2026-09-08' and r.get('phase') is None and r.get('served') and r.get('model') in MODELS:
        lanes[r['model']][r['served']]+=1
PROMPT="Write 220 words explaining how a TCP congestion window grows and shrinks. Plain prose, no lists."
def call(model, prefs, max_tokens=300):
    body={'model':model,'messages':[{'role':'user','content':PROMPT}],'stream':True,'max_tokens':max_tokens,'provider':prefs,'reasoning':{'enabled':False}}
    req=urllib.request.Request(URL,data=json.dumps(body).encode(),headers={'Authorization':'Bearer '+KEY,'Content-Type':'application/json'})
    t0=time.time(); ttft=None; toks=0; served=None; status=None; err=None; last=t0; chars=0
    try:
        with urllib.request.urlopen(req,timeout=60) as resp:
            status=resp.status
            for line in resp:
                line=line.decode('utf-8','replace').strip()
                if not line.startswith('data:'): continue
                d=line[5:].strip()
                if d=='[DONE]': break
                try: j=json.loads(d)
                except: continue
                served=j.get('provider') or served
                ch=j.get('choices') or []
                if ch and ch[0].get('delta',{}).get('content'):
                    if ttft is None: ttft=time.time()-t0
                    chars+=len(ch[0]['delta']['content']); last=time.time()
                if j.get('usage'): toks=j['usage'].get('completion_tokens',0)
    except urllib.error.HTTPError as e:
        status=e.code; err=e.read().decode('utf-8','replace')[:160]
    except Exception as e:
        err=str(e)[:160]
    total=time.time()-t0
    if not toks: toks=chars//4
    gen=(last-t0-ttft) if ttft else 0
    tps=toks/gen if gen>0.3 else None
    return dict(status=status,served=served,ttft=ttft and round(ttft,2),tps=tps and round(tps),toks=toks,total=round(total,1),err=err)
out=[]
print("== per-lane probe (provider.only, allow_fallbacks=false) ==")
for m in MODELS:
    for lane,_ in lanes[m].most_common(8):
        r=call(m,{'only':[lane],'allow_fallbacks':False})
        r.update(model=m,asked=lane); out.append(r)
        print(f"{m:30s} {lane:14s} status={r['status']} served={r['served']} ttft={r['ttft']} tps={r['tps']} toks={r['toks']} total={r['total']} {r['err'] or ''}")
        sys.stdout.flush()
print("\n== OpenRouter default routing (no prefs) x3, and sort=latency / sort=throughput ==")
for m in MODELS[:2]:
    for prefs in ({}, {}, {}, {'sort':'latency'}, {'sort':'throughput'}):
        r=call(m,prefs); r.update(model=m,asked=json.dumps(prefs)); out.append(r)
        print(f"{m:30s} {json.dumps(prefs):22s} served={r['served']} ttft={r['ttft']} tps={r['tps']} total={r['total']} {r['err'] or ''}"); sys.stdout.flush()
print("\n== conflict test: order=[X] AND ignore=[X] with fallbacks on ==")
for m,x in (('z-ai/glm-5.3-flash','Relace'),('deepseek/deepseek-v4.1-flash','DeepInfra'),('deepseek/deepseek-v4.1-flash','Novita')):
    r=call(m,{'order':[x],'ignore':[x],'allow_fallbacks':True},max_tokens=40); r.update(model=m,asked='order+ignore '+x); out.append(r)
    print(f"{m:30s} order+ignore={x:10s} served={r['served']} status={r['status']} {r['err'] or ''}"); sys.stdout.flush()
json.dump(out,open('probe.json','w'),indent=1)
