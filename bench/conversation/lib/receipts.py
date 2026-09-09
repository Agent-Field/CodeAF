#!/usr/bin/env python3
"""Read one cell's receipts and normalise them into one shape.

A receipt is what the harness itself recorded about the call it made: which
model was billed, how many tokens went each way, what it cost. Three families
of receipt exist among the arms this suite runs, and this module is the only
place that knows the difference between them.

    pi-events        pi and omp stream JSON Lines events on stdout under
                     `--mode json`; assistant messages carry `model` and a
                     `usage` block with a `cost` breakdown.
    aforge-home      aforge writes `v3/usage.jsonl` (one row per call, with the
                     role that made it) and `logs/calls.jsonl` (one row per
                     wire call, with time-to-first-token) under AFORGE_HOME.
    opencode-events  opencode streams events under `--format json`; the
                     `step_finish` part carries tokens and cost — and no model.

TWO RULES HOLD IN ALL THREE READERS.

**Unknown stays unknown.** A harness that reported no usage gets `cost_usd:
null` and `cost_source: "none"`, never `0`. Zero is a measurement and null is
the absence of one, and a Pareto plot that reads an absence as a zero puts the
harness that reports nothing on the frontier.

**Every model that was billed is named, including the auxiliary ones.** A
title call, a reflex, a summariser or a fallback is a call somebody paid for
and a call the open-model law applies to. `models` is every distinct id seen;
`aux_models` is the subset that a role other than the turn itself made.
"""
import argparse
import glob
import json
import os
import math
import sys


def jsonl(path):
    """Yield parsed lines, skipping torn ones. A truncated last line is normal
    when a harness was killed at a cap, and it must not lose the rest."""
    try:
        with open(path, errors="replace") as handle:
            for line in handle:
                line = line.strip()
                if not line:
                    continue
                try:
                    yield json.loads(line)
                except ValueError:
                    continue
    except OSError:
        return


def plausible_cost(value):
    """A cost the provider could actually have charged: a finite number, not
    negative. A NaN or an infinity parses as JSON in Python and sums as a
    number, so it has to be refused explicitly rather than by accident."""
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return False
    return math.isfinite(value) and value >= 0


