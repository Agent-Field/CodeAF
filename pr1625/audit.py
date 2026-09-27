import json,sys
from pathlib import Path
from decimal import Decimal
root=Path(sys.argv[1])
def rows(path):
 return [json.loads(s,parse_float=Decimal) for s in path.read_text().splitlines() if s.strip()] if path.exists() else []
calls=rows(root/'logs/calls.jsonl')
paid=[r for r in calls if 'status' in r]
usage=rows(root/'v3/usage.jsonl')
assert all(r.get('model')=='deepseek/deepseek-v4.1-flash' for r in calls), 'unexpected transport model'
assert all(r.get('model')=='deepseek/deepseek-v4.1-flash' for r in usage), 'unexpected ledger model'
def cost(r):
 return Decimal(str(r.get('cost',r.get('usd',0)) or 0))
a=sum(map(cost,paid),Decimal(0));b=sum(map(cost,usage),Decimal(0))
result={'transport_calls':len(paid),'ledger_rows':len(usage),'transport_cost':str(a),'ledger_cost':str(b),'known_transport_minus_ledger':str(a-b),'transport_missing_cost':[r for r in paid if 'cost' not in r],'reconciled_ledger_rows':[r for r in usage if r.get('reconciled')],'all_models_exact':True,'empty_at_ceiling':[r for r in paid if r.get('empty_at_ceiling')]}
print(json.dumps(result,indent=2,default=str))
