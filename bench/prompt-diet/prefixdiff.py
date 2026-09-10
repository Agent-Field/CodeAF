#!/usr/bin/env python3
"""prefixdiff.py — where the cached prefix broke, and what broke it.

WHY THIS EXISTS. `compare.py` reports what the provider CHARGED, and the first
dev→diet run said the median cached share fell from about 91% to about 69%
(BENCH.md §1a). A charge is an outcome and not a cause: a cached share can fall
because the prefix genuinely moved, or because the mix of requests changed —
nineteen turn-requests instead of sixty-nine means a larger fraction of them are
turn-OPENERS, whose tail necessarily differs from the round before it. Those two
want opposite responses, and nothing already in this bench can tell them apart.

So this reads the REQUEST BODIES and answers the question directly: for every
request, how many leading bytes did it share with the request before it in the
same conversation, and — when the answer is "not all of them" — which block,
which field, and which sixty bytes.

WHAT IT COMPARES, AND IN WHAT ORDER. A provider caches on the leading bytes of
the prompt IT assembles, which is not the order of the JSON body it was handed.
Two families, and the body says which:

  Anthropic     top-level `tools`, then top-level `system`, then `messages`.
                Cached behind explicit `cache_control` markers.
  OpenAI-style  one `messages` array with the system message at index 0, and
  (OpenRouter,  `tools` in the body. DeepSeek and most of what OpenRouter fronts
   DeepSeek)    cache automatically on the leading prompt in 64-token blocks.

Both are serialised here as [tools][system][messages], which is the Anthropic
order exactly and the conservative reading of the other: whatever the server
does with the tool block, a client that keeps tools AND system AND the head of
the transcript byte-stable is stable under either assembly. A run where the
tool block moves and nothing else will say so on its own row, so the two causes
are never confused even if a given server happens to forgive one of them.

WHAT COUNTS AS "the request before it". Requests are grouped into sessions by
the conversation they belong to — the log file they were written to, the task
node, and the opening human message — and ordered by their timestamp. A request
whose predecessor is in another conversation has no comparison and is reported
as an opener rather than as a miss, because a first request cannot break a cache
that does not exist yet.

Usage:
    prefixdiff.py <run-dir> [<run-dir> …]      one table per run, then the roll-up
    prefixdiff.py <run-dir> --jsonl rows.jsonl one row per request, machine-readable
    prefixdiff.py <run-dir> --all              asides too, not just belt-carrying turns

`<run-dir>` is a `run.sh` evidence tree (`~/bench-diet-out/<label>/`). Only the
call log can answer this: the guard deliberately keeps no bodies, so the run
must have been made with AFORGE_CALL_LOG_BODIES=1, which `run.sh` sets.
"""

import argparse
import hashlib
import json
import os
import statistics
import sys

# ── the blocks of message[0], in the order refreshSystemLocked writes them ───
#
# internal/session/memory.go builds message[0] as
# `a.system + a.placesText + a.standingText + a.memoryText + record`, and each
# of the four tails is introduced by a marker the Go side spells exactly once.
# Finding them in the OLD text is how a byte offset becomes the name of a thing
# somebody can go and fix. The list is in wire order and is scanned in order, so
# a marker that also appears inside quoted instructions cannot pull a later
# block in front of an earlier one.
SYSTEM_BLOCKS = [
    ("page", None),                       # prompts/system.md as rendered
    ("# Project footer", "\n\n# Project\n"),  # …including the clock line
    ("attached folders", "\n## Instructions for "),
    ("<standing>", "\n<standing>\n"),
    ("<memory>", "\n<memory>\n"),
    ("the record", "\n\nthe record\n"),
]


def rows(path):
    """Every JSON object in a JSON Lines file, skipping what will not parse.

    A half-written last line is the ordinary end of a file whose writer was
    killed, and it must not cost the run every row in front of it. This is
    `lib/wire.py`'s reader and behaves the same way on purpose."""
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


def canonical(value):
    """Bytes as the wire carried them, with the KEY ORDER LEFT ALONE.

    Sorting keys here would be the one mistake this instrument cannot make. A
    schema marshalled from a Go map arrives with its keys in a random order and
    is a prefix-buster on every request; sorting them would hide exactly that,
    and it is one of the suspects this file was written to convict."""
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("utf-8")


