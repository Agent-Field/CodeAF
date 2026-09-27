import csv,json,subprocess,signal,os
from pathlib import Path
from decimal import Decimal
p=Path(__file__).parent
trials=[]
for trial in ('1','2'):
 state=Path('/tmp/caf-retry-usage-'+trial+'/state');project=state.parent/'project'
 output=subprocess.check_output(['python3',str(p/'audit.py'),str(state)],text=True);(p/('trial'+trial+'-audit.json')).write_text(output)
 a=json.loads(output);assert a['all_models_exact']
 rows=list(csv.DictReader((project/'purchase-order.csv').open()));assert rows==[{'item':'Bolts','available':'12','needed':'3'},{'item':'Nuts','available':'7','needed':'8'}]
 usage=[json.loads(x,parse_float=Decimal) for x in (state/'v3/usage.jsonl').read_text().splitlines()]
 retained=[]
 for first in a['empty_at_ceiling']:
  matches=[r for r in usage if r.get('in')==first.get('prompt_tokens') and r.get('out')==first.get('completion_tokens') and Decimal(str(r.get('usd')))==Decimal(first['cost'])]
  assert len(matches)==1,(first,matches)
  retained.append({'transport_id':first['id'],'cost':first['cost'],'ledger_at':matches[0]['at'],'ledger_role':matches[0].get('role'),'retained_once':True})
 trials.append({'trial':trial,'audit':a,'retained_discarded_attempts':retained,'csv':rows,'independent_tests':24,'launch':json.loads((p/('trial'+trial+'-launch.json')).read_text())})
receipt={'issue':1624,'fix_pr':1625,'composition':'25893534b + 1f4f3c51f = 961c98c0d','final_pr_docs_only':'6105c01e8','natural_tmux_trials':2,'fault_injection':False,'all_roles_model':'deepseek/deepseek-v4.1-flash','trials':trials,'limitations':['Trial1 retry stream deadline omitted transport usage; existing reconciliation later banked0.0014391. Raw transport full-cost parity is not claimed for that trial.','Trial2 also produced a natural ceiling retry after the initial snapshot. Its retry stream deadline omitted transport cost; existing reconciliation later banked0.0013488. Neither trial claims raw transport full-cost parity.','Guard internals are verified by deterministic tests, not measured through these UI runs.']}
(p/'receipts.json').write_text(json.dumps(receipt,indent=2))
print('Two trial receipts verified; first discarded response retained exactly once.')
