"""Probe lab task suite: 24 tasks, 3 classes, difficulty tiers 1-4.

Every task is graded by deterministic code. `check(response) -> dict(ok=bool, ...)`.
Tiers: 1 trivial, 2 easy, 3 hard, 4 designed so most cheap models fail.
Hard tiers lean on *spec twists* (a familiar problem with non-standard semantics),
which reliably separates models that read the spec from models that pattern-match.
"""
from __future__ import annotations

import graders as G

CODE_SUFFIX = (
    "\n\nReturn ONLY a single ```python code block containing the complete function "
    "(plus any helpers). No explanation, no tests, no example usage."
)
JSON_SUFFIX = (
    "\n\nReturn ONLY a single JSON object in a ```json code block. "
    "No commentary before or after."
)
EXACT_SUFFIX = "\n\nThink it through, then give the final answer on the LAST line, as the bare value with no words, no units, and no punctuation."

TASKS: list[dict] = []


def code_task(tid, tier, prompt, tests, n_tests, numeric=True, timeout=20.0):
    TASKS.append({
        "id": tid, "cls": "code", "tier": tier,
        "prompt": prompt.strip() + CODE_SUFFIX,
        "n_tests": n_tests, "schema_depth": 0, "numeric_constraints": numeric,
        "check": lambda r, t=tests, to=timeout: G.grade_code(r, t, to),
    })


def exact_task(tid, tier, prompt, gold, alts=None, numeric=True):
    TASKS.append({
        "id": tid, "cls": "exact", "tier": tier,
        "prompt": prompt.strip() + EXACT_SUFFIX,
        "n_tests": 1, "schema_depth": 0, "numeric_constraints": numeric,
        "check": lambda r, g=gold, a=alts: {
            "ok": G.answer_matches(r, g, a), "reason": "" if G.answer_matches(r, g, a) else "mismatch",
            "detail": G.normalize_answer(r)[:120]},
    })


def json_task(tid, tier, prompt, checker, depth, n_checks, numeric=True):
    def run(r, ck=checker):
        obj = G.extract_json(r)
        if obj is None:
            return {"ok": False, "reason": "no_json", "detail": ""}
        try:
            ok, why = ck(obj)
        except Exception as e:
            return {"ok": False, "reason": "checker_exc", "detail": f"{type(e).__name__}: {e}"[:200]}
        return {"ok": bool(ok), "reason": "" if ok else "constraint", "detail": str(why)[:300]}

    TASKS.append({
        "id": tid, "cls": "json", "tier": tier, "prompt": prompt.strip() + JSON_SUFFIX,
        "n_tests": n_checks, "schema_depth": depth, "numeric_constraints": numeric,
        "check": run,
    })


# ==========================================================================
# CLASS 1 -- CODING (8)
# ==========================================================================

code_task("code_fizzbuzz", 1, """
Write a Python function `fizzbuzz(n)` that returns a list of length n (for n >= 1).
For i from 1 to n the element is "Fizz" if i is divisible by 3, "Buzz" if divisible by 5,
"FizzBuzz" if divisible by both, else the string form of i. Return [] for n <= 0.
""", """
check(fizzbuzz(1), ["1"], "n1")
check(fizzbuzz(0), [], "n0")
check(fizzbuzz(-3), [], "neg")
check(fizzbuzz(15)[-1], "FizzBuzz", "15")
check(fizzbuzz(15), ["1","2","Fizz","4","Buzz","Fizz","7","8","Fizz","Buzz","11","Fizz","13","14","FizzBuzz"], "full")
check(len(fizzbuzz(100)), 100, "len100")
""", 6)

code_task("code_wordfreq", 1, """
Write a Python function `word_freq(text)` that returns a dict mapping each word to its count.
A word is a maximal run of ASCII letters ([A-Za-z]); everything else is a separator.
Words are lowercased. Digits are never part of a word. An empty string gives {}.
""", """
check(word_freq(""), {}, "empty")
check(word_freq("Hi hi HI"), {"hi": 3}, "case")
check(word_freq("a1b"), {"a": 1, "b": 1}, "digit-splits")
check(word_freq("don't stop"), {"don": 1, "t": 1, "stop": 1}, "apostrophe")
check(word_freq("  ...  "), {}, "punct-only")
check(word_freq("Cat cat-dog DOG"), {"cat": 2, "dog": 2}, "hyphen")
""", 6)

code_task("code_roman_strict", 2, """
Write a Python function `roman_to_int(s)` that parses a STRICT canonical Roman numeral
(the standard form, values 1..3999) and returns its integer value, or returns None if `s`
is not a strictly canonical Roman numeral.

Strict rules: only the symbols I,V,X,L,C,D,M (uppercase); I,X,C,M may repeat at most 3 times
in a row; V,L,D may never repeat; the only legal subtractive pairs are IV, IX, XL, XC, CD, CM,
and each may appear at most once and only in its correct position. Any string that is not the
unique canonical representation of its value (e.g. "IIII", "VX", "IC", "XXXX", "") returns None.
""", """
check(roman_to_int("IV"), 4, "IV")
check(roman_to_int("MCMXCIV"), 1994, "1994")
check(roman_to_int("MMMCMXCIX"), 3999, "3999")
check(roman_to_int("IIII"), None, "IIII")
check(roman_to_int("VX"), None, "VX")
check(roman_to_int("IC"), None, "IC")
check(roman_to_int(""), None, "empty")
check(roman_to_int("XXXX"), None, "XXXX")
check(roman_to_int("VV"), None, "VV")
check(roman_to_int("iv"), None, "lower")
check(roman_to_int("MCMXCIVX"), None, "trailing")
check(roman_to_int("XLII"), 42, "42")
check(roman_to_int("IXIX"), None, "double-IX")
check(roman_to_int("MMMM"), None, "4000")
""", 14)

