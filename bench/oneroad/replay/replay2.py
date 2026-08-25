#!/usr/bin/env python3
"""REPLAY-2: off-policy test of a DIGEST-based mark reader.

Live wave 1e failure: the mastermind mark reader read the RAW transcript at
rounds 10/20/40 on the four-issue batch cell (57k/65k/91k tokens) and answered
CONTINUE all three times, at $0.62. Hypothesis: raw, result-heavy context drowns
the ask. This script builds a compact DIGEST instead - the person's ask verbatim,
a ledger of tool calls with NO results, the last thing said, and the files
touched - bounds it to ~6k tokens, and asks the same sketch question.

The first line of every answer is parsed exactly as
internal/session/checkpoint.go's parseCheckpointSketch + topLevelParts do.

Usage:
  python3 replay2.py --probe        # build every digest, print token counts
  python3 replay2.py --show CELL:N  # print one digest verbatim
  python3 replay2.py                # full run (answers2.jsonl + RESULTS-2.md)
  python3 replay2.py --score-only   # re-score existing answers2.jsonl
"""
import argparse
import glob
import json
import os
import re
import threading
import time
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor

import requests

HERE = os.path.dirname(os.path.abspath(__file__))
RESULTS_ROOT = os.path.join(os.path.dirname(HERE), "results")
ANSWERS = os.path.join(HERE, "answers2.jsonl")
RESULTS_MD = os.path.join(HERE, "RESULTS-2.md")

CELLS = [f"aforge-final-{fam}-{t}-s1"
         for fam in ("crew", "flash")
         for t in ("20", "21", "22", "23", "batch")]

CUTS = [10, 20, 40]

# Ground truth at the mark. batch at round 40 is recorded, not forced.
TRUTH = {"20": "CONTINUE", "21": "CONTINUE", "22": "CONTINUE",
         "23": "CONTINUE", "batch": "SPLIT"}

MODELS = ["moonshotai/kimi-k3", "deepseek/deepseek-v4-flash"]
SAMPLES = 3
TEMPERATURE = 0.0
CONCURRENCY = 8
MAX_TOKENS = {"deepseek/deepseek-v4-flash": 2500, "moonshotai/kimi-k3": 1200}

ARG_CLIP = 80            # per spec: ledger arg clipped to ~80 chars
LAST_CLIP = 600          # per spec: last assistant text clipped to 600 chars
DIGEST_TOKEN_CAP = 6000  # per spec: bound the digest to ~6k tokens

BUDGET_CAP = 5.00

SYSTEM_PROMPT = "You are reading a short account of work in progress."

ASK_QUESTION = (
    "WHAT WAS ASKED is above, and what has been done so far. Sketch WHAT "
    "REMAINS OF THE ASK as parts and arrows: independent parts separated by "
    "' | ', ordered steps joined by ' > '. Example shapes: 'A | B | C' or "
    "'A > B > C' or 'A > (B | C)'. If nothing remains, write '(done)'. Nothing "
    "else on that line. Then one sentence saying what each letter is."
)

# ---------------------------------------------------------------- transcript


def find_transcript(cell):
    pat = os.path.join(RESULTS_ROOT, cell, "profile", "v3", "projects",
                       "*", "*", "transcript.jsonl")
    hits = [h for h in sorted(glob.glob(pat)) if "/tasks/" not in h]
    if len(hits) != 1:
        raise RuntimeError(f"{cell}: expected 1 main transcript, found {len(hits)}")
    return hits[0]


def load(cell):
    path = find_transcript(cell)
    raw = [json.loads(l) for l in open(path) if l.strip()]
    return path, [d for d in raw if d.get("type") == "message"]


def clip(s, n):
    s = s if isinstance(s, str) else json.dumps(s)
    s = " ".join(s.split())          # ledger lines are one line each
    return s if len(s) <= n else s[:n] + "..."


WORKDIR_RX = re.compile(r"/home/[^\s\"']*?/results/[^/\s\"']+/work/[^/\s\"']+/?")


def short_path(p):
    """Drop the absolute cell/work prefix so the 80-char clip carries signal.

    Without this every ledger line is the same 80 characters of cell path and
    the reader learns nothing. Applied identically everywhere, so the digest
    stays deterministic. The running agent's own cwd is that directory, so this
    is what a live reader would be shown anyway.
    """
    return WORKDIR_RX.sub("", p) or "."


