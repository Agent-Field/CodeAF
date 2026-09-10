"""Read provider usage from native mini trajectories without changing measured runs.
Requests decompresses JSON responses; the inherited raw guard cannot parse their compressed wire bytes.
Keep this supplementary ledger separate and never add cumulative model_stats to per-generation charges.
"""
import datetime,json,os,signal,subprocess,sys,time
from pathlib import Path
CELL=Path(sys.argv[1]).resolve()
BASE=CELL.parents[2]
OUT=BASE/'mini-usage';OUT.mkdir(exist_ok=True)
known={};stopped=set()
def snapshot():
 for trajectory in [CELL/'trajectory.json']:
  cell=trajectory.parent.name
  try:data=json.loads(trajectory.read_text())
  except (ValueError,OSError):continue
  seen=known.setdefault(cell,{})
  for m in data.get('messages',[]):
   response=m.get('extra',{}).get('response') or {};gid=response.get('id');usage=response.get('usage')
   if not gid or not usage:continue
   entry={'generation_id':gid,'model':response.get('model'),'usage':usage}
   if gid in seen and seen[gid]!=entry:raise RuntimeError('Conflicting usage for '+gid)
   seen[gid]=entry
  total=sum(x['usage'].get('cost') or 0 for x in seen.values())
  receipt={'cell':cell,'source':'native OpenRouter response usage in mini trajectory','known_cost_usd':total,'generations':list(seen.values()),'note':'Raw guard unknown rows overlap these generations; never add unknown counts without matching attempts. Lost responses/retries can still be unpriced.'}
  target=OUT/(cell+'.json');tmp=target.with_suffix('.tmp');tmp.write_text(json.dumps(receipt,indent=2));tmp.replace(target)
  if total>=5 and cell not in stopped and not (trajectory.parent/'agent-result.json').exists():
   for proc in Path('/proc').glob('[0-9]*'):
    try:
     argv=(proc/'cmdline').read_bytes().split(b'\0');pid=int(proc.name)
     if len(argv)>=3 and argv[1].decode()==str(BASE/'runner/mini_entry.py') and argv[2].decode()==str(trajectory.parent) and os.getpgid(pid)==pid:
      os.killpg(pid,signal.SIGTERM)
      event={'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'cell':cell,'reason':'external $5 known provider usage stop','known_cost_usd':total,'pid':pid}
      (OUT/(cell+'-cost-stop.json')).write_text(json.dumps(event,indent=2));print(json.dumps(event),flush=True)
      cid=trajectory.parent/'container-id.txt'
      if cid.exists():subprocess.run(['docker','rm','-f',cid.read_text().strip()],capture_output=True)
    except (ProcessLookupError,FileNotFoundError,PermissionError):continue
   stopped.add(cell)
while True:
 snapshot()
 if (CELL/'result.json').exists():break
 time.sleep(1)
snapshot();print('Mini native usage capture complete',flush=True)
