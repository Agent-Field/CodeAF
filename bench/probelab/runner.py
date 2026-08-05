"""Probe lab runner: collect cheap pre-flight signals, then the full attempt, then grade.

Per (model, task) cell:
  probes  (a) self-consistency  k=3, temp 0.8, max_tokens 256
          (b) logprob draft     temp 0, max_tokens 128, logprobs+top_logprobs=5
          (c) verbalized conf   temp 0, max_tokens 8
          (d) sketch stability  2x temp 0.8, max_tokens 96
  attempt temp 0.2, max_tokens ATTEMPT_MAX

All probes run with reasoning disabled -- a probe that pays for a reasoning trace is not
a cheap probe. The attempt uses the model's production default.

Usage:
  python runner.py --pilot          # 3 models x 3 tasks, validates + prices the run
  python runner.py --full           # everything
  python runner.py --tasks a,b,c    # re-run specific tasks (round 2)
"""
from __future__ import annotations

import argparse
import asyncio
import difflib
import json
import math
import os
import random
import re
import statistics
import sys
import time

import aiohttp

import tasks as T
from graders import normalize_answer

API = "https://openrouter.ai/api/v1/chat/completions"
KEY = os.environ.get("OPENROUTER_API_KEY", "")

MODELS = [
    # (slug, short name, $ per M in, $ per M out)  -- prices from the live catalog
    ("~deepseek/deepseek-v4-flash-latest", "deepseek-v4-flash", 0.09, 0.18),
    ("qwen/qwen3-30b-a3b-instruct-2507", "qwen3-30b-a3b", 0.048, 0.193),
    ("z-ai/glm-4.7", "glm-4.7", 0.40, 1.75),
]

K_SC = 3
# Spec said 4096, but the pilot showed 4/12 attempts hitting finish_reason=length with the
# reasoning trace alone consuming 2000-4200 tokens (glm-4.7 truncated 3/4, deepseek 1/4).
# At 4096 the outcome label measures the token budget, not the model's ability, so the
# budget is raised to 16384. Truncation is still recorded per attempt and reported.
ATTEMPT_MAX = 16384
CONCURRENCY = 24
SPEND_CAP = 25.0

_spend = 0.0
_calls = 0
_errors = 0
_lock = asyncio.Lock()


class SpendCapExceeded(Exception):
    pass


# --------------------------------------------------------------------------
# transport
# --------------------------------------------------------------------------

async def call(session, model, prompt, *, temperature, max_tokens, logprobs=False,
               no_reasoning=True, tag=""):
    """One chat completion with retries. Returns a dict of everything we record."""
    global _spend, _calls, _errors
    body = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": temperature,
        "max_tokens": max_tokens,
        "usage": {"include": True},
    }
    if no_reasoning:
        body["reasoning"] = {"enabled": False}
    if logprobs:
        body["logprobs"] = True
        body["top_logprobs"] = 5

    headers = {"Authorization": f"Bearer {KEY}", "Content-Type": "application/json",
               "HTTP-Referer": "https://github.com/aforge/probelab", "X-Title": "probelab"}
    last = None
    t0 = time.time()
    for attempt in range(6):
        async with _lock:
            if _spend >= SPEND_CAP:
                raise SpendCapExceeded(f"spend {_spend:.4f} >= cap {SPEND_CAP}")
        try:
            async with session.post(API, json=body, headers=headers,
                                    timeout=aiohttp.ClientTimeout(total=300)) as r:
                if r.status in (429, 500, 502, 503, 504, 520, 524):
                    last = f"HTTP {r.status}"
                    await asyncio.sleep(min(2 ** attempt + random.random() * 2, 40))
                    continue
                if r.status != 200:
                    txt = (await r.text())[:300]
                    last = f"HTTP {r.status}: {txt}"
                    if r.status in (400, 404, 422):
                        break
                    await asyncio.sleep(2 ** attempt)
                    continue
                data = await r.json()
        except (aiohttp.ClientError, asyncio.TimeoutError) as e:
            last = f"{type(e).__name__}: {e}"
            await asyncio.sleep(min(2 ** attempt + random.random() * 2, 40))
            continue

        if "error" in data and not data.get("choices"):
            last = f"api_error: {str(data['error'])[:200]}"
            await asyncio.sleep(2 ** attempt)
            continue

        ch = data["choices"][0]
        msg = ch.get("message") or {}
        usage = data.get("usage") or {}
        cost = float(usage.get("cost") or 0.0)
        async with _lock:
            _spend += cost
            _calls += 1
        return {
            "ok": True, "tag": tag,
            "text": msg.get("content") or "",
            "reasoning_chars": len(str(msg.get("reasoning") or "")),
            "finish_reason": ch.get("finish_reason"),
            "prompt_tokens": usage.get("prompt_tokens", 0),
            "completion_tokens": usage.get("completion_tokens", 0),
            "reasoning_tokens": (usage.get("completion_tokens_details") or {}).get("reasoning_tokens", 0),
            "cost": cost,
            "latency_s": round(time.time() - t0, 2),
            "logprobs": _lp_stats(ch.get("logprobs")),
            "served_by": data.get("model"),
        }
    async with _lock:
        _errors += 1
    return {"ok": False, "tag": tag, "error": last, "text": "", "cost": 0.0,
            "prompt_tokens": 0, "completion_tokens": 0, "reasoning_tokens": 0,
            "latency_s": round(time.time() - t0, 2), "logprobs": None,
            "finish_reason": None, "reasoning_chars": 0, "served_by": None}


