#!/usr/bin/env python3
"""Off-policy replay evaluation of mid-turn escalation questions.

Reads recorded aforge benchmark transcripts, truncates each at ~10 completed
tool rounds, appends ONE candidate checkpoint question as a user injection,
and asks two OpenRouter models what they would do. Scores the resulting
decision distribution (SPLIT vs CONTINUE vs AMBIG) per variant per model.

Usage:
  python3 replay.py                 # full run (writes answers.jsonl + RESULTS.md)
  python3 replay.py --probe         # one call, print reconstruction stats
  python3 replay.py --dry           # reconstruct only, print token estimates
  python3 replay.py --score-only    # re-score existing answers.jsonl
"""
import argparse
import glob
import json
import os
import re
import sys
import time
import threading
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor

import requests

HERE = os.path.dirname(os.path.abspath(__file__))
RESULTS_ROOT = os.path.join(os.path.dirname(HERE), "results")
ANSWERS = os.path.join(HERE, "answers.jsonl")
RESULTS_MD = os.path.join(HERE, "RESULTS.md")

CELLS = [f"aforge-pre-{fam}-{t}-s1"
         for fam in ("crew", "flash")
         for t in ("20", "21", "22", "23", "batch")]

# Ground truth at the checkpoint mark.
TRUTH = {"20": "CONTINUE", "21": "CONTINUE", "22": "CONTINUE",
         "23": "CONTINUE", "batch": "SPLIT"}

MODELS = ["deepseek/deepseek-v4-flash", "moonshotai/kimi-k3"]
SAMPLES = 3
TEMPERATURE = 0.7
MAX_ROUNDS = 10
TOOL_CLIP = 500          # chars per tool result, per spec
ARG_CLIP = 500           # chars per tool-call argument string (same for all variants)
ASST_CLIP = 2000         # chars per assistant prose block
CONCURRENCY = 8
# Both models are reasoning models: reasoning tokens are emitted BEFORE content,
# so too small a cap yields empty content. Both keep their default reasoning
# effort (fidelity with the crew tier). The cap only truncates the tail of the
# answer (the propose_task brief that A and B ask for) - the stated decision
# always comes in the first sentence, which is all we score. Identical per model
# across all four variants, so the comparison stays fair.
MAX_TOKENS = {"deepseek/deepseek-v4-flash": 2500, "moonshotai/kimi-k3": 1200}

# Hard live budget breaker (spec cap $5). Seeded with spend already incurred by
# an earlier aborted run of this script.
BUDGET_CAP = 4.55
PRIOR_SPEND = 0.7692

# The system prompt is NOT recorded in these transcripts (no role="system" line
# exists in any of them). We therefore use this synthesized minimal stand-in,
# identical for every cell / variant / model, so the comparison stays fair.
SYSTEM_PROMPT = (
    "You are a coding assistant working inside a user's repository. You are in "
    "the middle of a task: you have already run a number of tool calls and have "
    "more work left to do. You have the usual file and shell tools, and you also "
    "have a propose_task tool that hands a self-contained piece of work to a "
    "separate worker who runs on its own, in its own session, and can see nothing "
    "of this conversation — whatever they need must be written into the brief. "
    "Answer the user directly and concisely."
)

VARIANTS = {
    "A": ("current",
          "[checkpoint] This answer has now cost more than handing it over would "
          "have. Say in one line what is still left: one job, or several "
          "independent parts. Several parts — hand it over now with propose_task, "
          "and write into the brief everything you have learned here, because "
          "whoever takes it cannot see any of this. One job — say what is left, "
          "and carry on."),
    "B": ("delegation test",
          "[checkpoint] Stop for one line. Would a short note telling someone else "
          "what remains take less time to write than doing what remains yourself? "
          "If yes, say HAND OVER and write that note now — everything you have "
          "learned, so they need none of this conversation. If no, say what remains "
          "and carry on."),
    "C": ("dependency sketch",
          "[checkpoint] In one line, sketch what remains as parts and arrows: "
          "independent parts separated by ' | ', ordered steps joined by ' > '. "
          "Example shapes: 'A | B | C' or 'A > B > C' or 'A > (B | C)'. Nothing "
          "else on that line. Then one sentence saying what each letter is."),
    "D": ("recruitment",
          "[checkpoint] Two extra hands are free right now and can start "
          "immediately. Name anything in what remains that they could each take "
          "start-to-finish without seeing this conversation — or say NO, NOTHING "
          "SEPARABLE and carry on alone."),
}

