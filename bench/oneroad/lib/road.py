#!/usr/bin/env python3
"""road.py — the road columns, and the parallelism timeline, read from the store.

bench/oneroad/README.md's rule for the last five columns is that they come from
the run and never from the run's own account of itself. So nothing here reads a
reply: every number is taken from the v3 session folder the profile left behind —
the task checkpoint (tasks.json), the per-node journals (tasks/*.jsonl, whose
NAME carries the minute the node opened and whose entries carry their own
timestamps) and the conversation transcript (transcript.jsonl, where propose_task
and divide_work are recorded as tool calls with their arguments).

Usage:
    road.py <session-dir> [--snapshots <dir>] [--timeline <out.json>]

Prints one JSON object of the CSV columns on stdout. --timeline writes the fuller
per-worker record the parallelism autopsy wants.
"""
import glob, json, os, sys, datetime, re

# The three refusals divide_work can answer with, and the receipt it answers on
# success. Spelled here as the prefixes internal/session/task_divide.go writes
# (divisionTooNarrow, divisionNoLane, divisionNotAsWritten, divisionDone); the
# tails carry the counts and are matched loosely so a reworded tail does not
# silently turn a refusal into a no-evidence row.
REFUSAL = "not split:"
GATE_EVIDENCE = "and work is only split at"
GATE_LANE_ONE = "runs one task at a time"
GATE_LANE_BUSY = "every lane is busy"
GATE_REVIEW = "the parts are one job rather than several"
SPLIT_OK = re.compile(r"split into (\d+) parts:")

JOURNAL_NAME = re.compile(r"^(\d{8}-\d{6})_(\d+)(.*)\.jsonl$")


def parse_ts(raw):
    if not raw:
        return None
    raw = str(raw).strip()
    try:
        return datetime.datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except Exception:
        return None


def read_jsonl(path):
    out = []
    try:
        with open(path, errors="replace") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    out.append(json.loads(line))
                except Exception:
                    continue
    except OSError:
        pass
    return out


def tool_calls(entries):
    """Every tool call in a jsonl transcript, as (name, arguments-dict, entry)."""
    for entry in entries:
        for call in entry.get("toolCalls") or []:
            name = call.get("name") or (call.get("function") or {}).get("name") or ""
            raw = call.get("arguments")
            if raw is None:
                raw = (call.get("function") or {}).get("arguments")
            args = {}
            if isinstance(raw, dict):
                args = raw
            elif isinstance(raw, str):
                try:
                    args = json.loads(raw)
                except Exception:
                    args = {}
            yield name, args, entry


def tool_results(entries):
    """Tool result bodies — where a refusal or a receipt is actually written."""
    for entry in entries:
        if entry.get("role") == "tool" or entry.get("toolCallId"):
            body = entry.get("content")
            if isinstance(body, list):
                body = " ".join(
                    part.get("text", "") for part in body if isinstance(part, dict))
            if isinstance(body, str):
                yield body, entry


def load_tasks(session):
    path = os.path.join(session, "tasks.json")
    try:
        with open(path) as fh:
            doc = json.load(fh)
    except (OSError, ValueError):
        return []
    return doc.get("nodes") or []


def journals(session):
    """Per-node journals, keyed by node id: (start-from-name, entries, path)."""
    found = {}
    for path in sorted(glob.glob(os.path.join(session, "tasks", "*.jsonl"))):
        match = JOURNAL_NAME.match(os.path.basename(path))
        if not match:
            continue
        stamp, node_id, suffix = match.groups()
        try:
            opened = datetime.datetime.strptime(stamp, "%Y%m%d-%H%M%S")
        except ValueError:
            opened = None
        found.setdefault(int(node_id), []).append((opened, path, suffix))
    return found


def transcripts(session):
    """The conversation's own transcript entries."""
    return read_jsonl(os.path.join(session, "transcript.jsonl"))


def cost(session):
    """Every dollar the session's own usage accounting recorded.

    The conversation's usage records live in transcript.jsonl (one per model, the
    ones made beside a turn marked aux). A task's bill is folded onto its
    checkpoint record as costUsd — [Agent.foldTaskUsage] — and is NOT repeated in
    the conversation's transcript, so both are summed and neither is derived from
    the other.
    """
    total, seen = 0.0, False
    for entry in transcripts(session):
        used = entry.get("usage")
        if used and used.get("costUsd") is not None:
            total += float(used["costUsd"])
            seen = True
    for record in load_tasks(session):
        if record.get("costUsd"):
            total += float(record["costUsd"])
            seen = True
    return (round(total, 6), "self-reported") if seen else ("", "no-usage-record")


