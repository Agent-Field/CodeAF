#!/usr/bin/env python3
"""wire.py — one normalised row per model request, from the two records that
already exist. Nothing here stands up a new proxy.

TWO SOURCES, AND THEY MEASURE DIFFERENT THINGS.

  the guard   `bench/conversation/lib/guard.py` is a loopback forwarder every
              conversation cell already runs in front of OpenRouter, handed to
              the harness as AFORGE_BASE_URL with a sentinel key. It writes
              `guard-usage.jsonl`: an `admitted` row carrying the request's
              SHAPE (bytes on the wire, message count, tool count) and a
              `settled` row carrying what the provider said it CHARGED
              (prompt_tokens, completion_tokens, prompt_tokens_details.
              cached_tokens, cost). It is arm-neutral — the same measurement for
              aforge and for any peer — and it deliberately keeps no bodies.

  the call log `internal/calllog` is aforge's own always-on record, pinned to a
              path with AFORGE_CALL_LOG. With AFORGE_CALL_LOG_BODIES=1 it keeps
              the whole request body, which is the only place the TOOL BLOCK's
              own bytes can be counted per request rather than inferred.

So the guard answers "what did this turn cost" for every arm, and the call log
answers "how much of it was the tool block" for the aforge arm. A row from
either carries `source` saying which, and the two are never added together.

A call-log row also carries the PREFIX FINGERPRINTS — `system_bytes`,
`system_sha`, `tool_block_bytes`, `tools_sha`, `prefix_sha` — which are what a
byte-stability question is answered from: the same `prefix_sha` on every request
of a conversation is a prefix that stayed cacheable, and a new one per turn is
the frontier bill DESIGN.md §0 opens with. The whole body is NOT copied in here;
one run's bodies are sixteen megabytes and this file is meant to be read as a
table. `body_source` names the call log they are in, beside this file.

The exact static split — page bytes against tool-block bytes — is not sampled
here at all. It comes from layer A's budget test, which weighs the rendered page
and the marshalled belt directly and is the scoreboard the wave commits against.

Usage: wire.py <run-dir>   → JSON Lines on stdout
"""

import hashlib
import json
import os
import sys


def rows(path):
    """Every JSON object in a JSON Lines file, skipping what will not parse.

    A half-written last line is the ordinary end of a file whose writer was
    killed, and it must not cost the run every row in front of it."""
    try:
        handle = open(path, "r", errors="replace")
    except OSError:
        return
    with handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            try:
                yield json.loads(line)
            except ValueError:
                continue


def detail(row, block, name):
    """One nested count, or None. Absent is unknown and never zero: a cache
    figure of zero is a claim that nothing was cached, which is a different
    statement from a provider that reported no cache block at all."""
    inner = row.get(block)
    if isinstance(inner, dict) and isinstance(inner.get(name), int):
        return inner[name]
    return None


def from_guard(path, cell):
    """Pair the guard's two phases back into one row per request."""
    admitted, out = {}, []
    for row in rows(path):
        key = row.get("request_id")
        if not key:
            continue
        if row.get("phase") == "admitted":
            admitted[key] = row
            continue
        if row.get("phase") != "settled":
            continue
        opened = admitted.get(key, {})
        out.append({
            "source": "guard",
            "cell": cell,
            "request_id": key,
            "model": row.get("model") or opened.get("model"),
            "prompt_tokens": row.get("prompt_tokens"),
            "cached_tokens": detail(row, "prompt_tokens_details", "cached_tokens"),
            "completion_tokens": row.get("completion_tokens"),
            "reasoning_tokens": detail(row, "completion_tokens_details", "reasoning_tokens"),
            "cost_usd": row.get("cost_usd"),
            "request_bytes": opened.get("request_bytes"),
            "message_count": opened.get("message_count"),
            "tool_count": opened.get("tool_count"),
            "tool_block_bytes": None,
            "elapsed_ms": row.get("elapsed_ms"),
            "outcome": row.get("outcome"),
        })
    # AN ADMISSION WITH NO SETTLEMENT IS A REAL EVENT and is reported as one: it
    # is a call that may well have been billed and whose price nobody knows. It
    # would be invisible if only settled rows were read, and invisible is the
    # one thing a bill must never be.
    for key, opened in admitted.items():
        if any(row["request_id"] == key for row in out):
            continue
        out.append({
            "source": "guard",
            "cell": cell,
            "request_id": key,
            "model": opened.get("model"),
            "prompt_tokens": None,
            "cached_tokens": None,
            "completion_tokens": None,
            "reasoning_tokens": None,
            "cost_usd": None,
            "request_bytes": opened.get("request_bytes"),
            "message_count": opened.get("message_count"),
            "tool_count": opened.get("tool_count"),
            "tool_block_bytes": None,
            "elapsed_ms": None,
            "outcome": "unsettled",
        })
    return out