def message_text(message):
    """The text of one message, whatever shape its content arrived in."""
    content = message.get("content")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for piece in content:
            if isinstance(piece, dict) and isinstance(piece.get("text"), str):
                parts.append(piece["text"])
        return "".join(parts)
    return ""


def segments(body):
    """The request as a list of (label, bytes), in the provider's cache order.

    The labels are what a report can name: `tools[3] search`, `messages[0]
    system`, `messages[7] tool`. Concatenating the bytes gives the string whose
    common prefix with the last request is the thing being measured."""
    out = []
    for index, tool in enumerate(body.get("tools") or []):
        name = ""
        if isinstance(tool, dict):
            inner = tool.get("function") if isinstance(tool.get("function"), dict) else tool
            name = str(inner.get("name") or "")
        out.append((f"tools[{index}] {name}", canonical(tool)))
    # Anthropic keeps the system prompt out of the messages array; OpenAI-style
    # bodies keep it at messages[0]. Either way it is one segment sitting
    # between the tools and the transcript.
    if body.get("system") is not None:
        out.append(("system", canonical(body["system"])))
    for index, message in enumerate(body.get("messages") or []):
        role = ""
        if isinstance(message, dict):
            role = str(message.get("role") or "")
        out.append((f"messages[{index}] {role}", canonical(message)))
    return out


def common_prefix(left, right):
    """How many leading bytes two byte strings share."""
    limit = min(len(left), len(right))
    # A byte-at-a-time loop over a megabyte of transcript is slow enough to be
    # felt across a hundred requests, so the search is halved down to the byte.
    low, high = 0, limit
    while low < high:
        middle = (low + high + 1) // 2
        if left[:middle] == right[:middle]:
            low = middle
        else:
            high = middle - 1
    return low


def locate(marks, offset):
    """Which segment a byte offset falls in, and how far into it."""
    for label, start, end in marks:
        if start <= offset < end:
            return label, offset - start
    if marks:
        label, start, end = marks[-1]
        return label, end - start
    return "?", 0


def system_block(text, offset):
    """Which block of message[0] a byte offset lands in.

    `text` is the JSON-encoded message, `offset` a byte offset into it; the
    encoding is close enough to the plain text that a marker search over the
    encoded form finds the right block, because every marker is plain ASCII
    that JSON does not escape."""
    found = []
    cursor = 0
    for name, marker in SYSTEM_BLOCKS:
        if marker is None:
            found.append((name, 0))
            continue
        # The markers are searched forward from the last one found, so wire
        # order is what decides, not whichever copy comes first in the file.
        where = text.find(marker.encode("utf-8"), cursor)
        if where < 0:
            continue
        cursor = where
        found.append((name, where))
    name = found[0][0] if found else "message"
    for candidate, where in found:
        if offset >= where:
            name = candidate
    return name


def excerpt(raw, offset, width=60):
    """Sixty bytes around the first difference, printable on one line."""
    start = max(0, offset - 10)
    piece = raw[start:offset + width]
    return piece.decode("utf-8", errors="replace").replace("\n", "\\n").replace("\t", "\\t")


def classify(before, after, label, block):
    """One word for the cause, from what actually moved.

    The classification is deliberately about MECHANISM and not about blame: a
    tool block that grew by one tool at the end is `tool-armed`, which is a
    legitimate once-per-load cost, while one whose second tool changed is
    `tool-reordered`, which is a defect. A reader who cannot tell those apart
    cannot act on this table at all."""
    if label.startswith("tools"):
        names_before = [t.get("function", t).get("name") if isinstance(t, dict) else None
                        for t in (before.get("tools") or [])]
        names_after = [t.get("function", t).get("name") if isinstance(t, dict) else None
                       for t in (after.get("tools") or [])]
        if names_before == names_after[:len(names_before)] and len(names_after) > len(names_before):
            return "tool-armed"
        if sorted(x for x in names_before if x) == sorted(x for x in names_after if x):
            return "tool-reordered" if names_before != names_after else "tool-text"
        return "tool-set-changed"
    if label.startswith("messages[0]") or label == "system":
        return {
            "page": "page",
            "# Project footer": "clock",
            "attached folders": "places",
            "<standing>": "standing",
            "<memory>": "memory",
            "the record": "record",
        }.get(block, "message0")
    return "history-rewritten"