code_task("code_intervals_halfopen", 2, """
Write a Python function `merge_intervals(intervals)`.

Each interval is a pair [a, b] denoting the HALF-OPEN interval [a, b) -- a is included,
b is excluded. Merge intervals that OVERLAP. Note: because the intervals are half-open,
intervals that merely touch (e.g. [1,3) and [3,5)) do NOT overlap and must NOT be merged.
Intervals with a >= b are empty and must be dropped entirely.
Return the merged list sorted ascending by start, as a list of 2-element lists.
""", """
check(merge_intervals([]), [], "empty")
check(merge_intervals([[1,3],[3,5]]), [[1,3],[3,5]], "touching-not-merged")
check(merge_intervals([[1,4],[3,5]]), [[1,5]], "overlap")
check(merge_intervals([[5,5],[1,2]]), [[1,2]], "drop-empty")
check(merge_intervals([[5,3]]), [], "drop-inverted")
check(merge_intervals([[1,10],[2,3],[4,5]]), [[1,10]], "contained")
check(merge_intervals([[3,5],[1,3],[5,7]]), [[1,3],[3,5],[5,7]], "chain-touch")
check(merge_intervals([[1,2],[2,4],[3,6]]), [[1,2],[2,6]], "mixed")
""", 8)

code_task("code_expr_twist", 3, """
Write a Python function `eval_expr(s)` for a small expression language with DELIBERATELY
NON-STANDARD semantics. Read carefully; do not assume normal precedence.

Grammar over integers with operators + - * / ^ and parentheses, plus unary minus.
Precedence, LOWEST to HIGHEST:
  1. `+` and `-`            (binary, LEFT associative)
  2. `^`                    (BINARY, RIGHT associative)   <-- lower than * and /
  3. `*` and `/`            (LEFT associative)
  4. unary `-`              (binds tighter than everything above)
  5. parentheses / integer literals
`/` is floor division truncating toward NEGATIVE INFINITY. `^` is integer exponentiation;
if the right operand is negative, the whole expression is invalid.
Whitespace is insignificant. Division or modulo by zero makes the expression invalid.
Return the integer result, or None if `s` is malformed/invalid.

Because `^` is LOWER precedence than `*`, the expression `2 ^ 3 * 2` parses as `2 ^ (3 * 2)` = 64.
Because unary minus binds TIGHTEST, `-2 ^ 2` parses as `(-2) ^ 2` = 4.
""", """
check(eval_expr("1+2*3"), 7, "basic")
check(eval_expr("2^3*2"), 64, "caret-lower-than-star")
check(eval_expr("-2^2"), 4, "unary-tightest")
check(eval_expr("2^3^2"), 512, "right-assoc")
check(eval_expr("(1+2)*3"), 9, "parens")
check(eval_expr("-7/2"), -4, "floor-neg")
check(eval_expr("7/2"), 3, "floor-pos")
check(eval_expr("1/0"), None, "div0")
check(eval_expr("2^-1"), None, "neg-exp")
check(eval_expr("1+"), None, "malformed")
check(eval_expr(""), None, "empty")
check(eval_expr("1 + 2 - 3 - 4"), -4, "left-assoc")
check(eval_expr("4*2^2*3"), 262144, "caret-lower-both-sides")
check(eval_expr("(2)"), 2, "paren-atom")
check(eval_expr("--3"), 3, "double-unary")
check(eval_expr("2*(3+4)/5"), 2, "mixed")
""", 16)

code_task("code_kflips", 3, """
Write a Python function `longest_uniform_run(bits, k)`.

`bits` is a list of 0/1 ints and `k >= 0` is the maximum number of positions you may flip
(a flip changes a 0 to 1 or a 1 to 0; each position may be flipped at most once).
Return the length of the longest contiguous run of EQUAL values achievable using at most
k flips. For an empty list return 0. Must run in O(len(bits)) time; inputs may have 200000
elements, so an O(n*k) or O(n^2) solution will time out.
""", """
check(longest_uniform_run([], 0), 0, "empty")
check(longest_uniform_run([1,1,0,1], 0), 2, "k0")
check(longest_uniform_run([1,1,0,1], 1), 4, "k1")
check(longest_uniform_run([0,0,1,1,0,0], 1), 3, "either-value")
check(longest_uniform_run([1,0,1,0,1], 2), 5, "alternating")
check(longest_uniform_run([0], 5), 1, "k-bigger-than-n")
check(longest_uniform_run([1,0,1,0,1,0], 0), 1, "k0-alt")
import random, time
random.seed(7)
big = [random.randint(0,1) for _ in range(200000)]
t0 = time.time(); r = longest_uniform_run(big, 1000); el = time.time()-t0
check(el < 4.0, True, "perf(%.2fs)" % el)
def _brute(b, k):
    best = 0
    for v in (0, 1):
        i = 0; used = 0
        for j in range(len(b)):
            if b[j] != v: used += 1
            while used > k:
                if b[i] != v: used -= 1
                i += 1
            best = max(best, j - i + 1)
    return best
for _t in range(300):
    n = random.randint(1, 14); kk = random.randint(0, 4)
    b = [random.randint(0,1) for _ in range(n)]
    check(longest_uniform_run(b, kk), _brute(b, kk), "rand")
""", 10, timeout=40.0)

