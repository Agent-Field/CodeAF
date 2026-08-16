#!/usr/bin/env python3
"""Validate the suite before spending a cent on it.

Three things go wrong in a hand-written benchmark and all three are silent:
a hidden test that no correct program can pass, a "verified" answer that is
just wrong, and a plan grader that rejects a perfectly good plan. Each one
looks exactly like "the models are weak" in the results.

So: every coding task gets a reference solution here and its hidden tests must
pass against it; every reasoning answer is recomputed from first principles by
brute force where that is possible; every plan grader is shown one correct
answer it must accept and several near-misses it must reject.

Run: python3 validate_suite.py
"""
import json
import math
import sys
from itertools import permutations, product

import tasks as T

FAILS = []


def check(name, cond, detail=""):
    if not cond:
        FAILS.append(f"{name}: {detail}")
        print(f"  FAIL {name} {detail}")
    return cond


# ===========================================================================
# 1. coding tasks -- reference solutions must pass the hidden tests
# ===========================================================================

REFERENCE = {
"C01": '''
def reverse_words(s):
    return " ".join(reversed(s.split()))
''',
"C02": '''
def count_vowels(s):
    return sum(1 for c in s.lower() if c in "aeiou")
''',
"C03": '''
def fizz_list(n):
    out = []
    for i in range(1, max(n, 0) + 1):
        s = ("Fizz" if i % 3 == 0 else "") + ("Buzz" if i % 5 == 0 else "")
        out.append(s or str(i))
    return out
''',
"C04": '''
def rle(s):
    out, i = [], 0
    while i < len(s):
        j = i
        while j < len(s) and s[j] == s[i]:
            j += 1
        out.append(s[i] + str(j - i))
        i = j
    return "".join(out)
''',
"C05": '''
def merge_intervals(intervals):
    if not intervals:
        return []
    xs = sorted([list(x) for x in intervals])
    out = [xs[0]]
    for a, b in xs[1:]:
        if a <= out[-1][1]:
            out[-1][1] = max(out[-1][1], b)
        else:
            out.append([a, b])
    return out
''',
"C06": '''
def balanced(s):
    pairs = {")": "(", "]": "[", "}": "{"}
    st = []
    for c in s:
        if c in "([{":
            st.append(c)
        elif c in pairs:
            if not st or st.pop() != pairs[c]:
                return False
    return not st
''',
"C07": '''
def longest_common_prefix(strs):
    if not strs:
        return ""
    a, b = min(strs), max(strs)
    i = 0
    while i < len(a) and i < len(b) and a[i] == b[i]:
        i += 1
    return a[:i]
''',
"C08": '''
def min_coins(coins, target):
    INF = float("inf")
    dp = [0] + [INF] * target
    for t in range(1, target + 1):
        for c in coins:
            if c <= t and dp[t - c] + 1 < dp[t]:
                dp[t] = dp[t - c] + 1
    return -1 if dp[target] == INF else dp[target]
''',
"C09": '''
def weighted_edit(a, b):
    INS, DEL, SUB = 1, 2, 3
    n, m = len(a), len(b)
    dp = [[0] * (m + 1) for _ in range(n + 1)]
    for i in range(1, n + 1):
        dp[i][0] = i * DEL
    for j in range(1, m + 1):
        dp[0][j] = j * INS
    for i in range(1, n + 1):
        for j in range(1, m + 1):
            if a[i - 1] == b[j - 1]:
                dp[i][j] = dp[i - 1][j - 1]
            else:
                dp[i][j] = min(dp[i - 1][j - 1] + SUB,
                               dp[i - 1][j] + DEL,
                               dp[i][j - 1] + INS)
    return dp[n][m]
''',
"C10": '''
def search_rotated(nums, target):
    lo, hi = 0, len(nums) - 1
    while lo <= hi:
        mid = (lo + hi) // 2
        if nums[mid] == target:
            return mid
        if nums[lo] <= nums[mid]:
            if nums[lo] <= target < nums[mid]:
                hi = mid - 1
            else:
                lo = mid + 1
        else:
            if nums[mid] < target <= nums[hi]:
                lo = mid + 1
            else:
                hi = mid - 1
    return -1
''',
"C11": '''
def count_subarrays(nums, target):
    from collections import defaultdict
    seen = defaultdict(int)
    seen[0] = 1
    run = total = 0
    for x in nums:
        run += x
        total += seen[run - target]
        seen[run] += 1
    return total
''',
"C12": '''
def max_sum_k_no_adjacent(nums, k):
    NEG = float("-inf")
    n = len(nums)
    if k < 0 or k > (n + 1) // 2:
        return None
    if k == 0:
        return 0
    # dp[j][0] best with j picked so far, last index not taken
    # dp[j][1] best with j picked so far, last index taken
    prev = [[NEG, NEG] for _ in range(k + 1)]
    prev[0][0] = 0
    for i in range(n):
        cur = [[NEG, NEG] for _ in range(k + 1)]
        for j in range(k + 1):
            best_skip = max(prev[j][0], prev[j][1])
            if best_skip > NEG:
                cur[j][0] = best_skip
            if j > 0 and prev[j - 1][0] > NEG:
                cur[j][1] = prev[j - 1][0] + nums[i]
        prev = cur
    best = max(prev[k][0], prev[k][1])
    return None if best == NEG else best
''',
}