def ledger_detail(name, args_raw):
    """The one identifying argument of a tool call: path / command / query."""
    try:
        o = json.loads(args_raw)
    except Exception:                                  # noqa: BLE001
        return clip(short_path(args_raw), ARG_CLIP)
    if not isinstance(o, dict):
        return clip(short_path(str(o)), ARG_CLIP)
    if "command" in o:
        return clip(short_path(o["command"]), ARG_CLIP)
    if "pattern" in o:
        p = o["pattern"]
        where = o.get("path") or o.get("glob") or ""
        s = f"{p}   in {short_path(where)}" if where else p
        return clip(s, ARG_CLIP)
    if "path" in o:
        return clip(short_path(o["path"]), ARG_CLIP)
    return clip(short_path(json.dumps(o)), ARG_CLIP)


def scan(cell, cut):
    """Walk the transcript to `cut` completed tool rounds.

    A round = one assistant message with toolCalls. Returns
    (ask, ledger, last_text, files, rounds_reached) or None if never reached.
    """
    _, msgs = load(cell)
    ask = None
    ledger, files, last_text = [], [], ""
    rounds = 0
    for d in msgs:
        role = d.get("role")
        if role == "user":
            if ask is None:
                ask = d.get("content") or ""
            continue                    # mid-turn harness nudges are not the ask
        if role != "assistant":
            continue
        tcs = d.get("toolCalls") or []
        txt = (d.get("content") or "").strip()
        if txt:
            last_text = txt
        if not tcs:
            break                       # turn finished
        for t in tcs:
            name = t["function"]["name"]
            raw = t["function"].get("arguments", "{}")
            ledger.append(f"{name}: {ledger_detail(name, raw)}")
            if name in ("write", "edit"):
                try:
                    p = json.loads(raw).get("path")
                except Exception:                      # noqa: BLE001
                    p = None
                if p:
                    p = short_path(p)
                    if p not in files:
                        files.append(p)
        rounds += 1
        if rounds >= cut:
            break
    if rounds < cut:
        return None
    return ask or "", ledger, last_text, files, rounds


def render(ask, ledger, last_text, files):
    head = ["WHAT WAS ASKED", "", ask.strip(), "", "WHAT HAS BEEN DONE "
            f"({len(ledger)} tool calls, results not shown)", ""]
    tail = []
    if last_text:
        tail += ["", "LAST THING SAID", "", clip(last_text, LAST_CLIP)]
    if files:
        tail += ["", "FILES WRITTEN OR EDITED", ""] + [f"- {f}" for f in files]
    fixed = len("\n".join(head + tail))
    keep = list(ledger)
    dropped = 0
    while keep and (fixed + len("\n".join(keep)) + 40) // 4 > DIGEST_TOKEN_CAP:
        keep.pop(0)                     # truncate the ledger from the FRONT
        dropped += 1
    body = ([f"... {dropped} earlier tool calls omitted ..."] if dropped else []) + keep
    return "\n".join(head + body + tail)


def build_digest(cell, cut):
    got = scan(cell, cut)
    if got is None:
        return None
    ask, ledger, last_text, files = got[0], got[1], got[2], got[3]
    text = render(ask, ledger, last_text, files)
    return {"cell": cell, "cut": cut, "digest": text,
            "chars": len(text), "est_tokens": len(text) // 4,
            "calls": len(ledger), "files": len(files)}


# ------------------------------------------------------- checkpoint.go parser


def top_level_parts(shape):
    """Port of topLevelParts in internal/session/checkpoint.go."""
    parts, depth = 0, 0
    cur = []

    def close_one():
        nonlocal parts
        if "".join(cur).strip():
            parts += 1
        cur.clear()

    for ch in shape:
        if ch in "([{":
            depth += 1
        elif ch in ")]}":
            if depth > 0:
                depth -= 1
        elif ch == "|" and depth == 0:
            close_one()
            continue
        cur.append(ch)
    close_one()
    return parts


def parse_sketch(answer):
    """Port of parseCheckpointSketch. -> (shape, parts)."""
    lines = (answer or "").strip().split("\n")
    for line in lines:
        trimmed = line.strip().strip("`*_ ")
        if trimmed:
            return trimmed, top_level_parts(trimmed)
    return "", 0


DONE_RX = re.compile(r"^\(?\s*done\s*\)?[.]?$", re.I)


def decide(answer):
    """-> (decision, shape, parts). AMBIG when the first line has no grammar."""
    shape, parts = parse_sketch(answer)
    if not shape:
        return "AMBIG", "", 0
    if DONE_RX.match(shape):
        return "CONTINUE", shape, 0
    if parts >= 2:
        return "SPLIT", shape, parts
    if "|" in shape or ">" in shape:
        return "CONTINUE", shape, parts          # chain, or a fork behind a step
    return "AMBIG", shape, parts                 # prose: no arrow/bar grammar


