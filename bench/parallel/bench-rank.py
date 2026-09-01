#!/usr/bin/env python3
"""Merge the parallel cells' results.csv and rank v3 `do` against master's recorded node-mode rows."""
import csv, glob, os, sys

root = open('/home/santosh/af-v3/.bench-par-root').read().strip()

# Master's recorded numbers, BENCHMARKS.md, AFORGE_MODE=node, same model+judge.
MASTER = {
    '20': dict(sec=85,  passed=None, cost=0.005, note='workflow added'),
    '21': dict(sec=356, passed=554,  cost=0.185, note='529-554 range; 554 = no contract'),
    '22': dict(sec=589, passed=558,  cost=0.205, note='~$0.17-0.24'),
    '23': dict(sec=112, passed=321,  cost=0.014, note=''),
}
PI = {'20': (180, None), '21': (735, 548), '22': (656, 580), '23': (157, 324)}

rows = []
for f in sorted(glob.glob(os.path.join(root, 'issue-*/results.csv'))):
    with open(f) as fh:
        for r in csv.DictReader(fh):
            if r.get('harness') == 'aforge':
                rows.append(r)

if not rows:
    print('no rows yet'); sys.exit(0)

def num(v, cast=float):
    try: return cast(v)
    except Exception: return None

def mmss(s):
    if s is None: return 'n/a'
    return f'{int(s)//60}m{int(s)%60:02d}s'

print(f'ROOT: {root}\n')
print('## v3 `aforge do` — raw cells\n')
hdr = f"{'issue':<6}{'exit':<6}{'files':<7}{'pass':<7}{'fail':<6}{'time':<9}{'cost':<10}{'nodes_failed'}"
print(hdr); print('-'*len(hdr))
tot_cost = 0.0; tot_sec = 0
for r in sorted(rows, key=lambda x: x['issue']):
    c = num(r.get('cost_usd')); s = num(r.get('seconds'), float)
    if c: tot_cost += c
    if s: tot_sec = max(tot_sec, int(s))
    print(f"{'#'+r['issue']:<6}{r['exit']:<6}{r['changed_files']:<7}{r['passed']:<7}{r['failed']:<6}"
          f"{mmss(s):<9}{('$%.3f'%c) if c else 'n/a':<10}{r.get('nodes_failed','?')}")
print(f"\ntotal cost ${tot_cost:.3f} | wall clock (parallel, slowest cell) {mmss(tot_sec)}")

print('\n## v3 `do` vs master `node` vs pi\n')
h2 = f"{'issue':<7}{'v3 pass':<10}{'master':<10}{'pi':<8}{'v3 time':<10}{'master':<10}{'v3 cost':<10}{'master':<10}{'verdict'}"
print(h2); print('-'*len(h2))
for r in sorted(rows, key=lambda x: x['issue']):
    i = r['issue']; m = MASTER[i]; p = PI[i]
    vp = num(r['passed'], int); vs = num(r['seconds'], float); vc = num(r.get('cost_usd'))
    if vp is None or m['passed'] is None:
        verdict = 'no test judge (#20)'
    elif vp > m['passed']: verdict = f"v3 BETTER +{vp-m['passed']}"
    elif vp < m['passed']: verdict = f"v3 worse {vp-m['passed']}"
    else: verdict = 'tie'
    if vp is not None and p[1] and vp > p[1]: verdict += ' | beats pi'
    print(f"{'#'+i:<7}{str(vp):<10}{str(m['passed']):<10}{str(p[1]):<8}{mmss(vs):<10}{mmss(m['sec']):<10}"
          f"{('$%.3f'%vc) if vc else 'n/a':<10}{'$%.3f'%m['cost']:<10}{verdict}")

print('\n## ranking (per axis, v3 do vs master node)\n')
qual = sum(1 for r in rows if MASTER[r['issue']]['passed'] and num(r['passed'],int) and num(r['passed'],int) >= MASTER[r['issue']]['passed'])
scored = sum(1 for r in rows if MASTER[r['issue']]['passed'])
faster = sum(1 for r in rows if num(r['seconds'],float) and num(r['seconds'],float) < MASTER[r['issue']]['sec'])
cheaper = sum(1 for r in rows if num(r.get('cost_usd')) and num(r.get('cost_usd')) < MASTER[r['issue']]['cost'])
mtot = sum(MASTER[r['issue']]['cost'] for r in rows)
print(f'quality  : v3 >= master on {qual}/{scored} scored issues')
print(f'time     : v3 faster on {faster}/{len(rows)} cells (per-cell, parallel run)')
print(f'cost     : v3 cheaper on {cheaper}/{len(rows)} cells | total v3 ${tot_cost:.3f} vs master ${mtot:.3f}')

print('\n## model audit — every model id recorded in each cell store\n')
import subprocess, re
for i in ['20','21','22','23']:
    db = os.path.join(root, f'issue-{i}/aforge-{i}/store/graph.db')
    if not os.path.exists(db):
        print(f'#{i}: no store'); continue
    out = subprocess.run(['strings', db], capture_output=True, text=True).stdout
    ids = set(re.findall(r'(?:deepseek|qwen|nex-agi|anthropic|openai|moonshotai|z-ai|google|x-ai|mistralai)/[a-zA-Z0-9._:-]+', out))
    ids = {x.rstrip('WX') for x in ids}
    ok = ids <= {'deepseek/deepseek-v4-flash-0731'}
    print(f"#{i}: {'CLEAN' if ok else 'MIXED'} — {sorted(ids)}")