# ---------------------------------------------------------------- transcript


def clip(s, n, tag="[clipped]"):
    s = s if isinstance(s, str) else json.dumps(s)
    if len(s) <= n:
        return s
    return s[:n] + " ... " + tag


def clip_args(raw):
    """Clip long string values inside a tool call's JSON arguments."""
    try:
        obj = json.loads(raw)
    except Exception:
        return clip(raw, ARG_CLIP)
    if not isinstance(obj, dict):
        return clip(raw, ARG_CLIP)
    out = {}
    for k, v in obj.items():
        out[k] = clip(v, ARG_CLIP) if isinstance(v, str) else v
    return json.dumps(out)


def find_transcript(cell):
    pat = os.path.join(RESULTS_ROOT, cell, "profile", "v3", "projects",
                       "*", "*", "transcript.jsonl")
    hits = sorted(glob.glob(pat))
    # main conversation only: never a tasks/ sub-transcript
    hits = [h for h in hits if "/tasks/" not in h]
    if len(hits) != 1:
        raise RuntimeError(f"{cell}: expected 1 main transcript, found {len(hits)}")
    return hits[0]


def reconstruct(cell):
    """Return (messages, stats). messages ends after <=MAX_ROUNDS complete rounds."""
    path = find_transcript(cell)
    raw = [json.loads(l) for l in open(path) if l.strip()]
    msgs = [d for d in raw if d.get("type") == "message"]

    if any(d.get("role") == "system" for d in msgs):
        raise RuntimeError(f"{cell}: unexpected recorded system prompt; handle it")

    out = [{"role": "system", "content": SYSTEM_PROMPT}]
    rounds = 0
    i = 0
    pending = []          # buffer for the round currently being assembled
    open_ids = set()
    first_user_chars = 0

    while i < len(msgs):
        d = msgs[i]
        role = d.get("role")
        if role == "user":
            if not pending and not open_ids:
                txt = d.get("content") or ""
                first_user_chars = max(first_user_chars, len(txt))
                out.append({"role": "user", "content": txt})
            else:
                break     # a mid-turn user message: stop, we only replay one turn
            i += 1
            continue

        if role == "assistant":
            if open_ids:          # previous round never completed -> stop
                break
            tcs = d.get("toolCalls") or []
            m = {"role": "assistant",
                 "content": clip(d.get("content") or "", ASST_CLIP)}
            if tcs:
                m["tool_calls"] = [{
                    "id": t["id"],
                    "type": "function",
                    "function": {"name": t["function"]["name"],
                                 "arguments": clip_args(t["function"].get("arguments", "{}"))},
                } for t in tcs]
                open_ids = {t["id"] for t in tcs}
            pending = [m]
            if not tcs:
                # a bare assistant message (turn finished) -> stop here
                out.extend(pending)
                pending = []
                break
            i += 1
            continue

        if role == "tool":
            if not pending:
                i += 1
                continue
            tcid = d.get("toolCallId")
            pending.append({"role": "tool", "tool_call_id": tcid,
                            "content": clip(d.get("content") or "", TOOL_CLIP)})
            open_ids.discard(tcid)
            if not open_ids:              # round complete
                out.extend(pending)
                pending = []
                rounds += 1
                if rounds >= MAX_ROUNDS:
                    i += 1
                    break
            i += 1
            continue
        i += 1

    if rounds == 0:
        raise RuntimeError(f"{cell}: no complete tool round reconstructed")

    chars = sum(len(json.dumps(m)) for m in out)
    stats = {"transcript": path, "rounds": rounds, "messages": len(out),
             "chars": chars, "est_tokens": chars // 4,
             "total_rounds_available": sum(
                 1 for d in msgs if d.get("role") == "assistant" and d.get("toolCalls"))}
    return out, stats


