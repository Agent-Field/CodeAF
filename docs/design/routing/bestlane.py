#!/usr/bin/env python3
"""Does the shipped binary pick the best lane right now?

Two halves. First a live probe: every lane the call log has seen serve each model
is asked once with `provider.only` and timed, which is the oracle for "best lane
now". Then the real binary answers the same prompt a few times with --debug, and
the call log says which lane it asked for, which one served, and how long the
first token took. A run passes when the lane that served is within ONE AND A HALF
times what the best probed lane would have taken for the run's own answer
length, plus a second, or is that lane. Every lane is probed twice. The request bodies
in the debug record are also checked for a `max_price` riding beside an order.

    OPENROUTER_API_KEY=... python3 docs/design/routing/bestlane.py \
        --bin bin/codeaf --models z-ai/glm-5.3-flash deepseek/deepseek-v4.1-flash --runs 3
"""
import argparse, collections, glob, json, os, subprocess, sys, time, urllib.request

HOME = os.path.expanduser("~/.codeaf")
URL = "https://openrouter.ai/api/v1/chat/completions"
PROMPT = "Write 220 words explaining how a TCP congestion window grows and shrinks. Plain prose, no lists."
READ_RATE = 18.0  # lane.ReadRate: tokens a person reads per second


def perceived(ttft, rate, visible):
    if not rate:
        return float("inf")
    return ttft + visible * max(0.0, 1 / rate - 1 / READ_RATE)


def log_rows(since=None):
    rows = []
    for line in open(os.path.join(HOME, "logs", "calls.jsonl")):
        try:
            r = json.loads(line)
        except ValueError:
            continue
        if since is None or r.get("ts", "") >= since:
            rows.append(r)
    return rows


def lanes_seen(rows, model, days=3):
    cutoff = time.strftime("%Y-%m-%d", time.localtime(time.time() - days * 86400))
    seen = collections.Counter(r["served"] for r in rows if r.get("ts", "") >= cutoff and r.get("phase") is None and r.get("served") and r.get("model") == model)
    return [lane for lane, _ in seen.most_common(10)]