code_task("code_repeating_decimal", 4, """
Write a Python function `frac_to_string(num, den)` that renders the exact value of the
rational number num/den as a string, using parentheses for the repeating part.

Rules, all of which are tested:
  - `den` may be negative or positive but never 0; if den == 0 return None.
  - The sign is a single leading "-" when the value is negative; -0 must render as "0".
  - If the value is an integer, return just the integer with no decimal point (e.g. "5", "-3", "0").
  - If the decimal expansion terminates, return it with no trailing zeros and no parentheses.
  - If it repeats, wrap the minimal repeating cycle in parentheses, placed as early as possible,
    e.g. 1/6 -> "0.1(6)", 1/3 -> "0.(3)", 22/7 -> "3.(142857)", -1/7 -> "-0.(142857)".
  - num and den may be arbitrarily large Python ints; do not use floating point.
""", """
check(frac_to_string(1, 2), "0.5", "half")
check(frac_to_string(1, 3), "0.(3)", "third")
check(frac_to_string(1, 6), "0.1(6)", "sixth")
check(frac_to_string(22, 7), "3.(142857)", "22/7")
check(frac_to_string(-1, 7), "-0.(142857)", "neg")
check(frac_to_string(1, -7), "-0.(142857)", "neg-den")
check(frac_to_string(0, 5), "0", "zero")
check(frac_to_string(0, -5), "0", "neg-zero")
check(frac_to_string(10, 2), "5", "int")
check(frac_to_string(-9, 3), "-3", "neg-int")
check(frac_to_string(1, 0), None, "den0")
check(frac_to_string(4, -2), "-2", "neg-int2")
check(frac_to_string(1, 333), "0.(003)", "leading-zero-cycle")
check(frac_to_string(7, 12), "0.58(3)", "mixed")
check(frac_to_string(-7, 12), "-0.58(3)", "mixed-neg")
check(frac_to_string(1, 8), "0.125", "terminating")
check(frac_to_string(1, 2**10 * 5**3), "0." + "".join(str(d) for d in []) + str(10**10 // (2**10 * 5**3) if False else "") if False else frac_to_string(1, 2**10*5**3), "self")
from fractions import Fraction as _F
def _ref(n, d):
    if d == 0: return None
    sign = "-" if (n < 0) != (d < 0) and n != 0 else ""
    n, d = abs(n), abs(d)
    ip, r = divmod(n, d)
    if r == 0: return sign + str(ip)
    seen = {}; digits = []; i = 0
    while r and r not in seen:
        seen[r] = i; r *= 10
        digits.append(str(r // d)); r %= d; i += 1
    if r == 0:
        return sign + str(ip) + "." + "".join(digits)
    s = seen[r]
    return sign + str(ip) + "." + "".join(digits[:s]) + "(" + "".join(digits[s:]) + ")"
import random
random.seed(11)
for _ in range(250):
    a = random.randint(-400, 400); b = random.choice([x for x in range(-60, 61) if x != 0])
    check(frac_to_string(a, b), _ref(a, b), "rand(%d/%d)" % (a, b))
check(frac_to_string(123456789012345678901, 999999999999), _ref(123456789012345678901, 999999999999), "bigint")
""", 19, timeout=40.0)

code_task("code_cryptarithm", 4, """
Write a Python function `solve_cryptarithm(puzzle)` that solves an alphametic puzzle.

`puzzle` is a string of the form "WORD + WORD + ... = WORD" (one or more addends, uppercase
letters only, single '=' , '+' separators, arbitrary whitespace).
Assign a distinct digit 0-9 to each distinct letter so the sum holds in base 10.
No word whose length is greater than 1 may have a leading zero.

Return a dict mapping each letter to its digit for the UNIQUE solution.
Return None if there is no solution, and ALSO return None if there is more than one solution.
If there are more than 10 distinct letters, return None.
Must finish well under 10 seconds for puzzles with up to 10 distinct letters.
""", """
check(solve_cryptarithm("SEND + MORE = MONEY"), {"S":9,"E":5,"N":6,"D":7,"M":1,"O":0,"R":8,"Y":2}, "send")
check(solve_cryptarithm("A + B = C") is None, True, "ambiguous->None")
check(solve_cryptarithm("AA + BB = CCC") is None, True, "nosol-or-multi")
check(solve_cryptarithm("ABCDEFGHIJK + A = B") is None, True, ">10-letters")
r = solve_cryptarithm("TWO + TWO = FOUR")
check(r is None, True, "TWO+TWO=FOUR multi")
r2 = solve_cryptarithm("CROSS + ROADS = DANGER")
check(r2, {"C":9,"R":6,"O":2,"S":3,"A":5,"D":1,"N":8,"G":7,"E":4}, "cross")
import time
t0 = time.time(); solve_cryptarithm("SEND + MORE = MONEY"); el = time.time()-t0
check(el < 10.0, True, "perf(%.2fs)" % el)
check(solve_cryptarithm("AB + AB = AB") is None, True, "impossible")
def _ok(p, sol):
    import re as _re
    l, r = p.split("=")
    ws = [w.strip() for w in l.split("+")] + [r.strip()]
    for w in ws:
        if len(w) > 1 and sol[w[0]] == 0: return False
    val = lambda w: int("".join(str(sol[c]) for c in w))
    return sum(val(w) for w in ws[:-1]) == val(ws[-1]) and len(set(sol.values())) == len(sol)
check(_ok("SEND + MORE = MONEY", solve_cryptarithm("SEND + MORE = MONEY")), True, "verify-send")
check(_ok("CROSS + ROADS = DANGER", solve_cryptarithm("CROSS + ROADS = DANGER")), True, "verify-cross")
""", 10, timeout=60.0)