def read_guard_usage(path):
    """What the provider said it charged, for every call in this cell.

    This outranks a harness's self-report, which is that harness's arithmetic
    over its own price table — and a custom provider config can put zeroes in
    that table, which is how a paid pilot reported $0.00. Tokens without a cost
    stay tokens: no price is invented from them here.

    The guard opens a row when it ADMITS a request and closes one when the
    request ends, and both carry the same request id. The only sound way to
    know every admitted call was accounted for is to pair them id by id:
    counting admissions against settlements can be fooled by a torn ledger
    where one settlement is written twice and another is missing, and the
    counts agree while a billed call went unrecorded. So: an admission with no
    settlement, a settlement naming an admission that never was, a repeated
    id on either side, or a cost that is not a finite non-negative number —
    any of these makes the whole cell's cost unknown rather than part-counted.

    One shape is still read without ids: the rows the guard wrote before it
    recorded admissions have no phase and no request id, and evidence from
    those runs still reads. Everything else must reconcile exactly."""
    rows = []
    torn = False
    try:
        with open(path, errors="replace") as handle:
            for line in handle:
                if not line.strip():
                    continue
                try:
                    row = json.loads(line)
                    if not isinstance(row, dict):
                        torn = True
                    else:
                        rows.append(row)
                except ValueError:
                    torn = True
    except OSError:
        return None
    if not rows:
        return None
    inference = [row for row in rows if row.get("path", "").find("chat/completions") >= 0
                 or row.get("prompt_tokens") is not None]
    if not inference:
        return None
    admitted_ids = []
    settlements = {}
    legacy = []
    repeated = []
    malformed = ["unreadable ledger row"] if torn else []
    for row in inference:
        phase = row.get("phase")
        request_id = row.get("request_id")
        if phase and (not isinstance(request_id, str) or not request_id):
            malformed.append("missing request identity")
            continue
        if phase not in (None, "", "admitted", "settled", "generation"):
            malformed.append("unknown phase")
            continue
        if phase == "generation":
            continue
        if phase == "admitted":
            if request_id in admitted_ids:
                repeated.append(("admission", request_id))
            else:
                admitted_ids.append(request_id)
        elif phase == "settled":
            if request_id in settlements:
                repeated.append(("settlement", request_id))
            else:
                settlements[request_id] = row
        elif not phase and not request_id:
            # The shape the guard wrote before admissions were recorded.
            legacy.append(row)
    orphan_ids = [request_id for request_id in settlements
                  if request_id not in admitted_ids]
    unaccounted = [request_id for request_id in admitted_ids
                   if request_id not in settlements]
    settled_rows = list(settlements.values()) + legacy
    priced_rows = [row for row in settled_rows if plausible_cost(row.get("cost_usd"))]
    got = {
        "calls": len(admitted_ids) or len(settled_rows),
        "models": [],
        "tokens_in": sum(int(row.get("prompt_tokens") or 0) for row in settled_rows) or None,
        "tokens_out": sum(int(row.get("completion_tokens") or 0) for row in settled_rows) or None,
        "cost_usd": None,
        "cost_source": "none",
        "notes": [],
    }
    for row in inference:
        model = row.get("model")
        if model and model not in got["models"]:
            got["models"].append(model)
    if legacy and (admitted_ids or settlements):
        malformed.append("mixed legacy and identified ledger")
    if malformed:
        got["notes"].append("invalid ledger: " + "; ".join(malformed))
    if repeated:
        got["notes"].append(
            "the usage ledger repeated %s — a torn ledger, so the cell's cost is "
            "unknown rather than part-counted"
            % ", ".join("%s for %s" % pair for pair in repeated))
    if orphan_ids:
        got["notes"].append(
            "the usage ledger settled %d call(s) no admission booked — a torn "
            "ledger, so the cell's cost is unknown rather than part-counted"
            % len(orphan_ids))
    if unaccounted:
        got["notes"].append(
            "%d of %d admitted calls never settled — billed and unaccounted, so the "
            "cell's cost is unknown rather than part-counted"
            % (len(unaccounted), len(admitted_ids)))
    implausible = [request_id for request_id, row in settlements.items()
                   if row.get("cost_usd") is not None
                   and not plausible_cost(row.get("cost_usd"))]
    if implausible:
        got["notes"].append(
            "the provider's usage named a cost that cannot be a price for %d call(s) "
            "— cost unknown rather than part-counted" % len(implausible))
    if not (repeated or orphan_ids or unaccounted or implausible or malformed):
        if settled_rows and len(priced_rows) == len(settled_rows):
            got["cost_usd"] = sum(float(row["cost_usd"]) for row in priced_rows)
            got["cost_source"] = "guard-upstream"
        elif settled_rows:
            got["notes"].append(
                "upstream priced %d of %d calls — cost left unknown rather than part-counted"
                % (len(priced_rows), len(settled_rows)))
    return got


def blank():
    return {
        "cost_usd": None,
        "cost_source": "none",
        "tokens_in": None,
        "tokens_out": None,
        "tokens_total": None,
        "models": [],
        "aux_models": [],
        "calls": 0,
        "turns": 0,
        "ttft_ms": None,
        "reply": "",
        "notes": [],
    }


def text_of(content):
    """Flatten a message's content parts into the text a person would have read.
    Thinking parts are deliberately left out: they are not the reply."""
    if isinstance(content, str):
        return content
    out = []
    for part in content or []:
        if isinstance(part, dict) and part.get("type") == "text":
            out.append(part.get("text") or "")
    return "".join(out)