def _lp_stats(lp):
    """Mean token logprob, entropy over the returned top-k, and tail statistics."""
    if not lp or not lp.get("content"):
        return None
    toks = lp["content"]
    lps, ents = [], []
    for t in toks:
        v = t.get("logprob")
        if v is None:
            continue
        lps.append(float(v))
        tops = t.get("top_logprobs") or []
        ps = [math.exp(float(x["logprob"])) for x in tops if x.get("logprob") is not None]
        s = sum(ps)
        if s > 0:
            q = [p / s for p in ps]
            ents.append(-sum(p * math.log(p + 1e-12) for p in q))
    if not lps:
        return None
    return {
        "n_tokens": len(lps),
        "mean_logprob": sum(lps) / len(lps),
        "min_logprob": min(lps),
        "p10_logprob": sorted(lps)[max(0, int(0.10 * len(lps)) - 1)],
        "frac_below_1": sum(1 for v in lps if v < -1.0) / len(lps),
        "mean_entropy": (sum(ents) / len(ents)) if ents else None,
        "max_entropy": max(ents) if ents else None,
    }


# --------------------------------------------------------------------------
# similarity helpers for sketch/self-consistency signals
# --------------------------------------------------------------------------

_STOP = set("a an the of to in for is are be by with and or on that this it as we i you use "
            "using then would will can should each all not no if from at into over ".split())
_W = re.compile(r"[a-z0-9_]+")


def _bag(s: str) -> set[str]:
    return {w for w in _W.findall((s or "").lower()) if w not in _STOP and len(w) > 1}


def jaccard(a: str, b: str) -> float:
    A, B = _bag(a), _bag(b)
    if not A or not B:
        return 0.0
    return len(A & B) / len(A | B)


def char_sim(a: str, b: str) -> float:
    a, b = (a or "").strip().lower(), (b or "").strip().lower()
    if not a or not b:
        return 0.0
    return difflib.SequenceMatcher(None, a[:600], b[:600]).ratio()


def pairwise(vals, fn):
    ps = [fn(vals[i], vals[j]) for i in range(len(vals)) for j in range(i + 1, len(vals))]
    return sum(ps) / len(ps) if ps else 0.0


def majority_frac(items):
    items = [i for i in items if i]
    if not items:
        return 0.0
    return max(items.count(x) for x in set(items)) / len(items)


# --------------------------------------------------------------------------
# probe prompt construction
# --------------------------------------------------------------------------

def sc_prompt(task):
    if task["cls"] == "exact":
        return (task["prompt"].split("\n\nThink it through")[0]
                + "\n\nAnswer from intuition, fast. Reply with ONLY the final value on one line. "
                  "No working, no words, no units.")
    core = task["prompt"].split("\n\nReturn ONLY")[0]
    return (core + "\n\nDo NOT write the solution. In at most 25 words, name the single key "
                   "algorithm or data structure you would use. One line only.")


def sketch_prompt(task):
    core = task["prompt"].split("\n\nReturn ONLY")[0].split("\n\nThink it through")[0]
    return (core + "\n\nState your approach in ONE short line (max 20 words). "
                   "Do not solve it, do not show any answer.")


def conf_prompt(task):
    core = task["prompt"].split("\n\nReturn ONLY")[0].split("\n\nThink it through")[0]
    return (core + "\n\nRate 0-10 your confidence you can solve this correctly. "
                   "Reply with the number only.")


def draft_prompt(task):
    core = task["prompt"].split("\n\nReturn ONLY")[0].split("\n\nThink it through")[0]
    return core + "\n\nIn 2-3 sentences, outline how you would solve this. Do not solve it."


_CONF = re.compile(r"(\d+(?:\.\d+)?)")


def parse_conf(text):
    m = _CONF.search((text or "").strip())
    if not m:
        return None
    try:
        v = float(m.group(1))
    except ValueError:
        return None
    if v > 10:
        v = 10.0
    return max(0.0, min(1.0, v / 10.0))


# --------------------------------------------------------------------------
# one cell
# --------------------------------------------------------------------------