# ---------------------------------------------------------------- openrouter

_lock = threading.Lock()
_usage = defaultdict(lambda: [0, 0, 0.0])   # model -> [prompt, completion, cost]

PRICE = {  # $ per token, from openrouter /models at run time
    "deepseek/deepseek-v4-flash": (0.000000088606, 0.000000177212),
    "moonshotai/kimi-k3": (0.000003, 0.000015),
}


def spent():
    with _lock:
        return PRIOR_SPEND + sum(v[2] for v in _usage.values())


def call(model, messages, temperature, seed_tag):
    if spent() >= BUDGET_CAP:
        return None, {}, "budget cap reached"
    key = os.environ["OPENROUTER_API_KEY"]
    body = {"model": model, "messages": messages, "temperature": temperature,
            "max_tokens": MAX_TOKENS.get(model, 1200)}
    last = None
    for attempt in range(4):
        try:
            r = requests.post(
                "https://openrouter.ai/api/v1/chat/completions",
                headers={"Authorization": f"Bearer {key}",
                         "Content-Type": "application/json"},
                json=body, timeout=180)
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
                             "finish": j["choices"][0].get("finish_reason"),
                             "reasoning_chars": len(msg.get("reasoning") or "")}, None
            last = f"HTTP {r.status_code}: {r.text[:400]}"
        except Exception as e:                       # noqa: BLE001
            last = f"{type(e).__name__}: {e}"
        time.sleep(2 * (attempt + 1))
    return None, {}, last


# ---------------------------------------------------------------- scoring

NO_SEP = re.compile(r"\bno[,:]?\s*nothing\s+separable\b", re.I)
HAND_OVER = re.compile(r"\bhand[\s\-]?(over|it over|this over)\b", re.I)


def sketch_line(text):
    """First line of a C answer that looks like a shape."""
    seen = 0
    for ln in text.splitlines():
        s = ln.strip().strip("`").strip()
        if not s:
            continue
        seen += 1
        if seen > 3:                 # the shape must come first, per the prompt
            break
        s = re.sub(r"^(shape|sketch|line)\s*[:\-]\s*", "", s, flags=re.I)
        if len(s) <= 200 and re.search(r"[|>]", s):
            return s
    return None