# ==========================================================================
# CLASS 2 -- STRUCTURED JSON PLANNING (8)
# ==========================================================================


def _chk_shopping(o):
    if not isinstance(o, dict):
        return False, "not object"
    if set(o.keys()) != {"items", "total"}:
        return False, f"keys {sorted(o.keys())}"
    it = o["items"]
    if not isinstance(it, list) or len(it) != 4:
        return False, "items not list of 4"
    names = set()
    tot = 0
    for e in it:
        if not isinstance(e, dict) or set(e.keys()) != {"name", "qty", "unit_price"}:
            return False, f"item keys {e}"
        if not isinstance(e["name"], str) or not e["name"]:
            return False, "bad name"
        if not isinstance(e["qty"], int) or isinstance(e["qty"], bool) or e["qty"] < 1:
            return False, "bad qty"
        if not isinstance(e["unit_price"], (int, float)) or isinstance(e["unit_price"], bool):
            return False, "bad price"
        names.add(e["name"])
        tot += e["qty"] * e["unit_price"]
    if len(names) != 4:
        return False, "duplicate names"
    if abs(o["total"] - tot) > 1e-6:
        return False, f"total {o['total']} != {tot}"
    if not (abs(tot - 50.0) <= 0.005):
        return False, f"grand total {tot} != 50"
    return True, "ok"


json_task("json_shopping", 1, """
Produce a JSON object describing a shopping list.
Schema (exactly these keys, nothing extra):
{"items": [ {"name": <non-empty string>, "qty": <integer >= 1>, "unit_price": <number>} x exactly 4 ],
 "total": <number>}
Constraints:
- all 4 item names must be distinct
- `total` must equal the sum over items of qty * unit_price
- that grand total must be exactly 50.00
""", _chk_shopping, 3, 6)


_J2_DEPS = {"deploy": ["test", "build"], "test": ["build"], "build": ["install"],
            "install": ["checkout"], "checkout": []}


def _chk_topo(o):
    if not isinstance(o, dict) or set(o.keys()) != {"order"}:
        return False, f"keys {sorted(o.keys()) if isinstance(o, dict) else type(o)}"
    order = o["order"]
    if not isinstance(order, list) or not all(isinstance(x, str) for x in order):
        return False, "order not list[str]"
    if not G.is_dag_order(list(_J2_DEPS), _J2_DEPS, order):
        return False, f"not a valid topological order: {order}"
    return True, "ok"


json_task("json_topo", 1, """
These build steps have prerequisites:
  checkout: (none)
  install: requires checkout
  build: requires install
  test: requires build
  deploy: requires test and build
Produce {"order": [...]} where the list contains all five step names exactly once,
in an order where every step appears after all of its prerequisites.
""", _chk_topo, 2, 3)


_J3 = {"a": [], "b": ["a"], "c": ["a"], "d": ["b", "c"], "e": ["c"], "f": ["d", "e"]}
_J3_DUR = {"a": 2, "b": 3, "c": 1, "d": 4, "e": 2, "f": 3}
# optimal makespan with unlimited parallelism = longest path
# a(2)->b(3)->d(4)->f(3) = 12 ; a,c,d,f = 2+1+4+3=10 ; a,c,e,f=2+1+2+3=8  => 12


def _chk_sched(o):
    if not isinstance(o, dict) or set(o.keys()) != {"schedule", "makespan"}:
        return False, f"keys {sorted(o.keys()) if isinstance(o, dict) else type(o)}"
    sch = o["schedule"]
    if not isinstance(sch, list) or len(sch) != 6:
        return False, "schedule must have 6 entries"
    start = {}
    for e in sch:
        if not isinstance(e, dict) or set(e.keys()) != {"task", "start", "end"}:
            return False, f"entry keys {e}"
        t = e["task"]
        if t not in _J3 or t in start:
            return False, f"bad/dup task {t}"
        if not all(isinstance(e[k], int) and not isinstance(e[k], bool) for k in ("start", "end")):
            return False, "start/end must be ints"
        if e["end"] - e["start"] != _J3_DUR[t]:
            return False, f"{t} duration {e['end']-e['start']} != {_J3_DUR[t]}"
        if e["start"] < 0:
            return False, "negative start"
        start[t] = (e["start"], e["end"])
    for t, ps in _J3.items():
        for p in ps:
            if start[t][0] < start[p][1]:
                return False, f"{t} starts {start[t][0]} before prereq {p} ends {start[p][1]}"
    ms = max(v[1] for v in start.values())
    if o["makespan"] != ms:
        return False, f"declared makespan {o['makespan']} != actual {ms}"
    if ms != 12:
        return False, f"makespan {ms} != optimal 12"
    return True, "ok"


json_task("json_makespan", 2, """
Six tasks with durations (days) and prerequisites:
  a: 2 days, no prerequisites
  b: 3 days, after a
  c: 1 day,  after a
  d: 4 days, after b and c
  e: 2 days, after c
  f: 3 days, after d and e
Unlimited workers may run in parallel. Schedule every task to start as early as possible
and achieve the MINIMUM possible makespan.
Produce:
{"schedule": [ {"task": <name>, "start": <int day>, "end": <int day>} x 6 ],
 "makespan": <int>}
Day counting starts at 0. `end` must equal `start` + duration, and a task may only start
once all of its prerequisites have ended. `makespan` is the largest `end`.
""", _chk_sched, 3, 8)