def probe(model, lane, key, max_tokens=300):
    body = {"model": model, "messages": [{"role": "user", "content": PROMPT}], "stream": True, "max_tokens": max_tokens,
            "provider": {"only": [lane], "allow_fallbacks": False}}
    req = urllib.request.Request(URL, data=json.dumps(body).encode(), headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
    t0 = time.time(); ttft = None; toks = 0; last = t0; err = None; status = None
    try:
        with urllib.request.urlopen(req, timeout=90) as resp:
            status = resp.status
            for line in resp:
                line = line.decode("utf-8", "replace").strip()
                if not line.startswith("data:"):
                    continue
                d = line[5:].strip()
                if d == "[DONE]":
                    break
                try:
                    j = json.loads(d)
                except ValueError:
                    continue
                ch = j.get("choices") or []
                delta = ch[0].get("delta", {}) if ch else {}
                if delta.get("content") or delta.get("reasoning"):
                    if ttft is None:
                        ttft = time.time() - t0
                    last = time.time()
                if j.get("usage"):
                    toks = j["usage"].get("completion_tokens", 0)
    except urllib.error.HTTPError as e:
        status = e.code; err = e.read().decode("utf-8", "replace")[:100]
    except Exception as e:  # noqa: BLE001
        err = str(e)[:100]
    gen = (last - t0 - ttft) if ttft else 0
    rate = toks / gen if gen > 0.3 and toks else None
    return {"lane": lane, "status": status, "ttft": ttft, "rate": rate, "toks": toks, "err": err,
            "perceived": perceived(ttft, rate, max_tokens) if ttft and rate else float("inf")}


def run_binary(binary, model, prompt):
    since = time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime())
    out = subprocess.run([binary, "exec", prompt, "--model", model, "--max-turns", "1", "--debug", "--timeout", "3m"],
                         capture_output=True, text=True, timeout=240)
    record = None
    for line in (out.stdout + out.stderr).splitlines():
        if line.startswith("debug record:"):
            record = line.split(":", 1)[1].strip()
    rows = [r for r in log_rows(since) if r.get("model") == model and r.get("phase") is None]
    prefs = []
    if record:
        for f in glob.glob(os.path.join(record, "calls", "*.json")):
            try:
                prefs.append(json.load(open(f)).get("request", {}).get("provider"))
            except ValueError:
                pass
    return rows, prefs, record


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--bin", default="bin/codeaf")
    ap.add_argument("--models", nargs="+", default=["z-ai/glm-5.3-flash", "deepseek/deepseek-v4.1-flash"])
    ap.add_argument("--runs", type=int, default=3)
    args = ap.parse_args()
    key = os.environ.get("OPENROUTER_API_KEY")
    if not key:
        sys.exit("OPENROUTER_API_KEY is not set")
    rows = log_rows()
    tariffs = {}
    try:
        for b in json.load(open(os.path.join(HOME, "v3", "lanes.json")))["beliefs"]:
            tariffs[(b["ID"]["Model"], b["ID"]["Lane"])] = b["Facts"].get("PriceOut", 0)
    except (OSError, ValueError, KeyError):
        pass
    failed = 0
    for model in args.models:
        print(f"\n=== {model} ===")
        probes = []
        for lane in lanes_seen(rows, model):
            pair = [probe(model, lane, key), probe(model, lane, key)]
            good = [p for p in pair if p["ttft"] and p["rate"]]
            if good:
                p = dict(good[0]); p["ttft"] = sum(x["ttft"] for x in good) / len(good); p["rate"] = sum(x["rate"] for x in good) / len(good)
                p["perceived"] = perceived(p["ttft"], p["rate"], 300); p["answered"] = len(good)
            else:
                p = pair[0]; p["answered"] = 0
            probes.append(p)
        probes.sort(key=lambda p: p["perceived"])
        for p in probes:
            print(f"  probe {p['lane']:14s} answered={p['answered']}/2 ttft={p['ttft'] and round(p['ttft'], 2)} tok/s={p['rate'] and round(p['rate'])} perceived={round(p['perceived'], 1) if p['perceived'] != float('inf') else 'inf'} {p['err'] or ''}")
        best = probes[0] if probes and probes[0]["perceived"] != float("inf") else None
        if not best:
            print("  no lane answered the probe; nothing to compare against"); continue
        by_lane = {p["lane"]: p for p in probes}
        print(f"  best now: {best['lane']} ({best['perceived']:.1f}s perceived)")
        for i in range(args.runs):
            calls, prefs, record = run_binary(args.bin, model, PROMPT)
            answered = [r for r in calls if r.get("status") == 200 and r.get("served")]
            if not answered:
                print(f"  run {i+1}: no answered call in the log (record {record})"); failed += 1; continue
            r = answered[-1]
            served = r["served"]; asked = r.get("lane")
            if not r.get("completion_tokens"):
                # No usage on the row means no answer length, and a felt wait
                # cannot be compared with a lane's rate without one. Say so
                # rather than judge on a number that was never measured.
                print(f"  run {i+1}: not judged (no usage on the answered call) asked={asked} served={served} ttft={r.get('ttft_ms')}ms ms={r.get('ms')} calls={len(calls)}")
                continue
            tokens = r["completion_tokens"]
            # The felt wait for THIS run's own answer length: what the person sat
            # through, against what the best probed lane would have taken for the
            # same length. Every token counts as waited-for (a reasoning model's
            # answer is mostly hidden), and a second of slack covers the noise of
            # two probes.
            tps = None
            if r.get("ms") and r.get("ttft_ms") and r["ms"] - r["ttft_ms"] > 300:
                tps = round(tokens / ((r["ms"] - r["ttft_ms"]) / 1000))
            realized = r["ms"] / 1000 if r.get("ms") else float("inf")
            oracle = best["ttft"] + tokens / best["rate"]
            ratio = realized / oracle if oracle else None
            # A ceiling may ride beside an order, but it must cover the lane that
            # served: a ceiling under the served lane's own tariff is the veto the
            # assessment recorded, paid for by a rescue.
            ceiling_beside_order = False
            for p in prefs:
                if not (p and p.get("order") and p.get("max_price")):
                    continue
                tariff = tariffs.get((model, served))
                if tariff and p["max_price"].get("completion", 0) < tariff * 1_000_000:
                    ceiling_beside_order = True
            ok = served == best["lane"] or realized <= 1.5 * oracle + 1.0
            verdict = "PASS" if ok and not ceiling_beside_order else "FAIL"
            if verdict == "FAIL":
                failed += 1
            print(f"  run {i+1}: {verdict} asked={asked} served={served} ttft={r.get('ttft_ms')}ms tok/s={tps} tokens={tokens} "
                  f"felt={realized:.1f}s best-lane-would-take={oracle:.1f}s calls={len(calls)} ceiling-under-served-lane={ceiling_beside_order}")
    print(f"\n{'ALL PASS' if not failed else str(failed) + ' FAILED'}")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
