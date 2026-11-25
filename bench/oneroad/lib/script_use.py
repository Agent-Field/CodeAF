#!/usr/bin/env python3
"""script_use.py — how much a codeaf cell's task workers used the script tool,
and what they spent.

Usage: script_use.py <cell>

Prints one JSON object:

  steps, script_steps, script_calls
      The tool calls the workers made, how many were script calls, and how many
      calls those scripts made in turn. Two records hold them, and they never
      describe the same worker:
      - a plan run's trajectory (<store>/tasks/<id>/trajectory.jsonl,
        internal/run/trajectory.go): one line per step, a script's calls on it;
      - a chat task's transcript (.cell/tasks/*.jsonl): one assistant tool call
        per step, and a script's own result line, "script <ending> in <took>
        after N calls" (internal/session's tool_script.go), for its calls.
  chat_steps, chat_script_steps, chat_script_calls
      The same three readings for the conversation itself, from its own
      transcript (.cell/transcript.jsonl): the chat-side arm's measure, where
      CODEAF_CHAT_SCRIPT puts the tool on the conversation's belt.
  worker_usd, worker_tokens_in, worker_tokens_out, worker_tokens_cache,
  worker_model_calls
      Every model call a task worker made: the rows of the profile's usage
      ledger (v3/usage.jsonl) on the worker seat or naming a task, once per
      row id. A chat task's rows name their task; a plan run's workers' rows
      carry only the seat and their run. The ledger
      records every call codeaf makes, a chat task's and a plan run's alike;
      the plan store's spend rows hold only a plan run's, so a cell whose
      tasks chat started read near zero there however much its workers spent.
      The conversation's own calls are road.py's tokens_* columns.

The cell's store copy is read when cell.sh has made one ($CELL/store, taken so
nothing reads a file a process still holds), otherwise the live profile; never
both, which would count everything twice. A cell that ran no task prints
zeros, the honest reading for a turn that ran no task.

The script arms differ from the new arms by CODEAF_TASK_SCRIPT=on alone, so the
script counts say whether the switch changed the work at all before the worker
spend is read as its effect.
"""
import json, os, re, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from road import read_jsonl  # noqa: E402

SCRIPT_LINE = re.compile(r"^script \w+ in \S+ after (\d+) calls?\b")


def roots(cell):
    store = os.path.join(cell, "store")
    first = store if os.path.isdir(store) else os.path.join(cell, "profile")
    return [first, os.path.join(cell, "home")]


def text_of(content):
    """A transcript message's content as text: a string, or its text parts."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "".join(p.get("text", "") for p in content if isinstance(p, dict))
    return ""


def trajectory(path, out):
    for step in read_jsonl(path):
        if not isinstance(step, dict) or step.get("kind") != "step":
            continue
        out["steps"] += 1
        if step.get("tool") == "script":
            out["script_steps"] += 1
            out["script_calls"] += len(step.get("calls") or [])


def transcript(path, out, prefix=""):
    scripts = set()
    for line in read_jsonl(path):
        if not isinstance(line, dict) or line.get("type") != "message":
            continue
        if line.get("role") == "assistant":
            for call in line.get("toolCalls") or []:
                out[prefix + "steps"] += 1
                if (call.get("function") or {}).get("name") == "script":
                    out[prefix + "script_steps"] += 1
                    scripts.add(call.get("id"))
        elif line.get("role") == "tool" and line.get("toolCallId") in scripts:
            ran = SCRIPT_LINE.match(text_of(line.get("content")))
            if ran:
                out[prefix + "script_calls"] += int(ran.group(1))


def usage(path, out):
    rows = {}
    for row in read_jsonl(path):
        if isinstance(row, dict) and (row.get("seat") == "worker" or row.get("task")):
            rows[row.get("id") or len(rows)] = row
    for row in rows.values():
        out["worker_usd"] += float(row.get("usd") or 0)
        out["worker_tokens_in"] += int(row.get("in") or 0)
        out["worker_tokens_out"] += int(row.get("out") or 0)
        out["worker_tokens_cache"] += int(row.get("cache_read") or 0)
        out["worker_model_calls"] += int(row.get("calls") or 1)


def count(cell):
    out = {"steps": 0, "script_steps": 0, "script_calls": 0,
           "worker_usd": 0.0, "worker_tokens_in": 0, "worker_tokens_out": 0,
           "worker_tokens_cache": 0, "worker_model_calls": 0,
           "chat_steps": 0, "chat_script_steps": 0, "chat_script_calls": 0}
    for root in roots(cell):
        for base, _, names in os.walk(root):
            for name in names:
                path = os.path.join(base, name)
                if name == "trajectory.jsonl":
                    trajectory(path, out)
                elif name.endswith(".jsonl") and os.path.basename(base) == "tasks" \
                        and os.path.basename(os.path.dirname(base)) == ".cell":
                    transcript(path, out)
                elif name == "transcript.jsonl" and os.path.basename(base) == ".cell":
                    transcript(path, out, "chat_")
                elif name == "usage.jsonl":
                    usage(path, out)
    out["worker_usd"] = round(out["worker_usd"], 6)
    return out


if __name__ == "__main__":
    print(json.dumps(count(sys.argv[1])))