def score_answer(variant, text):
    """-> (decision, reason). decision in SPLIT / CONTINUE / AMBIG."""
    t = text.strip()
    if not t:
        return "IGNORED", "empty answer"
    # The single most important failure mode: the model does not answer the
    # checkpoint at all and simply keeps working. Native tool_calls with no
    # prose, or pure "let me go read X" prose with no decision, are both this.
    if t.startswith("[tool_call]"):
        return "IGNORED", "answered with a tool call, not a decision"
    low = t.lower()
    # the decision is stated up front in every variant; kimi sometimes emits its
    # tool call inline as one giant line, so clip each leading line as well.
    lines = [ln for ln in t.splitlines() if ln.strip()][:4]
    head = "\n".join(ln[:400] for ln in lines).lower()

    if variant == "A":
        one = re.search(r"\b(one job|a single job|single job|one coherent job|"
                        r"single coherent job|one continuous job)\b", head)
        several = re.search(r"\bseveral (independent )?(parts|pieces|jobs)\b", head) or \
            re.search(r"\b(four|three|two|multiple) independent\b", head) or \
            re.search(r"\bindependent parts\b", head)
        handed = re.search(r"\bhand(ing|s|ed)?\s*(it\s+|this\s+)?over\b", head) or \
            "propose_task" in head
        if several and one:
            return "AMBIG", "says one job AND several parts"
        if several or handed:
            return "SPLIT", "several parts / hand over"
        if one:
            return "CONTINUE", "one job"
        if re.search(r"\b(carry on|carrying on|continue myself|continuing)\b", low):
            return "CONTINUE", "carry on, no split marker"
        return (decided_nothing_left(t) or ignored_or_ambig(t))

    if variant == "B":
        # the instruction makes HAND OVER a leading literal token; a mention of a
        # "hand-over note" inside a comparison is not a decision.
        lead = t.strip()[:80].lower()
        if re.match(r"[^a-z0-9]{0,6}(\*\*)?hand[\s\-]?over\b", lead) or \
                re.search(r"^\s*(\*\*)?hand[\s\-]?over\b", low, re.M):
            return "SPLIT", "leading HAND OVER"
        neg = re.search(r"\b(no|not|don'?t|would not|wouldn'?t|never|rather than|"
                        r"instead of|no need for a?)\s[^.\n]{0,40}hand[\s\-]?over\b", head)
        if neg or re.match(r"[^a-z0-9]{0,6}no\b", lead):
            return "CONTINUE", "declines hand over / leading no"
        if re.search(r"\b(carry on|carrying on|continue myself|doing it myself|"
                     r"faster to (just )?do|quicker to (just )?do)\b", low):
            return "CONTINUE", "carry on"
        if re.search(r"\bhand[\s\-]?over\b", head):
            return "AMBIG", "hand over mentioned, not as leading decision"
        return (decided_nothing_left(t) or ignored_or_ambig(t))

    if variant == "C":
        s = sketch_line(t)
        if s is None:
            # the model may still have named exactly one remaining atom, or said
            # there is nothing left; both are CONTINUE.
            d = decided_nothing_left(t)
            if d:
                return d
            first = next((ln.strip().strip("`") for ln in t.splitlines()
                          if ln.strip()), "")
            if 0 < len(first) <= 90 and not is_ignored_prose(t):
                return "CONTINUE", f"single atom, no operator: {first[:70]}"
            return ignored_or_ambig(t, "no shape line")
        core = re.sub(r"[`*_\s]", "", s)
        if core in ("|", "|_", "_|_", ">", "_>_"):
            return "AMBIG", f"degenerate shape: {s[:60]}"
        if "|" in s:
            return "SPLIT", f"pipe in shape: {s[:80]}"
        if ">" in s:
            return "CONTINUE", f"pure chain: {s[:80]}"
        return "AMBIG", f"shape without operators: {s[:80]}"

    if variant == "D":
        nosep = NO_SEP.search(t) or \
            re.search(r"\bnothing\s+[a-z ]{0,20}separable\b", low) or \
            re.search(r"\b(not|isn'?t|aren'?t)\s+[a-z ]{0,20}separable\b", low)
        names = re.search(r"\b(hand\s*(1|2|one|two|a|b)\b|"
                          r"(they|each|one hand|the other)\s+(could|can|should)\s+take\b|"
                          r"^yes\b|yes[,:—-]|two separable|separable parts?\b|"
                          r"(is|are)\s+(fully|perfectly|cleanly|entirely)?\s*separable\b|"
                          r"hand (that|it|this|one|#?\d+) off|"
                          r"could each take|take start-to-finish)\b", low, re.M)
        if names and not re.match(r"[^a-z0-9]{0,6}no\b", low):
            return "SPLIT", "names separable work"
        if nosep:
            return "CONTINUE", "nothing separable"
        if re.search(r"\b(carry on alone|alone|carrying on)\b", low):
            return "CONTINUE", "carry on alone"
        return (decided_nothing_left(t) or ignored_or_ambig(t))

    return "AMBIG", "unknown variant"


IGNORED_PROSE = re.compile(
    r"^\s*(ok(ay)?[,.]?\s*)?(let me|i('ll| will| need to| should)\s+(read|check|"
    r"continue|proceed|look|get|re-?read|first|gather|examine|take another)|"
    r"understand[,.]|i need to continue|let's)\b", re.I)


def ignored_or_ambig(t, why="no clear marker"):
    if is_ignored_prose(t):
        return "IGNORED", "narrates the next action, states no decision"
    return "AMBIG", why


def is_ignored_prose(t):
    """Prose that never states a decision - it just narrates the next action."""
    first = next((ln.strip() for ln in t.splitlines() if ln.strip()), "")
    return bool(IGNORED_PROSE.match(first)) and len(t) < 700


NOTHING_LEFT = re.compile(
    r"\b(nothing (is )?(left|remains|remaining|to do)|no remaining work|"
    r"already (fully )?(complete|implemented|done)|"
    r"the work is (complete|done)|no work remains)\b", re.I)