REFERENCE_V2 = {
"C07": '''
def longest_arith_subseq(nums):
    if not nums:
        return 0
    best = 1
    dp = []
    for i in range(len(nums)):
        cur = {}
        for j in range(i):
            d = nums[i] - nums[j]
            cur[d] = max(cur.get(d, 0), dp[j].get(d, 1) + 1)
            best = max(best, cur[d])
        dp.append(cur)
    return best
''',
"C08": '''
def min_coins_bounded(coins, target):
    # State is a count vector over denominations sorted ascending, never the
    # expanded list: comparing expanded lists is what makes the naive solution
    # too slow. With denominations ascending, the ascending expanded list of
    # one vector is lexicographically smaller exactly when the vector has more
    # of the smallest denomination, then more of the next, and so on -- so the
    # comparison key is (total_coins, -c0, -c1, ...), which is O(#denoms).
    ds = sorted(coins)
    n = len(ds)
    dp = [None] * (target + 1)
    dp[0] = (0,) * n
    for i, (denom, cap) in enumerate(ds):
        nxt = [None] * (target + 1)
        for t in range(target + 1):
            if dp[t] is None:
                continue
            for c in range(0, cap + 1):
                tt = t + denom * c
                if tt > target:
                    break
                cand = dp[t][:i] + (c,) + dp[t][i + 1:]
                cur = nxt[tt]
                if cur is None:
                    nxt[tt] = cand
                else:
                    kc = (sum(cand),) + tuple(-x for x in cand)
                    ku = (sum(cur),) + tuple(-x for x in cur)
                    if kc < ku:
                        nxt[tt] = cand
        dp = nxt
    v = dp[target]
    if v is None:
        return None
    out = []
    for (denom, _cap), c in zip(ds, v):
        out.extend([denom] * c)
    return sorted(out)
''',
"C09": '''
def count_paths(grid, k):
    MOD = 1000000007
    R, C = len(grid), len(grid[0])
    if grid[0][0] == "#" or grid[R-1][C-1] == "#":
        return 0
    dp = [[[0] * (k + 1) for _ in range(C)] for _ in range(R)]
    s0 = 1 if grid[0][0] == "*" else 0
    if s0 <= k:
        dp[0][0][s0] = 1
    for r in range(R):
        for c in range(C):
            if grid[r][c] == "#":
                continue
            add = 1 if grid[r][c] == "*" else 0
            if r == 0 and c == 0:
                continue
            for s in range(k + 1):
                if s - add < 0:
                    continue
                tot = 0
                if r > 0 and grid[r-1][c] != "#":
                    tot += dp[r-1][c][s-add]
                if c > 0 and grid[r][c-1] != "#":
                    tot += dp[r][c-1][s-add]
                dp[r][c][s] = tot % MOD
    return dp[R-1][C-1][k] % MOD
''',
"C10": '''
def kth_smallest_pair_sum(a, b, k):
    import bisect
    lo, hi = a[0] + b[0], a[-1] + b[-1]
    while lo < hi:
        mid = (lo + hi) // 2
        cnt = 0
        for x in a:
            cnt += bisect.bisect_right(b, mid - x)
            if cnt >= k:
                break
        if cnt >= k:
            hi = mid
        else:
            lo = mid + 1
    return lo
''',
"C11": '''
def count_subarrays_range(nums, limit):
    from collections import deque
    mx, mn = deque(), deque()
    left = 0
    total = 0
    for right, v in enumerate(nums):
        while mx and nums[mx[-1]] <= v:
            mx.pop()
        mx.append(right)
        while mn and nums[mn[-1]] >= v:
            mn.pop()
        mn.append(right)
        while nums[mx[0]] - nums[mn[0]] > limit:
            if mx[0] == left:
                mx.popleft()
            if mn[0] == left:
                mn.popleft()
            left += 1
        total += right - left + 1
    return total
''',
"C12": '''
def min_cost_merge(stones, k):
    n = len(stones)
    if n == 1:
        return 0
    if (n - 1) % (k - 1) != 0:
        return -1
    pre = [0] * (n + 1)
    for i, v in enumerate(stones):
        pre[i + 1] = pre[i] + v
    INF = float("inf")
    dp = [[INF] * n for _ in range(n)]
    for i in range(n):
        dp[i][i] = 0
    for length in range(2, n + 1):
        for i in range(0, n - length + 1):
            j = i + length - 1
            best = INF
            m = i
            while m < j:
                best = min(best, dp[i][m] + dp[m + 1][j])
                m += k - 1
            if (length - 1) % (k - 1) == 0:
                best += pre[j + 1] - pre[i]
            dp[i][j] = best
    return dp[0][n - 1]
''',
}


