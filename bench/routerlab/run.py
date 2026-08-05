#!/usr/bin/env python3
"""Execute the design against OpenRouter and write results.jsonl.

Every cell is one chat completion and one deterministic grade. All cells are
in flight at once behind a semaphore, because the entire wall-clock cost of
this experiment is round-trip latency and nothing about cell i depends on
cell j. Grading runs in a worker thread so a 15-second sandboxed subprocess
never stalls the event loop.

Two guards on spend:
  * an upfront projection from panel prices x expected tokens, which refuses
    to start if the design could plausibly cost more than the cap;
  * a live running total, which stops issuing new calls the moment recorded
    spend crosses the cap. Calls already in flight are allowed to land -- the
    money is already committed and throwing the answer away would waste it.

Usage:  python3 run.py [--out results.jsonl] [--only-cells FILE] [--cap 10]
"""
import argparse
import asyncio
import json
import os
import sys
import time

import design
import orclient
from tasks import BY_ID

HERE = os.path.dirname(os.path.abspath(__file__))

# Expected tokens per cell, used only for the upfront projection. Deliberately
# pessimistic: the projection exists to refuse a run that could overspend, so
# it should overestimate, not flatter.
EXP_PROMPT_TOK = 400
EXP_COMPLETION_TOK = {"plan": 900, "code": 1400, "reason": 700}

MAX_INFLIGHT = 24


def project_cost(panel, cells):
    price = {m["slug"]: m for m in panel}
    total = 0.0
    per_model = {}
    for slug, tid, _rep in cells:
        m = price[slug]
        cls = BY_ID[tid]["cls"]
        c = (EXP_PROMPT_TOK / 1e6 * m["price_in_per_mtok"]
             + EXP_COMPLETION_TOK[cls] / 1e6 * m["price_out_per_mtok"])
        total += c
        per_model[slug] = per_model.get(slug, 0.0) + c
    return total, per_model


class Ledger:
    """Live spend accounting shared by every worker."""

    def __init__(self, cap):
        self.cap = cap
        self.spent = 0.0
        self.done = 0
        self.stopped = False
        self._lock = asyncio.Lock()

    async def charge(self, amount):
        async with self._lock:
            self.spent += amount
            self.done += 1
            if self.spent >= self.cap:
                self.stopped = True
            return self.spent


async def run_cell(client, sem, ledger, model, task, rep, out, total):
    async with sem:
        if ledger.stopped:
            return None
        text, usage, latency, err = await orclient.chat(
            client, model["slug"],
            [{"role": "system", "content": task["system"]},
             {"role": "user", "content": task["user"]}],
            price_in=model["price_in_per_mtok"],
            price_out=model["price_out_per_mtok"],
            json_mode=task["json_mode"],
        )

    if err:
        ok, why = False, f"api-error: {err}"
    else:
        # Grading is CPU/subprocess work; keep it off the event loop.
        ok, why = await asyncio.to_thread(task["grade"], text)

    spent = await ledger.charge(usage.cost)
    rec = {
        "model": model["slug"],
        "label": model["label"],
        "task": task["id"],
        "cls": task["cls"],
        "level": task["level"],
        "rep": rep,
        "ok": bool(ok),
        "reason": why,
        "error": err,
        "latency_s": round(latency, 3),
        "prompt_tokens": usage.prompt,
        "completion_tokens": usage.completion,
        "reasoning_tokens": usage.reasoning,
        "cost_usd": usage.cost,
        "reply": text,
    }
    out.write(json.dumps(rec) + "\n")
    out.flush()
    mark = "." if ok else ("!" if err else "x")
    print(f"{mark}", end="", flush=True)
    if ledger.done % 50 == 0:
        print(f"  [{ledger.done}/{total} ${spent:.4f}]", flush=True)
    return rec


async def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=os.path.join(HERE, "results.jsonl"))
    ap.add_argument("--cap", type=float, default=10.0, help="hard USD cap")
    ap.add_argument("--only", default=None,
                    help="JSON file with a list of [model, task, rep] cells to rerun")
    ap.add_argument("--append", action="store_true")
    ap.add_argument("--yes", action="store_true", help="skip the confirmation")
    args = ap.parse_args()

    panel = json.load(open(os.path.join(HERE, "panel.json")))["panel"]
    by_slug = {m["slug"]: m for m in panel}

    if args.only:
        cells = [tuple(c) for c in json.load(open(args.only))]
    else:
        cells = design.build_cells(panel)
        if not design.connected(cells):
            sys.exit("FATAL: design graph is not connected; IRT scale would be "
                     "unidentified")

    proj, per_model = project_cost(panel, cells)
    print(f"cells:          {len(cells)}")
    print(f"models:         {len(panel)}  tasks: {len({c[1] for c in cells})}")
    print(f"projected cost: ${proj:.4f}   (cap ${args.cap:.2f})")
    for s, c in sorted(per_model.items(), key=lambda kv: -kv[1]):
        print(f"   {s:<36} ${c:.4f}")
    if proj > args.cap:
        sys.exit(f"ABORT: projection ${proj:.4f} exceeds cap ${args.cap:.2f}")
    if not args.yes:
        print("\npress enter to run, ctrl-c to abort")
        input()

    mode = "a" if args.append else "w"
    sem = asyncio.Semaphore(MAX_INFLIGHT)
    ledger = Ledger(args.cap)
    t0 = time.monotonic()
    with open(args.out, mode) as out:
        async with orclient.new_client() as client:
            await asyncio.gather(*[
                run_cell(client, sem, ledger, by_slug[m], BY_ID[t], r, out, len(cells))
                for m, t, r in cells
            ])
    wall = time.monotonic() - t0
    print(f"\n\ncells run:  {ledger.done}")
    print(f"actual:     ${ledger.spent:.4f}  (projected ${proj:.4f})")
    print(f"wall clock: {wall:.1f}s ({wall/60:.1f} min)")
    print(f"wrote {args.out}")
    if ledger.stopped:
        print("WARNING: spend cap reached, some cells were skipped")


if __name__ == "__main__":
    asyncio.run(main())