def decided_nothing_left(t):
    """Uniform fallback, applied identically to all four variants: an explicit
    'there is nothing left / it is already done' is a CONTINUE (do not split)."""
    if NOTHING_LEFT.search(t):
        return "CONTINUE", "explicitly says nothing remains"
    return None


BATCH_ISSUES = {
    "20 workflow": r"(workflow|github action|\bci\b|pull request|\bpr\b|"
                   r"issue\s*#?20|validation\.yml|code-check)",
    "21 arithmetic": r"(arithmetic|operator|\bmath\b|issue\s*#?21|\barith\b)",
    "22 currency": r"(currency|currencies|\bmoney\b|\bcfa\b|issue\s*#?22|\bcurr\b)",
    "23 cli-extras": r"(optional (cli )?depend|\bextras?\b|cli depend|pyproject|"
                     r"issue\s*#?23|\bcli\b)",
}


def first_stage_pipe(shape):
    """True if the FIRST stage of the shape already forks, i.e. work can be
    handed out now. `(A|B|C) > D` -> True.  `A > (B|C)` -> False (one job first,
    a fork only later).  `A | B | C` -> True.  `A > B > C` -> False."""
    depth, stage = 0, []
    for ch in shape:
        if ch == "(":
            depth += 1
        elif ch == ")":
            depth = max(0, depth - 1)
        elif ch == ">" and depth == 0:
            break
        stage.append(ch)
    return "|" in "".join(stage)


def batch_sketch_coverage(text):
    """For variant C on the batch cell: which of the four real issues appear?"""
    low = text.lower()
    return [k for k, rx in BATCH_ISSUES.items() if re.search(rx, low)]


# ---------------------------------------------------------------- run

def build_jobs(dry=False):
    jobs, recon, dropped = [], {}, []
    for cell in CELLS:
        try:
            msgs, st = reconstruct(cell)
        except Exception as e:                        # noqa: BLE001
            dropped.append((cell, str(e)))
            continue
        recon[cell] = (msgs, st)
    # sample-major order: if the budget breaker fires, we lose a whole sample
    # round rather than skewing coverage of some cells/variants.
    for s in range(SAMPLES):
        for cell, (msgs, st) in recon.items():
            task = cell.split("-")[3]
            for v, (_, qtext) in VARIANTS.items():
                full = msgs + [{"role": "user", "content": qtext}]
                for model in MODELS:
                    jobs.append({"cell": cell, "task": task, "variant": v,
                                 "model": model, "sample": s, "messages": full})
    return jobs, recon, dropped


def run(samples=SAMPLES):
    global SAMPLES
    SAMPLES = samples
    jobs, recon, dropped = build_jobs()
    print(f"cells reconstructed: {len(recon)}  dropped: {len(dropped)}")
    for c, e in dropped:
        print(f"  DROPPED {c}: {e}")
    for c, (_, st) in recon.items():
        print(f"  {c}: rounds={st['rounds']}/{st['total_rounds_available']} "
              f"msgs={st['messages']} est_tokens={st['est_tokens']}")
    print(f"jobs: {len(jobs)}")

    out = open(ANSWERS, "w")
    done = [0]

    def work(j):
        try:
            txt, usage, err = call(j["model"], j["messages"], TEMPERATURE,
                                   f"{j['cell']}-{j['variant']}-{j['sample']}")
        except Exception as e:                        # noqa: BLE001
            txt, usage, err = None, {}, f"{type(e).__name__}: {e}"
        dec, why = ("ERROR", err) if txt is None else score_answer(j["variant"], txt)
        rec = {"cell": j["cell"], "task": j["task"], "variant": j["variant"],
               "model": j["model"], "sample": j["sample"], "answer": txt,
               "decision": dec, "why": why, "usage": usage, "error": err}
        with _lock:
            out.write(json.dumps(rec) + "\n")
            out.flush()
            done[0] += 1
            if done[0] % 20 == 0:
                print(f"  ... {done[0]}/{len(jobs)}")
        return rec

    with ThreadPoolExecutor(max_workers=CONCURRENCY) as ex:
        list(ex.map(work, jobs))
    out.close()
    print("spend:", {m: round(v[2], 4) for m, v in _usage.items()},
          "total $%.4f" % sum(v[2] for v in _usage.values()))
    score(recon)