def validate_code():
    print("coding tasks -- reference solutions vs hidden tests")
    for tid, lvl, spec, entry, tests in T.CODE:
        ref = REFERENCE.get(tid)
        if not check(tid, ref is not None, "no reference solution"):
            continue
        ok, why = T.run_code(ref, tests, entry)
        check(tid, ok, why)
        # the grader must also reject an empty / wrong solution
        bad, _ = T.run_code(f"def {entry}(*a, **k):\n    return None\n", tests, entry)
        check(tid + "/neg", not bad, "grader accepted a stub solution")
    print("  (round 2 hardened)")
    for tid, (lvl, spec, entry, tests) in T.CODE_V2.items():
        ref = REFERENCE_V2.get(tid)
        if not check(tid + "v2", ref is not None, "no reference solution"):
            continue
        ok, why = T.run_code(ref, tests, entry)
        check(tid + "v2", ok, why)
        bad, _ = T.run_code(f"def {entry}(*a, **k):\n    return None\n", tests, entry)
        check(tid + "v2/neg", not bad, "grader accepted a stub solution")
    print()


# ===========================================================================
# 2. reasoning answers -- recomputed, not remembered
# ===========================================================================

def validate_reason():
    print("reasoning tasks -- ground truth recomputed")
    exp = {tid: e for tid, lvl, p, e, k in T.REASON}

    check("R01", exp["R01"] == 3 * 12 - 7, f"{exp['R01']}")
    check("R02", exp["R02"] == 42.50, f"{exp['R02']}")
    check("R03", exp["R03"] == len("the quick brown fox jumps over the lazy dog".split()))
    check("R04", exp["R04"] == 80 * 1.25 * 0.80, f"{exp['R04']}")
    check("R05", exp["R05"] == math.ceil(3 * 3.785 * 1000 / 250),
          f"{exp['R05']} vs {math.ceil(3*3.785*1000/250)}")

    import datetime
    d = datetime.date(2024, 2, 15) + datetime.timedelta(days=45)
    check("R06", exp["R06"] == d.isoformat(), f"{exp['R06']} vs {d.isoformat()}")

    apr = round(340 * 0.85 * (18.00 + 2.50), 2)
    check("R07", abs(exp["R07"] - apr) < 1e-9, f"{exp['R07']} vs {apr}")

    # R08 by exhaustive search over assignments
    people = ["ana", "ben", "cleo", "dan"]
    sols = set()
    for perm in permutations(["tea", "coffee", "water", "juice"]):
        a = dict(zip(people, perm))
        if a["ana"] in ("coffee", "water"):
            continue
        if a["cleo"] != "water":
            continue
        if a["dan"] == "juice":
            continue
        # juice drinker sits next to ben in the seating ana,ben,cleo,dan
        juice = [p for p in people if a[p] == "juice"][0]
        if juice not in ("ana", "cleo"):
            continue
        sols.add(juice)
    check("R08", sols == {exp["R08"]}, f"{exp['R08']} vs solutions {sols}")

    words = "solar wind mapping project".split()
    kept = [w for w in words if len(w) >= 5]
    r09 = "-".join(w.upper() for w in reversed(kept))
    check("R09", exp["R09"] == r09, f"{exp['R09']!r} vs {r09!r}")

    r10 = sum(1 for n in range(1000, 10000)
              if n % 5 == 0 and len(set(str(n))) == 4)
    check("R10", exp["R10"] == r10, f"{exp['R10']} vs {r10}")

    r11 = f"{pow(7, 2024, 100):02d}"
    check("R11", exp["R11"] == r11, f"{exp['R11']!r} vs {r11!r}")

    # R12 by exhaustive search over week assignments
    weeks = {}
    sols12 = set()
    for perm in permutations([1, 2, 3, 4, 5]):
        w = dict(zip(["s1", "s2", "s3", "s4", "s5"], perm))
        if not w["s3"] < w["s1"]:
            continue
        if w["s5"] != w["s2"] + 2:
            continue
        if w["s4"] not in (1, 5):
            continue
        if w["s1"] == 5:
            continue
        if w["s2"] == 1:
            continue
        if not w["s4"] > w["s1"]:
            continue
        sols12.add(w["s5"])
        weeks = w
    check("R12", sols12 == {exp["R12"]},
          f"{exp['R12']} vs solutions {sols12} (one witness {weeks})")
    print()