def go_decision(answer):
    """What checkpoint.go itself would do (AMBIG fails open to CONTINUE)."""
    d, _, _ = decide(answer)
    return "CONTINUE" if d == "AMBIG" else d


def first_stage_parts(shape):
    """DIAGNOSTIC reading: how many parts stand in the FIRST stage of the shape.

    checkpoint.go counts parts at the TOP level of the whole line, so
    `(A | B | C) > D` - three jobs that can start right now, joined at the end -
    counts as ONE part and reads CONTINUE. That is the exact shape both models
    write for a genuinely four-way batch. This reading takes the segment before
    the first top-level `>`, strips one enclosing bracket, and counts inside it:

      `(A | B | C) > D` -> 3   (fork now)
      `A > (B | C)`     -> 1   (one job first, fork later)
      `A | B | C`       -> 3
      `A > B > C`       -> 1
    """
    depth, stage = 0, []
    for ch in shape:
        if ch in "([{":
            depth += 1
        elif ch in ")]}":
            depth = max(0, depth - 1)
        elif ch == ">" and depth == 0:
            break
        stage.append(ch)
    s = "".join(stage).strip()
    while len(s) > 1 and s[0] in "([{" and s[-1] in ")]}":
        s = s[1:-1].strip()
    return top_level_parts(s)


def decide_first_stage(answer):
    d, shape, _ = decide(answer)
    if d in ("AMBIG", "ERROR") or not shape:
        return d
    if DONE_RX.match(shape):
        return "CONTINUE"
    return "SPLIT" if first_stage_parts(shape) >= 2 else "CONTINUE"


# ---------------------------------------------------------------- openrouter

# RLock, not Lock: the progress line calls spent(), which takes this lock, from
# inside a block that already holds it. With a plain Lock that is a self-deadlock
# that hangs the first worker to finish and then every other worker behind it -
# which is exactly what killed two runs before it was found.
_lock = threading.RLock()
_usage = defaultdict(lambda: [0, 0, 0.0])

PRICE = {
    "deepseek/deepseek-v4-flash": (0.000000088606, 0.000000177212),
    "moonshotai/kimi-k3": (0.000003, 0.000015),
}


def spent():
    with _lock:
        return sum(v[2] for v in _usage.values())


# ONE pooled session for every thread. A bare requests.post() opens a fresh
# socket per call and leaks it until GC; after roughly two dozen the sandbox's
# outbound proxy stops accepting new connections and every worker hangs on
# connect with no error. Two runs died that way before this was found. The
# session caps the pool at CONCURRENCY and reuses sockets, so the connection
# count stays flat however many reads are made.
SESSION = requests.Session()
SESSION.mount("https://", requests.adapters.HTTPAdapter(
    pool_connections=CONCURRENCY, pool_maxsize=CONCURRENCY, max_retries=0))


def call(model, messages):
    if spent() >= BUDGET_CAP:
        return None, {}, "budget cap reached"
    key = os.environ["OPENROUTER_API_KEY"]
    body = {"model": model, "messages": messages, "temperature": TEMPERATURE,
            "max_tokens": MAX_TOKENS.get(model, 1200)}
    last = None
    for attempt in range(4):
        try:
            r = SESSION.post("https://openrouter.ai/api/v1/chat/completions",
                             headers={"Authorization": f"Bearer {key}",
                                      "Content-Type": "application/json"},
                             json=body, timeout=(10, 120))
            if r.status_code == 200:
                j = r.json()
                msg = j["choices"][0]["message"]
                txt = (msg.get("content") or "").strip()
                if not txt and msg.get("tool_calls"):
                    txt = "[tool_call] " + json.dumps(msg["tool_calls"])[:1500]
                u = j.get("usage") or {}
                pt, ct = u.get("prompt_tokens", 0), u.get("completion_tokens", 0)
                pin, pout = PRICE.get(model, (0, 0))
                cost = u.get("cost")
                if cost is None:
                    cost = pt * pin + ct * pout
                with _lock:
                    a = _usage[model]
                    a[0] += pt
                    a[1] += ct
                    a[2] += cost
                return txt, {"prompt_tokens": pt, "completion_tokens": ct,
                             "cost": cost,
                             "finish": j["choices"][0].get("finish_reason")}, None
            last = f"HTTP {r.status_code}: {r.text[:400]}"
        except Exception as e:                          # noqa: BLE001
            last = f"{type(e).__name__}: {e}"
        time.sleep(2 * (attempt + 1))
    return None, {}, last