def read_pi_events(path, _stdout):
    """pi and omp: the same event schema, verified on pi 0.84.2 and omp 18.1.2.

    `message_end` and `turn_end` repeat the SAME assistant message, and
    `agent_end` repeats all of them again. Summing every usage block that goes
    past would treble the cost of every run, so messages are collected into a
    dict keyed by their own identity first and summed once at the end."""
    got = blank()
    rows = []
    if os.path.isdir(path):
        # The interactive door leaves no stdout to read, so the session files
        # are the only receipt there is. They are read opportunistically: same
        # parser, and an unrecognised schema ends as "no usage found" rather
        # than as a zero.
        for session in sorted(glob.glob(os.path.join(path, "**", "*.jsonl"), recursive=True)):
            rows.extend(jsonl(session))
        if not rows:
            got["notes"].append("no session JSONL under %s" % path)
            return got
    else:
        rows = list(jsonl(path))
        if not rows:
            got["notes"].append("no events on stdout — the harness streamed nothing")
            return got

    messages = {}

    def remember(message):
        if not isinstance(message, dict) or message.get("role") != "assistant":
            return
        key = message.get("responseId") or message.get("timestamp") or len(messages)
        messages.setdefault(key, message)

    for row in rows:
        if not isinstance(row, dict):
            continue
        kind = row.get("type")
        if kind in ("message_end", "turn_end"):
            remember(row.get("message"))
        elif kind == "agent_end":
            for message in row.get("messages") or []:
                remember(message)
        elif row.get("role") == "assistant":
            # A session file on disk holds the messages themselves rather than
            # the events that carried them.
            remember(row)

    if not messages:
        got["notes"].append("no assistant message carried usage")
        return got

    cost = 0.0
    tokens_in = tokens_out = tokens_total = 0
    saw_usage = False
    models, last_text = [], ""
    for message in messages.values():
        model = message.get("model")
        if model and model not in models:
            models.append(model)
        usage = message.get("usage") or {}
        if usage:
            saw_usage = True
            tokens_in += int(usage.get("input") or 0)
            tokens_out += int(usage.get("output") or 0)
            tokens_total += int(usage.get("totalTokens") or 0)
            price = usage.get("cost")
            if isinstance(price, dict):
                cost += float(price.get("total") or 0)
            elif isinstance(price, (int, float)):
                cost += float(price)
        text = text_of(message.get("content"))
        if text.strip():
            last_text = text

    got["models"] = models
    got["calls"] = len(messages)
    got["turns"] = len(messages)
    got["reply"] = last_text
    if saw_usage and (tokens_in or tokens_out or tokens_total):
        got["cost_usd"] = cost
        got["cost_source"] = "self-reported"
        got["tokens_in"] = tokens_in
        got["tokens_out"] = tokens_out
        got["tokens_total"] = tokens_total or (tokens_in + tokens_out)
    elif saw_usage:
        # An all-zero usage block is what a refused or failed call leaves
        # behind. Recording it as a $0 run would put a harness that never
        # reached the provider at the cheap end of the frontier.
        got["notes"].append("usage block was all zeros — nothing was billed")
    else:
        got["notes"].append("assistant messages carried no usage block")
    return got


def read_opencode_events(path, _stdout):
    """opencode: `step_finish` carries tokens and cost. It does NOT carry the
    model, so the billed model is not verifiable from a run's own output —
    recorded as a note rather than guessed from the flag that was passed."""
    got = blank()
    rows = list(jsonl(path))
    if not rows:
        got["notes"].append("no events on stdout — the harness streamed nothing")
        return got

    cost = 0.0
    tokens_in = tokens_out = tokens_total = 0
    steps = 0
    chunks = []
    for row in rows:
        if not isinstance(row, dict):
            continue
        part = row.get("part") or {}
        if row.get("type") == "text" and part.get("text"):
            chunks.append(part["text"])
        if row.get("type") == "step_finish":
            steps += 1
            tokens = part.get("tokens") or {}
            tokens_in += int(tokens.get("input") or 0)
            tokens_out += int(tokens.get("output") or 0)
            tokens_total += int(tokens.get("total") or 0)
            price = part.get("cost")
            if isinstance(price, (int, float)):
                cost += float(price)

    got["reply"] = "".join(chunks)
    got["calls"] = steps
    got["turns"] = steps
    got["notes"].append("opencode events do not name the billed model")
    if steps:
        got["cost_usd"] = cost
        got["cost_source"] = "self-reported"
        got["tokens_in"] = tokens_in
        got["tokens_out"] = tokens_out
        got["tokens_total"] = tokens_total or (tokens_in + tokens_out)
    else:
        got["notes"].append("no step_finish event — nothing reported usage")
    return got