def digest(text):
    """Twelve hex characters of SHA-256 — enough to tell two prefixes apart in a
    table, short enough to read down a column."""
    return hashlib.sha256(text.encode("utf-8")).hexdigest()[:12]


def prefix_of(body):
    """The size and the fingerprint of everything a request pays for twice.

    BYTE-STABILITY IS A DIFFERENT QUESTION FROM SIZE, and it is the one the
    frontier cached bill turns on (DESIGN.md §0): a prefix that is one byte
    different from the last request's re-prices the whole conversation cold, so
    a diet that shortened the page while making it vary per turn would look like
    a win in every size column here and cost more in practice. These fields are
    what answer it — the system message and the tool block, each weighed and
    each fingerprinted, per request — so a reader can see at a glance whether
    the same prefix travelled every time or a new one did.

    Everything is computed from the request body the call log already keeps
    under AFORGE_CALL_LOG_BODIES; nothing here needs a second capture. Absent
    fields mean the body was not kept, never that the prefix was empty."""
    out = {"tool_block_bytes": None, "tools_sha": None,
           "system_bytes": None, "system_sha": None, "prefix_sha": None}
    if not body:
        return out
    try:
        payload = json.loads(body)
    except ValueError:
        return out
    tools = payload.get("tools")
    tools_text = ""
    if isinstance(tools, list):
        tools_text = json.dumps(tools, separators=(",", ":"), sort_keys=True)
        out["tool_block_bytes"] = len(tools_text)
        out["tools_sha"] = digest(tools_text)
    # The system prompt is message[0] where the wire carries one, and some
    # adapters put it in a top-level `system` field instead. Both are read, and
    # a conversation that opens with an ordinary user message simply has none.
    system_text = ""
    messages = payload.get("messages")
    if isinstance(messages, list) and messages:
        first = messages[0]
        if isinstance(first, dict) and first.get("role") == "system":
            content = first.get("content")
            system_text = content if isinstance(content, str) else json.dumps(
                content, separators=(",", ":"), sort_keys=True)
    if not system_text and isinstance(payload.get("system"), str):
        system_text = payload["system"]
    if system_text:
        out["system_bytes"] = len(system_text)
        out["system_sha"] = digest(system_text)
    if tools_text or system_text:
        out["prefix_sha"] = digest(system_text + "\x00" + tools_text)
    return out


def from_calllog(path, cell):
    """The call log's end rows, which are the ones carrying the bill."""
    out = []
    for row in rows(path):
        if row.get("phase") == "start":
            continue
        out.append({
            "source": "calllog",
            "cell": cell,
            "request_id": row.get("id"),
            "model": row.get("model"),
            "served": row.get("served"),
            "tag": row.get("tag"),
            "prompt_tokens": row.get("prompt_tokens"),
            "cached_tokens": row.get("cached_tokens"),
            "completion_tokens": row.get("completion_tokens"),
            "reasoning_tokens": row.get("reasoning_tokens"),
            "cost_usd": row.get("cost"),
            "request_bytes": len(row["request_body"]) if row.get("request_body") else None,
            "message_count": row.get("messages"),
            "tool_count": row.get("tools"),
            "elapsed_ms": row.get("ms"),
            "outcome": row.get("finish"),
            # WHERE THE WHOLE BODY IS, for anyone who needs the bytes rather
            # than a fingerprint of them. It is deliberately not copied in here:
            # one run's bodies are sixteen megabytes and this file is meant to
            # be read as a table.
            "body_source": os.path.basename(path),
        })
        out[-1].update(prefix_of(row.get("request_body")))
    return out


def main():
    root = sys.argv[1]
    out = []
    for base, _, names in os.walk(root):
        # The cell is the directory the evidence sits in, relative to the run,
        # which is how a reader gets from a row back to the scrollback that
        # produced it.
        cell = os.path.relpath(base, root)
        present = set(names)
        # THE RECONCILED FILE SUPERSEDES THE RAW ONE. `bench/conversation`
        # rewrites the guard's ledger against the provider's own generation
        # records where it can, and reading both would bill this cell twice —
        # which is the one arithmetic error a cost table must not make. The
        # choice is made per directory rather than by file order, because
        # "guard-reconciled" sorts before "guard-usage" and a first-wins rule
        # would silently become a both-win rule.
        if "guard-reconciled.jsonl" in present:
            out.extend(from_guard(os.path.join(base, "guard-reconciled.jsonl"), cell))
        elif "guard-usage.jsonl" in present:
            out.extend(from_guard(os.path.join(base, "guard-usage.jsonl"), cell))
        for name in sorted(present):
            if name.endswith("calllog.jsonl"):
                out.extend(from_calllog(os.path.join(base, name), cell))
    for row in out:
        sys.stdout.write(json.dumps(row, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
