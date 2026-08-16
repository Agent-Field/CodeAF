#!/usr/bin/env python3
"""The 36-task suite and its graders.

Three work-classes, twelve tasks each, every one graded by code with no LLM
judge anywhere in the loop:

  plan  -- strict JSON against a schema plus semantic checks (acyclicity,
           sums, critical paths, exclusion constraints)
  code  -- a self-contained Python function run against hidden unit tests in a
           subprocess with a hard timeout
  reason-- exact / normalised match against a single verified answer

Each task carries `level`, the difficulty the suite was *designed* to hit
(1 = every model should pass, 5 = most models should fail). `level` is an
intent, not a measurement -- the empirical difficulty is what the IRT fit
recovers, and the two are compared in the report.

Graders return (passed: bool, reason: str). The reason string is recorded for
every cell so a failure can be diagnosed without re-running the call.
"""
import json
import os
import re
import subprocess
import sys
import tempfile

# ---------------------------------------------------------------------------
# answer extraction
# ---------------------------------------------------------------------------

_FENCE = re.compile(r"```(?:python|py)?\s*\n(.*?)```", re.S | re.I)


def extract_json(text):
    """Parse a JSON object out of a reply. json_object mode makes this a plain
    json.loads almost always; the fallbacks cover a model that fences it or
    prefixes prose anyway."""
    text = (text or "").strip()
    if not text:
        return None
    try:
        v = json.loads(text)
        return v if isinstance(v, dict) else None
    except Exception:
        pass
    m = _FENCE.search(text)
    if m:
        try:
            v = json.loads(m.group(1))
            return v if isinstance(v, dict) else None
        except Exception:
            pass
    # balanced-brace scan for the first complete object
    start = text.find("{")
    while start != -1:
        depth, instr, esc = 0, False, False
        for i in range(start, len(text)):
            c = text[i]
            if esc:
                esc = False
                continue
            if c == "\\":
                esc = True
                continue
            if c == '"':
                instr = not instr
                continue
            if instr:
                continue
            if c == "{":
                depth += 1
            elif c == "}":
                depth -= 1
                if depth == 0:
                    try:
                        v = json.loads(text[start:i + 1])
                        if isinstance(v, dict):
                            return v
                    except Exception:
                        pass
                    break
        start = text.find("{", start + 1)
    return None


def extract_code(text):
    """Last fenced python block, else the whole reply."""
    blocks = _FENCE.findall(text or "")
    if blocks:
        return blocks[-1]
    return text or ""


_ANSWER = re.compile(r"ANSWER\s*[:\-]\s*(.+?)\s*$", re.I | re.M)


def extract_answer(text):
    """Last `ANSWER: x` line, else the last non-empty line."""
    hits = _ANSWER.findall(text or "")
    if hits:
        return hits[-1].strip()
    lines = [ln.strip() for ln in (text or "").splitlines() if ln.strip()]
    return lines[-1] if lines else ""


def norm(s):
    """Normalise a scalar answer: strip formatting noise that is not the
    answer -- markdown emphasis, thousands separators, trailing punctuation,
    surrounding quotes, case."""
    s = (s or "").strip()
    s = re.sub(r"[*_`]", "", s)
    s = s.strip().strip('"').strip("'").strip()
    s = re.sub(r"[.,;]+$", "", s)
    s = s.replace(",", "") if re.fullmatch(r"[\d,]+(\.\d+)?", s) else s
    return s.strip().lower()


def num_eq(got, want, tol=1e-9):
    try:
        return abs(float(norm(got)) - float(want)) <= tol
    except Exception:
        return False


# ---------------------------------------------------------------------------
# graph helpers for the plan class
# ---------------------------------------------------------------------------

def acyclic(deps):
    """deps: {node: [prereqs]}. True when the graph has no cycle."""
    colour = {}

    def visit(n):
        c = colour.get(n, 0)
        if c == 1:
            return False
        if c == 2:
            return True
        colour[n] = 1
        for p in deps.get(n, []):
            if p in deps and not visit(p):
                return False
        colour[n] = 2
        return True

    return all(visit(n) for n in deps)


def ancestors(deps, n, seen=None):
    seen = set() if seen is None else seen
    for p in deps.get(n, []):
        if p not in seen:
            seen.add(p)
            ancestors(deps, p, seen)
    return seen


def valid_topo(order, deps):
    """order lists nodes so that every prerequisite appears before its node."""
    if sorted(order) != sorted(deps):
        return False
    pos = {n: i for i, n in enumerate(order)}
    return all(pos[p] < pos[n] for n in deps for p in deps[n] if p in pos)


def longest_path_len(deps, weights):
    """Length of the heaviest chain, weights keyed by node."""
    memo = {}

    def f(n):
        if n in memo:
            return memo[n]
        best = max((f(p) for p in deps.get(n, []) if p in deps), default=0)
        memo[n] = best + weights.get(n, 0)
        return memo[n]

    return max((f(n) for n in deps), default=0)


def as_list(v):
    return v if isinstance(v, list) else []


def ids_of(items, key="id"):
    out = []
    for it in items if isinstance(items, list) else []:
        if isinstance(it, dict) and isinstance(it.get(key), str):
            out.append(it[key])
    return out


# ---------------------------------------------------------------------------
# code sandbox
# ---------------------------------------------------------------------------

CODE_TIMEOUT_S = 15


def run_code(model_code, tests, entry):
    """Run the model's code plus hidden tests in a subprocess.

    The harness prints PASS/FAIL and nothing else, so a model that decorates
    its solution with prints cannot be mistaken for a passing one. No network
    is used by any test; the subprocess gets an empty stdin and a hard timeout
    so an accidental infinite loop costs 15 seconds, not the run.
    """
    prog = (
        "import sys\n"
        "sys.setrecursionlimit(20000)\n"
        "# ---- model code ----\n"
        f"{model_code}\n"
        "# ---- hidden tests ----\n"
        "def _main():\n"
        f"    if '{entry}' not in globals():\n"
        f"        print('__RL_FAIL__ missing function {entry}'); return\n"
        "    try:\n"
        + "".join(f"        {ln}\n" for ln in tests.strip().splitlines())
        + "    except AssertionError as e:\n"
        "        print('__RL_FAIL__ assertion: ' + str(e)[:200]); return\n"
        "    except Exception as e:\n"
        "        print('__RL_FAIL__ ' + type(e).__name__ + ': ' + str(e)[:200]); return\n"
        "    print('__RL_PASS__')\n"
        "_main()\n"
    )
    with tempfile.TemporaryDirectory() as d:
        path = os.path.join(d, "cell.py")
        with open(path, "w") as f:
            f.write(prog)
        try:
            r = subprocess.run([sys.executable, "-I", path], capture_output=True,
                               text=True, timeout=CODE_TIMEOUT_S, cwd=d,
                               stdin=subprocess.DEVNULL)
        except subprocess.TimeoutExpired:
            return False, "timeout"
        out = r.stdout or ""
        if "__RL_PASS__" in out:
            return True, "ok"
        m = re.search(r"__RL_FAIL__ (.*)", out)
        if m:
            return False, m.group(1)[:200]
        err = (r.stderr or "").strip().splitlines()
        return False, ("syntax/import: " + err[-1][:200]) if err else "no output"


# ---------------------------------------------------------------------------
# prompt preambles
# ---------------------------------------------------------------------------

