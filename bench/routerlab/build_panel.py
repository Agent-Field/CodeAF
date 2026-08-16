#!/usr/bin/env python3
"""Build panel.json from the LIVE OpenRouter catalog.

The panel is never written by hand: slugs drift, prices drift, and a slug that
was right last month 404s today. This script fetches /api/v1/models, asserts
every chosen slug still exists, and copies price/context/supported_parameters
out of the catalog response so panel.json is a record of what was actually
true at run time.

Selection rule (Phase A):
  - output price <= $4.00 / M tokens (hard cap), <= $2.50 strongly preferred
  - open-weight families only; no closed frontier models
  - must span the ability range: one very cheap small model at the floor, the
    current aforge default, mid-tier, and the strongest open model under cap
"""
import json
import os
import sys
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
CATALOG = os.path.join(HERE, "data", "catalog.json")
PANEL = os.path.join(HERE, "panel.json")

PRICE_CAP_OUT = 4.00  # $/M output tokens, hard

# (slug, short label, role in the design)
CHOSEN = [
    ("google/gemma-3-12b-it", "gemma-3-12b",
     "floor: very cheap small dense model, expected weakest theta"),
    ("qwen/qwen3-30b-a3b-instruct-2507", "qwen3-30b-a3b",
     "cheap MoE, Qwen 30B-class, non-reasoning"),
    ("~deepseek/deepseek-v4-flash-latest", "ds-v4-flash",
     "INCUMBENT: current aforge default (internal/config/config.go)"),
    ("deepseek/deepseek-v4-pro", "ds-v4-pro",
     "mid: same family as incumbent, one tier up -- the natural upgrade"),
    ("z-ai/glm-4.7", "glm-4.7",
     "mid: strong open generalist at sub-$2 output"),
    ("moonshotai/kimi-k2.6", "kimi-k2.6",
     "upper-mid: Moonshot open-weight flagship under the cap"),
    ("z-ai/glm-5.2", "glm-5.2",
     "TOP RUNG: strongest open model under the price cap; cascade terminus"),
]

# Deliberate exclusions, recorded so the report can justify them.
EXCLUDED = {
    "moonshotai/kimi-k3": "$15.00/M out -- 3.75x over the hard cap",
    "~moonshotai/kimi-latest": "$14.00/M out -- over the hard cap",
    "moonshotai/kimi-k2.7-code": "$3.50/M out -- under hard cap but code-specialised, "
                                 "would confound the plan/reason work-classes",
    "z-ai/glm-5-turbo": "$4.00/M out -- at the cap, and glm-5.2 is stronger for less",
    "qwen/qwen3.7-max": "$4.425/M out -- over the hard cap",
}


def fetch_catalog(refresh=False):
    if refresh or not os.path.exists(CATALOG):
        os.makedirs(os.path.dirname(CATALOG), exist_ok=True)
        with urllib.request.urlopen(
            "https://openrouter.ai/api/v1/models", timeout=60
        ) as r:
            raw = r.read()
        with open(CATALOG, "wb") as f:
            f.write(raw)
    with open(CATALOG) as f:
        return json.load(f)["data"]


def main():
    cat = {m["id"]: m for m in fetch_catalog("--refresh" in sys.argv)}
    panel = []
    for slug, label, role in CHOSEN:
        m = cat.get(slug)
        if m is None:
            print(f"FATAL: {slug} is not in the live catalog", file=sys.stderr)
            sys.exit(1)
        p = m.get("pricing", {})
        pin = float(p.get("prompt", 0)) * 1e6
        pout = float(p.get("completion", 0)) * 1e6
        if pout > PRICE_CAP_OUT:
            print(f"FATAL: {slug} at ${pout:.3f}/M out exceeds cap", file=sys.stderr)
            sys.exit(1)
        sp = m.get("supported_parameters", []) or []
        panel.append({
            "slug": slug,
            "label": label,
            "role": role,
            "name": m.get("name"),
            "price_in_per_mtok": round(pin, 4),
            "price_out_per_mtok": round(pout, 4),
            "context_length": m.get("context_length"),
            "supported_parameters": sorted(sp),
            "structured_outputs": "structured_outputs" in sp,
            "response_format": "response_format" in sp,
            "reasoning": "reasoning" in sp,
        })
    out = {
        "price_cap_out_per_mtok": PRICE_CAP_OUT,
        "catalog_fetched_from": "https://openrouter.ai/api/v1/models",
        "excluded": EXCLUDED,
        "panel": panel,
    }
    with open(PANEL, "w") as f:
        json.dump(out, f, indent=2)
    w = max(len(m["slug"]) for m in panel)
    for m in panel:
        print(f"{m['slug']:<{w}}  in={m['price_in_per_mtok']:>6.3f} "
              f"out={m['price_out_per_mtok']:>6.3f}  ctx={m['context_length']:>8}  "
              f"SO={m['structured_outputs']}")
    print(f"\nwrote {PANEL} ({len(panel)} models)")


if __name__ == "__main__":
    main()
