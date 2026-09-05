#!/usr/bin/env python3
"""Recover missing prices from generation metadata, without another inference.

Raw guard evidence is immutable. This writes a derived ledger and keeps the
provider receipts beside it; an unavailable or mismatched receipt stays unknown.
See https://openrouter.ai/docs/api/api-reference/generations/get-generation.
"""
import argparse
import json
import math
import os
from pathlib import Path
import time
import urllib.parse
import urllib.request


def valid_receipt(data, row):
    cost = data.get("total_cost")
    return (data.get("id") == row.get("generation_id")
            and data.get("model") == row.get("model")
            and not isinstance(cost, bool) and isinstance(cost, (float, int))
            and math.isfinite(cost) and cost >= 0
            and (data.get("finish_reason") is not None or data.get("cancelled") is True))


def reconcile(rows, fetch, evidence):
    # A guard killed after receiving a generation ID may never write settlement.
    # Reconstruct only one-to-one recorded identities; duplicates stay invalid.
    admissions = {}
    generations = {}
    settled_ids = set()
    for row in rows:
        key = row.get("request_id")
        if row.get("phase") == "admitted":
            admissions.setdefault(key, []).append(row)
        if row.get("phase") == "generation":
            generations.setdefault(key, []).append(row)
        if row.get("phase") == "settled":
            settled_ids.add(key)
    rows = list(rows)
    for key, admitted in admissions.items():
        observed = generations.get(key, [])
        if key and key not in settled_ids and len(admitted) == 1 and len(observed) == 1:
            if observed[0].get("model") == admitted[0].get("model"):
                rows.append(dict(observed[0], phase="settled", cost_usd=None,
                                 note="derived from recorded generation after missing settlement"))
    result = []
    for original in rows:
        row = dict(original)
        if row.get("phase") == "settled" and row.get("cost_usd") is None and row.get("generation_id"):
            data = fetch(row["generation_id"])
            if data is not None:
                evidence.append({"request_id": row.get("request_id"), "data": data})
                if valid_receipt(data, row):
                    row.update(cost_usd=data["total_cost"], cost_source="openrouter-generation",
                               prompt_tokens=data.get("tokens_prompt"),
                               completion_tokens=data.get("tokens_completion"))
        result.append(row)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source")
    parser.add_argument("out")
    args = parser.parse_args()
    key = os.environ.get("OPENROUTER_API_KEY", "")
    rows = [json.loads(line) for line in Path(args.source).read_text().splitlines() if line.strip()]
    deadline = time.monotonic() + 30

    def fetch(generation):
        if not key or time.monotonic() >= deadline:
            return None
        url = "https://openrouter.ai/api/v1/generation?" + urllib.parse.urlencode({"id": generation})
        request = urllib.request.Request(url, headers={"Authorization": "Bearer " + key})
        try:
            with urllib.request.urlopen(request, timeout=max(0.1, min(5, deadline-time.monotonic()))) as response:
                data = json.load(response).get("data")
                # Keep only billing metadata, never request or completion content.
                if isinstance(data, dict):
                    return {k: data.get(k) for k in ("id", "model", "total_cost", "cancelled",
                        "finish_reason", "tokens_prompt", "tokens_completion", "provider_name")}
        except Exception:
            return None
        return None

    evidence = []
    recovered = reconcile(rows, fetch, evidence)
    with open(args.out, "x") as handle:
        handle.write("".join(json.dumps(row) + "\n" for row in recovered))
    with open(args.out + ".receipts.json", "x") as handle:
        json.dump(evidence, handle, indent=2)


if __name__ == "__main__":
    main()