async def run_cell(session, sem, model_slug, model_name, task):
    async def go(**kw):
        async with sem:
            return await call(session, model_slug, **kw)

    sc_p, sk_p = sc_prompt(task), sketch_prompt(task)
    probe_jobs = [
        *[go(prompt=sc_p, temperature=0.8, max_tokens=256, tag=f"sc{i}") for i in range(K_SC)],
        go(prompt=draft_prompt(task), temperature=0.0, max_tokens=128, logprobs=True, tag="lp"),
        go(prompt=conf_prompt(task), temperature=0.0, max_tokens=8, tag="conf"),
        *[go(prompt=sk_p, temperature=0.8, max_tokens=96, tag=f"sk{i}") for i in range(2)],
    ]
    t_probe0 = time.time()
    probes = await asyncio.gather(*probe_jobs)
    probe_wall = round(time.time() - t_probe0, 2)
    by = {p["tag"]: p for p in probes}

    # ---- signal (a) self-consistency ----
    sc_texts = [by[f"sc{i}"]["text"] for i in range(K_SC)]
    if task["cls"] == "exact":
        norm = [normalize_answer(t) for t in sc_texts]
        sc_agree = pairwise(norm, lambda a, b: 1.0 if (a and b and a == b) else 0.0)
        sc_major = majority_frac(norm)
    else:
        sc_agree = pairwise(sc_texts, jaccard)
        sc_major = pairwise(sc_texts, char_sim)
    sc_len_cv = 0.0
    lens = [len(t) for t in sc_texts if t]
    if len(lens) > 1 and statistics.mean(lens) > 0:
        sc_len_cv = statistics.pstdev(lens) / statistics.mean(lens)

    # ---- signal (b) logprobs ----
    lp = by["lp"]["logprobs"]

    # ---- signal (c) verbalized confidence ----
    vconf = parse_conf(by["conf"]["text"])

    # ---- signal (d) sketch stability ----
    s0, s1 = by["sk0"]["text"], by["sk1"]["text"]
    sketch_jac = jaccard(s0, s1)
    sketch_chr = char_sim(s0, s1)

    probe_cost = sum(p["cost"] for p in probes)
    probe_tok_in = sum(p["prompt_tokens"] for p in probes)
    probe_tok_out = sum(p["completion_tokens"] for p in probes)

    # ---- the full attempt ----
    async with sem:
        att = await call(session, model_slug, prompt=task["prompt"], temperature=0.2,
                         max_tokens=ATTEMPT_MAX, no_reasoning=False, tag="attempt")

    graded = task["check"](att["text"]) if att["ok"] else {"ok": False, "reason": "api_error",
                                                           "detail": att.get("error", "")[:200]}

    row = {
        "model": model_name, "model_slug": model_slug, "served_by": att.get("served_by"),
        "task": task["id"], "cls": task["cls"], "tier": task["tier"],
        "solved": bool(graded["ok"]),
        "grade_reason": graded.get("reason", ""), "grade_detail": graded.get("detail", "")[:300],
        "features": T.features(task),
        "signals": {
            "sc_agree": round(sc_agree, 4),
            "sc_major": round(sc_major, 4),
            "sc_len_cv": round(sc_len_cv, 4),
            "sketch_jaccard": round(sketch_jac, 4),
            "sketch_charsim": round(sketch_chr, 4),
            "vconf": vconf,
            "lp_available": lp is not None,
            "lp_mean_logprob": (round(lp["mean_logprob"], 5) if lp else None),
            "lp_min_logprob": (round(lp["min_logprob"], 5) if lp else None),
            "lp_p10_logprob": (round(lp["p10_logprob"], 5) if lp else None),
            "lp_frac_below_1": (round(lp["frac_below_1"], 5) if lp else None),
            "lp_mean_entropy": (round(lp["mean_entropy"], 5) if lp and lp["mean_entropy"] is not None else None),
            "lp_max_entropy": (round(lp["max_entropy"], 5) if lp and lp["max_entropy"] is not None else None),
            "lp_n_tokens": (lp["n_tokens"] if lp else 0),
        },
        "probe": {
            "cost": round(probe_cost, 8), "tokens_in": probe_tok_in, "tokens_out": probe_tok_out,
            "n_calls": len(probes), "wall_s": probe_wall,
            "latency_max_s": max(p["latency_s"] for p in probes),
            "n_failed_calls": sum(1 for p in probes if not p["ok"]),
            "per_signal_cost": {
                "self_consistency": round(sum(by[f"sc{i}"]["cost"] for i in range(K_SC)), 8),
                "logprob": round(by["lp"]["cost"], 8),
                "vconf": round(by["conf"]["cost"], 8),
                "sketch": round(by["sk0"]["cost"] + by["sk1"]["cost"], 8),
            },
        },
        "attempt": {
            "cost": round(att["cost"], 8), "tokens_in": att["prompt_tokens"],
            "tokens_out": att["completion_tokens"], "reasoning_tokens": att["reasoning_tokens"],
            "latency_s": att["latency_s"], "finish_reason": att["finish_reason"],
            "ok": att["ok"], "error": att.get("error"),
            "text_chars": len(att["text"]),
        },
        "raw": {
            "sc": [t[:400] for t in sc_texts],
            "sketches": [s0[:300], s1[:300]],
            "conf_text": by["conf"]["text"][:40],
            "attempt_text": att["text"][:6000],
        },
        "ts": time.time(),
    }
    return row