# ===========================================================================
# 3. plan graders -- must accept a correct answer, reject near-misses
# ===========================================================================

GOOD = {
"P01": {"tasks": [{"id": "t1", "title": "Grind the beans", "depends_on": []},
                  {"id": "t2", "title": "Boil the water", "depends_on": []},
                  {"id": "t3", "title": "Pour", "depends_on": ["t1", "t2"]}]},
"P02": {"allocations": {"eng": 60, "design": 20, "qa": 20}},
"P03": {"buckets": {"fruit": ["mango", "plum", "fig"],
                    "vegetable": ["leek", "turnip"]}},
"P04": {"nodes": [{"id": "survey", "depends_on": []},
                  {"id": "schema", "depends_on": ["survey"]},
                  {"id": "ingest", "depends_on": ["schema"]},
                  {"id": "dashboard", "depends_on": ["ingest"]},
                  {"id": "signoff", "depends_on": ["dashboard"]}],
        "topo_order": ["survey", "schema", "ingest", "dashboard", "signoff"]},
"P05": {"estimates": {"login": 5, "signup": 5, "reset": 3, "profile": 3,
                      "settings": 3, "logout": 2}},
"P06": {"phases": [{"name": "prep", "tasks": [
            {"name": "audit", "subtasks": ["a", "b", "c"]},
            {"name": "plan", "subtasks": ["d", "e", "f"]}]},
        {"name": "cutover", "tasks": [
            {"name": "move", "subtasks": ["g", "h", "i"]},
            {"name": "verify", "subtasks": ["j", "k", "l"]}]}],
        "counts": {"phases": 2, "tasks": 4, "subtasks": 12}},
"P07": {"nodes": [{"id": "spec", "days": 2, "depends_on": []},
                  {"id": "impl", "days": 5, "depends_on": ["spec"]},
                  {"id": "tests", "days": 3, "depends_on": ["impl"]},
                  {"id": "docs", "days": 4, "depends_on": ["spec"]},
                  {"id": "deploy", "days": 1, "depends_on": ["tests"]}],
        "critical_path_days": 11},
"P08": {"allocations": {"platform": 400, "product": 300, "growth": 175,
                        "support": 125}},
"P09": None,  # built below
"P10": {"nodes": [{"id": "A", "days": 3, "depends_on": []},
                  {"id": "B", "days": 2, "depends_on": ["A"]},
                  {"id": "C", "days": 4, "depends_on": ["A"]},
                  {"id": "D", "days": 1, "depends_on": ["B", "C"]},
                  {"id": "review", "days": 2, "depends_on": ["D"]}],
        "critical_path_days": 10,
        "topo_order": ["A", "B", "C", "D", "review"]},
"P11": {"selected": ["c1", "c2", "c3", "c5", "c9"], "rejected": ["c4", "c6", "c7", "c8"],
        "total_cost": 40 + 55 + 30 + 25 + 50},
"P12": {"items": [{"id": "T-1", "priority": "high", "owner": "rita"},
                  {"id": "T-2", "priority": "low", "owner": None},
                  {"id": "T-3", "priority": "medium", "owner": "dev"},
                  {"id": "T-4", "priority": "high", "owner": None}]},
}


