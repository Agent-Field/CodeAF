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


def escalations(session):
    """Did a task leave an answer that had ALREADY BEGUN the work — mid-turn?

    The line a person reads ("this one wants more hands · handing it over with
    everything found so far") is a transient UI event: internal/session/task.go
    sends it on the hub immediately before the task proposal and NEVER writes it
    down, and the v3 surface is an alt-screen app whose pane keeps no scrollback.
    So grepping for the sentence can only ever catch it by luck, and a benchmark
    that reported "no" on a missed frame would be reporting the sampling and not
    the system.

    What IS durable is the condition the note is emitted under, and it is exactly
    [Agent.alreadyWorking]: a tool result stands in the transcript below the last
    thing the person said. So the same question is asked of the same record —
    was there a finished tool call between the person's message and the
    propose_task — and the answer is derived rather than observed.
    """
    out = []
    worked_since_user = False
    for entry in transcripts(session):
        role = entry.get("role")
        names = [name for name, _, _ in tool_calls([entry])]
        if "propose_task" in names:
            out.append({
                "at": entry.get("timestamp", ""),
                "mid_turn": worked_since_user,
            })
        if role == "user":
            worked_since_user = False
        elif role == "tool" or entry.get("toolCallId"):
            worked_since_user = True
    return out


# The told-after line the pre-turn judge ends the turn on (route_judge.go's
# launchRouteTask). Unlike the mid-turn escalation note, this one IS durable:
# routeAhead records it as the turn's answer, so it stands in the transcript as
# the assistant's reply and can be read back rather than caught on a frame.
PRE_TURN_LINE = re.compile(r"this looked like work, so task (\d+) (started|queued)")

# Wave 1d's three mechanisms, each by its own exact bytes (internal/session's
# route_judge.go and checkpoint.go). They are kept as separate constants rather
# than one alternation because WHICH of them fired is the measurement: they are
# three different answers to the same failure and a row that merged them would
# say only "something happened".
# WAVE 1E RENAMED MOST OF THESE, AND THE RENAME COST A WHOLE WAVE'S READING.
# The 1d detectors matched the exact sentences of the 1d binary; c0e4f5a7 changed
# the raced screen to TRIAGE (it no longer converts with a note of its own) and
# rewrote the checkpoint ask, so every 1e cell read `route=none, marks=0` over
# mechanisms that may well have run. The lesson is in the matching now: the mark
# is counted by the STABLE `[checkpoint]` PREFIX the harness itself keys on
# (checkpoint.go's isCheckpointNote), not by the sentence after it, and every
# conversion note is listed so a future rename shows up as a new value rather
# than as silence.
RACE_NOTE = "this reads like work · moving it to a task that is watched and can split"   # 1d only
SPLIT_NOTE = "this has parts · handing it to a task that can take them side by side"      # 1e
CEILING_NOTE = "this is running long · moving it to a task that is watched and can split"
NOTHING_LEFT = "NOTHING LEFT TO DO"
# An auxiliary call reading this many input tokens is reading the turn itself,
# not writing a title (~0.4k) or running the reflex pass (~1.6k).
MARK_READER_FLOOR = 10_000
# The prefix, not the sentence: this is what the harness matches on, so it
# survives a rewording of the question that follows it.
CHECKPOINT_OPEN = "[checkpoint]"
HANDOFF_OPEN = "[handing over]"


def mark_lines(session):
    """Wave 1f journals its own decisions, so they are READ rather than inferred.

    Every mark writes a `mark` line (n, rounds, model, costUsd, sketch, decision)
    and the last one writes a `ceiling` line (rounds, decision, taskId). Until 1f
    the sidecar was invisible — it is an auxiliary call, never a message — and
    wave 1e had to be reconstructed from the SHAPE of untagged usage records.
    That heuristic is now retired: where the journal states the decision, the
    journal is the source, and the shape-based reader below stays only as the
    fallback for the older waves whose stores predate these lines.
    """
    marks, ceiling = [], None
    for entry in transcripts(session):
        kind = entry.get("type")
        if kind == "mark" and entry.get("mark"):
            m = entry["mark"]
            marks.append({"n": m.get("n"), "rounds": m.get("rounds"),
                          "decision": m.get("decision"), "model": m.get("model"),
                          "cost_usd": m.get("costUsd"),
                          "sketch": (m.get("sketch") or "").split("\n")[0][:160],
                          "duration_ms": m.get("durationMs")})
        elif kind == "ceiling" and entry.get("ceiling"):
            c = entry["ceiling"]
            ceiling = {"rounds": c.get("rounds"), "decision": c.get("decision"),
                       "task_id": c.get("taskId")}
    return marks, ceiling