# --------------------------------------------------------------------------
# driver
# --------------------------------------------------------------------------

def estimate(n_cells):
    """Upfront cost estimate, calibrated on the 12-cell pilot (measured $/cell),
    with attempts scaled 2.5x for the raised 16384-token attempt budget."""
    probe_per_cell = 0.00055   # pilot: $0.00656 over 12 cells
    att_per_cell = 0.0026 * 2.5  # pilot: $0.00261/cell at 4096; budget now 16384
    return n_cells * (probe_per_cell + att_per_cell), n_cells * probe_per_cell, n_cells * att_per_cell


async def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--pilot", action="store_true")
    ap.add_argument("--full", action="store_true")
    ap.add_argument("--tasks", default="")
    ap.add_argument("--models", default="")
    ap.add_argument("--out", default="results.jsonl")
    a = ap.parse_args()

    if not KEY:
        sys.exit("OPENROUTER_API_KEY not set")

    tasklist = T.TASKS
    if a.pilot:
        want = ["code_fizzbuzz", "code_expr_twist", "json_rcpsp", "exact_dice"]
        tasklist = [t for t in T.TASKS if t["id"] in want]
    if a.tasks:
        ids = set(a.tasks.split(","))
        tasklist = [t for t in T.TASKS if t["id"] in ids]
        missing = ids - {t["id"] for t in tasklist}
        if missing:
            sys.exit(f"unknown task ids: {sorted(missing)}")
    models = MODELS
    if a.models:
        keep = set(a.models.split(","))
        models = [m for m in MODELS if m[1] in keep]

    cells = [(ms, mn, t) for (ms, mn, _, _) in models for t in tasklist]
    est, epr, eat = estimate(len(cells))
    print(f"probe lab: {len(models)} models x {len(tasklist)} tasks = {len(cells)} cells")
    print(f"  calls: {len(cells) * 8} (7 probe + 1 attempt per cell)")
    print(f"  ESTIMATED SPEND: ${est:.2f}  (probes ${epr:.2f} + attempts ${eat:.2f})   cap ${SPEND_CAP}")
    print(f"  concurrency: {CONCURRENCY}")
    sys.stdout.flush()

    sem = asyncio.Semaphore(CONCURRENCY)
    t0 = time.time()
    done = 0
    conn = aiohttp.TCPConnector(limit=CONCURRENCY + 8)
    out_path = a.out
    mode = "a" if a.tasks else "w"
    async with aiohttp.ClientSession(connector=conn) as session:
        tasks_ = [asyncio.create_task(run_cell(session, sem, ms, mn, t)) for ms, mn, t in cells]
        with open(out_path, mode) as fh:
            try:
                for fut in asyncio.as_completed(tasks_):
                    try:
                        row = await fut
                    except SpendCapExceeded as e:
                        print("!! SPEND CAP HIT:", e)
                        for x in tasks_:
                            x.cancel()
                        break
                    fh.write(json.dumps(row) + "\n")
                    fh.flush()
                    done += 1
                    print(f"[{done}/{len(cells)}] {row['model']:<18} {row['task']:<24} "
                          f"{'PASS' if row['solved'] else 'FAIL':<4} "
                          f"probe=${row['probe']['cost']:.5f} att=${row['attempt']['cost']:.5f} "
                          f"| spend ${_spend:.3f} | {time.time()-t0:.0f}s")
                    sys.stdout.flush()
            except KeyboardInterrupt:
                for x in tasks_:
                    x.cancel()

    print(f"\ndone: {done}/{len(cells)} cells, {_calls} API calls, {_errors} failed calls")
    print(f"ACTUAL SPEND: ${_spend:.4f}   wall: {time.time()-t0:.0f}s")
    with open("spend.json", "w") as fh:
        json.dump({"spend": _spend, "calls": _calls, "errors": _errors,
                   "cells": done, "wall_s": round(time.time() - t0, 1),
                   "estimate": est, "mode": "pilot" if a.pilot else ("partial" if a.tasks else "full")},
                  fh, indent=2)


if __name__ == "__main__":
    asyncio.run(main())