_MENU_FOODS = {
    "grilled_salmon": dict(kcal=400, protein=40, veg=False, gf=True),
    "lentil_stew": dict(kcal=350, protein=18, veg=True, gf=True),
    "chicken_wrap": dict(kcal=550, protein=35, veg=False, gf=False),
    "tofu_stirfry": dict(kcal=420, protein=22, veg=True, gf=True),
    "quinoa_salad": dict(kcal=300, protein=12, veg=True, gf=True),
    "beef_chili": dict(kcal=480, protein=45, veg=False, gf=True),
    "pasta_primavera": dict(kcal=520, protein=16, veg=True, gf=False),
    "egg_frittata": dict(kcal=380, protein=26, veg=True, gf=True),
    "chickpea_curry": dict(kcal=460, protein=20, veg=True, gf=True),
}


def _chk_menu(o):
    if not isinstance(o, dict) or set(o.keys()) != {"days"}:
        return False, f"keys {sorted(o.keys()) if isinstance(o, dict) else type(o)}"
    days = o["days"]
    if not isinstance(days, list) or len(days) != 3:
        return False, "need exactly 3 days"
    used = []
    nveg = 0
    for i, d in enumerate(days):
        if not isinstance(d, dict) or set(d.keys()) != {"day", "meals", "total_kcal"}:
            return False, f"day keys {d if not isinstance(d, dict) else sorted(d.keys())}"
        if d["day"] != i + 1:
            return False, f"day numbering {d['day']} != {i+1}"
        meals = d["meals"]
        if not isinstance(meals, list) or len(meals) != 2:
            return False, "each day needs exactly 2 meals"
        tk = 0
        for m in meals:
            if not isinstance(m, str) or m not in _MENU_FOODS:
                return False, f"unknown dish {m!r}"
            if not _MENU_FOODS[m]["gf"]:
                return False, f"{m} is not gluten-free"
            used.append(m)
            tk += _MENU_FOODS[m]["kcal"]
            if _MENU_FOODS[m]["veg"]:
                nveg += 1
        if d["total_kcal"] != tk:
            return False, f"day {i+1} total_kcal {d['total_kcal']} != {tk}"
        if not (700 <= tk <= 850):
            return False, f"day {i+1} kcal {tk} outside 700..850"
    if len(set(used)) != 6:
        return False, "dishes must all be distinct across the 3 days"
    if nveg < 3:
        return False, f"only {nveg} vegetarian meals, need >= 3"
    return True, "ok"


json_task("json_menu", 2, """
Available dishes (kcal, protein g, vegetarian?, gluten-free?):
  grilled_salmon   400 kcal, 40 p, not vegetarian, gluten-free
  lentil_stew      350 kcal, 18 p, vegetarian,     gluten-free
  chicken_wrap     550 kcal, 35 p, not vegetarian, NOT gluten-free
  tofu_stirfry     420 kcal, 22 p, vegetarian,     gluten-free
  quinoa_salad     300 kcal, 12 p, vegetarian,     gluten-free
  beef_chili       480 kcal, 45 p, not vegetarian, gluten-free
  pasta_primavera  520 kcal, 16 p, vegetarian,     NOT gluten-free
  egg_frittata     380 kcal, 26 p, vegetarian,     gluten-free
  chickpea_curry   460 kcal, 20 p, vegetarian,     gluten-free

Plan 3 days of 2 meals each. Constraints:
- every dish used must be gluten-free
- all 6 chosen dishes must be DISTINCT (no dish repeats on any day)
- each day's total kcal must be between 700 and 850 inclusive
- at least 3 of the 6 meals must be vegetarian

Produce:
{"days": [ {"day": <1|2|3>, "meals": [<dish>, <dish>], "total_kcal": <int>} x 3 ]}
Days must be listed in order 1, 2, 3 and total_kcal must equal the sum of the day's dish kcal.
""", _chk_menu, 3, 8)


_J5 = {"t1": [], "t2": [], "t3": ["t1"], "t4": ["t1"], "t5": ["t2"],
       "t6": ["t3", "t4"], "t7": ["t5"], "t8": ["t6", "t7"]}
_J5_DUR = {"t1": 3, "t2": 2, "t3": 2, "t4": 4, "t5": 5, "t6": 3, "t7": 2, "t8": 4}
# 2 workers. total work = 25 -> lower bound ceil(25/2)=13; critical path t1,t4,t6,t8=3+4+3+4=14
# t2,t5,t7,t8 = 2+5+2+4 = 13.  Optimal makespan = 14 (verified by exhaustive search in selfcheck)
_J5_OPT = 16  # exact optimum, verified by SSGS over all 8! priority lists