def forks(session):
    """Did any hand actually run.

    Two independent signals, because they answer different halves: a `fork` tool
    call is the model ASKING for hands, and the notice ("<n> hands on it · back
    when they are done", fork.go) is the harness saying it gave them. A row with
    the call and no notice is a fork that was refused.
    """
    asked = 0
    for name, _, _ in tool_calls(transcripts(session)):
        if name == "fork":
            asked += 1
    hands = 0
    for entry in transcripts(session):
        body = entry.get("content")
        if isinstance(body, str) and "hands on it · back when they are done" in body:
            hands += 1
    return {"fork_calls": asked, "fork_notices": hands}


def mark_reader(session):
    """Did the mark-reader sidecar run, and how often.

    IT IS INVISIBLE TO EVERY OTHER COLUMN, which is why it needs its own. The
    sidecar is not injected into the conversation — it is an auxiliary call that
    reads the running turn and answers whether what is left has parts
    (checkpoint.go's readMark, roles.RoleMarkReader on the mastermind tier). So
    it never appears in the transcript as a message, and a `route=none` row says
    only "nothing converted", never "nothing looked".

    It is counted by its SHAPE rather than by a role tag, because the usage
    records for it carry no role: it is the auxiliary call that reads the WHOLE
    running turn, so its input is tens of thousands of tokens where the reflex
    pass is ~1.6k and the title ~0.4k. The floor is deliberately well above both.
    """
    calls = []
    for entry in transcripts(session):
        used = entry.get("usage")
        if not used or not used.get("aux") or used.get("role"):
            continue
        if int(used.get("input") or 0) >= MARK_READER_FLOOR:
            calls.append({"model": used.get("model"),
                          "input": used.get("input"), "output": used.get("output")})
    return calls


def call_lines(session):
    """The per-request `call` lines wave 1e added, summed and grouped.

    Each is one request: model, ENDPOINT, tokens and the billed costUsd. Two
    things come out of it that no earlier wave could see. The bill is now a sum
    of requests rather than one rolled-up usage record, so `cost_usd_calls` can
    be checked against the usage total instead of trusted. And the endpoint is
    named, which is the only way to see what the new latency-under-a-price-
    ceiling routing actually DID — a routing default is a claim about which
    endpoint served the turn, and this is the evidence for it.
    """
    total, endpoints, tok, n = 0.0, {}, {"input": 0, "output": 0, "cache_read": 0}, 0
    # WAVE 1F MADE THE SUM OF CALL LINES THE BILL. Every auxiliary call now
    # writes one with its `role`, so the gap wave 1e measured — $0.62 of
    # mastermind sidecar that the call lines did not carry — is closed, and the
    # spend can be attributed to the role that caused it rather than to a total.
    by_role = {}
    for entry in transcripts(session):
        if entry.get("type") != "call":
            continue
        call = entry.get("call") or {}
        n += 1
        total += float(call.get("costUsd") or 0)
        name = call.get("endpoint") or "(unnamed)"
        endpoints[name] = endpoints.get(name, 0) + 1
        tok["input"] += int(call.get("input") or 0)
        tok["output"] += int(call.get("output") or 0)
        tok["cache_read"] += int(call.get("cacheRead") or 0)
        role = call.get("role") or "chat"
        by_role[role] = round(by_role.get(role, 0.0) + float(call.get("costUsd") or 0), 6)
    return {"calls": n, "cost_usd_calls": round(total, 6),
            "endpoints": endpoints, "tokens": tok,
            "endpoint_mix": ";".join(f"{k}={v}" for k, v in
                                     sorted(endpoints.items(), key=lambda kv: -kv[1])),
            "by_role": by_role,
            "cost_by_role": ";".join(f"{k}=${v:.4f}" for k, v in
                                     sorted(by_role.items(), key=lambda kv: -kv[1]))}


def mechanisms(session):
    """Which of wave 1d's three doors fired, and how far the turn got.

    The order below is the order the harness itself would reach them, and it is
    also the order of decreasing "the model was given a chance": the raced screen
    converts before the model answers, a checkpoint converts a turn the model was
    already grinding, and the ceiling is the harness giving up on asking. A
    propose_task with no checkpoint before it is the model reaching for the verb
    on its own, which is the thing every earlier wave failed to produce.
    """
    entries = transcripts(session)
    marks = 0
    race = ceiling = handoff = split = nothing_left = False
    propose_after_marks = None
    for entry in entries:
        body = entry.get("content")
        body = body if isinstance(body, str) else ""
        role = entry.get("role")
        if role == "user" and body.startswith(CHECKPOINT_OPEN):
            marks += 1
        if role == "user" and body.startswith(HANDOFF_OPEN):
            handoff = True
        if role == "assistant":
            if RACE_NOTE in body:
                race = True
            if SPLIT_NOTE in body:
                split = True
            if CEILING_NOTE in body:
                ceiling = True
            if NOTHING_LEFT in body:
                nothing_left = True
        for name, _, _ in tool_calls([entry]):
            if name == "propose_task" and propose_after_marks is None:
                propose_after_marks = marks
    return {
        "checkpoint_marks": marks,
        "race_note": race,
        "split_note": split,
        "nothing_left": nothing_left,
        "ceiling_note": ceiling,
        "handoff_ask": handoff,
        "propose_after_marks": propose_after_marks,
    }