def read_aforge_home(home, stdout_path):
    """aforge: the home is the witness, not the screen.

    `v3/usage.jsonl` has one row per call with the model, the token counts, the
    dollars and — for anything that was not the turn itself — the role that made
    it. Those role rows are the point of reading this file rather than a summary
    line: they are what an open-model law has to be enforced against."""
    got = blank()
    usage_path = os.path.join(home, "v3", "usage.jsonl")
    rows = list(jsonl(usage_path))
    if not rows:
        # A session that never reached the wire leaves no usage file at all.
        # That is an absence of measurement, not a free run.
        got["notes"].append("no v3/usage.jsonl under the cell home")
    cost = 0.0
    tokens_in = tokens_out = 0
    models, aux_models = [], []
    for row in rows:
        model = row.get("model")
        if model:
            if model not in models:
                models.append(model)
            if row.get("role") and model not in aux_models:
                aux_models.append(model)
        cost += float(row.get("usd") or 0)
        tokens_in += int(row.get("in") or 0)
        tokens_out += int(row.get("out") or 0)
        got["calls"] += int(row.get("calls") or 1)

    # Time to first token is the latency a person felt on the turn's own call;
    # a reflex or a title call that ran beside it is not that number.
    for row in rows:
        if row.get("ttft_ms") and not row.get("role"):
            got["ttft_ms"] = int(row["ttft_ms"])
            break

    # The transcript's non-aux `usage` seals are how many turns actually ended.
    seals = 0
    for path in glob.glob(os.path.join(home, "v3", "projects", "*", "*", "transcript.jsonl")):
        for row in jsonl(path):
            if row.get("type") == "usage" and row.get("usage") and not row["usage"].get("aux"):
                seals += 1

    got["models"] = models
    got["aux_models"] = aux_models
    got["turns"] = seals
    if rows and (tokens_in or tokens_out):
        got["cost_usd"] = cost
        got["cost_source"] = "self-reported"
        got["tokens_in"] = tokens_in
        got["tokens_out"] = tokens_out
        got["tokens_total"] = tokens_in + tokens_out
    elif rows:
        got["notes"].append("usage rows were all zeros — nothing was billed")
    if stdout_path and os.path.exists(stdout_path):
        with open(stdout_path, errors="replace") as handle:
            got["reply"] = handle.read()
    return got


READERS = {
    "pi-events": read_pi_events,
    "opencode-events": read_opencode_events,
    "aforge-home": read_aforge_home,
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kind", required=True, choices=sorted(READERS))
    parser.add_argument("--path", required=True, help="stdout log, or the cell's aforge home")
    parser.add_argument("--stdout", default="", help="stdout log, when --path is a home")
    parser.add_argument("--guard-usage", default="", help="the cell guard's usage log")
    parser.add_argument("--out", required=True, help="where to write receipt.json")
    parser.add_argument("--reply", default="", help="where to write the reply text")
    args = parser.parse_args()

    got = READERS[args.kind](args.path, args.stdout)
    got["kind"] = args.kind

    # A self-reported figure is kept for comparison, never as the answer when
    # the guard has one. A zero self-report against non-zero tokens is not
    # evidence of a free call — it is a price table with zeroes in it.
    got["self_reported_cost_usd"] = got["cost_usd"]
    got["self_reported_source"] = got["cost_source"]
    if (got["cost_source"] == "self-reported" and got["cost_usd"] == 0
            and (got.get("tokens_in") or got.get("tokens_out"))):
        got["cost_usd"] = None
        got["cost_source"] = "none"
        got["notes"].append(
            "self-reported cost was 0 with %s/%s tokens — a zero price table, not a free call"
            % (got.get("tokens_in"), got.get("tokens_out")))

    measured = read_guard_usage(args.guard_usage) if args.guard_usage else None
    if measured:
        got["guard_calls"] = measured["calls"]
        got["guard_models"] = measured["models"]
        got["notes"].extend(measured["notes"])
        if measured["cost_source"] == "guard-upstream":
            got["cost_usd"] = measured["cost_usd"]
            got["cost_source"] = "guard-upstream"
        else:
            # The guard admitted calls and could not price all of them. The
            # harness's own figure covers the calls it knows about, so quoting
            # it here would report a total for a cell where a billed call went
            # unaccounted — the most confident possible under-count.
            got["cost_usd"] = None
            got["cost_source"] = "none"
        if measured["tokens_in"] or measured["tokens_out"]:
            got["tokens_in"] = measured["tokens_in"]
            got["tokens_out"] = measured["tokens_out"]
        # The guard sees every call, including the auxiliary ones a harness may
        # not put in its own transcript, so its model list is the one the
        # allowlist is judged on.
        for model in measured["models"]:
            if model not in got["models"]:
                got["models"].append(model)
    with open(args.out, "w") as handle:
        json.dump(got, handle, indent=1, sort_keys=True)
        handle.write("\n")
    if args.reply:
        with open(args.reply, "w") as handle:
            handle.write(got["reply"])
    # A reader that found nothing at all says so on stderr, so that a cell's log
    # carries the reason rather than only a null in a file.
    if got["cost_source"] == "none":
        print("receipts: no usage found (%s)" % "; ".join(got["notes"] or ["no reason recorded"]),
              file=sys.stderr)


if __name__ == "__main__":
    main()
