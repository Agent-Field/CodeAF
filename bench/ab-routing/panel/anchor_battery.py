#!/usr/bin/env python3
"""Pin a candidate model onto Phase A's ability scale, cheaply.

Phase A (bench/routerlab) fitted a Rasch ability scale over seven models on 36
deterministically-graded tasks and recommended a four-model panel. This script
answers the only question Phase B still needs from that scale: *would adding
this other model move the frontier?* -- without re-running the whole design.

It runs routerlab's **anchor block** (10 tasks spanning all three work classes
and the whole intended difficulty range, 2 replicates each) against a candidate
model. That block is what pinned the original scale, so a candidate's pass rate
on it is directly comparable to the seven models already measured. Phase A
costs this at about $0.02 per model.

Two things are measured here that Phase A did not measure and that Phase B
cannot ignore, because Phase B runs the *real aforge pipeline* rather than
isolated chat completions:

  * **tools + structured_outputs support**, read from the live catalog. aforge's
    executor is a tool-calling loop (internal/exec/tools.go) and its planner
    asks for structured output. A model without both cannot be a panel member
    at any ability, so this is a hard gate applied before any spend.
  * **reasoning on as well as off**. Phase A ran every call with
    `reasoning: {enabled: false}` because that is aforge production
    (`DefaultReasoning`/`DefaultExecReasoning` are both EffortOff) and its report
    explicitly flags that this understates reasoning-first models -- glm-5.2 in
    particular. `--reasoning on` re-measures that.

Usage:
  python3 anchor_battery.py --list                    # candidates + live prices
  python3 anchor_battery.py --run [--cap 1.00]
  python3 anchor_battery.py --report

The anchor tasks and their graders come from bench/routerlab; point at it with
--routerlab or $AFORGE_ROUTERLAB (default: ../../routerlab relative to here,
then the repository's bench/routerlab).
"""
import argparse
import asyncio
import json
import os
import sys
import time
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
CATALOG = os.path.join(HERE, "catalog.json")
RESULTS = os.path.join(HERE, "anchor-results.jsonl")

# Hard cap from the brief. Everything considered must be at or under this.
PRICE_CAP_OUT = 5.00

# routerlab's ANCHOR_IDS -- the block that pins the scale.
ANCHOR_IDS = ["P02", "P07", "P09", "C02", "C05", "C08", "C12", "R01", "R08", "R10"]
REPLICATES = 2
MAX_INFLIGHT = 24

# (slug, label, reasoning_mode, why it is worth measuring)
#
# reasoning_mode: "off" matches aforge production; "on" is the fairness re-run
# Phase A's report asked for. A model listed twice is measured both ways.
CANDIDATES = [
    # ---- controls: already on the Phase A scale, re-run to detect drift -----
    ("~deepseek/deepseek-v4-flash-latest", "ds-v4-flash", "off",
     "CONTROL/INCUMBENT: aforge's default; re-run to check the scale has not drifted"),
    ("moonshotai/kimi-k2.6", "kimi-k2.6", "off",
     "CONTROL: Phase A's only significantly-above-pack model (theta +4.09)"),

    # ---- the fairness re-run Phase A explicitly asked for ------------------
    ("z-ai/glm-5.2", "glm-5.2", "on",
     "Phase A ran it reasoning-off and flagged that as unfair; this is the re-measure"),
    ("z-ai/glm-5.2", "glm-5.2", "off",
     "the reasoning-off arm of the same re-measure, so the delta is attributable"),

    # ---- second strong-but-cheaper top rung candidates ---------------------
    ("qwen/qwen3.7-plus", "qwen3.7-plus", "on",
     "cheapest 1M-context reasoning generalist that could rival kimi-k2.6 at half its price"),
    ("minimax/minimax-m2.7", "minimax-m2.7", "on",
     "strong open MiniMax at $1.08/M out -- a different family from every Phase A member"),

    # ---- coder specialists -------------------------------------------------
    ("qwen/qwen3-coder-plus", "qwen3-coder-plus", "off",
     "code-specialised, 1M context, tools -- the strongest coder under the cap"),
    ("qwen/qwen3-coder", "qwen3-coder", "off",
     "the cheap coder rung at $1.00/M out; only worth a slot if it tracks coder-plus"),
    ("moonshotai/kimi-k2.7-code", "kimi-k2.7-code", "on",
     "Moonshot's code model; Phase A excluded it as confounding, Phase B wants a specialist"),

    # ---- floor candidates --------------------------------------------------
    ("google/gemma-3-12b-it", "gemma-3-12b", "off",
     "CONTROL: Phase A's floor (theta -1.72), carrying a 17s latency tail"),
    ("z-ai/glm-4.7-flash", "glm-4.7-flash", "off",
     "a floor with tools and reasoning at $0.40/M out -- gemma's role without gemma's tail"),
]


