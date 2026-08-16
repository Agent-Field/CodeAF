"""Re-run the ATTEMPT only (same settings) for every cell, to measure label noise.

The graded outcome is a stochastic label: the same model on the same task at temp 0.2 may
pass once and fail the next time. The flip rate bounds the AUROC any predictor could reach,
so it is reported alongside the classifier numbers.
"""
from __future__ import annotations

import asyncio
import json
import time

import aiohttp

import runner as R
import tasks as T


async def main():
    rows = [json.loads(l) for l in open("results.jsonl")]
    sem = asyncio.Semaphore(R.CONCURRENCY)
    conn = aiohttp.TCPConnector(limit=R.CONCURRENCY + 8)
    t0 = time.time()

    async def one(row):
        task = T.TASK_BY_ID[row["task"]]
        async with sem:
            att = await R.call(session, row["model_slug"], prompt=task["prompt"],
                               temperature=0.2, max_tokens=R.ATTEMPT_MAX,
                               no_reasoning=False, tag="rep")
        g = task["check"](att["text"]) if att["ok"] else {"ok": False, "reason": "api_error"}
        return {"model": row["model"], "task": row["task"],
                "solved_1": row["solved"], "solved_2": bool(g["ok"]),
                "cost": att["cost"], "finish_reason": att["finish_reason"],
                "grade_reason": g.get("reason", "")}

    async with aiohttp.ClientSession(connector=conn) as session:
        out = []
        jobs = [asyncio.create_task(one(r)) for r in rows]
        for i, fut in enumerate(asyncio.as_completed(jobs), 1):
            r = await fut
            out.append(r)
            if i % 12 == 0:
                print(f"  [{i}/{len(rows)}] spend ${R._spend:.3f} {time.time()-t0:.0f}s", flush=True)

    with open("replicate.jsonl", "w") as fh:
        for r in out:
            fh.write(json.dumps(r) + "\n")
    flips = sum(1 for r in out if r["solved_1"] != r["solved_2"])
    print(f"\nreplicate: {len(out)} cells, {flips} label flips ({100*flips/len(out):.1f}%)")
    print(f"replicate spend: ${R._spend:.4f}  wall {time.time()-t0:.0f}s")
    for r in out:
        if r["solved_1"] != r["solved_2"]:
            print(f"  FLIP {r['model']:<18}{r['task']:<24} {r['solved_1']} -> {r['solved_2']}")


if __name__ == "__main__":
    asyncio.run(main())
