#!/usr/bin/env python3
"""Fetch one model's lane sheet from OpenRouter and save it verbatim.

A model id is an address; the LANE is the machine behind it. OpenRouter
publishes, per model, one row per endpoint with that endpoint's own tariff,
its own capability facts, and a thirty-minute distribution of first-token
wait and throughput. That sheet is the prior the whole lane design starts
from -- see ideation/provider-routing.md section 1 -- and this script is the
only thing in the lab that touches the network.

TWO RULES, AND THEY ARE THE WHOLE FILE.

VERBATIM. The router's JSON body is written through untouched, under a "data"
key that is the body itself, with exactly one key added around it:
"fetched_at". Nothing is renamed, unitised or flattened. sim.py reads the
router's own field names -- `pricing.completion`, `uptime_last_5m`,
`latency_last_30m.p90` -- so that a reader can diff a saved sheet against a
fresh curl and see the same words. A reshaping step here would be a place for
the sim and the router to disagree silently about what a number meant.

DATED. A sheet is a thirty-minute aggregate, so it is a photograph and not a
fact. The timestamp is what lets a report say which afternoon its numbers
came from, and it is why the file is committed next to the report rather
than refetched at read time.

Usage:  python3 fetch_sheet.py --model deepseek/deepseek-v4-flash [--out sheets/]
"""
import argparse
import datetime
import json
import os
import sys
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
BASE_URL = os.environ.get("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
TIMEOUT_S = 60.0


def slug_filename(model):
    """`deepseek/deepseek-v4-flash` -> `deepseek-deepseek-v4-flash.json`.

    The author stays in the name. Two authors can ship a model with the same
    short name, and a sheets/ directory where one silently overwrote the other
    would be a lab that measured the wrong machine.
    """
    return model.replace("/", "-") + ".json"


def fetch(model):
    key = os.environ.get("OPENROUTER_API_KEY", "")
    if not key:
        sys.exit("OPENROUTER_API_KEY is not set -- export it and retry")
    url = f"{BASE_URL}/models/{model}/endpoints"
    req = urllib.request.Request(url, headers={
        "Authorization": f"Bearer {key}",
        "Accept": "application/json",
        "HTTP-Referer": "https://github.com/Agent-Field/aforge-v2",
        "X-Title": "aforge lanelab",
    })
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT_S) as r:
            raw = r.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", "replace")[:400]
        sys.exit(f"http {e.code} from {url}: {body}")
    except urllib.error.URLError as e:
        sys.exit(f"could not reach {url}: {e.reason}")
    return json.loads(raw)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="deepseek/deepseek-v4-flash",
                    help="author/slug as OpenRouter spells it")
    ap.add_argument("--out", default=os.path.join(HERE, "sheets"),
                    help="directory the sheet is written to")
    args = ap.parse_args()

    body = fetch(args.model)
    endpoints = (body.get("data") or {}).get("endpoints") or []
    if not endpoints:
        # Not an error the script should paper over: a model with no endpoints
        # is a model nothing can serve, and writing an empty sheet would let a
        # sim run on it and report zeroes as if they were measurements.
        sys.exit(f"{args.model}: the sheet carries no endpoints; refusing to write it")

    os.makedirs(args.out, exist_ok=True)
    path = os.path.join(args.out, slug_filename(args.model))
    doc = {
        "fetched_at": datetime.datetime.now(datetime.timezone.utc)
                              .isoformat(timespec="seconds"),
        "model": args.model,
        "source": f"{BASE_URL}/models/{args.model}/endpoints",
        "data": body.get("data", body),
    }
    with open(path, "w") as f:
        json.dump(doc, f, indent=2, sort_keys=False)
        f.write("\n")

    print(f"model:     {args.model}")
    print(f"endpoints: {len(endpoints)}")
    print(f"fetched:   {doc['fetched_at']}")
    print(f"wrote:     {path}")
    # A one-line census, so a person can see at a glance whether the sheet is
    # plausible before any simulator quotes it.
    timed = sum(1 for e in endpoints
                if ((e.get("latency_last_30m") or {}).get("p50") or 0) > 0
                and ((e.get("throughput_last_30m") or {}).get("p50") or 0) > 0)
    tools = sum(1 for e in endpoints
                if (e.get("supports_tool_choice") or {}).get("function"))
    print(f"           {timed} with p50 timing, {tools} that honour a tool call")


if __name__ == "__main__":
    main()