def chat_work(session):
    """What the CONVERSATION itself did with its own hands, and what it said.

    Two numbers and a sentence. `tool_calls` counts the grind the chat did
    inline — read, edit, bash — which is the thing the pre-turn judge exists to
    move off the conversation and into a task; a handover that fires and then
    grinds anyway has not actually handed over. `pre_turn_line` is the told-after
    notice if the turn ended on it.
    """
    entries = transcripts(session)
    grind = 0
    for name, _, _ in tool_calls(entries):
        if name != "propose_task":
            grind += 1
    line, task_id = "", 0
    for entry in entries:
        body = entry.get("content")
        if entry.get("role") == "assistant" and isinstance(body, str):
            found = PRE_TURN_LINE.search(body)
            if found:
                line = found.group(0)
                task_id = int(found.group(1))
    return grind, line, task_id


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
    escs = escalations(session)
    grind, pre_line, pre_id = chat_work(session)
    mech = mechanisms(session)
    calls = call_lines(session)
    marks_journal, ceiling_journal = mark_lines(session)
    fork = forks(session)
    marks_read = mark_reader(session)
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

    # THE ESCALATION COLUMN, and the three answers are genuinely different facts.
    # A task proposed mid-turn is the handoff the teaching is for. A task
    # proposed on the turn's first step is an ordinary proposal that never needed
    # a handoff. No task at all is the words road, where the question does not
    # arise.
    if any(e["mid_turn"] for e in escs):
        escalated = "yes(mid-turn)"
    elif escs:
        escalated = "no(proposed at turn start)"
    else:
        escalated = "n/a(no task)"

    # WHICH DOOR THE WORK CAME THROUGH, and there are now three of them. The
    # pre-turn judge admits its node with graph.admit directly and makes no
    # propose_task call at all (route_judge.go's launchRouteTask), so "a task
    # exists and nothing proposed it" is that door's signature — and the
    # told-after line the turn ended on confirms it in the model's own reply.
    tasks_exist = bool(load_tasks(session))
    split_mark = next((m for m in marks_journal if m["decision"] == "split"), None)
    if split_mark:
        route = "mark-%s-split" % split_mark["rounds"]
    elif ceiling_journal and ceiling_journal.get("decision") not in (None, "", "drop"):
        route = "ceiling(%s)" % ceiling_journal["decision"]
    elif mech["split_note"]:
        route = "mark-%d-split" % max(mech["checkpoint_marks"], 1)
    elif mech["race_note"]:
        route = "pre-race-triage"
    elif mech["ceiling_note"] or mech["handoff_ask"]:
        route = "ceiling"
    elif escs and mech["propose_after_marks"]:
        route = "mark-%d-split" % mech["propose_after_marks"]
    elif escs:
        # The model reached for the verb with no harness question in front of it.
        route = "model-own-propose"
    elif pre_line and tasks_exist:
        route = "pre-turn"
    elif tasks_exist:
        route = "task(no door identified)"
    else:
        route = "none"

    # THE BILL. From wave 1f every auxiliary call writes a `call` line with its
    # role, so their sum IS the bill and is preferred. Older stores have no call
    # lines and fall back to the usage records, which is what every earlier wave
    # was measured on — both figures stay in the row so the two can be compared
    # rather than silently swapped.
    spend_calls = calls["cost_usd_calls"]
    if calls["calls"] > 0 and spend_calls > 0:
        billed, billed_source = spend_calls, "call-lines"
    else:
        billed, billed_source = spend, source

    columns = {
        "road": road,
        "armed": armed,
        "parts": parts,
        "peak_workers": conc["peak"],
        "refused": refused,
        "cost_usd": billed,
        "cost_source": billed_source,
        "cost_usd_usage": spend,
        "escalated": escalated,
        "route": route,
        "chat_tool_calls": grind,
        "pre_turn_line": pre_line,
        "checkpoint_marks": mech["checkpoint_marks"],
        "calls": calls["calls"],
        "cost_usd_calls": calls["cost_usd_calls"],
        "endpoint_mix": calls["endpoint_mix"],
        "marks": ",".join(f"{m['rounds']}:{m['decision']}" for m in marks_journal),
        "mark_decisions": len(marks_journal),
        "ceiling_decision": (ceiling_journal or {}).get("decision", ""),
        "forks": fork["fork_notices"],
        "fork_calls": fork["fork_calls"],
        "cost_by_role": calls["cost_by_role"],
        "mark_reader_calls": len(marks_read),
        "mark_reader_model": marks_read[0]["model"] if marks_read else "",
        "propose_calls": len(escs),
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
                "escalations": escs,
                "mechanisms": mech,
                "call_lines": calls,
                "mark_reader": marks_read,
                "mark_lines": marks_journal,
                "ceiling_line": ceiling_journal,
                "forks": fork,
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