def divisions(session):
    """Every divide_work call this session made, and what came back.

    A call is read from whichever journal made it — the parent worker's, never
    the conversation's, because a worker is the only thing holding divide_work.
    The answer is the tool result that follows it in the same file.
    """
    out = []
    for node_id, files in sorted(journals(session).items()):
        for _, path, _ in files:
            entries = read_jsonl(path)
            calls = [(name, args, entry) for name, args, entry in tool_calls(entries)
                     if name == "divide_work"]
            results = [(body, entry) for body, entry in tool_results(entries)
                       if body.startswith(REFUSAL) or SPLIT_OK.search(body or "")]
            for index, (_, args, entry) in enumerate(calls):
                answer = results[index][0] if index < len(results) else ""
                refused, gate = "", ""
                if answer.startswith(REFUSAL):
                    refused = answer.split("\n")[0][:200]
                    if GATE_EVIDENCE in answer:
                        gate = "evidence"
                    elif GATE_LANE_ONE in answer or GATE_LANE_BUSY in answer:
                        gate = "capacity"
                    elif GATE_REVIEW in answer:
                        gate = "review"
                    else:
                        gate = "unknown"
                admitted = 0
                match = SPLIT_OK.search(answer or "")
                if match:
                    admitted = int(match.group(1))
                evidence = args.get("evidence") or ""
                parts = args.get("parts") or []
                out.append({
                    "by_node": node_id,
                    "at": entry.get("timestamp", ""),
                    "evidence_chars": len(evidence),
                    "parts_requested": len(parts) if isinstance(parts, list) else 0,
                    "parts_admitted": admitted,
                    "refused": refused,
                    "gate": gate,
                    "journal": os.path.basename(path),
                })
    return out


def proposals(session):
    """propose_task calls from the conversation, with the `wide` the model set.

    `wide` is the one durable trace of the road being ARMED: armDivision's answer
    is a reading and is never written down (task_store.go says so), so the honest
    record is what the model asked for plus whether a divide_work call ever
    happened at all.
    """
    out = []
    for name, args, entry in tool_calls(transcripts(session)):
        if name != "propose_task":
            continue
        out.append({
            "at": entry.get("timestamp", ""),
            "title": (args.get("title") or "")[:120],
            "wide": bool(args.get("wide")),
        })
    return out


def worker_timeline(session):
    """One row per worker: when its journal opened, its first and last entry.

    DERIVED, NOT POLLED. A node journal's NAME carries the second it was minted
    (taskJournalPath) and every entry inside carries its own timestamp, so the
    interval a worker was alive is a fact on disk rather than a sampling of it.
    """
    records = {int(r["id"]): r for r in load_tasks(session) if r.get("id") is not None}
    rows = []
    for node_id, files in sorted(journals(session).items()):
        opened, first, last, paths = None, None, None, []
        for stamp, path, _ in files:
            paths.append(os.path.basename(path))
            if stamp and (opened is None or stamp < opened):
                opened = stamp
            entries = read_jsonl(path)
            stamps = [parse_ts(e.get("timestamp")) for e in entries]
            stamps = [s for s in stamps if s]
            if stamps:
                low, high = min(stamps), max(stamps)
                if first is None or low < first:
                    first = low
                if last is None or high > last:
                    last = high
        record = records.get(node_id, {})
        rows.append({
            "id": node_id,
            "title": record.get("title", ""),
            "parent": record.get("parent", 0),
            "depth": record.get("depth", 0),
            "state": record.get("state", ""),
            "model": record.get("model", ""),
            "cost_usd": record.get("costUsd", 0),
            "elapsed_ms": record.get("elapsed_ms", 0),
            "changed": record.get("changed", []),
            "worktree": record.get("worktree", ""),
            "journal_opened": opened.isoformat() if opened else "",
            "first_entry": first.isoformat() if first else "",
            "last_entry": last.isoformat() if last else "",
            "journals": paths,
        })
    # A node admitted and never started has no journal at all, and leaving it out
    # would hide exactly the case the idle-gap question is about.
    for node_id, record in sorted(records.items()):
        if node_id in journals(session):
            continue
        rows.append({
            "id": node_id, "title": record.get("title", ""),
            "parent": record.get("parent", 0), "depth": record.get("depth", 0),
            "state": record.get("state", ""), "model": record.get("model", ""),
            "cost_usd": record.get("costUsd", 0), "elapsed_ms": record.get("elapsed_ms", 0),
            "changed": record.get("changed", []), "worktree": record.get("worktree", ""),
            "journal_opened": "", "first_entry": "", "last_entry": "",
            "journals": [], "never_started": True,
        })
    return rows