# ---------------------------------------------------------------- run


def all_digests():
    out, skipped = [], []
    for cell in CELLS:
        for cut in CUTS:
            d = build_digest(cell, cut)
            if d is None:
                skipped.append((cell, cut))
            else:
                out.append(d)
    return out, skipped


def run(samples=SAMPLES, limit=0):
    digests, skipped = all_digests()
    print(f"digests: {len(digests)}  skipped cut points: {len(skipped)}")
    for c, k in skipped:
        print(f"  skip {c} @ {k} (transcript never reaches {k} rounds)")
    for d in digests:
        print(f"  {d['cell']} @{d['cut']}: {d['est_tokens']} tok, "
              f"{d['calls']} calls, {d['files']} files")

    # resume: a stalled run's completed reads are kept, not paid for twice
    have = set()
    if os.path.exists(ANSWERS):
        for l in open(ANSWERS):
            try:
                r = json.loads(l)
            except Exception:                           # noqa: BLE001
                continue
            if r.get("answer"):
                have.add((r["cell"], r["cut"], r["model"], r["sample"]))

    jobs = []
    for s in range(samples):
        for d in digests:
            for m in MODELS:
                if (d["cell"], d["cut"], m, s) in have:
                    continue
                jobs.append({**d, "model": m, "sample": s})
    total_left = len(jobs)
    # This sandbox wedges a long-lived process's outbound sockets after a few
    # dozen requests: the server accepts and never replies, and every worker
    # blocks. A fresh short process is unaffected. So the sweep runs in chunks;
    # --limit caps one chunk and the resume above picks up where it stopped.
    if limit:
        jobs = jobs[:limit]
    print(f"jobs this chunk: {len(jobs)} of {total_left} left "
          f"({len(have)} already answered)")

    out = open(ANSWERS, "a")
    done = [0]

    def work(j):
        msgs = [{"role": "system", "content": SYSTEM_PROMPT},
                {"role": "user", "content": j["digest"] + "\n\n" + ASK_QUESTION}]
        txt, usage, err = call(j["model"], msgs)
        dec, shape, parts = ("ERROR", "", 0) if txt is None else decide(txt)
        rec = {"cell": j["cell"], "task": j["cell"].split("-")[3],
               "cut": j["cut"], "model": j["model"], "sample": j["sample"],
               "answer": txt, "decision": dec, "shape": shape, "parts": parts,
               "digest_tokens": j["est_tokens"], "digest_calls": j["calls"],
               "usage": usage, "error": err}
        with _lock:
            out.write(json.dumps(rec) + "\n")
            out.flush()
            done[0] += 1
            print(f"  {done[0]}/{len(jobs)} {j['cell'].split('-')[2]}@{j['cut']} "
                  f"{j['model'].split('/')[-1]} s{j['sample']} -> {dec} "
                  f"| ${spent():.4f}", flush=True)
        return rec

    with ThreadPoolExecutor(max_workers=CONCURRENCY) as ex:
        list(ex.map(work, jobs))
    out.close()
    print("spend this chunk: $%.4f" % spent(),
          {m: round(v[2], 4) for m, v in _usage.items()})
    print("REMAINING", total_left - len(jobs))
    if total_left - len(jobs) <= 0:
        score()


# ---------------------------------------------------------------- report