def session_key(path, row, body):
    """The conversation a request belongs to.

    The opening human message identifies it: every request of one conversation
    carries it, and no two conversations of a bench run share one. The log file
    and the task node are in the key because a node runs its own transcript
    that legitimately starts from the same words as its parent's."""
    opening = ""
    for message in body.get("messages") or []:
        if isinstance(message, dict) and message.get("role") not in ("system", None):
            opening = message_text(message)[:400]
            break
    digest = hashlib.sha1(opening.encode("utf-8", "replace")).hexdigest()[:8]
    return f"{path}|node={row.get('node') or '-'}|{digest}"


def collect(root, belt_only=True):
    """Every request of one run, in order, with its body parsed."""
    out = []
    for base, _, names in os.walk(root):
        for name in sorted(names):
            if not name.endswith("calllog.jsonl"):
                continue
            path = os.path.join(base, name)
            cell = os.path.relpath(path, root)
            for row in rows(path):
                if row.get("phase") == "start":
                    continue
                raw = row.get("request_body")
                if not raw:
                    continue
                try:
                    body = json.loads(raw)
                except ValueError:
                    continue
                tools = body.get("tools") or []
                # `carries_the_belt` is compare.py's rule and BENCH.md §1b is
                # why: a request with no tool definitions is a title call, a
                # router or a judge, and averaging it beside a turn is what
                # produced the 94% that was never real.
                if belt_only and not tools:
                    continue
                out.append({
                    "cell": cell,
                    "ts": row.get("ts") or "",
                    "id": row.get("id"),
                    "tag": row.get("tag"),
                    "model": row.get("model"),
                    "tools": len(tools),
                    "prompt_tokens": row.get("prompt_tokens"),
                    "cached_tokens": row.get("cached_tokens"),
                    "session": session_key(cell, row, body),
                    "body": body,
                })
    out.sort(key=lambda item: (item["session"], item["ts"], str(item["id"])))
    return out


def analyse(root, belt_only=True):
    """One row per request: stable bytes, where it stopped, and why."""
    requests = collect(root, belt_only)
    previous = {}
    results = []
    for item in requests:
        marks, cursor = [], 0
        blob = bytearray()
        for label, raw in segments(item["body"]):
            marks.append((label, cursor, cursor + len(raw)))
            blob.extend(raw)
            cursor += len(raw)
        blob = bytes(blob)
        last = previous.get(item["session"])
        previous[item["session"]] = (item["body"], blob, marks)
        record = {
            "cell": item["cell"],
            "id": item["id"],
            "tag": item["tag"],
            "session": item["session"][-8:],
            "tools": item["tools"],
            "bytes": len(blob),
            "prompt_tokens": item["prompt_tokens"],
            "cached_tokens": item["cached_tokens"],
        }
        if last is None:
            record.update(kind="opener", stable=0, share=0.0, cause="-", where="-")
            results.append(record)
            continue
        old_body, old_blob, old_marks = last
        stable = common_prefix(old_blob, blob)
        record["stable"] = stable
        record["share"] = stable / len(blob) if blob else 0.0
        if stable == len(old_blob):
            # THE ONLY SHAPE THAT COSTS NOTHING: everything the last request
            # sent is still there, unchanged, at the head of this one.
            record.update(kind="append", cause="-", where="-")
            results.append(record)
            continue
        label, inner = locate(old_marks, stable)
        block = "-"
        if label.startswith("messages[0]") or label == "system":
            start = next(s for lab, s, _ in old_marks if lab == label)
            block = system_block(old_blob[start:], inner)
        record.update(
            kind="broken",
            cause=classify(old_body, item["body"], label, block),
            where=f"{label}" + (f" · {block}" if block != "-" else ""),
            old=excerpt(old_blob, stable),
            new=excerpt(blob, stable),
        )
        results.append(record)
    return results


