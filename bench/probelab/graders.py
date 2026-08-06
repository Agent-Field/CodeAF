"""Deterministic graders for probe lab. No LLM judging anywhere.

Three grader families:
  code  -- extract a python function, run hidden unit tests in a subprocess
  json  -- parse strict JSON, check schema + semantic constraints in code
  exact -- normalized exact match against a ground-truth string
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import tempfile

# --------------------------------------------------------------------------
# extraction helpers
# --------------------------------------------------------------------------

_FENCE = re.compile(r"```(?:python|py|json)?\s*\n(.*?)```", re.S | re.I)


def extract_code(text: str) -> str:
    """Pull the python source out of a model response."""
    if not text:
        return ""
    blocks = _FENCE.findall(text)
    if blocks:
        # prefer the longest block that looks like python
        cand = [b for b in blocks if "def " in b] or blocks
        return max(cand, key=len)
    return text


def extract_json(text: str):
    """Pull the first well-formed JSON object/array out of a model response."""
    if not text:
        return None
    blocks = _FENCE.findall(text)
    for b in blocks:
        try:
            return json.loads(b)
        except Exception:
            pass
    # brace matching scan
    for opener, closer in (("{", "}"), ("[", "]")):
        start = text.find(opener)
        while start != -1:
            depth, in_str, esc = 0, False, False
            for i in range(start, len(text)):
                ch = text[i]
                if in_str:
                    if esc:
                        esc = False
                    elif ch == "\\":
                        esc = True
                    elif ch == '"':
                        in_str = False
                    continue
                if ch == '"':
                    in_str = True
                elif ch == opener:
                    depth += 1
                elif ch == closer:
                    depth -= 1
                    if depth == 0:
                        try:
                            return json.loads(text[start:i + 1])
                        except Exception:
                            break
            start = text.find(opener, start + 1)
    try:
        return json.loads(text.strip())
    except Exception:
        return None


_NUM = re.compile(r"-?\d[\d,]*(?:\.\d+)?(?:/\d+)?")


def normalize_answer(text: str) -> str:
    """Normalize a short free-text answer for exact match."""
    if not text:
        return ""
    t = text.strip()
    # strip fences and common wrappers
    b = _FENCE.findall(t)
    if b:
        t = b[-1].strip()
    t = t.replace("\\boxed", "").replace("$", "")
    # take the last non-empty line -- models often reason then answer
    lines = [l.strip() for l in t.splitlines() if l.strip()]
    if lines:
        t = lines[-1]
    t = re.sub(r"(?i)^(the\s+)?(final\s+)?answer\s*(is)?\s*[:=]?\s*", "", t)
    t = t.strip().strip(".").strip()
    t = t.strip("*").strip("`").strip().strip(".").strip()
    t = re.sub(r"[{}]", "", t)
    return t.lower()


def answer_matches(text: str, gold: str, alts: list[str] | None = None) -> bool:
    got = normalize_answer(text)
    cands = [gold] + list(alts or [])
    for c in cands:
        c = c.strip().lower()
        if got == c:
            return True
        # numeric compare with comma/space tolerance
        gn = got.replace(",", "").replace(" ", "")
        cn = c.replace(",", "").replace(" ", "")
        if gn == cn:
            return True
        m = _NUM.findall(got)
        if m and m[-1].replace(",", "") == cn:
            return True
    return False


# --------------------------------------------------------------------------
# code grader -- subprocess, timeout, no network
# --------------------------------------------------------------------------

_HARNESS = '''
import sys, json
_FAILS = []
def check(got, want, label):
    if got != want:
        _FAILS.append("%s: got %r want %r" % (label, got, want))
try:
    __TESTS__
except Exception as _e:
    import traceback
    _FAILS.append("EXC " + traceback.format_exc(limit=3).replace("\\n", " | ")[:400])
print("__PROBELAB__" + json.dumps({"fails": _FAILS[:6], "n_fail": len(_FAILS)}))
'''

_NET_BLOCK = '''
import socket as _s
def _blocked(*a, **k):
    raise OSError("network disabled in probelab grader")
_s.socket = _blocked
_s.create_connection = _blocked
'''


def grade_code(response: str, tests_src: str, timeout: float = 20.0) -> dict:
    """Run hidden unit tests against the model's code. Returns dict with ok/detail."""
    src = extract_code(response)
    if not src.strip():
        return {"ok": False, "reason": "no_code", "detail": ""}
    prog = _NET_BLOCK + "\n" + src + "\n\n" + _HARNESS.replace(
        "__TESTS__", "\n    ".join(tests_src.strip().splitlines()))
    with tempfile.TemporaryDirectory() as td:
        path = os.path.join(td, "cand.py")
        with open(path, "w") as fh:
            fh.write(prog)
        env = {"PATH": "/usr/bin:/bin", "HOME": td, "PYTHONDONTWRITEBYTECODE": "1"}
        try:
            p = subprocess.run([sys.executable, path], capture_output=True, text=True,
                               timeout=timeout, cwd=td, env=env)
        except subprocess.TimeoutExpired:
            return {"ok": False, "reason": "timeout", "detail": ""}
        out = p.stdout
        marker = out.rfind("__PROBELAB__")
        if marker == -1:
            err = (p.stderr or "")[-300:]
            return {"ok": False, "reason": "crash", "detail": err}
        try:
            res = json.loads(out[marker + len("__PROBELAB__"):].strip())
        except Exception:
            return {"ok": False, "reason": "bad_harness_output", "detail": out[-200:]}
        return {"ok": res["n_fail"] == 0, "reason": "" if res["n_fail"] == 0 else "test_fail",
                "detail": "; ".join(res["fails"])[:400], "n_fail": res["n_fail"]}


# --------------------------------------------------------------------------
# json grader helpers
# --------------------------------------------------------------------------

def is_dag_order(nodes: list, deps: dict, order: list) -> bool:
    """order is a valid topological order of nodes under deps (node -> list of prereqs)."""
    if sorted(map(str, order)) != sorted(map(str, nodes)):
        return False
    pos = {str(n): i for i, n in enumerate(order)}
    for n, ps in deps.items():
        for p in ps:
            if str(p) not in pos or str(n) not in pos:
                return False
            if pos[str(p)] >= pos[str(n)]:
                return False
    return True


def has_cycle(graph: dict) -> bool:
    color = {}

    def dfs(u):
        color[u] = 1
        for v in graph.get(u, []) or []:
            c = color.get(v, 0)
            if c == 1:
                return True
            if c == 0 and dfs(v):
                return True
        color[u] = 2
        return False

    return any(color.get(u, 0) == 0 and dfs(u) for u in list(graph))


def typed(obj, key, typ):
    return key in obj and isinstance(obj[key], typ)