def score():
    seen = {}
    for l in open(ANSWERS):
        r = json.loads(l)
        k = (r["cell"], r["cut"], r["model"], r["sample"])
        if k not in seen or (r.get("answer") and not seen[k].get("answer")):
            seen[k] = r          # dedupe a resumed run; prefer a real answer
    recs = list(seen.values())
    for r in recs:
        if r.get("answer") is not None:
            r["decision"], r["shape"], r["parts"] = decide(r["answer"])

    digests, skipped = all_digests()
    dmap = {(d["cell"], d["cut"]): d for d in digests}

    L = ["# REPLAY-2: a DIGEST mark reader, off-policy", ""]
    L.append(f"Generated {time.strftime('%Y-%m-%d %H:%M')} | {len(recs)} completions "
             f"| temp {TEMPERATURE} | {SAMPLES} samples per cell x cut x model")
    L.append("")
    L.append("**What this tests.** In live wave 1e the mastermind mark reader read "
             "the RAW transcript at rounds 10/20/40 of the four-issue batch cell "
             "(57k / 65k / 91k tokens) and answered CONTINUE all three times, at "
             "$0.62. Here the same question is asked of a deterministic DIGEST "
             "instead: the person's ask verbatim, one line per tool call with NO "
             "results, the last thing said (600 chars), and the files touched - "
             "bounded to ~6k tokens.")
    L.append("")
    L.append("Answers are parsed exactly as `internal/session/checkpoint.go` parses "
             "them: first non-empty line, stripped of `` ` ``/`*`/`_`, split on "
             "`|` at bracket depth 0; **>= 2 top-level parts -> SPLIT**; a chain, a "
             "fork nested behind a step, or `(done)` -> CONTINUE. AMBIG is recorded "
             "when the first line carries no arrow/bar grammar at all (checkpoint.go "
             "fails those open to CONTINUE; both readings are given below).")
    L.append("")
    L.append("Ground truth: `batch` -> SPLIT at rounds 10 and 20 (four independent "
             "issues). `20`/`21`/`22`/`23` -> CONTINUE at every cut. The batch cell "
             "at round 40 is **recorded, not scored** - by then a short chain is an "
             "honest answer.")
    L.append("")
    if skipped:
        L.append("Skipped cut points (transcript never reaches them): " +
                 ", ".join(f"`{c}` @{k}" for c, k in skipped) + ".")
        L.append("")

    # ---- score table
    agg = defaultdict(lambda: {"tok": 0, "tn": 0, "to": 0, "amb": 0, "n": 0,
                               "cost": 0.0, "batch": defaultdict(int),
                               "dtok": []})
    for r in recs:
        k = (r["model"], r["cut"])
        a = agg[k]
        a["n"] += 1
        a["cost"] += (r.get("usage") or {}).get("cost", 0) or 0
        a["dtok"].append(r["digest_tokens"])
        if r["decision"] == "AMBIG":
            a["amb"] += 1
        if r["task"] == "batch":
            a["batch"][r["decision"]] += 1
        else:
            a["tn"] += 1
            a["to"] += r["decision"] == "CONTINUE"

    L.append("## Score table")
    L.append("")
    L.append("| model | cut | trap acc (20/21/22/23 -> CONTINUE) | batch decision "
             "(truth SPLIT) | ambig | mean digest tokens | mean $/read |")
    L.append("|---|---|---|---|---|---|---|")
    for m in MODELS:
        for c in CUTS:
            a = agg.get((m, c))
            if not a or not a["n"]:
                continue
            b = "/".join(f"{k} x{v}" for k, v in sorted(a["batch"].items())) or "-"
            note = " *(unscored)*" if c == 40 else ""
            L.append(f"| {m.split('/')[-1]} | {c} | "
                     f"{a['to']/a['tn'] if a['tn'] else 0:.2f} ({a['to']}/{a['tn']}) | "
                     f"{b}{note} | {a['amb']/a['n']:.2f} | "
                     f"{sum(a['dtok'])//len(a['dtok']):,} | "
                     f"${a['cost']/a['n']:.5f} |")
    L.append("")

    # combined per model over the SCORED cuts (10 and 20)
    L.append("### Combined over the scored cuts (rounds 10 and 20)")
    L.append("")
    L.append("| model | trap acc | batch -> SPLIT | ambig | mean $/read |")
    L.append("|---|---|---|---|---|")
    for m in MODELS:
        to = tn = wo = wn = amb = n = 0
        cost = 0.0
        for r in recs:
            if r["model"] != m or r["cut"] == 40:
                continue
            n += 1
            cost += (r.get("usage") or {}).get("cost", 0) or 0
            amb += r["decision"] == "AMBIG"
            if r["task"] == "batch":
                wn += 1
                wo += r["decision"] == "SPLIT"
            else:
                tn += 1
                to += r["decision"] == "CONTINUE"
        if not n:
            continue
        L.append(f"| {m.split('/')[-1]} | {to/tn if tn else 0:.2f} ({to}/{tn}) | "
                 f"{wo/wn if wn else 0:.2f} ({wo}/{wn}) | {amb/n:.2f} | "
                 f"${cost/n:.5f} |")
    L.append("")

    # ---- per cell x cut
    L.append("## Per cell x cut decision distribution")
    L.append("")
    L.append("| cell | cut | truth | " +
             " | ".join(m.split("/")[-1] for m in MODELS) + " |")
    L.append("|---|---|---|" + "---|" * len(MODELS))
    per = defaultdict(list)
    for r in recs:
        per[(r["cell"], r["cut"], r["model"])].append(r["decision"])
    for cell in CELLS:
        for cut in CUTS:
            if (cell, cut) not in dmap:
                continue
            task = cell.split("-")[3]
            truth = TRUTH[task] if not (task == "batch" and cut == 40) else "(either)"
            row = [cell, str(cut), truth]
            for m in MODELS:
                ds = per.get((cell, cut, m), [])
                cnt = defaultdict(int)
                for d in ds:
                    cnt[d] += 1
                row.append("/".join(f"{k}x{v}" for k, v in sorted(cnt.items())) or "-")
            L.append("| " + " | ".join(row) + " |")
    L.append("")

    # ---- the batch digest at round 10, verbatim. One cell: the two batch cells
    # share the same ask verbatim and differ only in the ledger.
    for cell in ("aforge-final-crew-batch-s1",):
        d = dmap.get((cell, 10))
        if not d:
            continue
        L.append(f"## The digest the reader sees: `{cell}` at round 10 "
                 f"({d['est_tokens']} est. tokens, {d['chars']:,} chars)")
        L.append("")
        # four backticks: the issue text itself contains a ```bash fence
        L.append("````")
        L.append(d["digest"])
        L.append("````")
        L.append("")

    # ---- verbatim answers
    L.append("## Verbatim answers")
    L.append("")
    picks = []
    for want, test in (
            ("batch @10, kimi", lambda r: r["task"] == "batch" and r["cut"] == 10
             and r["model"].startswith("moonshot") and r["sample"] == 0),
            ("batch @10, flash", lambda r: r["task"] == "batch" and r["cut"] == 10
             and r["model"].startswith("deepseek") and r["sample"] == 0),
            ("batch @20, kimi", lambda r: r["task"] == "batch" and r["cut"] == 20
             and r["model"].startswith("moonshot") and r["sample"] == 0),
            ("trap @20, kimi", lambda r: r["task"] != "batch" and r["cut"] == 20
             and r["model"].startswith("moonshot") and r["sample"] == 0),
            ("batch @40, kimi", lambda r: r["task"] == "batch" and r["cut"] == 40
             and r["model"].startswith("moonshot") and r["sample"] == 0)):
        for r in recs:
            if test(r) and r.get("answer"):
                picks.append((want, r))
                break
    for want, r in picks:
        L.append(f"**{want}** - `{r['cell']}` -> `{r['decision']}` "
                 f"({r['parts']} top-level parts)")
        L.append("")
        L.append("> " + (r["answer"][:900].replace("\n", "\n> ")))
        L.append("")

    # ---- the bracket rule
    L.append("## The bracket rule is what loses the batch, not the digest")
    L.append("")
    L.append("`checkpoint.go` counts parts at the TOP level of the whole line, so "
             "`(A | B | C) > D` - three jobs that can all start now, joined by a "
             "test run at the end - counts as **one** part and reads CONTINUE. "
             "That is the shape the readers actually write for the batch cell. "
             "The diagnostic column below re-reads each shape by its FIRST STAGE "
             "instead: take the segment before the first top-level `>`, strip one "
             "enclosing bracket, count inside. `A > (B | C)` still reads CONTINUE "
             "(one job stands in front), which is the property the strict rule was "
             "adopted for.")
    L.append("")
    L.append("| model | cut | trap acc (top-level) | batch (top-level) | "
             "trap acc (first-stage) | batch (first-stage) |")
    L.append("|---|---|---|---|---|---|")
    for m in MODELS:
        for c in CUTS:
            rs = [r for r in recs if r["model"] == m and r["cut"] == c]
            if not rs:
                continue
            cells_ = [[0, 0], [0, 0]]      # [top, first] x [ok, n] for traps
            bt, bf = defaultdict(int), defaultdict(int)
            for r in rs:
                fd = decide_first_stage(r["answer"] or "")
                if r["task"] == "batch":
                    bt[r["decision"]] += 1
                    bf[fd] += 1
                else:
                    cells_[0][1] += 1
                    cells_[1][1] += 1
                    cells_[0][0] += r["decision"] == "CONTINUE"
                    cells_[1][0] += fd == "CONTINUE"
            f = lambda d: "/".join(f"{k} x{v}" for k, v in sorted(d.items())) or "-"
            L.append(
                f"| {m.split('/')[-1]} | {c} | "
                f"{cells_[0][0]}/{cells_[0][1]} | {f(bt)} | "
                f"{cells_[1][0]}/{cells_[1][1]} | {f(bf)} |")
    L.append("")

    # ---- what the first-stage reading would cost on the traps
    L.append("### What the first-stage reading costs on the traps")
    L.append("")
    L.append("Every trap read where the two rules disagree - the strict rule says "
             "CONTINUE (right) and the first-stage rule says SPLIT (wrong):")
    L.append("")
    bad = [r for r in recs if r["task"] != "batch"
           and r["decision"] == "CONTINUE"
           and decide_first_stage(r["answer"] or "") == "SPLIT"]
    if bad:
        L.append("| cell | cut | model | sample | shape |")
        L.append("|---|---|---|---|---|")
        for r in sorted(bad, key=lambda r: (r["cell"], r["cut"])):
            L.append(f"| {r['cell']} | {r['cut']} | {r['model'].split('/')[-1]} | "
                     f"{r['sample']} | `{r['shape'][:90].replace('|', chr(92)+'|')}` |")
    else:
        L.append("None.")
    L.append("")

    # ---- every batch shape line
    L.append("## Every batch shape line")
    L.append("")
    L.append("| cell | cut | model | sample | shape | top-level parts | decision | "
             "first-stage reading |")
    L.append("|---|---|---|---|---|---|---|---|")
    for r in sorted(recs, key=lambda r: (r["cut"], r["cell"], r["model"], r["sample"])):
        if r["task"] != "batch":
            continue
        s = (r["shape"] or "(none)")[:110].replace("|", "\\|")
        L.append(f"| {r['cell'].split('-')[2]} | {r['cut']} | "
                 f"{r['model'].split('/')[-1]} | {r['sample']} | `{s}` | "
                 f"{r['parts']} | {r['decision']} | "
                 f"{decide_first_stage(r['answer'] or '')} |")
    L.append("")

    # ---- flash's failure mode on the batch
    L.append("## deepseek-v4-flash's failure mode: it declares the batch finished")
    L.append("")
    L.append("On the batch cell the low-tier reader does not draw a narrow shape - "
             "it writes `(done)`, on a digest whose FILES list shows two of the four "
             "issues barely started. This is not a parser problem and no rule change "
             "recovers it.")
    L.append("")
    for r in recs:
        if (r["task"] == "batch" and r["model"].startswith("deepseek")
                and DONE_RX.match(r["shape"] or "")):
            L.append(f"`{r['cell']}` @{r['cut']} sample {r['sample']}:")
            L.append("")
            L.append("> " + (r["answer"][:600].replace("\n", "\n> ")))
            L.append("")
            break
    n_done = sum(1 for r in recs if r["task"] == "batch"
                 and r["model"].startswith("deepseek") and DONE_RX.match(r["shape"] or ""))
    n_b = sum(1 for r in recs if r["task"] == "batch" and r["model"].startswith("deepseek"))
    k_done = sum(1 for r in recs if r["task"] == "batch"
                 and r["model"].startswith("moonshot") and DONE_RX.match(r["shape"] or ""))
    L.append(f"`(done)` on the batch cell: **deepseek-v4-flash {n_done}/{n_b}** reads, "
             f"kimi-k3 {k_done}/{n_b}.")
    L.append("")

    # ---- determinism at temp 0
    L.append("## Determinism at temp 0")
    L.append("")
    L.append("Shape wording drifts at temp 0 on both models; the DECISION taken off "
             "the shape is far steadier, which is the only thing the harness reads.")
    L.append("")
    L.append("| model | identical shape line 3/3 | identical decision 3/3 |")
    L.append("|---|---|---|")
    vs, vd = defaultdict(set), defaultdict(set)
    for r in recs:
        vs[(r["cell"], r["cut"], r["model"])].add(r["shape"])
        vd[(r["cell"], r["cut"], r["model"])].add(r["decision"])
    for m in MODELS:
        ks = [k for k in vs if k[2] == m]
        L.append(f"| {m.split('/')[-1]} | "
                 f"{sum(1 for k in ks if len(vs[k]) == 1)}/{len(ks)} | "
                 f"{sum(1 for k in ks if len(vd[k]) == 1)}/{len(ks)} |")
    L.append("")

    # ---- spend
    tot = sum((r.get("usage") or {}).get("cost", 0) or 0 for r in recs)
    tin = sum((r.get("usage") or {}).get("prompt_tokens", 0) for r in recs)
    tout = sum((r.get("usage") or {}).get("completion_tokens", 0) for r in recs)
    bym = defaultdict(float)
    for r in recs:
        bym[r["model"]] += (r.get("usage") or {}).get("cost", 0) or 0
    L.append("## Spend")
    L.append("")
    L.append(f"- total on the {len(recs)} scored reads: **${tot:.4f}** "
             f"({tin:,} prompt tokens, {tout:,} completion tokens)")
    for m, c in sorted(bym.items()):
        n = sum(1 for r in recs if r["model"] == m)
        L.append(f"- {m}: ${c:.4f} over {n} reads (${c/n:.5f} per read)")
    L.append("- roughly $0.31 more went on probes and two runs that wedged before "
             "the deadlock in this script's own progress line was found (their "
             "completed reads were resumed, not repaid). Lane total under $1.05 "
             f"against the ${BUDGET_CAP:.0f} cap.")
    L.append("")
    L.append("For scale: the live wave-1e reader spent **$0.62 on three raw reads** "
             "of one cell. Sixty kimi reads of the digest - every cell, every cut, "
             "three samples - cost $0.71.")
    L.append("")

    L.append("## Recommendation")
    L.append("")
    L.append(
        "**The digest fixes the reading, and `checkpoint.go`'s parser then throws "
        "the fix away.** Live, on the raw 57k/65k/91k-token transcript, the reader "
        "answered CONTINUE on the batch three times out of three and cost $0.62. On "
        "the digest - 876 tokens at round 10, 998 at round 20, 1,591 at round 40 - "
        "kimi-k3 draws the fork in **6 of 6** batch reads at round 10 (`(A | B | C) "
        "> D`, `(A | B | C | D) > E`, `A | B > C | D > E | F > G`), and its legends "
        "name the right four issues. It also never once loses a trap: **36/36** on "
        "cells 20/21/22/23 across both cut points, with zero ambiguity. The whole "
        "sweep of 120 reads cost $0.73, and one digest read costs **$0.0118** "
        "against roughly $0.207 for one live raw read - about an 18x saving with a "
        "context 60x smaller. So the digest shape and the question are right and "
        "should ship. What must ship WITH them is a change to how the first line is "
        "counted: `topLevelParts` reads `(A | B | C) > D` as one part, and that one "
        "shape is what the reader writes for a genuinely four-way batch, so the "
        "strict rule turns a correct reading into a CONTINUE and the batch recall "
        "stays at 2/12. Counting the FIRST STAGE instead - the segment before the "
        "first top-level `>`, with one enclosing bracket stripped - takes kimi's "
        "batch recall to 6/6 at round 10 and 8/12 over both scored cuts while "
        "keeping `A > (B | C)` a CONTINUE, and it costs only 4 trap reads out of "
        "72, every one of them a real two-way `(A | B) > C`. The cheaper "
        "alternative, if the parser must stay as it is, is to change the ASK: its "
        "own example line `'A > (B | C)'` is what teaches the bracketed form, so "
        "dropping that example and saying plainly that parts which can start now go "
        "at the top level with no brackets should move the same reads without "
        "touching the counting rule. **The reader cannot drop to the low tier.** "
        "deepseek-v4-flash matches kimi on the traps (34/36) and costs 32x less "
        "($0.00037 a read), but on the batch it answers `(done)` in **15 of 18** "
        "reads - it declares four half-finished issues finished, on a digest whose "
        "own FILES list contradicts it. That is a comprehension failure, not a "
        "parsing one, and no rule change recovers it: a mark reader that says "
        "`(done)` mid-turn is worse than no mark reader. Keep the mastermind tier, "
        "ship the digest, and fix the counting.")
    L.append("")

    open(RESULTS_MD, "w").write("\n".join(L) + "\n")
    print(f"wrote {RESULTS_MD}")


def probe():
    digests, skipped = all_digests()
    for c, k in skipped:
        print(f"SKIP {c} @{k}")
    for d in digests:
        print(f"{d['cell']} @{d['cut']}: est_tokens={d['est_tokens']} "
              f"chars={d['chars']} calls={d['calls']} files={d['files']}")


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--probe", action="store_true")
    ap.add_argument("--show", default=None, help="CELL:CUT")
    ap.add_argument("--score-only", action="store_true")
    ap.add_argument("--samples", type=int, default=SAMPLES)
    ap.add_argument("--limit", type=int, default=0,
                    help="cap this chunk; rerun to continue (see run())")
    a = ap.parse_args()
    if a.probe:
        probe()
    elif a.show:
        cell, cut = a.show.rsplit(":", 1)
        print(build_digest(cell, int(cut))["digest"])
    elif a.score_only:
        score()
    else:
        SAMPLES = a.samples
        run(a.samples, a.limit)