def _p09_good():
    # 3 phases x 2 tasks x 2 subtasks = 12 subtasks, all multiples of 4,
    # summing to 240. 12 subtasks of 20 hours each = 240.
    def task(n):
        return {"name": n, "hours": 40,
                "subtasks": [{"name": n + "-a", "hours": 20},
                             {"name": n + "-b", "hours": 20}]}
    phases = [{"name": f"phase{i}", "hours": 80,
               "tasks": [task(f"t{i}a"), task(f"t{i}b")]} for i in (1, 2, 3)]
    return {"phases": phases, "total_hours": 240}


GOOD["P09"] = _p09_good()

# near-misses each grader must reject, as (label, mutation)
BAD = {
"P01": [("wrong dep", {"tasks": [{"id": "t1", "title": "x", "depends_on": []},
                                 {"id": "t2", "title": "x", "depends_on": ["t1"]},
                                 {"id": "t3", "title": "x", "depends_on": ["t2"]}]})],
"P02": [("sum 99", {"allocations": {"eng": 60, "design": 20, "qa": 19}}),
        ("eng low", {"allocations": {"eng": 40, "design": 30, "qa": 30}})],
"P03": [("misfiled", {"buckets": {"fruit": ["mango", "plum", "fig", "turnip"],
                                  "vegetable": ["leek"]}})],
"P04": [("cycle", {"nodes": [{"id": "survey", "depends_on": ["signoff"]},
                             {"id": "schema", "depends_on": ["survey"]},
                             {"id": "ingest", "depends_on": ["schema"]},
                             {"id": "dashboard", "depends_on": ["ingest"]},
                             {"id": "signoff", "depends_on": ["dashboard"]}],
                   "topo_order": ["survey", "schema", "ingest", "dashboard", "signoff"]}),
        ("bad topo", {"nodes": GOOD["P04"]["nodes"],
                      "topo_order": ["schema", "survey", "ingest", "dashboard", "signoff"]})],
"P05": [("total 22", {"estimates": {"login": 5, "signup": 5, "reset": 3,
                                    "profile": 3, "settings": 3, "logout": 3}}),
        ("off scale", {"estimates": {"login": 4, "signup": 5, "reset": 3,
                                     "profile": 3, "settings": 3, "logout": 3}})],
"P06": [("count lie", {"phases": GOOD["P06"]["phases"],
                       "counts": {"phases": 2, "tasks": 4, "subtasks": 11}})],
"P07": [("docs ancestor", {"nodes": [{"id": "spec", "days": 2, "depends_on": []},
                                     {"id": "impl", "days": 5, "depends_on": ["spec"]},
                                     {"id": "tests", "days": 3, "depends_on": ["impl"]},
                                     {"id": "docs", "days": 4, "depends_on": ["spec"]},
                                     {"id": "deploy", "days": 1,
                                      "depends_on": ["tests", "docs"]}],
                           "critical_path_days": 11}),
        ("wrong cp", {"nodes": GOOD["P07"]["nodes"], "critical_path_days": 10})],
"P08": [("not decreasing", {"allocations": {"platform": 300, "product": 400,
                                            "growth": 175, "support": 125}}),
        ("not mult 25", {"allocations": {"platform": 410, "product": 300,
                                         "growth": 165, "support": 125}}),
        ("sum off", {"allocations": {"platform": 400, "product": 300,
                                     "growth": 175, "support": 100}})],
"P09": [("total 200", {"phases": _p09_good()["phases"], "total_hours": 200})],
"P10": [("no review", {"nodes": GOOD["P10"]["nodes"][:4],
                       "critical_path_days": 8,
                       "topo_order": ["A", "B", "C", "D"]}),
        ("wrong cp", {"nodes": GOOD["P10"]["nodes"], "critical_path_days": 12,
                      "topo_order": GOOD["P10"]["topo_order"]})],
"P11": [("unsorted", {"selected": ["c9", "c2", "c3", "c5", "c6"],
                      "rejected": ["c1", "c4", "c7", "c8"], "total_cost": 220}),
        ("excl pair", {"selected": ["c1", "c2", "c5", "c7", "c9"],
                       "rejected": ["c3", "c4", "c6", "c8"], "total_cost": 215}),
        ("over budget", {"selected": ["c2", "c4", "c7", "c8", "c9"],
                         "rejected": ["c1", "c3", "c5", "c6"], "total_cost": 255})],
"P12": [("unknown owner", {"items": [{"id": "T-1", "priority": "high", "owner": "rita"},
                                     {"id": "T-2", "priority": "low", "owner": "unknown"},
                                     {"id": "T-3", "priority": "medium", "owner": "dev"},
                                     {"id": "T-4", "priority": "high", "owner": None}]}),
        ("team as owner", {"items": [{"id": "T-1", "priority": "high", "owner": "rita"},
                                     {"id": "T-2", "priority": "low", "owner": None},
                                     {"id": "T-3", "priority": "medium", "owner": "dev"},
                                     {"id": "T-4", "priority": "high", "owner": "platform"}]})],
}