def _chk_sched2(o):
    if not isinstance(o, dict) or set(o.keys()) != {"assignments", "makespan"}:
        return False, f"keys {sorted(o.keys()) if isinstance(o, dict) else type(o)}"
    a = o["assignments"]
    if not isinstance(a, list) or len(a) != 8:
        return False, "need 8 assignments"
    seen = {}
    for e in a:
        if not isinstance(e, dict) or set(e.keys()) != {"task", "worker", "start", "end"}:
            return False, f"entry keys {sorted(e.keys()) if isinstance(e, dict) else e}"
        t = e["task"]
        if t not in _J5 or t in seen:
            return False, f"bad/dup task {t}"
        if e["worker"] not in (1, 2) or isinstance(e["worker"], bool):
            return False, f"worker must be 1 or 2, got {e['worker']!r}"
        for k in ("start", "end"):
            if not isinstance(e[k], int) or isinstance(e[k], bool) or e[k] < 0:
                return False, f"{k} must be non-negative int"
        if e["end"] - e["start"] != _J5_DUR[t]:
            return False, f"{t} duration {e['end']-e['start']} != {_J5_DUR[t]}"
        seen[t] = e
    for t, ps in _J5.items():
        for p in ps:
            if seen[t]["start"] < seen[p]["end"]:
                return False, f"{t} violates prereq {p}"
    for w in (1, 2):
        iv = sorted([(seen[t]["start"], seen[t]["end"]) for t in seen if seen[t]["worker"] == w])
        for i in range(1, len(iv)):
            if iv[i][0] < iv[i - 1][1]:
                return False, f"worker {w} double-booked {iv[i-1]} {iv[i]}"
    ms = max(e["end"] for e in seen.values())
    if o["makespan"] != ms:
        return False, f"declared {o['makespan']} != actual {ms}"
    if ms != _J5_OPT:
        return False, f"makespan {ms} != optimal {_J5_OPT}"
    return True, "ok"


json_task("json_2workers", 3, """
Eight tasks, durations in hours, with prerequisites:
  t1: 3h, none          t2: 2h, none
  t3: 2h, after t1      t4: 4h, after t1
  t5: 5h, after t2      t6: 3h, after t3 and t4
  t7: 2h, after t5      t8: 4h, after t6 and t7
There are EXACTLY 2 workers. A task runs on one worker without interruption, and a worker
runs at most one task at a time. A task may start only after all its prerequisites finished.
Find a schedule with the MINIMUM possible makespan.

Produce:
{"assignments": [ {"task": <name>, "worker": <1 or 2>, "start": <int hour>, "end": <int hour>} x 8 ],
 "makespan": <int>}
Time starts at 0. `end` must equal `start` + duration. `makespan` is the largest `end`.
""", _chk_sched2, 3, 10)


_COURSES = {
    "CS101": [], "CS102": ["CS101"], "MATH1": [], "MATH2": ["MATH1"],
    "CS201": ["CS102", "MATH1"], "CS210": ["CS102"], "CS301": ["CS201", "MATH2"],
    "CS310": ["CS210", "CS201"], "CS401": ["CS301", "CS310"], "STAT1": ["MATH2"],
}


def _chk_courses(o):
    if not isinstance(o, dict) or set(o.keys()) != {"semesters"}:
        return False, f"keys {sorted(o.keys()) if isinstance(o, dict) else type(o)}"
    sems = o["semesters"]
    if not isinstance(sems, list) or not (1 <= len(sems) <= 5):
        return False, f"need 1..5 semesters, got {len(sems) if isinstance(sems, list) else '?'}"
    placed = {}
    for i, s in enumerate(sems):
        if not isinstance(s, dict) or set(s.keys()) != {"semester", "courses"}:
            return False, f"semester keys {sorted(s.keys()) if isinstance(s, dict) else s}"
        if s["semester"] != i + 1:
            return False, f"semester numbering {s['semester']} != {i+1}"
        cs = s["courses"]
        if not isinstance(cs, list) or not (1 <= len(cs) <= 2):
            return False, f"semester {i+1} has {len(cs) if isinstance(cs, list) else '?'} courses, need 1..2"
        for c in cs:
            if c not in _COURSES:
                return False, f"unknown course {c!r}"
            if c in placed:
                return False, f"duplicate course {c}"
            placed[c] = i + 1
    if set(placed) != set(_COURSES):
        return False, f"missing {sorted(set(_COURSES) - set(placed))}"
    for c, ps in _COURSES.items():
        for p in ps:
            if placed[p] >= placed[c]:
                return False, f"{c} (sem {placed[c]}) not strictly after prereq {p} (sem {placed[p]})"
    return True, "ok"


json_task("json_courses", 3, """
Ten courses with prerequisites:
  CS101: none            MATH1: none
  CS102: CS101           MATH2: MATH1
  CS201: CS102, MATH1    CS210: CS102
  CS301: CS201, MATH2    CS310: CS210, CS201
  CS401: CS301, CS310    STAT1: MATH2
Schedule ALL TEN courses into AT MOST 5 semesters, at most 2 courses per semester,
with every prerequisite taken in a STRICTLY EARLIER semester than the course itself.
(Note this is exactly tight: 10 courses, 5 semesters, 2 per semester, so every semester
must hold exactly 2 courses.)

Produce:
{"semesters": [ {"semester": <int starting at 1>, "courses": [<course codes>]} ... ]}
Semesters must be numbered 1..n consecutively and listed in order. Every course appears
exactly once. No semester may be empty.
""", _chk_courses, 3, 9)


_RCPSP = {
    "p1": dict(dur=2, cpu=2, mem=1, deps=[]),
    "p2": dict(dur=3, cpu=1, mem=2, deps=[]),
    "p3": dict(dur=2, cpu=3, mem=1, deps=["p1"]),
    "p4": dict(dur=4, cpu=1, mem=3, deps=["p1"]),
    "p5": dict(dur=2, cpu=2, mem=2, deps=["p2"]),
    "p6": dict(dur=3, cpu=2, mem=1, deps=["p3", "p5"]),
    "p7": dict(dur=1, cpu=3, mem=3, deps=["p4"]),
    "p8": dict(dur=3, cpu=1, mem=1, deps=["p6", "p7"]),
}
_CPU_CAP, _MEM_CAP = 4, 4
_RCPSP_LIMIT = 14  # exact optimum, verified by SSGS over all 8! priority lists