# ---------------------------------------------------------------------------
# catalog

def fetch_catalog(refresh=False):
    if refresh or not os.path.exists(CATALOG):
        with urllib.request.urlopen(
                "https://openrouter.ai/api/v1/models", timeout=60) as r:
            raw = r.read()
        with open(CATALOG, "wb") as f:
            f.write(raw)
    with open(CATALOG) as f:
        return {m["id"]: m for m in json.load(f)["data"]}


def describe(cat, slug):
    m = cat.get(slug)
    if m is None:
        return None
    p = m.get("pricing", {})
    sp = sorted(m.get("supported_parameters") or [])
    return {
        "slug": slug,
        "name": m.get("name"),
        "price_in_per_mtok": round(float(p.get("prompt", 0)) * 1e6, 4),
        "price_out_per_mtok": round(float(p.get("completion", 0)) * 1e6, 4),
        "context_length": m.get("context_length"),
        "tools": "tools" in sp,
        "structured_outputs": "structured_outputs" in sp,
        "reasoning": "reasoning" in sp,
        "supported_parameters": sp,
    }


def gate(info):
    """The hard gates a Phase B panel member must pass, in order of severity.

    Ability is irrelevant to a model the harness cannot drive: aforge's executor
    is a tool-calling loop and its planner asks for structured output. A model
    failing either gate is excluded before a cent is spent on measuring it."""
    problems = []
    if info["price_out_per_mtok"] > PRICE_CAP_OUT:
        problems.append(f"${info['price_out_per_mtok']:.2f}/M out over ${PRICE_CAP_OUT:.2f} cap")
    if not info["tools"]:
        problems.append("no tool-calling: aforge's executor cannot drive it")
    if not info["structured_outputs"]:
        problems.append("no structured_outputs: aforge's planner cannot drive it")
    return problems


# ---------------------------------------------------------------------------
# running

def load_routerlab(path):
    for candidate in filter(None, [path, os.environ.get("AFORGE_ROUTERLAB"),
                                   os.path.join(HERE, "..", "..", "routerlab"),
                                   os.path.join(HERE, "..", "..", "..", "..", "..",
                                                "bench", "routerlab")]):
        candidate = os.path.abspath(candidate)
        if os.path.exists(os.path.join(candidate, "tasks.py")):
            sys.path.insert(0, candidate)
            import tasks as routerlab_tasks  # noqa: E402
            import orclient  # noqa: E402
            return routerlab_tasks, orclient, candidate
    raise SystemExit(
        "could not find bench/routerlab (needs tasks.py + orclient.py).\n"
        "Pass --routerlab PATH or set AFORGE_ROUTERLAB.")


async def run_cell(orclient, client, sem, ledger, cand, info, task, rep, out):
    async with sem:
        if ledger["stopped"]:
            return
        text, usage, latency, err = await orclient.chat(
            client, info["slug"],
            [{"role": "system", "content": task["system"]},
             {"role": "user", "content": task["user"]}],
            price_in=info["price_in_per_mtok"],
            price_out=info["price_out_per_mtok"],
            json_mode=task["json_mode"],
            # The whole point of the glm-5.2 arm: reasoning_off is a parameter,
            # not a constant.
            reasoning_off=(cand["reasoning_mode"] == "off"),
            # Probe lab's forced deviation, applied here: at 4096 tokens a
            # reasoning-first model spends the whole budget thinking and returns
            # an empty message, so the cell measures the ceiling rather than the
            # model. The reasoning-off arms keep Phase A's 4096 exactly, so the
            # controls stay comparable to the scale they are checking.
            max_tokens=(4096 if cand["reasoning_mode"] == "off" else 16384),
        )
    ok = False
    detail = err or ""
    if not err:
        try:
            ok, detail = await asyncio.to_thread(task["grade"], text)
        except Exception as e:  # a grader that throws is a grader bug, recorded
            ok, detail = False, f"grader raised {type(e).__name__}: {e}"
    ledger["spent"] += usage.cost
    ledger["done"] += 1
    if ledger["spent"] >= ledger["cap"]:
        ledger["stopped"] = True
        print(f"  !! spend cap ${ledger['cap']:.2f} reached; no new calls", file=sys.stderr)
    rec = {
        "arm": cand["arm"], "slug": info["slug"], "label": cand["label"],
        "reasoning_mode": cand["reasoning_mode"], "task": task["id"],
        "cls": task["cls"], "rep": rep, "pass": bool(ok),
        "detail": str(detail)[:300], "error": err,
        "prompt_tokens": usage.prompt, "completion_tokens": usage.completion,
        "reasoning_tokens": usage.reasoning, "cost": usage.cost,
        "latency_s": round(latency, 3),
        "empty_output": (not err) and len(text.strip()) == 0,
    }
    out.write(json.dumps(rec) + "\n")
    out.flush()