SYS_PLAN = ("You are a planning engine. You reply with a single JSON object and "
            "nothing else: no prose, no markdown fence, no commentary. Every "
            "constraint in the request is hard.")
SYS_CODE = ("You are a Python engineer. Reply with exactly one ```python code "
            "block containing the complete function and any helpers it needs. "
            "No prose outside the block. Do not include tests, examples, or "
            "input() calls.")
SYS_REASON = ("You solve the problem exactly. You may think briefly, but the "
              "final line of your reply must be `ANSWER: <value>` and nothing "
              "else on that line.")


# ---------------------------------------------------------------------------
# PLAN tasks
# ---------------------------------------------------------------------------

def _p01(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    ts = as_list(d.get("tasks"))
    if len(ts) != 3:
        return False, f"want 3 tasks, got {len(ts)}"
    by = {x.get("id"): x for x in ts if isinstance(x, dict)}
    if set(by) != {"t1", "t2", "t3"}:
        return False, f"ids {sorted(by)}"
    if set(as_list(by["t3"].get("depends_on"))) != {"t1", "t2"}:
        return False, "t3 deps wrong"
    if as_list(by["t1"].get("depends_on")) or as_list(by["t2"].get("depends_on")):
        return False, "t1/t2 must have no deps"
    if not all(isinstance(by[i].get("title"), str) and by[i]["title"] for i in by):
        return False, "missing title"
    return True, "ok"


def _p02(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    a = d.get("allocations")
    if not isinstance(a, dict) or set(a) != {"eng", "design", "qa"}:
        return False, f"keys {sorted(a) if isinstance(a, dict) else a}"
    if not all(isinstance(v, int) and not isinstance(v, bool) and v > 0
               for v in a.values()):
        return False, "values must be positive integers"
    if sum(a.values()) != 100:
        return False, f"sum {sum(a.values())} != 100"
    if a["eng"] < 50:
        return False, f"eng {a['eng']} < 50"
    return True, "ok"


_P03_TRUTH = {"fruit": {"mango", "plum", "fig"}, "vegetable": {"leek", "turnip"}}


def _p03(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    b = d.get("buckets")
    if not isinstance(b, dict) or set(b) != {"fruit", "vegetable"}:
        return False, "buckets keys wrong"
    got = {k: {str(x).strip().lower() for x in as_list(v)} for k, v in b.items()}
    if got != _P03_TRUTH:
        return False, f"got {got}"
    return True, "ok"


def _p04(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    nodes = as_list(d.get("nodes"))
    deps = {}
    for n in nodes:
        if not isinstance(n, dict) or not isinstance(n.get("id"), str):
            return False, "node without id"
        deps[n["id"]] = [str(x) for x in as_list(n.get("depends_on"))]
    want = {"survey", "schema", "ingest", "dashboard", "signoff"}
    if set(deps) != want:
        return False, f"nodes {sorted(deps)}"
    if not acyclic(deps):
        return False, "cyclic"
    req = {"schema": {"survey"}, "ingest": {"schema"},
           "dashboard": {"ingest"}, "signoff": {"dashboard"}}
    for n, need in req.items():
        if not need <= set(deps[n]):
            return False, f"{n} missing deps {need - set(deps[n])}"
    if deps["survey"]:
        return False, "survey must have no deps"
    if not valid_topo([str(x) for x in as_list(d.get("topo_order"))], deps):
        return False, "topo_order invalid"
    return True, "ok"


_P05_IDS = {"login", "signup", "reset", "profile", "settings", "logout"}


def _p05(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    e = d.get("estimates")
    if not isinstance(e, dict) or set(e) != _P05_IDS:
        return False, f"keys {sorted(e) if isinstance(e, dict) else e}"
    if not all(v in (1, 2, 3, 5, 8) for v in e.values()):
        return False, f"values off the scale: {e}"
    if sum(e.values()) != 21:
        return False, f"total {sum(e.values())} != 21"
    return True, "ok"


def _p06(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    ph = as_list(d.get("phases"))
    if len(ph) != 2:
        return False, f"{len(ph)} phases"
    ntask = nsub = 0
    for p in ph:
        if not isinstance(p, dict) or not p.get("name"):
            return False, "phase without name"
        ts = as_list(p.get("tasks"))
        if len(ts) != 2:
            return False, f"phase {p.get('name')} has {len(ts)} tasks"
        ntask += len(ts)
        for x in ts:
            if not isinstance(x, dict) or not x.get("name"):
                return False, "task without name"
            sub = as_list(x.get("subtasks"))
            if len(sub) != 3:
                return False, f"task {x.get('name')} has {len(sub)} subtasks"
            if not all(isinstance(s, str) and s.strip() for s in sub):
                return False, "subtasks must be non-empty strings"
            nsub += len(sub)
    c = d.get("counts")
    if not isinstance(c, dict):
        return False, "no counts object"
    if (c.get("phases"), c.get("tasks"), c.get("subtasks")) != (2, ntask, nsub):
        return False, f"counts {c} != (2,{ntask},{nsub})"
    return True, "ok"


def _p07(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    nodes = as_list(d.get("nodes"))
    deps, dur = {}, {}
    for n in nodes:
        if not isinstance(n, dict) or not isinstance(n.get("id"), str):
            return False, "node without id"
        deps[n["id"]] = [str(x) for x in as_list(n.get("depends_on"))]
        if not isinstance(n.get("days"), int) or isinstance(n.get("days"), bool):
            return False, f"{n['id']} days not an int"
        dur[n["id"]] = n["days"]
    want = {"spec": 2, "impl": 5, "tests": 3, "docs": 4, "deploy": 1}
    if set(deps) != set(want):
        return False, f"nodes {sorted(deps)}"
    if dur != want:
        return False, f"days {dur} != {want}"
    if not acyclic(deps):
        return False, "cyclic"
    if "docs" in ancestors(deps, "deploy"):
        return False, "deploy transitively depends on docs (forbidden)"
    if not {"spec"} <= set(deps["impl"]) or not {"impl"} <= set(deps["tests"]):
        return False, "impl<-spec / tests<-impl missing"
    if not {"spec"} <= set(deps["docs"]):
        return False, "docs<-spec missing"
    if "tests" not in ancestors(deps, "deploy"):
        return False, "deploy must depend on tests"
    if deps["spec"]:
        return False, "spec must have no deps"
    want_cp = longest_path_len(deps, dur)
    if d.get("critical_path_days") != want_cp:
        return False, f"critical_path_days {d.get('critical_path_days')} != {want_cp}"
    return True, "ok"


def _p08(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    a = d.get("allocations")
    order = ["platform", "product", "growth", "support"]
    if not isinstance(a, dict) or set(a) != set(order):
        return False, f"keys {sorted(a) if isinstance(a, dict) else a}"
    v = [a[k] for k in order]
    if not all(isinstance(x, int) and not isinstance(x, bool) for x in v):
        return False, "values must be integers"
    if sum(v) != 1000:
        return False, f"sum {sum(v)} != 1000"
    if not all(v[i] > v[i + 1] for i in range(3)):
        return False, f"not strictly decreasing: {v}"
    if any(x % 25 for x in v):
        return False, f"not all multiples of 25: {v}"
    if v[3] < 100:
        return False, f"support {v[3]} < 100"
    return True, "ok"


def _p09(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    ph = as_list(d.get("phases"))
    if len(ph) != 3:
        return False, f"{len(ph)} phases"
    total = 0
    for p in ph:
        if not isinstance(p, dict):
            return False, "phase not an object"
        ts = as_list(p.get("tasks"))
        if len(ts) < 2:
            return False, f"phase {p.get('name')} has {len(ts)} tasks (need >=2)"
        psum = 0
        for x in ts:
            if not isinstance(x, dict):
                return False, "task not an object"
            sub = as_list(x.get("subtasks"))
            if len(sub) < 2:
                return False, f"task {x.get('name')} has {len(sub)} subtasks (need >=2)"
            ssum = 0
            for s in sub:
                if not isinstance(s, dict) or not isinstance(s.get("hours"), int) \
                        or isinstance(s.get("hours"), bool):
                    return False, "subtask hours must be an int"
                h = s["hours"]
                if h < 4 or h % 4:
                    return False, f"subtask hours {h} not a multiple of 4 >= 4"
                ssum += h
            if x.get("hours") != ssum:
                return False, f"task hours {x.get('hours')} != subtask sum {ssum}"
            psum += ssum
        if p.get("hours") != psum:
            return False, f"phase hours {p.get('hours')} != task sum {psum}"
        total += psum
    if d.get("total_hours") != total:
        return False, f"total_hours {d.get('total_hours')} != {total}"
    if total != 240:
        return False, f"grand total {total} != 240"
    return True, "ok"


def _p10(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    nodes = as_list(d.get("nodes"))
    deps, dur = {}, {}
    for n in nodes:
        if not isinstance(n, dict) or not isinstance(n.get("id"), str):
            return False, "node without id"
        deps[n["id"]] = [str(x) for x in as_list(n.get("depends_on"))]
        dur[n["id"]] = n.get("days")
    want_days = {"A": 3, "B": 2, "C": 4, "D": 1, "review": 2}
    if set(deps) != set(want_days):
        return False, f"nodes {sorted(deps)} (must add 'review')"
    if dur != want_days:
        return False, f"days {dur} != {want_days}"
    want_deps = {"A": set(), "B": {"A"}, "C": {"A"}, "D": {"B", "C"}, "review": {"D"}}
    got_deps = {k: set(v) for k, v in deps.items()}
    if got_deps != want_deps:
        return False, f"deps {got_deps} != {want_deps}"
    if d.get("critical_path_days") != 10:
        return False, f"critical_path_days {d.get('critical_path_days')} != 10"
    if not valid_topo([str(x) for x in as_list(d.get("topo_order"))], deps):
        return False, "topo_order invalid"
    return True, "ok"


_P11_COST = {"c1": 40, "c2": 55, "c3": 30, "c4": 70, "c5": 25,
             "c6": 60, "c7": 45, "c8": 35, "c9": 50}
_P11_GROUP_A = {"c2", "c4", "c6", "c9"}
_P11_EXCL = [("c1", "c7"), ("c3", "c8"), ("c4", "c6")]
# 210 leaves 8 feasible selections out of C(9,5)=126. A 200 budget admits
# exactly one, which would make the task all-fail and therefore worthless to
# the IRT fit -- an item nobody passes carries no information about ability.
_P11_BUDGET = 210


def _p11(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    sel = [str(x) for x in as_list(d.get("selected"))]
    rej = [str(x) for x in as_list(d.get("rejected"))]
    if len(sel) != 5:
        return False, f"selected has {len(sel)}, need exactly 5"
    if len(set(sel)) != 5:
        return False, "selected has duplicates"
    if set(sel) | set(rej) != set(_P11_COST) or set(sel) & set(rej):
        return False, "selected+rejected must partition the 9 candidates"
    if sel != sorted(sel):
        return False, f"selected not sorted ascending: {sel}"
    for a, b in _P11_EXCL:
        if a in sel and b in sel:
            return False, f"mutually exclusive pair ({a},{b}) both selected"
    na = len(set(sel) & _P11_GROUP_A)
    if na < 2:
        return False, f"only {na} from group A, need >=2"
    cost = sum(_P11_COST[c] for c in sel)
    if cost > _P11_BUDGET:
        return False, f"cost {cost} > {_P11_BUDGET}"
    if d.get("total_cost") != cost:
        return False, f"total_cost {d.get('total_cost')} != {cost}"
    return True, "ok"


_P12_TRUTH = [
    {"id": "T-1", "priority": "high", "owner": "rita"},
    {"id": "T-2", "priority": "low", "owner": None},
    {"id": "T-3", "priority": "medium", "owner": "dev"},
    {"id": "T-4", "priority": "high", "owner": None},
]


def _p12(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    items = as_list(d.get("items"))
    if len(items) != 4:
        return False, f"{len(items)} items, need 4"
    got = []
    for x in items:
        if not isinstance(x, dict):
            return False, "item not an object"
        o = x.get("owner")
        got.append({
            "id": str(x.get("id", "")).strip(),
            "priority": str(x.get("priority", "")).strip().lower(),
            "owner": None if o is None else str(o).strip().lower(),
        })
    got.sort(key=lambda r: r["id"])
    if got != _P12_TRUTH:
        return False, f"got {got}"
    return True, "ok"


PLAN = [
    ("P01", 1, "Break 'make a cup of pour-over coffee' into exactly three tasks.\n"
     "Return JSON: {\"tasks\": [{\"id\": str, \"title\": str, \"depends_on\": [str]}]}\n"
     "Rules: use exactly the ids t1, t2, t3. t1 is grinding the beans and t2 is "
     "boiling the water; neither depends on anything. t3 is the pour and depends "
     "on both t1 and t2. Every task needs a non-empty title.", _p01),
    ("P02", 1, "Split a 100-point budget across three functions.\n"
     "Return JSON: {\"allocations\": {\"eng\": int, \"design\": int, \"qa\": int}}\n"
     "Rules: the three values are positive integers, they sum to exactly 100, and "
     "eng is at least 50.", _p02),
    ("P03", 1, "Sort these five items into buckets: mango, leek, plum, turnip, fig.\n"
     "Return JSON: {\"buckets\": {\"fruit\": [str], \"vegetable\": [str]}}\n"
     "Every item appears exactly once, lowercase, in one bucket.", _p03),
    ("P04", 2, "Plan a customer-analytics rollout with exactly these five nodes: "
     "survey, schema, ingest, dashboard, signoff.\n"
     "Return JSON: {\"nodes\": [{\"id\": str, \"depends_on\": [str]}], "
     "\"topo_order\": [str]}\n"
     "Rules: survey depends on nothing. schema depends on survey. ingest depends "
     "on schema. dashboard depends on ingest. signoff depends on dashboard. The "
     "graph must be acyclic and topo_order must be a valid topological ordering "
     "of all five nodes.", _p04),
    ("P05", 2, "Estimate six backlog items in story points: login, signup, reset, "
     "profile, settings, logout.\n"
     "Return JSON: {\"estimates\": {\"<item>\": int, ...}} with all six items as keys.\n"
     "Rules: every value must come from the Fibonacci scale {1, 2, 3, 5, 8}, and "
     "the six values must sum to exactly 21.", _p05),
    ("P06", 2, "Draft a two-phase migration plan.\n"
     "Return JSON: {\"phases\": [{\"name\": str, \"tasks\": [{\"name\": str, "
     "\"subtasks\": [str]}]}], \"counts\": {\"phases\": int, \"tasks\": int, "
     "\"subtasks\": int}}\n"
     "Rules: exactly 2 phases; each phase has exactly 2 tasks; each task has "
     "exactly 3 subtasks (non-empty strings). The counts object must report the "
     "true totals across the whole plan.", _p06),
    ("P07", 3, "Plan a release with exactly five nodes and these fixed durations "
     "in days: spec=2, impl=5, tests=3, docs=4, deploy=1.\n"
     "Return JSON: {\"nodes\": [{\"id\": str, \"days\": int, \"depends_on\": "
     "[str]}], \"critical_path_days\": int}\n"
     "Rules: spec depends on nothing. impl depends on spec. tests depends on "
     "impl. docs depends on spec. deploy must depend on tests. HARD CONSTRAINT: "
     "deploy must NOT depend on docs, directly or transitively -- documentation "
     "ships after the deploy. The graph must be acyclic. critical_path_days is "
     "the total duration of the heaviest dependency chain in the whole graph "
     "(sum of days along that chain, including both endpoints).", _p07),
    ("P08", 3, "Allocate a 1000-unit budget across four teams: platform, product, "
     "growth, support.\n"
     "Return JSON: {\"allocations\": {\"platform\": int, \"product\": int, "
     "\"growth\": int, \"support\": int}}\n"
     "Rules, all hard: the four integers sum to exactly 1000; "
     "platform > product > growth > support strictly; every value is a multiple "
     "of 25; support is at least 100.", _p08),
    ("P09", 4, "Produce a three-phase engineering plan with a fully consistent "
     "hour rollup.\n"
     "Return JSON: {\"phases\": [{\"name\": str, \"hours\": int, \"tasks\": "
     "[{\"name\": str, \"hours\": int, \"subtasks\": [{\"name\": str, "
     "\"hours\": int}]}]}], \"total_hours\": int}\n"
     "Rules, all hard: exactly 3 phases; each phase has at least 2 tasks; each "
     "task has at least 2 subtasks; every subtask's hours is a multiple of 4 and "
     "at least 4; each task's hours equals the sum of its subtasks' hours; each "
     "phase's hours equals the sum of its tasks' hours; total_hours equals the "
     "sum of the phase hours; and total_hours is exactly 240.", _p09),
    ("P10", 5, "You are given four tasks with durations in days: A=3, B=2, C=4, "
     "D=1. Dependencies: B depends on A; C depends on A; D depends on both B and "
     "C; A depends on nothing.\n"
     "Every plan in this system must end with a sign-off step, so you must ADD "
     "one node with id 'review', days=2, depending only on D, with nothing "
     "depending on it. Do not change any other node or dependency.\n"
     "Return JSON: {\"nodes\": [{\"id\": str, \"days\": int, \"depends_on\": "
     "[str]}], \"critical_path_days\": int, \"topo_order\": [str]}\n"
     "critical_path_days is the total days along the heaviest chain of the final "
     "five-node graph. topo_order must be a valid topological ordering of all "
     "five nodes.", _p10),
    ("P11", 5, "Select work items under constraints.\n"
     "Candidates and their costs: c1=40, c2=55, c3=30, c4=70, c5=25, c6=60, "
     "c7=45, c8=35, c9=50. Group A is {c2, c4, c6, c9}.\n"
     "Return JSON: {\"selected\": [str], \"rejected\": [str], \"total_cost\": int}\n"
     "Rules, all hard: select exactly 5 candidates; selected and rejected "
     "together must contain all 9 ids exactly once; selected must be sorted in "
     "ascending order; you may not select both members of any of these mutually "
     "exclusive pairs: (c1,c7), (c3,c8), (c4,c6); at least 2 of the selected must "
     "be from group A; the total cost of the selected must not exceed 210; and "
     "total_cost must equal that sum.", _p11),
    ("P12", 3, "Extract structured tickets from this log. Priority must be one of "
     "exactly \"low\", \"medium\", \"high\". Owner must be the person's first name "
     "in lowercase, or JSON null when the log does not name one -- never the "
     "string \"unknown\", never an empty string, never a team name.\n\n"
     "  T-1: production outage, Rita is on it. Drop everything.\n"
     "  T-2: someone should retitle the FAQ page eventually. No rush, nobody "
     "assigned.\n"
     "  T-3: flaky integration test, Dev picked it up, fix this sprint.\n"
     "  T-4: customer data is leaking into logs. Critical. The platform team is "
     "aware but no individual has taken it.\n\n"
     "Return JSON: {\"items\": [{\"id\": str, \"priority\": str, \"owner\": "
     "str|null}]} with all four tickets.", _p12),
]


# ---------------------------------------------------------------------------
# CODE tasks -- (id, level, signature+spec, entry, hidden tests)
# ---------------------------------------------------------------------------

CODE = [
    ("C01", 1,
     "def reverse_words(s: str) -> str\n"
     "Return the whitespace-separated words of s in reverse order, joined by a "
     "single space. Runs of whitespace collapse. An empty or whitespace-only "
     "string returns ''.",
     "reverse_words", """
assert reverse_words("hello world") == "world hello"
assert reverse_words("a b c d") == "d c b a"
assert reverse_words("  padded   out  ") == "out padded"
assert reverse_words("single") == "single"
assert reverse_words("") == ""
assert reverse_words("   ") == ""
"""),
    ("C02", 1,
     "def count_vowels(s: str) -> int\n"
     "Count the vowels a, e, i, o, u in s, case-insensitively. 'y' is not a vowel.",
     "count_vowels", """
assert count_vowels("hello") == 2
assert count_vowels("AEIOU") == 5
assert count_vowels("rhythm") == 0
assert count_vowels("") == 0
assert count_vowels("Programming Yearly") == 5
"""),
    ("C03", 1,
     "def fizz_list(n: int) -> list[str]\n"
     "Return a list of length n for the integers 1..n: 'Fizz' when divisible by "
     "3, 'Buzz' when divisible by 5, 'FizzBuzz' when divisible by both, "
     "otherwise the decimal string of the number. n <= 0 returns [].",
     "fizz_list", """
assert fizz_list(5) == ["1", "2", "Fizz", "4", "Buzz"]
assert fizz_list(15)[-1] == "FizzBuzz"
assert fizz_list(0) == []
assert fizz_list(-3) == []
assert len(fizz_list(100)) == 100
assert fizz_list(3)[2] == "Fizz"
"""),
    ("C04", 2,
     "def rle(s: str) -> str\n"
     "Run-length encode s: each maximal run of one character becomes the "
     "character followed by the run length as a decimal string, including runs "
     "of length 1 (so 'abb' -> 'a1b2'). Empty string returns ''.",
     "rle", """
assert rle("aaabbc") == "a3b2c1"
assert rle("abb") == "a1b2"
assert rle("") == ""
assert rle("x") == "x1"
assert rle("aabbaa") == "a2b2a2"
assert rle("a" * 12) == "a12"
"""),
    ("C05", 2,
     "def merge_intervals(intervals: list[list[int]]) -> list[list[int]]\n"
     "Merge all overlapping or touching closed intervals and return them sorted "
     "by start. [1,3] and [3,5] touch and merge into [1,5]. Input may be "
     "unsorted. Empty input returns [].",
     "merge_intervals", """
assert merge_intervals([[1,3],[2,6],[8,10],[15,18]]) == [[1,6],[8,10],[15,18]]
assert merge_intervals([[1,4],[4,5]]) == [[1,5]]
assert merge_intervals([]) == []
assert merge_intervals([[5,7],[1,3]]) == [[1,3],[5,7]]
assert merge_intervals([[1,10],[2,3],[4,5]]) == [[1,10]]
assert merge_intervals([[1,1]]) == [[1,1]]
"""),
    ("C06", 2,
     "def balanced(s: str) -> bool\n"
     "Return True when the brackets (), [], {} in s are correctly balanced and "
     "nested. Characters that are not brackets are ignored. '' is balanced.",
     "balanced", """
assert balanced("([]{})") is True
assert balanced("([)]") is False
assert balanced("") is True
assert balanced("a(b[c]{d})e") is True
assert balanced("(") is False
assert balanced(")(") is False
assert balanced("{[()()]}") is True
"""),
    ("C07", 2,
     "def longest_common_prefix(strs: list[str]) -> str\n"
     "Return the longest string that prefixes every element of strs. Returns '' "
     "for an empty list or when there is no common prefix.",
     "longest_common_prefix", """
assert longest_common_prefix(["flower","flow","flight"]) == "fl"
assert longest_common_prefix(["dog","racecar","car"]) == ""
assert longest_common_prefix([]) == ""
assert longest_common_prefix(["single"]) == "single"
assert longest_common_prefix(["abc",""]) == ""
assert longest_common_prefix(["same","same"]) == "same"
"""),
    ("C08", 3,
     "def min_coins(coins: list[int], target: int) -> int\n"
     "Return the fewest coins from `coins` (each usable unlimited times) summing "
     "exactly to target, or -1 when impossible. target may be 0 (answer 0). "
     "Must handle target up to 10000 without timing out.",
     "min_coins", """
assert min_coins([1,2,5], 11) == 3
assert min_coins([2], 3) == -1
assert min_coins([1], 0) == 0
assert min_coins([], 0) == 0
assert min_coins([], 5) == -1
assert min_coins([3,7], 5) == -1
assert min_coins([186,419,83,408], 6249) == 20
assert min_coins([1,5,10,25], 9999) == 405
"""),
    ("C09", 3,
     "def weighted_edit(a: str, b: str) -> int\n"
     "Minimum total cost to turn a into b where inserting a character costs 1, "
     "deleting a character costs 2, and substituting one character for a "
     "different one costs 3. Matching characters cost 0.",
     "weighted_edit", """
assert weighted_edit("", "") == 0
assert weighted_edit("", "abc") == 3
assert weighted_edit("abc", "") == 6
assert weighted_edit("abc", "abc") == 0
assert weighted_edit("a", "b") == 3
assert weighted_edit("ab", "ba") == 3
assert weighted_edit("kitten", "sitting") == 7
assert weighted_edit("sunday", "saturday") == 5
"""),
    ("C10", 3,
     "def search_rotated(nums: list[int], target: int) -> int\n"
     "nums is a sorted ascending array of DISTINCT integers rotated left by some "
     "unknown amount (possibly zero). Return the index of target, or -1 if "
     "absent. Must run in O(log n).",
     "search_rotated", """
assert search_rotated([4,5,6,7,0,1,2], 0) == 4
assert search_rotated([4,5,6,7,0,1,2], 3) == -1
assert search_rotated([1], 0) == -1
assert search_rotated([1], 1) == 0
assert search_rotated([], 5) == -1
assert search_rotated([3,1], 1) == 1
assert search_rotated([5,1,3], 3) == 2
assert search_rotated([1,2,3,4,5], 5) == 4
assert search_rotated([2,3,4,5,1], 1) == 4
"""),
    ("C11", 4,
     "def count_subarrays(nums: list[int], target: int) -> int\n"
     "Count the CONTIGUOUS subarrays of nums whose elements sum to exactly "
     "target. nums may contain negative numbers and zeros, so a sliding window "
     "is not correct. Must handle len(nums) up to 20000 without timing out.",
     "count_subarrays", """
assert count_subarrays([1,1,1], 2) == 2
assert count_subarrays([1,2,3], 3) == 2
assert count_subarrays([], 0) == 0
assert count_subarrays([0,0,0], 0) == 6
assert count_subarrays([1,-1,0], 0) == 3
assert count_subarrays([-1,-1,1], 0) == 1
assert count_subarrays([3,4,7,2,-3,1,4,2], 7) == 4
assert count_subarrays([1] * 20000, 3) == 19998
"""),
    ("C12", 5,
     "def max_sum_k_no_adjacent(nums: list[int], k: int) -> int | None\n"
     "Return the maximum possible sum of EXACTLY k elements of nums such that no "
     "two chosen elements are adjacent in the list. Return None when no such "
     "selection exists. nums may contain negative numbers, and you must still "
     "pick exactly k. k may be 0 (sum 0). Must handle len(nums) up to 400 and "
     "k up to 200 without timing out.",
     "max_sum_k_no_adjacent", """
assert max_sum_k_no_adjacent([1,2,3,4,5], 2) == 8
assert max_sum_k_no_adjacent([1,2,3,4,5], 3) == 9
assert max_sum_k_no_adjacent([1,2,3], 2) == 4
assert max_sum_k_no_adjacent([1,2,3], 3) is None
assert max_sum_k_no_adjacent([], 0) == 0
assert max_sum_k_no_adjacent([], 1) is None
assert max_sum_k_no_adjacent([5], 1) == 5
assert max_sum_k_no_adjacent([-5,-1,-5], 1) == -1
assert max_sum_k_no_adjacent([-5,-1,-5], 2) == -10
assert max_sum_k_no_adjacent([4,-1,4,-1,4], 3) == 12
assert max_sum_k_no_adjacent(list(range(400)), 200) == sum(range(1, 400, 2))
"""),
]


# ---------------------------------------------------------------------------
# REASON tasks -- (id, level, prompt, expected, kind)
#   kind: "num" numeric equality, "str" normalised string equality
# ---------------------------------------------------------------------------

REASON = [
    ("R01", 1, "A shelf holds 3 boxes with 12 pens each. You give away 7 pens. "
     "How many pens remain? Answer with the number only.", 29, "num"),
    ("R02", 1, "From this line, extract the total charged: \"Invoice 8812 -- "
     "subtotal 38.00 USD, shipping 4.50 USD, total 42.50 USD, due on the 30th.\" "
     "Answer with the number only, no currency symbol.", 42.50, "num"),
    ("R03", 1, "How many words are in this sentence: \"the quick brown fox jumps "
     "over the lazy dog\"? Answer with the number only.", 9, "num"),
    ("R04", 2, "An item costs 80. Its price is raised by 25%, and then the new "
     "price is reduced by 20%. What is the final price? Answer with the number "
     "only.", 80, "num"),
    ("R05", 2, "A tank holds 3 gallons. 1 gallon is 3.785 litres. A pump moves "
     "250 millilitres per second. How many whole seconds to fill the empty tank? "
     "Round UP to the next whole second. Answer with the number only.",
     46, "num"),
    ("R06", 2, "What date is 45 days after 2024-02-15? Answer in YYYY-MM-DD "
     "format only.", "2024-03-31", "str"),
    ("R07", 3, "A shop sold 340 units in March at 18.00 each. In April it sold "
     "15% fewer units, and it had raised the price by 2.50. The rent is 1200 a "
     "month, which you should ignore. What was April's revenue? Round to exactly "
     "2 decimal places. Answer with the number only.", 5924.50, "num"),
    ("R08", 3, "Four colleagues -- Ana, Ben, Cleo, Dan -- each drink exactly one "
     "of: tea, coffee, water, juice, all different.\n"
     "1. Ana does not drink coffee or water.\n"
     "2. The juice drinker sits next to Ben in the order Ana, Ben, Cleo, Dan.\n"
     "3. Cleo drinks water.\n"
     "4. Dan does not drink juice.\n"
     "Who drinks juice? Answer with the single name only.", "ana", "str"),
    ("R09", 3, "Take the phrase: solar wind mapping project.\n"
     "Apply these steps in order: (1) drop every word shorter than 5 letters; "
     "(2) reverse the order of the words that remain; (3) uppercase them; "
     "(4) join them with a single hyphen and no spaces.\n"
     "Answer with the resulting string only.", "PROJECT-MAPPING-SOLAR", "str"),
    ("R10", 4, "How many 4-digit positive integers have all distinct digits, do "
     "not start with 0, and are divisible by 5? Answer with the number only.",
     952, "num"),
    ("R11", 4, "What are the last two digits of 7 raised to the power 2024? "
     "Answer as a two-character string, including a leading zero if needed.",
     "01", "str"),
    ("R12", 5, "Five servers s1..s5 are being retired one per week, weeks 1 to 5, "
     "in some order.\n"
     "1. s3 is retired at some point before s1.\n"
     "2. s5 is retired exactly two weeks after s2.\n"
     "3. s4 is retired in week 1 or week 5.\n"
     "4. s1 is not retired in week 5.\n"
     "5. s2 is not retired in week 1.\n"
     "6. s4 is retired after s1.\n"
     "In which week is s5 retired? Answer with the number only.", 4, "num"),
]


# ===========================================================================
# ROUND 2 -- the hardened third
# ===========================================================================
# Round 1 came back near-degenerate: 28 of 36 tasks were passed by every model
# that saw them and the overall pass rate was 93.6%. An item everybody passes
# has zero Fisher information about ability, so most of the matrix was paying
# for nothing and the fit had almost no upper end to work with.
#
# The diagnosis is that the intended difficulty scale was calibrated against
# an older generation of open models. What used to be a hard interview
# question -- coin-change DP, rotated binary search, minimum-window -- is now
# inside the training distribution of a $0.15/M model.
#
# So twelve tasks, the intended-hardest third, are replaced once. The
# replacements are chosen to be hard for a reason that does not decay:
#   * plan  -- real critical-path-method arithmetic (forward AND backward
#              pass, per-node slack) instead of transcribing a dependency list
#   * code  -- problems whose naive solution is correct but too slow, plus
#              tie-breaking and impossibility rules that memorised solutions
#              get wrong
#   * reason-- longer arithmetic chains and larger constraint puzzles
#
# The other 24 tasks keep their round-1 definitions AND their round-1 results;
# only the 80 cells belonging to the hardened twelve are re-run. Both rounds
# are reported.

HARDENED_IDS = ["P07", "P10",
                "C07", "C08", "C09", "C10", "C11", "C12",
                "R07", "R10", "R11", "R12"]


def cpm(deps, dur):
    """Critical path method. Returns (earliest_start, slack, project_days)."""
    es = {}

    def ES(n):
        if n not in es:
            es[n] = max((ES(p) + dur[p] for p in deps[n]), default=0)
        return es[n]

    for n in deps:
        ES(n)
    proj = max(es[n] + dur[n] for n in deps)
    succ = {n: [] for n in deps}
    for n in deps:
        for p in deps[n]:
            succ[p].append(n)
    lf = {}

    def LF(n):
        if n not in lf:
            lf[n] = min((LF(s) - dur[s] for s in succ[n]), default=proj)
        return lf[n]

    for n in deps:
        LF(n)
    slack = {n: lf[n] - dur[n] - es[n] for n in deps}
    return es, slack, proj, succ


_P07V2_DUR = {"spec": 2, "design": 3, "impl": 5, "tests": 3,
              "docs": 4, "security": 2, "stage": 1, "deploy": 1}
_P07V2_DEPS = {"spec": [], "design": ["spec"], "impl": ["design"],
               "tests": ["impl"], "docs": ["design"], "security": ["impl"],
               "stage": ["tests", "security"], "deploy": ["stage"]}


def _p07v2(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    es, slack, proj, succ = cpm(_P07V2_DEPS, _P07V2_DUR)
    nodes = as_list(d.get("nodes"))
    got = {}
    for n in nodes:
        if not isinstance(n, dict) or not isinstance(n.get("id"), str):
            return False, "node without id"
        got[n["id"]] = n
    if set(got) != set(_P07V2_DUR):
        return False, f"nodes {sorted(got)}"
    for nid in _P07V2_DUR:
        n = got[nid]
        if n.get("days") != _P07V2_DUR[nid]:
            return False, f"{nid} days {n.get('days')} != {_P07V2_DUR[nid]}"
        if set(str(x) for x in as_list(n.get("depends_on"))) != set(_P07V2_DEPS[nid]):
            return False, f"{nid} depends_on {n.get('depends_on')} != {_P07V2_DEPS[nid]}"
        if n.get("earliest_start") != es[nid]:
            return False, f"{nid} earliest_start {n.get('earliest_start')} != {es[nid]}"
        if n.get("slack") != slack[nid]:
            return False, f"{nid} slack {n.get('slack')} != {slack[nid]}"
    if d.get("project_days") != proj:
        return False, f"project_days {d.get('project_days')} != {proj}"
    path = [str(x) for x in as_list(d.get("critical_path"))]
    if not path:
        return False, "no critical_path"
    if any(p not in _P07V2_DUR for p in path):
        return False, f"critical_path has unknown nodes: {path}"
    if _P07V2_DEPS[path[0]]:
        return False, f"critical_path must start at a node with no dependencies, got {path[0]}"
    if succ[path[-1]]:
        return False, f"critical_path must end at a node with no successors, got {path[-1]}"
    for a, b in zip(path, path[1:]):
        if a not in _P07V2_DEPS[b]:
            return False, f"critical_path broken: {b} does not depend on {a}"
    if sum(_P07V2_DUR[p] for p in path) != proj:
        return False, f"critical_path length {sum(_P07V2_DUR[p] for p in path)} != {proj}"
    if any(slack[p] for p in path):
        return False, "critical_path contains a node with non-zero slack"
    return True, "ok"


_P10V2_DUR = {"A": 3, "B": 2, "C": 4, "D": 1, "E": 5, "F": 2,
              "audit": 3, "release": 1}
_P10V2_DEPS = {"A": [], "B": ["A"], "C": ["A"], "D": ["B", "C"], "E": ["B"],
               "F": ["D", "E"], "audit": ["F"], "release": ["audit"]}


def _p10v2(t):
    d = extract_json(t)
    if not d:
        return False, "no json"
    es, slack, proj, succ = cpm(_P10V2_DEPS, _P10V2_DUR)
    nodes = as_list(d.get("nodes"))
    got = {}
    for n in nodes:
        if not isinstance(n, dict) or not isinstance(n.get("id"), str):
            return False, "node without id"
        got[n["id"]] = n
    if set(got) != set(_P10V2_DUR):
        return False, f"nodes {sorted(got)} (must add 'audit' and 'release')"
    for nid in _P10V2_DUR:
        n = got[nid]
        if n.get("days") != _P10V2_DUR[nid]:
            return False, f"{nid} days {n.get('days')} != {_P10V2_DUR[nid]}"
        if set(str(x) for x in as_list(n.get("depends_on"))) != set(_P10V2_DEPS[nid]):
            return False, f"{nid} depends_on wrong"
        if n.get("earliest_start") != es[nid]:
            return False, f"{nid} earliest_start {n.get('earliest_start')} != {es[nid]}"
    if d.get("project_days") != proj:
        return False, f"project_days {d.get('project_days')} != {proj}"
    if not valid_topo([str(x) for x in as_list(d.get("topo_order"))], _P10V2_DEPS):
        return False, "topo_order invalid"
    zero = sorted(n for n in slack if slack[n] == 0)
    got_zero = sorted(str(x) for x in as_list(d.get("zero_slack_nodes")))
    if got_zero != zero:
        return False, f"zero_slack_nodes {got_zero} != {zero}"
    return True, "ok"


PLAN_V2 = {
    "P07": (4,
            "Run critical-path method over this fixed eight-node release plan. "
            "Durations in days: spec=2, design=3, impl=5, tests=3, docs=4, "
            "security=2, stage=1, deploy=1. Dependencies: spec depends on "
            "nothing; design on spec; impl on design; tests on impl; docs on "
            "design; security on impl; stage on both tests and security; "
            "deploy on stage.\n"
            "The project starts at day 0. For every node compute "
            "earliest_start (the earliest day it can begin, given its "
            "dependencies) and slack (how many days it can be delayed without "
            "delaying the project end).\n"
            "Return JSON: {\"nodes\": [{\"id\": str, \"days\": int, "
            "\"depends_on\": [str], \"earliest_start\": int, \"slack\": int}], "
            "\"project_days\": int, \"critical_path\": [str]}\n"
            "project_days is the earliest day the whole project can finish. "
            "critical_path is the ordered chain of nodes, from a node with no "
            "dependencies to a node nothing depends on, whose durations sum to "
            "project_days.", _p07v2),
    "P10": (5,
            "You are given six tasks with durations in days: A=3, B=2, C=4, "
            "D=1, E=5, F=2. Dependencies: A depends on nothing; B on A; C on A; "
            "D on both B and C; E on B; F on both D and E.\n"
            "Policy requires every plan to end with an audit then a release, so "
            "you must ADD two nodes: 'audit' with days=3 depending only on F, "
            "and 'release' with days=1 depending only on audit. Change nothing "
            "else.\n"
            "The project starts at day 0. Compute earliest_start for every node "
            "of the final eight-node graph.\n"
            "Return JSON: {\"nodes\": [{\"id\": str, \"days\": int, "
            "\"depends_on\": [str], \"earliest_start\": int}], "
            "\"project_days\": int, \"topo_order\": [str], "
            "\"zero_slack_nodes\": [str]}\n"
            "project_days is the earliest completion day of the whole graph. "
            "zero_slack_nodes lists, sorted ascending as strings, every node "
            "that cannot be delayed at all without pushing out the project end "
            "date.", _p10v2),
}


CODE_V2 = {
"C07": (3,
    "def longest_arith_subseq(nums: list[int]) -> int\n"
    "Return the length of the longest arithmetic SUBSEQUENCE of nums (elements "
    "in order but not necessarily contiguous, with a constant difference "
    "between consecutive chosen elements). Any list of length 1 or 2 is "
    "arithmetic. An empty list returns 0. Must handle len(nums) up to 1000 "
    "without timing out.",
    "longest_arith_subseq", """
assert longest_arith_subseq([3,6,9,12]) == 4
assert longest_arith_subseq([9,4,7,2,10]) == 3
assert longest_arith_subseq([20,1,15,3,10,5,8]) == 4
assert longest_arith_subseq([]) == 0
assert longest_arith_subseq([7]) == 1
assert longest_arith_subseq([7,7]) == 2
assert longest_arith_subseq([1,2,4,8,16]) == 2
assert longest_arith_subseq([5,5,5,5]) == 4
assert longest_arith_subseq(list(range(0, 2000, 2))) == 1000
"""),
"C08": (4,
    "def min_coins_bounded(coins: list[tuple[int, int]], target: int) "
    "-> list[int] | None\n"
    "Each element of coins is (denomination, max_count): that denomination may "
    "be used at most max_count times. Return the multiset of coins, as a list "
    "sorted ascending, that sums to exactly target using the FEWEST coins. When "
    "several minimum-size multisets exist, return the one whose ascending-sorted "
    "list is lexicographically smallest. Return None when target cannot be made "
    "exactly. target may be 0 (answer []). Must handle target up to 2000.",
    "min_coins_bounded", """
assert min_coins_bounded([(1,5),(5,2)], 7) == [1,1,5]
assert min_coins_bounded([(2,3)], 5) is None
assert min_coins_bounded([(1,1)], 0) == []
assert min_coins_bounded([], 0) == []
assert min_coins_bounded([], 4) is None
assert min_coins_bounded([(3,2),(4,2)], 7) == [3,4]
assert min_coins_bounded([(1,10),(3,1),(4,1)], 7) == [3,4]
assert min_coins_bounded([(2,2),(3,2)], 6) == [3,3]
assert min_coins_bounded([(1,3),(2,2),(5,1)], 9) == [2,2,5]
assert min_coins_bounded([(7,300),(1,10)], 2000) == [1]*5 + [7]*285
assert min_coins_bounded([(5,1),(6,1),(11,1)], 11) == [11]
"""),
"C09": (4,
    "def count_paths(grid: list[str], k: int) -> int\n"
    "grid is a rectangular list of equal-length strings using '.', '#' and '*'. "
    "Count the paths from the top-left cell to the bottom-right cell that move "
    "only right or down, never enter a '#' cell, and pass through EXACTLY k "
    "cells marked '*' (the start and end cells count if they are '*'). Return "
    "the count modulo 1000000007. If the start or end cell is '#', the answer "
    "is 0. Must handle a 100x100 grid without timing out.",
    "count_paths", """
assert count_paths(["..", ".."], 0) == 2
assert count_paths(["*.", ".."], 1) == 2
assert count_paths(["*.", ".."], 0) == 0
assert count_paths(["#.", ".."], 0) == 0
assert count_paths([".#", ".."], 0) == 1
assert count_paths(["..*", "...", "*.."], 1) == 2
assert count_paths(["..*", "...", "*.."], 0) == 4
assert count_paths(["*"], 1) == 1
assert count_paths(["*"], 0) == 0
assert count_paths(["."], 0) == 1
import math as _m
_g = ["." * 100] * 100
assert count_paths(_g, 0) == _m.comb(198, 99) % 1000000007
"""),
"C10": (4,
    "def kth_smallest_pair_sum(a: list[int], b: list[int], k: int) -> int\n"
    "a and b are sorted ascending. Consider every sum a[i] + b[j] over all "
    "pairs (i, j) -- that is len(a)*len(b) sums, counted with multiplicity. "
    "Return the k-th smallest of them, 1-indexed. You may assume "
    "1 <= k <= len(a)*len(b). Must handle len(a) = len(b) = 2000 with k up to "
    "4000000 without timing out or exhausting memory, so materialising all "
    "sums is not acceptable.",
    "kth_smallest_pair_sum", """
assert kth_smallest_pair_sum([1,7,11], [2,4,6], 1) == 3
assert kth_smallest_pair_sum([1,7,11], [2,4,6], 3) == 7
assert kth_smallest_pair_sum([1,7,11], [2,4,6], 9) == 17
assert kth_smallest_pair_sum([1,1,2], [1,2,3], 5) == 3
assert kth_smallest_pair_sum([0], [0], 1) == 0
assert kth_smallest_pair_sum([-5,-1,3], [-2,0,4], 4) == -1
_a = list(range(2000)); _b = list(range(2000))
assert kth_smallest_pair_sum(_a, _b, 1) == 0
assert kth_smallest_pair_sum(_a, _b, 4000000) == 3998
assert kth_smallest_pair_sum(_a, _b, 2000000) == 1999
"""),
"C11": (4,
    "def count_subarrays_range(nums: list[int], limit: int) -> int\n"
    "Count the non-empty CONTIGUOUS subarrays of nums in which the difference "
    "between the maximum and the minimum element is at most limit. Must handle "
    "len(nums) up to 20000 without timing out, so an O(n^2) scan is not "
    "acceptable.",
    "count_subarrays_range", """
assert count_subarrays_range([1,2,3], 0) == 3
assert count_subarrays_range([1,2,3], 1) == 5
assert count_subarrays_range([1,2,3], 2) == 6
assert count_subarrays_range([], 5) == 0
assert count_subarrays_range([4,4,4], 0) == 6
assert count_subarrays_range([8,2,4,7], 4) == 6
assert count_subarrays_range([10,1,2,4,7,2], 5) == 14
assert count_subarrays_range([-3,7,-1,0,5], 8) == 11
assert count_subarrays_range(list(range(20000)), 0) == 20000
assert count_subarrays_range([5]*20000, 0) == 20000*20001//2
"""),
"C12": (5,
    "def min_cost_merge(stones: list[int], k: int) -> int\n"
    "There are len(stones) piles in a row. In one move you merge exactly k "
    "CONSECUTIVE piles into one pile, at a cost equal to the total number of "
    "stones in those k piles. Return the minimum total cost to merge all piles "
    "into a single pile, or -1 when it is impossible. A single pile already "
    "costs 0. Must handle len(stones) up to 30.",
    "min_cost_merge", """
assert min_cost_merge([3,2,4,1], 2) == 20
assert min_cost_merge([3,2,4,1], 3) == -1
assert min_cost_merge([3,5,1,2,6], 3) == 25
assert min_cost_merge([1], 2) == 0
assert min_cost_merge([1,2], 2) == 3
assert min_cost_merge([6,4,4,6], 2) == 40
assert min_cost_merge([69,39,79,78,16,6,36,97,79,27,14,5,64,44,23,42,90,17,4,5], 2) == 3326
assert min_cost_merge([1]*30, 2) == 148
"""),
}


REASON_V2 = {
"R07": (4, "A distributor buys 1250 units at 14.80 each. It sells 60% of them "
        "at a 35% markup on the unit cost, and the remaining units at a 10% "
        "discount off that marked-up price. Fixed overheads for the period are "
        "2400. Ignore tax. What is the net profit? Round to exactly 2 decimal "
        "places. Answer with the number only.", 3076.00, "num"),
"R10": (5, "How many 5-digit positive integers have strictly increasing digits "
        "(each digit greater than the one before it) and are divisible by 3? "
        "Answer with the number only.", 42, "num"),
"R11": (5, "What are the last three digits of 3^1234 + 7^5678? Answer as a "
        "three-character string, including leading zeros if needed.",
        "018", "str"),
"R12": (5, "Six servers s1..s6 are retired one per week, weeks 1 to 6, in some "
        "order.\n"
        "1. s2 is retired exactly three weeks after s6.\n"
        "2. s1 is retired after s6 but before s3.\n"
        "3. s4 is retired in an even-numbered week.\n"
        "4. s5 is retired neither in week 1 nor in week 6.\n"
        "5. s3 and s2 are not retired in consecutive weeks.\n"
        "In which week is s3 retired? Answer with the number only.", 6, "num"),
}


# ---------------------------------------------------------------------------
# assembly
# ---------------------------------------------------------------------------

def _mk_code_grader(entry, tests):
    def g(text):
        code = extract_code(text)
        if not code.strip():
            return False, "empty reply"
        return run_code(code, tests, entry)
    return g


def _mk_reason_grader(expected, kind):
    def g(text):
        got = extract_answer(text)
        if not got:
            return False, "no answer line"
        if kind == "num":
            return (True, "ok") if num_eq(got, expected) else (False, f"got {got!r} want {expected}")
        return (True, "ok") if norm(got) == norm(str(expected)) else (False, f"got {got!r} want {expected!r}")
    return g


def build(round=2):
    """round=1 is the original calibration suite; round=2 swaps in the twelve
    hardened tasks. Both are kept so the report can show the two rounds and so
    the round-1 results.jsonl remains reproducible from this file."""
    ts = []
    for tid, lvl, prompt, grader in PLAN:
        if round >= 2 and tid in PLAN_V2:
            lvl, prompt, grader = PLAN_V2[tid]
        ts.append({"id": tid, "cls": "plan", "level": lvl, "system": SYS_PLAN,
                   "user": prompt, "json_mode": True, "grade": grader,
                   "hardened": round >= 2 and tid in PLAN_V2})
    for tid, lvl, spec, entry, tests in CODE:
        if round >= 2 and tid in CODE_V2:
            lvl, spec, entry, tests = CODE_V2[tid]
        ts.append({"id": tid, "cls": "code", "level": lvl, "system": SYS_CODE,
                   "user": "Implement this function exactly.\n\n" + spec,
                   "json_mode": False, "grade": _mk_code_grader(entry, tests),
                   "hardened": round >= 2 and tid in CODE_V2})
    for tid, lvl, prompt, exp, kind in REASON:
        if round >= 2 and tid in REASON_V2:
            lvl, prompt, exp, kind = REASON_V2[tid]
        ts.append({"id": tid, "cls": "reason", "level": lvl, "system": SYS_REASON,
                   "user": prompt, "json_mode": False,
                   "grade": _mk_reason_grader(exp, kind),
                   "hardened": round >= 2 and tid in REASON_V2})
    assert len(ts) == 36, len(ts)
    assert len({t["id"] for t in ts}) == 36
    if round >= 2:
        assert sorted(t["id"] for t in ts if t["hardened"]) == sorted(HARDENED_IDS)
    return ts


TASKS = build(round=2)
BY_ID = {t["id"]: t for t in TASKS}

TASKS_R1 = build(round=1)
BY_ID_R1 = {t["id"]: t for t in TASKS_R1}

if __name__ == "__main__":
    from collections import Counter
    print("tasks:", len(TASKS))
    print("by class:", Counter(t["cls"] for t in TASKS))
    print("by level:", sorted(Counter(t["level"] for t in TASKS).items()))
    print("hardened:", sorted(t["id"] for t in TASKS if t["hardened"]))