def score(recon=None):
    recs = [json.loads(l) for l in open(ANSWERS)]
    # re-score from the stored raw answers so the parser can be revised offline
    for r in recs:
        if r.get("answer") is not None:
            r["decision"], r["why"] = score_answer(r["variant"], r["answer"])
    agg = defaultdict(lambda: {"trap_ok": 0, "trap_n": 0, "trap_split": 0,
                               "width_ok": 0, "width_n": 0, "ambig": 0,
                               "ignored": 0, "n": 0, "err": 0})
    for r in recs:
        k = (r["variant"], r["model"])
        a = agg[k]
        a["n"] += 1
        if r["decision"] == "ERROR":
            a["err"] += 1
            continue
        if r["decision"] == "AMBIG":
            a["ambig"] += 1
        if r["decision"] == "IGNORED":
            a["ignored"] += 1
        truth = TRUTH[r["task"]]
        if truth == "CONTINUE":
            a["trap_n"] += 1
            if r["decision"] == "CONTINUE":
                a["trap_ok"] += 1
            elif r["decision"] == "SPLIT":
                a["trap_split"] += 1
        else:
            a["width_n"] += 1
            if r["decision"] == "SPLIT":
                a["width_ok"] += 1

    lines = ["# Replay: off-policy evaluation of mid-turn escalation questions", ""]
    lines.append(f"Generated {time.strftime('%Y-%m-%d %H:%M')} | "
                 f"{len(recs)} completions | temp {TEMPERATURE} | "
                 f"{SAMPLES} samples/cell/variant/model")
    lines.append("")
    lines.append("System prompt: **synthesized stand-in** (no `system` role is "
                 "recorded in any transcript). Identical for every cell/variant/model.")
    lines.append("")
    lines.append("Ground truth at the mark: `batch` -> SPLIT (four independent "
                 "issues); `20`,`21`,`22`,`23` -> CONTINUE.")
    lines.append("")
    lines.append("## Score table")
    lines.append("")
    lines.append("Decision classes: SPLIT / CONTINUE / AMBIG (answered, no clear "
                 "decision) / IGNORED (did not answer the checkpoint at all - "
                 "emitted a tool call, or narrated the next action). "
                 "`unclear` below = AMBIG + IGNORED.")
    lines.append("")
    lines.append("| variant | model | trap-acc (20/21/22/23 -> CONTINUE) | "
                 "width-recall (batch -> SPLIT) | ambig | ignored | unclear | sum |")
    lines.append("|---|---|---|---|---|---|---|---|")
    for v in sorted(VARIANTS):
        for m in MODELS:
            a = agg[(v, m)]
            ta = a["trap_ok"] / a["trap_n"] if a["trap_n"] else 0.0
            wr = a["width_ok"] / a["width_n"] if a["width_n"] else 0.0
            am = a["ambig"] / a["n"] if a["n"] else 0.0
            ig = a["ignored"] / a["n"] if a["n"] else 0.0
            lines.append(f"| {v} ({VARIANTS[v][0]}) | {m.split('/')[-1]} | "
                         f"{ta:.2f} ({a['trap_ok']}/{a['trap_n']}) | "
                         f"{wr:.2f} ({a['width_ok']}/{a['width_n']}) | "
                         f"{am:.2f} | {ig:.2f} | {am+ig:.2f} | {ta+wr:.2f} |")
    lines.append("")
    lines.append("### Combined over both models")
    lines.append("")
    lines.append("| variant | trap-acc | width-recall | ambig | ignored | unclear | trap+width |")
    lines.append("|---|---|---|---|---|---|---|")
    combo = []
    for v in sorted(VARIANTS):
        to = tn = wo = wn = am = ig = n = 0
        for m in MODELS:
            a = agg[(v, m)]
            to += a["trap_ok"]; tn += a["trap_n"]
            wo += a["width_ok"]; wn += a["width_n"]
            am += a["ambig"]; ig += a["ignored"]; n += a["n"]
        ta = to / tn if tn else 0
        wr = wo / wn if wn else 0
        unclear = (am + ig) / n if n else 0
        combo.append((round(ta + wr, 4), -unclear, v))
        lines.append(f"| {v} ({VARIANTS[v][0]}) | {ta:.2f} ({to}/{tn}) | "
                     f"{wr:.2f} ({wo}/{wn}) | {am/n if n else 0:.2f} | "
                     f"{ig/n if n else 0:.2f} | {unclear:.2f} | {ta+wr:.2f} |")
    combo.sort(reverse=True)
    lines.append("")
    lines.append(f"**Winner: variant {combo[0][2]} "
                 f"({VARIANTS[combo[0][2]][0]})** by (trap-acc + width-recall), "
                 "ambiguity as tiebreak.")

    # per-cell decision distribution
    lines.append("")
    lines.append("## Per-cell decision distribution")
    lines.append("")
    lines.append("| cell | truth | " + " | ".join(
        f"{v}/{m.split('/')[-1][:6]}" for v in sorted(VARIANTS) for m in MODELS) + " |")
    lines.append("|---|---|" + "---|" * (len(VARIANTS) * len(MODELS)))
    percell = defaultdict(list)
    for r in recs:
        percell[(r["cell"], r["variant"], r["model"])].append(r["decision"])
    for cell in CELLS:
        task = cell.split("-")[3]
        row = [cell, TRUTH[task]]
        any_row = False
        for v in sorted(VARIANTS):
            for m in MODELS:
                ds = percell.get((cell, v, m), [])
                if ds:
                    any_row = True
                c = defaultdict(int)
                for d in ds:
                    c[d] += 1
                row.append("/".join(
                    f"{lab}{c[k]}" for k, lab in
                    (("SPLIT", "S"), ("CONTINUE", "C"), ("AMBIG", "A"),
                     ("IGNORED", "X"), ("ERROR", "E")) if c[k]) or "-")
        if any_row:
            lines.append("| " + " | ".join(row) + " |")

    # instructive verbatim examples
    lines.append("")
    lines.append("## Instructive answers (verbatim, truncated to 500 chars)")
    for v in sorted(VARIANTS):
        lines.append("")
        lines.append(f"### Variant {v} ({VARIANTS[v][0]})")
        picks = []
        for want in ("trap_hit", "trap_miss", "width_hit", "width_miss", "unclear"):
            for r in recs:
                if r["variant"] != v or not r.get("answer"):
                    continue
                truth = TRUTH[r["task"]]
                d = r["decision"]
                ok = ((want == "trap_hit" and truth == "CONTINUE" and d == "CONTINUE") or
                      (want == "trap_miss" and truth == "CONTINUE" and d == "SPLIT") or
                      (want == "width_hit" and truth == "SPLIT" and d == "SPLIT") or
                      (want == "width_miss" and truth == "SPLIT" and d != "SPLIT") or
                      (want == "unclear" and d in ("AMBIG", "IGNORED")))
                if ok:
                    picks.append((want, r))
                    break
        for want, r in picks:
            lines.append("")
            lines.append(f"**{want}** - {r['cell']} / {r['model'].split('/')[-1]} "
                         f"-> `{r['decision']}` ({r['why']})")
            lines.append("")
            body = r["answer"][:500].replace("\n", "\n> ")
            lines.append("> " + body)

    # variant C under the stricter read: only a TOP-LEVEL pipe means "split now"
    lines.append("")
    lines.append("## Variant C, read structurally: does the FIRST stage fork?")
    lines.append("")
    lines.append("The spec scores any ` | ` anywhere in the sketch as SPLIT. But the "
                 "shape carries more than that. `A > (B | C)` means *one job first, "
                 "a fork only later* - at the mark that is a CONTINUE - while "
                 "`(A | B | C) > D` means *hand out now, join at the end*. Re-reading "
                 "C by whether its first stage already contains a ` | `:")
    lines.append("")
    lines.append("| model | trap-acc | width-recall | unclear |")
    lines.append("|---|---|---|---|")
    for m in MODELS:
        to = tn = wo = wn = un = n = 0
        for r in recs:
            if r["variant"] != "C" or r["model"] != m:
                continue
            n += 1
            s = sketch_line(r["answer"] or "")
            if s is None or r["decision"] in ("AMBIG", "IGNORED", "ERROR"):
                dec = r["decision"]
            else:
                dec = "SPLIT" if first_stage_pipe(s) else "CONTINUE"
            if dec in ("AMBIG", "IGNORED", "ERROR"):
                un += 1
            if TRUTH[r["task"]] == "CONTINUE":
                tn += 1
                to += dec == "CONTINUE"
            else:
                wn += 1
                wo += dec == "SPLIT"
        lines.append(f"| {m.split('/')[-1]} | {to/tn if tn else 0:.2f} ({to}/{tn}) | "
                     f"{wo/wn if wn else 0:.2f} ({wo}/{wn}) | {un/n if n else 0:.2f} |")

    # variant C batch coverage
    lines.append("")
    lines.append("## Variant C: does the batch sketch match the four real issues?")
    lines.append("")
    lines.append("Real issues: #20 CI validation workflow, #21 arithmetic "
                 "normalization, #22 currency normalization, #23 optional CLI deps. "
                 "At the round-10 mark some issues are already partly done, so a "
                 "3/4 sketch is often correct - several answers explicitly name the "
                 "fourth as already finished.")
    lines.append("")
    lines.append("| model | sample | shape line | issues covered |")
    lines.append("|---|---|---|---|")
    for r in recs:
        if r["variant"] == "C" and r["task"] == "batch" and r["answer"]:
            s = (sketch_line(r["answer"]) or "(none)")[:110]
            s = s.replace("|", "\\|")
            cov = batch_sketch_coverage(r["answer"])
            covs = ",".join(cov) if cov else "none"
            lines.append(f"| {r['model'].split('/')[-1]} | {r['sample']} | "
                         f"`{s}` | {covs} ({len(cov)}/4) |")

    # spend
    tot_cost = sum((r.get("usage") or {}).get("cost", 0) or 0 for r in recs)
    tot_in = sum((r.get("usage") or {}).get("prompt_tokens", 0) for r in recs)
    tot_out = sum((r.get("usage") or {}).get("completion_tokens", 0) for r in recs)
    bym = defaultdict(float)
    for r in recs:
        bym[r["model"]] += (r.get("usage") or {}).get("cost", 0) or 0
    lines.append("")
    lines.append("## Spend")
    lines.append("")
    lines.append(f"- total: **${tot_cost:.4f}** ({tot_in:,} prompt tokens, "
                 f"{tot_out:,} completion tokens)")
    for m, c in bym.items():
        lines.append(f"- {m}: ${c:.4f}")

    open(RESULTS_MD, "w").write("\n".join(lines) + "\n")
    print("\n".join(lines[:40]))
    print(f"\nwrote {RESULTS_MD}")


def probe():
    for cell in CELLS:
        try:
            msgs, st = reconstruct(cell)
            print(f"{cell}: rounds={st['rounds']}/{st['total_rounds_available']} "
                  f"msgs={st['messages']} est_tokens={st['est_tokens']}")
        except Exception as e:                        # noqa: BLE001
            print(f"{cell}: DROPPED - {e}")


def probe_call():
    msgs, st = reconstruct("aforge-pre-crew-batch-s1")
    full = msgs + [{"role": "user", "content": VARIANTS["C"][1]}]
    print("est_tokens", st["est_tokens"])
    txt, u, err = call(MODELS[0], full, 0.7, "probe")
    print("ERR:", err)
    print("USAGE:", u)
    print("ANSWER:\n", txt)


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--probe", action="store_true")
    ap.add_argument("--probe-call", action="store_true")
    ap.add_argument("--score-only", action="store_true")
    ap.add_argument("--samples", type=int, default=SAMPLES)
    a = ap.parse_args()
    if a.probe:
        probe()
    elif a.probe_call:
        probe_call()
    elif a.score_only:
        score()
    else:
        run(a.samples)
