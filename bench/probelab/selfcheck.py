"""Validate the task suite before spending money.

For every task: a reference solution must be graded PASS and a plausible-but-wrong
solution must be graded FAIL. Claimed optima and exact answers are brute-forced here,
so no ground truth in tasks.py is taken on faith.
"""
from __future__ import annotations

import itertools
import json
import sys
from fractions import Fraction

import tasks as T

FAILS = []


def expect(cond, msg):
    if not cond:
        FAILS.append(msg)
        print("  FAIL:", msg)
    return cond


def grade(tid, response):
    return T.TASK_BY_ID[tid]["check"](response)


def case(tid, good, bad):
    g = grade(tid, good)
    expect(g["ok"], f"{tid}: reference solution failed -> {g}")
    if bad is not None:
        b = grade(tid, bad)
        expect(not b["ok"], f"{tid}: wrong solution wrongly PASSED")
    print(f"  {tid:<24} ref={'PASS' if g['ok'] else 'FAIL'} " +
          (f"neg={'ok' if not grade(tid, bad)['ok'] else 'LEAK'}" if bad is not None else ""))


print("=== ground truth: exact-answer tasks ===")
# apples
apples = 20 - (24 // 3) * 2
print("  apples change =", apples)
# day of week
import datetime
dow = datetime.date(2024, 3, 1).strftime("%A")
print("  Mar 1 2024 =", dow, "| Jan 1 2024 =", datetime.date(2024, 1, 1).strftime("%A"))
# bird
bird = 120 * (450 / (60 + 90))
print("  bird km =", bird)
# committee
from math import comb
comm = sum(comb(7, w) * comb(6, 5 - w) for w in range(5 + 1) if w >= 2 and 5 - w >= 2)
print("  committees =", comm)
# modpow
mp = pow(7, 803, 1000)
print("  7^803 mod 1000 =", mp, "->", f"{mp:03d}")
# dice
tot = [r for r in itertools.product(range(1, 7), repeat=3) if 6 in r]
num = [r for r in tot if sum(r) == 15]
dice = Fraction(len(num), len(tot))
print(f"  P(sum=15 | >=1 six) = {len(num)}/{len(tot)} = {dice}")
# derangement exactly 2 fixed
def D(n):
    return 1 if n == 0 else (0 if n == 1 else (n - 1) * (D(n - 1) + D(n - 2)))
der = comb(7, 2) * D(5)
brute_der = sum(1 for p in itertools.permutations(range(7))
                if sum(1 for i, v in enumerate(p) if i == v) == 2)
print("  exactly-2-fixed =", der, "| brute =", brute_der)
# digit sum 15 and divisible by 11
dg = sum(1 for n in range(1, 100001) if sum(map(int, str(n))) == 15 and n % 11 == 0)
print("  digitsum15 & div11 =", dg)

GT = {
    "exact_apples": str(apples), "exact_dayofweek": dow.lower(),
    "exact_trains": str(int(bird)), "exact_committee": str(comm),
    "exact_modpow": f"{mp:03d}", "exact_dice": str(dice),
    "exact_derangement": str(der), "exact_digits": str(dg),
}
print("\n=== exact-answer graders (ref vs decoy) ===")
for tid, gold in GT.items():
    r = grade(tid, f"Let me work it out.\nStep one, step two.\n{gold}")
    expect(r["ok"], f"{tid}: gold {gold!r} rejected by grader (task gold may be WRONG) -> {r}")
    r2 = grade(tid, "blah blah\n999999")
    expect(not r2["ok"], f"{tid}: decoy accepted")
    print(f"  {tid:<24} gold={gold!r} ref={'PASS' if r['ok'] else 'FAIL'}")

print("\n=== brute-force check of claimed optima ===")


def ssgs_best(spec, caps):
    """Serial schedule generation over all 8! priority lists.

    SSGS over the full set of activity lists provably contains an optimal schedule
    for RCPSP, and P|prec|Cmax is the special case of one resource with capacity m.
    So the minimum over all permutations is the exact optimum.
    """
    names = list(spec)
    res = list(caps)
    best, bestplan = 10 ** 9, None
    for perm in itertools.permutations(names):
        start, end, usage, ok = {}, {}, {}, True
        for t in perm:
            if any(d not in end for d in spec[t]["deps"]):
                ok = False
                break
            s = max([end[d] for d in spec[t]["deps"]] or [0])
            dur = spec[t]["dur"]
            while not all(usage.get((u, r), 0) + spec[t][r] <= caps[r]
                          for u in range(s, s + dur) for r in res):
                s += 1
            for u in range(s, s + dur):
                for r in res:
                    usage[(u, r)] = usage.get((u, r), 0) + spec[t][r]
            start[t], end[t] = s, s + dur
        if ok:
            ms = max(end.values())
            if ms < best:
                best, bestplan = ms, dict(start)
    return best, bestplan


_spec2 = {t: dict(dur=T._J5_DUR[t], deps=T._J5[t], slot=1) for t in T._J5_DUR}
ms2, plan2 = ssgs_best(_spec2, {"slot": 2})
print(f"  json_2workers: exact optimum = {ms2}, task asserts {T._J5_OPT}")
expect(ms2 == T._J5_OPT, f"json_2workers optimum mismatch: found {ms2}, asserts {T._J5_OPT}")

_spec3 = {k: dict(dur=v["dur"], deps=v["deps"], cpu=v["cpu"], mem=v["mem"])
          for k, v in T._RCPSP.items()}
rb, rplan = ssgs_best(_spec3, {"cpu": T._CPU_CAP, "mem": T._MEM_CAP})
print(f"  json_rcpsp: exact optimum = {rb}, task requires makespan <= {T._RCPSP_LIMIT}")
expect(rb <= T._RCPSP_LIMIT, f"json_rcpsp infeasible: best {rb} > limit {T._RCPSP_LIMIT}")
expect(rb == T._RCPSP_LIMIT, f"json_rcpsp limit is loose: optimum {rb} < limit {T._RCPSP_LIMIT} (task is easier than intended)")

print("\n=== JSON graders (ref vs decoy) ===")
case("json_shopping", json.dumps({"items": [
    {"name": "a", "qty": 2, "unit_price": 10.0}, {"name": "b", "qty": 1, "unit_price": 15.0},
    {"name": "c", "qty": 3, "unit_price": 4.0}, {"name": "d", "qty": 1, "unit_price": 3.0}],
    "total": 50.0}),
    json.dumps({"items": [{"name": "a", "qty": 1, "unit_price": 1.0}] * 4, "total": 4.0}))

case("json_topo", json.dumps({"order": ["checkout", "install", "build", "test", "deploy"]}),
     json.dumps({"order": ["checkout", "install", "test", "build", "deploy"]}))

case("json_makespan", json.dumps({"schedule": [
    {"task": "a", "start": 0, "end": 2}, {"task": "b", "start": 2, "end": 5},
    {"task": "c", "start": 2, "end": 3}, {"task": "d", "start": 5, "end": 9},
    {"task": "e", "start": 3, "end": 5}, {"task": "f", "start": 9, "end": 12}],
    "makespan": 12}),
    json.dumps({"schedule": [
        {"task": "a", "start": 0, "end": 2}, {"task": "b", "start": 2, "end": 5},
        {"task": "c", "start": 5, "end": 6}, {"task": "d", "start": 6, "end": 10},
        {"task": "e", "start": 6, "end": 8}, {"task": "f", "start": 10, "end": 13}],
        "makespan": 13}))


def solve_menu():
    F = T._MENU_FOODS
    gf = [k for k, v in F.items() if v["gf"]]
    for combo in itertools.permutations(gf, 6):
        days = [combo[0:2], combo[2:4], combo[4:6]]
        if not all(700 <= sum(F[m]["kcal"] for m in d) <= 850 for d in days):
            continue
        if sum(1 for m in combo if F[m]["veg"]) < 3:
            continue
        return {"days": [{"day": i + 1, "meals": list(d),
                          "total_kcal": sum(F[m]["kcal"] for m in d)} for i, d in enumerate(days)]}
    return None


menu = solve_menu()
expect(menu is not None, "json_menu has NO feasible solution")
case("json_menu", json.dumps(menu),
     json.dumps({"days": [{"day": i + 1, "meals": ["quinoa_salad", "lentil_stew"],
                           "total_kcal": 650} for i in range(3)]}))


def solve_2workers():
    """Take the optimal start times from SSGS and 2-colour the intervals by start time
    (greedy colouring of an interval graph with max overlap 2 needs exactly 2 colours)."""
    dur = T._J5_DUR
    order = sorted(plan2, key=lambda t: plan2[t])
    free = [0, 0]
    wass = {}
    for t in order:
        s = plan2[t]
        w = next((i for i in (0, 1) if free[i] <= s), None)
        if w is None:
            return None
        wass[t] = w + 1
        free[w] = s + dur[t]
    return {"assignments": [{"task": t, "worker": wass[t], "start": plan2[t],
                             "end": plan2[t] + dur[t]} for t in dur], "makespan": ms2}


s2 = solve_2workers()
expect(s2 is not None, "json_2workers: could not construct an optimal reference schedule")
case("json_2workers", json.dumps(s2),
     json.dumps({"assignments": [{"task": t, "worker": 1, "start": 0, "end": T._J5_DUR[t]}
                                 for t in T._J5_DUR], "makespan": 5}))


def solve_courses(max_sem=5, max_per=2):
    C = T._COURSES
    order, seen = [], set()

    def vis(c):
        if c in seen:
            return
        seen.add(c)
        for p in C[c]:
            vis(p)
        order.append(c)

    for c in C:
        vis(c)
    out = []

    def bt(i, pl):
        if out:
            return
        if i == len(order):
            used = sorted({v for v in pl.values()})
            if used == list(range(1, len(used) + 1)):
                out.append(dict(pl))
            return
        c = order[i]
        lo = max([pl[p] + 1 for p in C[c] if p in pl] or [1])
        for s in range(lo, max_sem + 1):
            if sum(1 for v in pl.values() if v == s) >= max_per:
                continue
            pl[c] = s
            bt(i + 1, pl)
            del pl[c]
            if out:
                return

    bt(0, {})
    if not out:
        return None
    pl = out[0]
    n = max(pl.values())
    return {"semesters": [{"semester": s, "courses": [c for c in C if pl[c] == s]}
                          for s in range(1, n + 1)]}


sc = solve_courses()
expect(sc is not None, "json_courses has NO feasible solution")
case("json_courses", json.dumps(sc),
     json.dumps({"semesters": [{"semester": 1, "courses": list(T._COURSES)[:3]}]}))

case("json_rcpsp", json.dumps({"plan": [{"id": k, "start": v, "end": v + T._RCPSP[k]["dur"]}
                                        for k, v in rplan.items()], "makespan": rb}),
     json.dumps({"plan": [{"id": k, "start": 0, "end": T._RCPSP[k]["dur"]}
                          for k in T._RCPSP], "makespan": 4}))


def solve_deploy():
    R, S = T._REGIONS, T._SERVICES
    for regs in itertools.combinations(R, 3):
        # cheapest two regions get 2 and 1 replicas of every service
        rs = sorted(regs, key=lambda r: R[r]["cost"])
        plan = {rs[0]: {s: 2 for s in S}, rs[1]: {s: 1 for s in S}, rs[2]: {}}
        # need 3rd region non-empty -> move one replica there
        plan[rs[2]] = {"db": 1}
        plan[rs[0]]["db"] = 2
        cost = sum(n * R[r]["cost"] for r, d in plan.items() for n in d.values())
        cap_ok = all(sum(d.values()) <= R[r]["cap"] for r, d in plan.items())
        if cost <= 260 and cap_ok:
            return {"regions": [{"name": r, "services": [{"name": s, "replicas": n}
                                                         for s, n in plan[r].items()]} for r in regs],
                    "service_graph": {k: list(v) for k, v in S.items()},
                    "total_cost": cost}
    return None


dp = solve_deploy()
expect(dp is not None, "json_deploy has NO feasible solution under budget")
if dp:
    print("   json_deploy reference cost =", dp["total_cost"])
case("json_deploy", json.dumps(dp),
     json.dumps({"regions": [{"name": "us-east", "services": [{"name": s, "replicas": 3}
                                                              for s in T._SERVICES]}],
                 "service_graph": {k: list(v) for k, v in T._SERVICES.items()},
                 "total_cost": 150}))

print("\n=== code graders (ref vs decoy) ===")
REFS = {
    "code_fizzbuzz": '''
def fizzbuzz(n):
    out = []
    for i in range(1, max(n, 0) + 1):
        s = ("Fizz" if i % 3 == 0 else "") + ("Buzz" if i % 5 == 0 else "")
        out.append(s or str(i))
    return out
''',
    "code_wordfreq": '''
import re
def word_freq(text):
    d = {}
    for w in re.findall(r"[A-Za-z]+", text or ""):
        w = w.lower()
        d[w] = d.get(w, 0) + 1
    return d
''',
    "code_roman_strict": '''
import re
_V = {"I":1,"V":5,"X":10,"L":50,"C":100,"D":500,"M":1000}
def _to_roman(n):
    vals = [(1000,"M"),(900,"CM"),(500,"D"),(400,"CD"),(100,"C"),(90,"XC"),
            (50,"L"),(40,"XL"),(10,"X"),(9,"IX"),(5,"V"),(4,"IV"),(1,"I")]
    out = []
    for v, s in vals:
        while n >= v:
            out.append(s); n -= v
    return "".join(out)
def roman_to_int(s):
    if not isinstance(s, str) or not s or any(c not in _V for c in s):
        return None
    total, i = 0, 0
    while i < len(s):
        if i + 1 < len(s) and _V[s[i]] < _V[s[i+1]]:
            total += _V[s[i+1]] - _V[s[i]]; i += 2
        else:
            total += _V[s[i]]; i += 1
    if not (1 <= total <= 3999):
        return None
    return total if _to_roman(total) == s else None
''',
    "code_intervals_halfopen": '''
def merge_intervals(intervals):
    iv = sorted([list(x) for x in intervals if x[0] < x[1]])
    out = []
    for a, b in iv:
        if out and a < out[-1][1]:
            out[-1][1] = max(out[-1][1], b)
        else:
            out.append([a, b])
    return out
''',
    "code_expr_twist": '''
import re
def eval_expr(s):
    toks = re.findall(r"\\d+|[-+*/^()]|\\S", (s or "").replace(" ", ""))
    if any(not (t.isdigit() or t in "+-*/^()") for t in toks):
        return None
    pos = 0
    class Bad(Exception): pass
    def peek():
        return toks[pos] if pos < len(toks) else None
    def eat(t=None):
        nonlocal pos
        if pos >= len(toks) or (t and toks[pos] != t): raise Bad()
        pos += 1; return toks[pos-1]
    def add():
        v = pw()
        while peek() in ("+", "-"):
            op = eat()
            r = pw()
            v = v + r if op == "+" else v - r
        return v
    def pw():
        v = mul()
        if peek() == "^":
            eat()
            e = pw()
            if e < 0: raise Bad()
            return v ** e
        return v
    def mul():
        v = un()
        while peek() in ("*", "/"):
            op = eat(); r = un()
            if op == "*": v = v * r
            else:
                if r == 0: raise Bad()
                v = v // r
        return v
    def un():
        if peek() == "-":
            eat(); return -un()
        return atom()
    def atom():
        t = peek()
        if t == "(":
            eat(); v = add(); eat(")"); return v
        if t is not None and t.isdigit():
            eat(); return int(t)
        raise Bad()
    try:
        v = add()
    except Exception:
        return None
    return v if pos == len(toks) else None
''',
    "code_kflips": '''
def longest_uniform_run(bits, k):
    best = 0
    for v in (0, 1):
        i = used = 0
        for j, b in enumerate(bits):
            if b != v: used += 1
            while used > k:
                if bits[i] != v: used -= 1
                i += 1
            if j - i + 1 > best: best = j - i + 1
    return best
''',
    "code_repeating_decimal": '''
def frac_to_string(num, den):
    if den == 0: return None
    neg = (num < 0) != (den < 0) and num != 0
    n, d = abs(num), abs(den)
    ip, r = divmod(n, d)
    sign = "-" if neg else ""
    if r == 0: return sign + str(ip)
    seen, digits, i = {}, [], 0
    while r and r not in seen:
        seen[r] = i
        r *= 10
        digits.append(str(r // d)); r %= d; i += 1
    if r == 0:
        return sign + str(ip) + "." + "".join(digits)
    s = seen[r]
    return sign + str(ip) + "." + "".join(digits[:s]) + "(" + "".join(digits[s:]) + ")"
''',
    "code_cryptarithm": '''
from itertools import permutations
def solve_cryptarithm(puzzle):
    try:
        left, right = puzzle.split("=")
    except ValueError:
        return None
    words = [w.strip() for w in left.split("+")] + [right.strip()]
    if any((not w) or (not w.isalpha()) or (not w.isupper()) for w in words):
        return None
    letters = sorted(set("".join(words)))
    if len(letters) > 10: return None
    lead = {w[0] for w in words if len(w) > 1}
    coef = {c: 0 for c in letters}
    for w in words[:-1]:
        for i, c in enumerate(reversed(w)): coef[c] += 10 ** i
    for i, c in enumerate(reversed(words[-1])): coef[c] -= 10 ** i
    sols = []
    for perm in permutations(range(10), len(letters)):
        m = dict(zip(letters, perm))
        if any(m[c] == 0 for c in lead): continue
        if sum(coef[c] * m[c] for c in letters) == 0:
            sols.append(m)
            if len(sols) > 1: return None
    return sols[0] if len(sols) == 1 else None
''',
}
DECOYS = {
    "code_fizzbuzz": "def fizzbuzz(n):\n    return [str(i) for i in range(1, n+1)]",
    "code_wordfreq": "def word_freq(text):\n    return {w: 1 for w in text.split()}",
    "code_roman_strict": "def roman_to_int(s):\n    return len(s)",
    "code_intervals_halfopen": ("def merge_intervals(intervals):\n"
                                "    iv = sorted(intervals)\n    out=[]\n"
                                "    for a,b in iv:\n"
                                "        if out and a <= out[-1][1]: out[-1][1]=max(out[-1][1],b)\n"
                                "        else: out.append([a,b])\n    return out"),
    "code_expr_twist": "def eval_expr(s):\n    try:\n        return eval(s.replace('^','**'))\n    except Exception:\n        return None",
    "code_kflips": "def longest_uniform_run(bits, k):\n    return len(bits)",
    "code_repeating_decimal": "def frac_to_string(num, den):\n    return str(num/den)",
    "code_cryptarithm": "def solve_cryptarithm(puzzle):\n    return None",
}
for tid, ref in REFS.items():
    case(tid, "```python\n" + ref + "\n```", "```python\n" + DECOYS[tid] + "\n```")

print("\n=== features ===")
for t in T.TASKS[:2]:
    print(" ", t["id"], T.features(t))

print("\n" + "=" * 60)
if FAILS:
    print(f"{len(FAILS)} PROBLEM(S):")
    for f in FAILS:
        print(" -", f)
    sys.exit(1)
print("ALL SELF-CHECKS PASSED")