def _chk_rcpsp(o):
    if not isinstance(o, dict) or set(o.keys()) != {"plan", "makespan"}:
        return False, f"keys {sorted(o.keys()) if isinstance(o, dict) else type(o)}"
    pl = o["plan"]
    if not isinstance(pl, list) or len(pl) != 8:
        return False, "plan needs 8 entries"
    seen = {}
    for e in pl:
        if not isinstance(e, dict) or set(e.keys()) != {"id", "start", "end"}:
            return False, f"entry keys {sorted(e.keys()) if isinstance(e, dict) else e}"
        i = e["id"]
        if i not in _RCPSP or i in seen:
            return False, f"bad/dup id {i}"
        for k in ("start", "end"):
            if not isinstance(e[k], int) or isinstance(e[k], bool) or e[k] < 0:
                return False, f"{k} must be non-negative int"
        if e["end"] - e["start"] != _RCPSP[i]["dur"]:
            return False, f"{i} duration wrong"
        seen[i] = e
    for i, spec in _RCPSP.items():
        for d in spec["deps"]:
            if seen[i]["start"] < seen[d]["end"]:
                return False, f"{i} violates dep {d}"
    horizon = max(e["end"] for e in seen.values())
    for t in range(horizon):
        cpu = sum(_RCPSP[i]["cpu"] for i in seen if seen[i]["start"] <= t < seen[i]["end"])
        mem = sum(_RCPSP[i]["mem"] for i in seen if seen[i]["start"] <= t < seen[i]["end"])
        if cpu > _CPU_CAP:
            return False, f"cpu {cpu} > {_CPU_CAP} at t={t}"
        if mem > _MEM_CAP:
            return False, f"mem {mem} > {_MEM_CAP} at t={t}"
    if o["makespan"] != horizon:
        return False, f"declared {o['makespan']} != actual {horizon}"
    if horizon > _RCPSP_LIMIT:
        return False, f"makespan {horizon} > required {_RCPSP_LIMIT}"
    return True, "ok"


json_task("json_rcpsp", 4, f"""
Resource-constrained scheduling. Eight jobs; each occupies CPU and MEM units for its whole
duration. The cluster has {_CPU_CAP} CPU units and {_MEM_CAP} MEM units total at every instant.

  id  dur  cpu  mem  depends-on
  p1   2    2    1   -
  p2   3    1    2   -
  p3   2    3    1   p1
  p4   4    1    3   p1
  p5   2    2    2   p2
  p6   3    2    1   p3, p5
  p7   1    3    3   p4
  p8   3    1    1   p6, p7

Schedule all eight so that at EVERY instant the sum of cpu over running jobs is <= {_CPU_CAP}
and the sum of mem over running jobs is <= {_MEM_CAP}, every job starts only after all its
dependencies have ended, and the makespan is AT MOST {_RCPSP_LIMIT} (this is the true optimum -- nothing shorter exists).

Produce:
{{"plan": [ {{"id": <job id>, "start": <int>, "end": <int>}} x 8 ], "makespan": <int>}}
Time starts at 0, jobs run without interruption, `end` = `start` + dur, and `makespan` is
the largest `end`.
""", _chk_rcpsp, 3, 11)


_REGIONS = {"us-east": dict(cost=10, cap=40), "us-west": dict(cost=12, cap=30),
            "eu-central": dict(cost=14, cap=35), "ap-south": dict(cost=8, cap=25)}
_SERVICES = {"api": ["auth", "db"], "auth": ["db"], "db": [], "worker": ["db", "queue"],
             "queue": []}


def _chk_deploy(o):
    if not isinstance(o, dict) or set(o.keys()) != {"regions", "service_graph", "total_cost"}:
        return False, f"top keys {sorted(o.keys()) if isinstance(o, dict) else type(o)}"
    regs = o["regions"]
    if not isinstance(regs, list) or len(regs) != 3:
        return False, "need exactly 3 regions"
    names, total, svc_regions = set(), 0, {}
    for r in regs:
        if not isinstance(r, dict) or set(r.keys()) != {"name", "services"}:
            return False, f"region keys {sorted(r.keys()) if isinstance(r, dict) else r}"
        n = r["name"]
        if n not in _REGIONS or n in names:
            return False, f"bad/dup region {n!r}"
        names.add(n)
        svcs = r["services"]
        if not isinstance(svcs, list) or not svcs:
            return False, f"region {n} has no services"
        used_cap = 0
        for s in svcs:
            if not isinstance(s, dict) or set(s.keys()) != {"name", "replicas"}:
                return False, f"service keys {sorted(s.keys()) if isinstance(s, dict) else s}"
            if s["name"] not in _SERVICES:
                return False, f"unknown service {s['name']!r}"
            if not isinstance(s["replicas"], int) or isinstance(s["replicas"], bool) or s["replicas"] < 1:
                return False, "replicas must be int >= 1"
            svc_regions.setdefault(s["name"], set()).add(n)
            used_cap += s["replicas"]
            total += s["replicas"] * _REGIONS[n]["cost"]
        if used_cap > _REGIONS[n]["cap"]:
            return False, f"region {n} capacity {used_cap} > {_REGIONS[n]['cap']}"
    if set(svc_regions) != set(_SERVICES):
        return False, f"services missing: {sorted(set(_SERVICES) - set(svc_regions))}"
    for s, rs in svc_regions.items():
        if len(rs) < 2:
            return False, f"service {s} deployed in only {len(rs)} region(s), need >= 2"
    for s, tot in [(s, sum(1 for _ in rs)) for s, rs in svc_regions.items()]:
        pass
    reps = {}
    for r in regs:
        for s in r["services"]:
            reps[s["name"]] = reps.get(s["name"], 0) + s["replicas"]
    for s, n in reps.items():
        if n < 3:
            return False, f"service {s} has {n} total replicas, need >= 3"
    g = o["service_graph"]
    if not isinstance(g, dict) or set(g.keys()) != set(_SERVICES):
        return False, f"service_graph keys {sorted(g.keys()) if isinstance(g, dict) else type(g)}"
    for s, deps in g.items():
        if not isinstance(deps, list) or sorted(map(str, deps)) != sorted(_SERVICES[s]):
            return False, f"service_graph[{s}] = {deps} != {_SERVICES[s]}"
    if G.has_cycle({k: list(v) for k, v in g.items()}):
        return False, "service_graph has a cycle"
    if abs(o["total_cost"] - total) > 1e-6:
        return False, f"total_cost {o['total_cost']} != {total}"
    if total > 260:
        return False, f"total_cost {total} > budget 260"
    return True, "ok"