async def run_all(cands, tasks_by_id, orclient, cap):
    cells = [(c, tasks_by_id[tid], rep)
             for c in cands for tid in ANCHOR_IDS for rep in range(REPLICATES)]
    print(f"{len(cells)} cells across {len(cands)} candidate arms, cap ${cap:.2f}")
    ledger = {"spent": 0.0, "done": 0, "cap": cap, "stopped": False}
    sem = asyncio.Semaphore(MAX_INFLIGHT)
    t0 = time.monotonic()
    with open(RESULTS, "a") as out:
        client = orclient.new_client()
        try:
            await asyncio.gather(*[
                run_cell(orclient, client, sem, ledger, c, c["info"], t, rep, out)
                for c, t, rep in cells])
        finally:
            await client.aclose()
    print(f"{ledger['done']} cells, ${ledger['spent']:.4f}, "
          f"{time.monotonic() - t0:.1f}s -> {RESULTS}")


# ---------------------------------------------------------------------------
# reporting

def report():
    rows = [json.loads(l) for l in open(RESULTS)]
    arms = {}
    for r in rows:
        a = arms.setdefault(r["arm"], {
            "slug": r["slug"], "mode": r["reasoning_mode"], "n": 0, "pass": 0,
            "cost": 0.0, "lat": [], "rt": 0, "empty": 0, "err": 0,
            "cls": {}})
        a["n"] += 1
        a["pass"] += int(r["pass"])
        a["cost"] += r["cost"]
        a["lat"].append(r["latency_s"])
        a["rt"] += r["reasoning_tokens"]
        a["empty"] += int(r.get("empty_output") or False)
        a["err"] += int(bool(r.get("error")))
        c = a["cls"].setdefault(r["cls"], [0, 0])
        c[0] += int(r["pass"]); c[1] += 1
    print(f"{'arm':<26} {'pass':>7} {'plan':>6} {'code':>6} {'reas':>6} "
          f"{'$':>9} {'p50 s':>7} {'rtok':>7} {'empty':>6} {'err':>4}")
    for arm, a in sorted(arms.items(), key=lambda kv: -kv[1]["pass"] / max(kv[1]["n"], 1)):
        lat = sorted(a["lat"])
        p50 = lat[len(lat) // 2] if lat else 0
        cls = {k: f"{v[0]}/{v[1]}" for k, v in a["cls"].items()}
        print(f"{arm:<26} {a['pass']:>3}/{a['n']:<3} "
              f"{cls.get('plan', '-'):>6} {cls.get('code', '-'):>6} {cls.get('reason', '-'):>6} "
              f"{a['cost']:>9.5f} {p50:>7.2f} {a['rt']:>7} {a['empty']:>6} {a['err']:>4}")
    print(f"\ntotal spend ${sum(a['cost'] for a in arms.values()):.4f}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--run", action="store_true")
    ap.add_argument("--report", action="store_true")
    ap.add_argument("--refresh", action="store_true")
    ap.add_argument("--cap", type=float, default=1.00)
    ap.add_argument("--routerlab", default=None)
    args = ap.parse_args()

    if args.report:
        return report()

    cat = fetch_catalog(args.refresh or args.list)
    cands, blocked = [], []
    for slug, label, mode, why in CANDIDATES:
        info = describe(cat, slug)
        if info is None:
            blocked.append((slug, mode, ["not in the live catalog"]))
            continue
        problems = gate(info)
        arm = f"{label}/{mode}"
        if problems:
            blocked.append((slug, mode, problems))
            continue
        cands.append({"arm": arm, "label": label, "slug": slug,
                      "reasoning_mode": mode, "why": why, "info": info})

    if args.list or not args.run:
        print(f"{'arm':<26} {'$in':>7} {'$out':>7} {'ctx':>9}  T S R  why")
        for c in cands:
            i = c["info"]
            print(f"{c['arm']:<26} {i['price_in_per_mtok']:>7.3f} "
                  f"{i['price_out_per_mtok']:>7.3f} {i['context_length']:>9}  "
                  f"{'T' if i['tools'] else '-'} {'S' if i['structured_outputs'] else '-'} "
                  f"{'R' if i['reasoning'] else '-'}  {c['why']}")
        if blocked:
            print("\nblocked before any spend:")
            for slug, mode, problems in blocked:
                print(f"  {slug} ({mode}): {'; '.join(problems)}")
        if not args.run:
            return

    routerlab_tasks, orclient, path = load_routerlab(args.routerlab)
    print(f"\nanchor tasks from {path}")
    by_id = routerlab_tasks.BY_ID
    missing = [t for t in ANCHOR_IDS if t not in by_id]
    if missing:
        raise SystemExit(f"anchor ids missing from routerlab tasks: {missing}")
    asyncio.run(run_all(cands, by_id, orclient, args.cap))
    report()


if __name__ == "__main__":
    main()