def validate_plan():
    print("plan graders -- accept correct, reject near-misses")
    graders = {tid: g for tid, lvl, p, g in T.PLAN}
    for tid, g in graders.items():
        ok, why = g(json.dumps(GOOD[tid]))
        check(tid + "/good", ok, why)
        for label, mut in BAD.get(tid, []):
            bad_ok, _ = g(json.dumps(mut))
            check(f"{tid}/bad[{label}]", not bad_ok, "grader accepted a wrong plan")
    # extraction must survive a fenced reply and a prose preamble
    ok, _ = graders["P02"](f"Sure!\n```json\n{json.dumps(GOOD['P02'])}\n```\nHope that helps.")
    check("extract/fenced", ok, "fenced json not extracted")
    print()


def validate_v2():
    print("round 2 -- hardened reasoning ground truth and plan graders")
    exp = {tid: (e, k) for tid, (lvl, p, e, k) in T.REASON_V2.items()}

    r07 = round(750 * (14.80 * 1.35) + 500 * (14.80 * 1.35) * 0.90
                - 1250 * 14.80 - 2400, 2)
    check("R07v2", abs(exp["R07"][0] - r07) < 1e-9, f"{exp['R07'][0]} vs {r07}")

    r10 = sum(1 for x in range(10000, 100000)
              if list(str(x)) == sorted(set(str(x))) and len(set(str(x))) == 5
              and x % 3 == 0)
    check("R10v2", exp["R10"][0] == r10, f"{exp['R10'][0]} vs {r10}")

    r11 = f"{(pow(3, 1234, 1000) + pow(7, 5678, 1000)) % 1000:03d}"
    check("R11v2", exp["R11"][0] == r11, f"{exp['R11'][0]!r} vs {r11!r}")

    sols = set()
    for p in permutations(range(1, 7)):
        w = dict(zip(["s1", "s2", "s3", "s4", "s5", "s6"], p))
        if w["s2"] != w["s6"] + 3:
            continue
        if not w["s6"] < w["s1"] < w["s3"]:
            continue
        if w["s4"] % 2:
            continue
        if w["s5"] in (1, 6):
            continue
        if abs(w["s3"] - w["s2"]) == 1:
            continue
        sols.add(w["s3"])
    check("R12v2", sols == {exp["R12"][0]}, f"{exp['R12'][0]} vs {sols}")

    # plan v2: build the correct answer straight from the CPM helper, then
    # perturb one field at a time and require rejection.
    for tid, dur, deps in [("P07", T._P07V2_DUR, T._P07V2_DEPS),
                           ("P10", T._P10V2_DUR, T._P10V2_DEPS)]:
        es, slack, proj, succ = T.cpm(deps, dur)
        nodes = [{"id": n, "days": dur[n], "depends_on": deps[n],
                  "earliest_start": es[n], "slack": slack[n]} for n in dur]
        good = {"nodes": nodes, "project_days": proj}
        if tid == "P07":
            # walk the zero-slack chain from a root to a sink
            path, cur = [], next(n for n in dur if not deps[n] and slack[n] == 0)
            while True:
                path.append(cur)
                nxt = [s for s in succ[cur] if slack[s] == 0]
                if not nxt:
                    break
                cur = nxt[0]
            good["critical_path"] = path
        else:
            order = []
            seen = set()

            def visit(n):
                if n in seen:
                    return
                seen.add(n)
                for p in deps[n]:
                    visit(p)
                order.append(n)

            for n in dur:
                visit(n)
            good["topo_order"] = order
            good["zero_slack_nodes"] = sorted(n for n in dur if slack[n] == 0)

        grader = T.PLAN_V2[tid][2]
        ok, why = grader(json.dumps(good))
        check(f"{tid}v2/good", ok, why)

        bad = json.loads(json.dumps(good))
        bad["project_days"] = proj + 1
        ok2, _ = grader(json.dumps(bad))
        check(f"{tid}v2/bad[project_days]", not ok2, "accepted wrong project_days")

        bad2 = json.loads(json.dumps(good))
        bad2["nodes"][1]["earliest_start"] = bad2["nodes"][1]["earliest_start"] + 1
        ok3, _ = grader(json.dumps(bad2))
        check(f"{tid}v2/bad[earliest_start]", not ok3, "accepted wrong earliest_start")

        bad3 = json.loads(json.dumps(good))
        bad3["nodes"] = bad3["nodes"][:-1]
        ok4, _ = grader(json.dumps(bad3))
        check(f"{tid}v2/bad[missing node]", not ok4, "accepted a missing node")

    # the two v2 plan answers are non-trivial; print them so the report can
    # quote the true values rather than re-deriving them by hand.
    es7, sl7, pr7, _ = T.cpm(T._P07V2_DEPS, T._P07V2_DUR)
    es10, sl10, pr10, _ = T.cpm(T._P10V2_DEPS, T._P10V2_DUR)
    print(f"  P07v2 project_days={pr7} slack={sl7}")
    print(f"  P10v2 project_days={pr10} zero_slack="
          f"{sorted(n for n in sl10 if sl10[n] == 0)}")
    print()


def main():
    validate_code()
    validate_reason()
    validate_plan()
    validate_v2()
    print(f"tasks: {len(T.TASKS)}")
    if FAILS:
        print(f"\n{len(FAILS)} VALIDATION FAILURES")
        sys.exit(1)
    print("\nsuite validated: every reference solution passes, every ground truth "
          "recomputed, every plan grader accepts good and rejects near-misses.")


if __name__ == "__main__":
    main()