def cached_share(row):
    """What the provider said it did NOT have to re-read, as a fraction.

    THIS IS A MEDIAN OF RATIOS AND NEVER A RATIO OF MEDIANS, which is the whole
    reason it is computed here rather than divided out of compare.py's table.
    The two are different numbers over the same rows — the request with the
    median prompt is not the request with the median cache — and the difference
    between them is large enough to invert the finding. BENCH.md §1c is the
    worked example: divided medians said the cached share fell from 91% to 69%,
    and the median request's own share had RISEN from 83% to 95%."""
    prompt, cached = row.get("prompt_tokens"), row.get("cached_tokens")
    if not prompt or cached is None:
        return None
    return cached / prompt


def median(values):
    """The median of what is known, or None when nothing is."""
    known = [value for value in values if value is not None]
    return statistics.median(known) if known else None


def share_text(value):
    return "-" if value is None else f"{value:.1%}"


def table(label, results):
    """The per-request table, and the roll-up under it."""
    lines = [f"### {label}", ""]
    lines.append("| # | cell | tag | tools | bytes | stable | share | kind | where | cause |")
    lines.append("| ---: | --- | --- | ---: | ---: | ---: | ---: | --- | --- | --- |")
    for index, row in enumerate(results, 1):
        lines.append("| {} | {} | {} | {} | {} | {} | {} | {} | {} | {} |".format(
            index, row["cell"], row["tag"] or "-", row["tools"], row["bytes"],
            row["stable"], f"{row['share']:.1%}", row["kind"], row["where"], row["cause"]))
    lines.append("")
    broken = [row for row in results if row["kind"] == "broken"]
    append = [row for row in results if row["kind"] == "append"]
    opener = [row for row in results if row["kind"] == "opener"]
    lines.append(f"{len(results)} belt-carrying requests · {len(opener)} openers · "
                 f"{len(append)} append-only · {len(broken)} with a moved prefix")
    carried = [row for row in results if row["kind"] != "opener"]
    if carried:
        lines.append("median stable share of the request: "
                     + share_text(median(row["share"] for row in carried)))
    # AND WHAT THE PROVIDER ACTUALLY BILLED, beside it. The two answer different
    # halves of the same question: the measured share is what this client made
    # cacheable, the billed share is what the server managed to reuse of it. A
    # run where they diverge is a run where the bytes were stable and the cache
    # was missed for some other reason — a cold replica, a block boundary, an
    # expired entry — which is a different investigation entirely.
    lines.append("median cached share the provider billed: all "
                 + share_text(median(cached_share(row) for row in results))
                 + " · openers " + share_text(median(cached_share(row) for row in opener))
                 + " · continuations " + share_text(median(cached_share(row) for row in carried)))
    lines.append(f"sessions: {len(set(row['session'] for row in results))} · "
                 f"openers are {len(opener) / len(results):.0%} of the requests")
    causes = {}
    for row in broken:
        causes[row["cause"]] = causes.get(row["cause"], 0) + 1
    if causes:
        lines.append("causes: " + ", ".join(f"{name} ×{count}" for name, count
                                            in sorted(causes.items(), key=lambda p: -p[1])))
    lines.append("")
    for row in broken:
        lines.append(f"- **{row['cause']}** at {row['where']}, {row['stable']} of "
                     f"{row['bytes']} bytes stable ({row['cell']}, {row['id']})")
        lines.append(f"  - was: `{row['old']}`")
        lines.append(f"  - now: `{row['new']}`")
    lines.append("")
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("runs", nargs="+", help="run.sh evidence trees")
    parser.add_argument("--jsonl", help="write one row per request here")
    parser.add_argument("--all", action="store_true",
                        help="include requests with no tool block (asides)")
    args = parser.parse_args()

    written = []
    for root in args.runs:
        label = os.path.basename(os.path.normpath(root))
        results = analyse(root, belt_only=not args.all)
        if not results:
            print(f"### {label}\n\nNo request bodies in {root} — the run needs "
                  f"AFORGE_CALL_LOG_BODIES=1.\n")
            continue
        print(table(label, results))
        for row in results:
            row["run"] = label
        written.extend(results)

    if args.jsonl:
        with open(args.jsonl, "w") as handle:
            for row in written:
                handle.write(json.dumps(row, sort_keys=True) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
