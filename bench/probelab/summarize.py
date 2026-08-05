import json
import sys
from collections import defaultdict

rows = [json.loads(l) for l in open(sys.argv[1] if len(sys.argv) > 1 else "results.jsonl")]
models = sorted({r["model"] for r in rows})
tasks = []
for r in rows:
    if r["task"] not in tasks:
        tasks.append(r["task"])

print(f"rows={len(rows)}  solved={sum(r['solved'] for r in rows)}/{len(rows)} "
      f"({100*sum(r['solved'] for r in rows)/len(rows):.1f}%)\n")

print("outcome matrix (rows=task, cols=model)")
hdr = f"{'task':<24}{'tier':<5}" + "".join(f"{m[:16]:<18}" for m in models) + "  n_solved"
print(hdr)
per_task = {}
for t in tasks:
    rs = {r["model"]: r for r in rows if r["task"] == t}
    tier = next(iter(rs.values()))["tier"]
    n = sum(1 for m in models if rs[m]["solved"])
    per_task[t] = n
    cells = "".join(f"{('PASS' if rs[m]['solved'] else 'fail'):<18}" for m in models)
    print(f"{t:<24}{tier:<5}{cells}  {n}/3")

print("\nper-model")
for m in models:
    rs = [r for r in rows if r["model"] == m]
    tr = sum(1 for r in rs if r["attempt"]["finish_reason"] == "length")
    print(f"  {m:<20} solved {sum(r['solved'] for r in rs):>2}/{len(rs)}  "
          f"truncated(length) {tr}/{len(rs)}  "
          f"probe=${sum(r['probe']['cost'] for r in rs):.4f} "
          f"attempt=${sum(r['attempt']['cost'] for r in rs):.4f}")

print("\nper-tier solve rate")
for tier in (1, 2, 3, 4):
    rs = [r for r in rows if r["tier"] == tier]
    print(f"  tier {tier}: {sum(r['solved'] for r in rs)}/{len(rs)}")
print("\nper-class solve rate")
for c in ("code", "json", "exact"):
    rs = [r for r in rows if r["cls"] == c]
    print(f"  {c:<6}: {sum(r['solved'] for r in rs)}/{len(rs)}")

print("\ndegeneracy check")
print("  tasks solved by all 3:", [t for t in tasks if per_task[t] == 3],
      f"({sum(1 for t in tasks if per_task[t]==3)}/{len(tasks)})")
print("  tasks solved by none :", [t for t in tasks if per_task[t] == 0],
      f"({sum(1 for t in tasks if per_task[t]==0)}/{len(tasks)})")
print("  discriminating tasks :", sum(1 for t in tasks if 0 < per_task[t] < 3))

print("\ntruncation detail (finish_reason=length)")
for r in rows:
    if r["attempt"]["finish_reason"] == "length":
        print(f"  {r['model']:<18}{r['task']:<24} out={r['attempt']['tokens_out']:<6}"
              f"reas={r['attempt']['reasoning_tokens']:<6}chars={r['attempt']['text_chars']:<5}"
              f"solved={r['solved']} grade={r['grade_reason']}")

print("\nfailure reasons")
d = defaultdict(int)
for r in rows:
    if not r["solved"]:
        d[r["grade_reason"]] += 1
for k, v in sorted(d.items(), key=lambda x: -x[1]):
    print(f"  {k or '(none)':<16} {v}")

print("\nsignal coverage")
sg = ["sc_agree", "sketch_jaccard", "vconf", "lp_mean_logprob"]
for m in models:
    rs = [r for r in rows if r["model"] == m]
    parts = []
    for s in sg:
        n = sum(1 for r in rs if r["signals"].get(s) is not None)
        parts.append(f"{s}={n}/{len(rs)}")
    print(f"  {m:<20} " + "  ".join(parts))
vals = [r["signals"]["vconf"] for r in rows if r["signals"]["vconf"] is not None]
print(f"\nvconf distribution: min={min(vals)} max={max(vals)} "
      f"mean={sum(vals)/len(vals):.3f} distinct={sorted(set(vals))}")