def concurrency(rows):
    """The concurrency series, swept over the workers' own intervals.

    Every interval endpoint is an event; between two consecutive events the count
    of live workers is constant, so the series is exact rather than sampled and
    `peak` is the real maximum and not the largest sample.
    """
    spans = []
    for row in rows:
        start = parse_ts(row["first_entry"]) or parse_ts(row["journal_opened"])
        end = parse_ts(row["last_entry"])
        if start and end and end >= start:
            spans.append((start, end, row["id"]))
    if not spans:
        return {"peak": 0, "series": [], "spans": 0}
    marks = sorted({point for span in spans for point in span[:2]})
    series, peak = [], 0
    for mark in marks:
        alive = [span[2] for span in spans if span[0] <= mark <= span[1]]
        peak = max(peak, len(alive))
        series.append({"at": mark.isoformat(), "alive": len(alive), "ids": alive})
    return {"peak": peak, "series": series, "spans": len(spans)}


def idle_gaps(rows, series):
    """Two kinds of nothing-happening, which are two different failures.

    `waiting` is a part that was admitted and had not opened a journal — work
    that existed and had no hand on it. `quiet` is a stretch with no worker alive
    at all while the conversation was still going, which is the shape of a
    division that finished serially.
    """
    gaps = []
    for row in rows:
        if row.get("never_started") and row.get("state") in ("queued", "pending"):
            gaps.append({"kind": "waiting", "id": row["id"], "title": row["title"]})
    last = None
    for point in series:
        if point["alive"] == 0 and last is not None:
            gaps.append({"kind": "quiet", "from": last, "to": point["at"]})
        last = point["at"] if point["alive"] == 0 else None
    return gaps


def road_of(session):
    tasks = load_tasks(session)
    if not tasks:
        return "words", 0
    children = [r for r in tasks if r.get("parent")]
    if children:
        return "task+divided", len(children)
    return "task", 0


def main():
    session = sys.argv[1]
    timeline_out = ""
    if "--timeline" in sys.argv:
        timeline_out = sys.argv[sys.argv.index("--timeline") + 1]

    road, parts = road_of(session)
    divides = divisions(session)
    props = proposals(session)
    rows = worker_timeline(session)
    conc = concurrency(rows)
    spend, source = cost(session)

    # ARMED. The reading itself is never written down, so this reports the
    # evidence and not a guess: a divide_work call PROVES the verb was on the
    # belt; a propose_task carrying wide=true proves the road was asked for. With
    # neither, the honest answer is that nothing on disk says.
    if divides:
        armed = "yes(divide_work called)"
    elif any(p["wide"] for p in props):
        armed = "yes(wide requested)"
    elif props:
        armed = "not-requested"
    else:
        armed = "n/a(no task)"

    refused = ";".join(f"{d['gate']}" for d in divides if d["refused"]) or ""

    columns = {
        "road": road,
        "armed": armed,
        "parts": parts,
        "peak_workers": conc["peak"],
        "refused": refused,
        "cost_usd": spend,
        "cost_source": source,
        "tasks_admitted": len(load_tasks(session)),
        "divide_calls": len(divides),
    }
    if timeline_out:
        with open(timeline_out, "w") as fh:
            json.dump({
                "session": session,
                "columns": columns,
                "workers": rows,
                "divisions": divides,
                "proposals": props,
                "concurrency": conc,
                "idle_gaps": idle_gaps(rows, conc["series"]),
                "note": "every field is derived from the session folder: tasks.json, "
                        "tasks/*.jsonl (name carries the minute the node opened, entries "
                        "carry their own timestamps) and transcript.jsonl. Nothing here "
                        "is polled and nothing is read from the reply.",
            }, fh, indent=2)
    print(json.dumps(columns))


if __name__ == "__main__":
    main()