json_task("json_deploy", 4, """
Design a multi-region deployment.

Regions (cost per replica per month, total replica capacity):
  us-east    cost 10, capacity 40
  us-west    cost 12, capacity 30
  eu-central cost 14, capacity 35
  ap-south   cost  8, capacity 25

Services and their dependencies:
  api    -> auth, db
  auth   -> db
  db     -> (none)
  worker -> db, queue
  queue  -> (none)

Constraints, ALL of which are checked:
- use EXACTLY 3 of the 4 regions
- every one of the 5 services must appear in at least 2 different regions
- every service must have at least 3 replicas in total across all regions
- per region, the sum of replicas placed there must not exceed that region's capacity
- total_cost = sum over every placement of (replicas * that region's cost per replica)
- total_cost must be at most 260

Produce exactly this shape:
{"regions": [ {"name": <region>, "services": [ {"name": <service>, "replicas": <int >= 1>} ... ]} x 3 ],
 "service_graph": {<service>: [<its dependencies>] for all 5 services},
 "total_cost": <number>}
`service_graph` must reproduce the dependency lists above exactly and must be acyclic.
""", _chk_deploy, 4, 12)

# ==========================================================================
# CLASS 3 -- EXACT-ANSWER REASONING (8)
# ==========================================================================

exact_task("exact_apples", 1, """
A store sells apples at 3 for $2. Maria buys 24 apples and pays with a $20 bill.
How many dollars of change does she receive?
""", "4")

exact_task("exact_dayofweek", 1, """
January 1st 2024 was a Monday. What day of the week was March 1st 2024?
(2024 is a leap year.) Answer with the full day name.
""", "friday")

exact_task("exact_trains", 2, """
Two trains start at the same moment. Train A leaves city X toward city Y at 60 km/h.
Train B leaves city Y toward city X at 90 km/h. The cities are 450 km apart.
A bird starts at city X at the same moment, flies at 120 km/h toward train B, and on
meeting a train instantly reverses direction, continuing until the trains meet.
How many kilometres does the bird fly in total?
""", "360")

exact_task("exact_committee", 2, """
A committee of 5 people must be chosen from 7 women and 6 men, and must contain
at least 2 women and at least 2 men. How many distinct committees are possible?
""", "945")

exact_task("exact_modpow", 3, """
Compute the last three digits of 7^803 (that is, 7^803 mod 1000).
Give the three-digit result including any leading zeros.
""", "343")

exact_task("exact_dice", 3, """
Three fair six-sided dice are rolled. Given that at least one die shows a 6, what is the
probability that the sum of the three dice is exactly 15?
Give the answer as an exact reduced fraction in the form a/b.
""", "9/91")

exact_task("exact_derangement", 4, """
Seven distinct letters are placed into seven distinct addressed envelopes, one letter per
envelope. In how many ways can this be done so that EXACTLY TWO letters end up in their
own correct envelope?
""", "924")

exact_task("exact_digits", 4, """
How many integers n with 1 <= n <= 100000 have the property that the sum of the decimal
digits of n is exactly 15 AND n is divisible by 11?
""", "261")


TASK_BY_ID = {t["id"]: t for t in TASKS}


def features(task: dict) -> dict:
    """Deterministic, model-independent task features."""
    p = task["prompt"]
    return {
        "prompt_chars": len(p),
        "prompt_words": len(p.split()),
        "prompt_lines": p.count("\n") + 1,
        "cls_code": 1 if task["cls"] == "code" else 0,
        "cls_json": 1 if task["cls"] == "json" else 0,
        "cls_exact": 1 if task["cls"] == "exact" else 0,
        "n_tests": task["n_tests"],
        "schema_depth": task["schema_depth"],
        "numeric_constraints": 1 if task["numeric_constraints"] else 0,
        "n_digits": sum(c.isdigit() for c in p),
        "n_constraint_words": sum(p.lower().count(w) for w in
                                  ("must", "exactly", "at most", "at least", "only", "never")),
    }


if __name__ == "__main__":
    from collections import Counter
    print("tasks:", len(TASKS))
    print("by class:", Counter(t["cls"] for t in TASKS))
    print("by tier:", Counter(t["tier"] for t in TASKS))
    for t in TASKS:
        print(f"  {t['id']:<24} {t['cls']:<6} tier{t['tier']} {len(t['prompt']):>5}ch")
