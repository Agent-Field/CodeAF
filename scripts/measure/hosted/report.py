#!/usr/bin/env python3
"""The tables of the hosted-relay validation, from the passes' results.json files.

  report.py <evidence dir containing pass1/ pass2/ ...> [card-test.json]

Prints Markdown: the step table (every pass, min and max, the target beside them and a verdict), the save
latency per tool call, the push samples and the integrity checks. The prose around the tables is written
by hand in docs/BENCH-MOVE.md; this file only does the arithmetic, so no number is typed twice.
"""
import json
import os
import statistics
import sys


def load(root):
    out = []
    n = 1
    while os.path.exists(os.path.join(root, f"pass{n}", "results.json")):
        out.append(json.load(open(os.path.join(root, f"pass{n}", "results.json"))))
        n += 1
    return out


def fmt_s(x):
    return f"{x:.1f} s" if x >= 10 else f"{x:.2f} s"


def row(name, vals, target, ok, fmt=fmt_s, note=""):
    cells = " | ".join(fmt(v) for v in vals)
    verdict = "-" if target == "n/a" else "met" if ok(max(vals)) else "MISSED"
    return f"| {name} | {cells} | {fmt(min(vals))} to {fmt(max(vals))} | {target} | {verdict} | {note} |"


def steps(passes):
    ps = [f"pass {p['pass']}" for p in passes]
    head = f"| Step | {' | '.join(ps)} | min to max | target | verdict | what is timed |\n|---|{'---|' * len(ps)}---|---|---|---|"
    f = lambda k: [p[k] for p in passes]
    seal = lambda who: [statistics.median(c["seal_ms"] for c in p[who]["calls"][1:] if c["seal_ms"] is not None) / 1000 for p in passes]
    seal_max = lambda who: [max(c["seal_ms"] for c in p[who]["calls"][1:]) / 1000 for p in passes]
    dur = lambda who: [statistics.median(c["durable_ms"] for c in p[who]["calls"] if c["durable_ms"] and c["durable_ms"] < 20000) / 1000 for p in passes]
    push = lambda k: [statistics.median(p[k]) / 1000 for p in passes]
    rows = [
        row("Pair (scripted)", f("pair_s"), "n/a", lambda v: True, note="`codeaf pair` shows a code, the other machine types it, `y` is typed; staging keeps a mailbox 4 s"),
        row("First upload durable, after the turn ended", f("first_upload_durable_s"), "n/a", lambda v: True, note="202 MB tree, 226 frame puts"),
        row("Save: seal after a tool call, A (median)", seal("a_latency"), "~0.15 s", lambda v: v <= 0.16, note="tool call ended to turn sealed"),
        row("Save: seal after a tool call, A (worst)", seal_max("a_latency"), "~0.15 s", lambda v: v <= 0.16),
        row("Save: seal after a tool call, B (median)", seal("b_latency"), "~0.15 s", lambda v: v <= 0.16),
        row("Save: seal to durable at the relay, A (median)", dur("a_latency"), "n/a", lambda v: True, note="calls after the first upload; relay clock within about 0.2 s"),
        row("Save: seal to durable at the relay, B (median)", dur("b_latency"), "n/a", lambda v: True),
        row("Push: durable at the relay to a new row on B's home (median of 5)", push("push_durable_to_home_ms"), "~0.1 s", lambda v: v <= 0.12, note="new chat on A; screen polled every 10 ms on B"),
        row("Push: seal on A to the row on B's home (median of 5)", push("push_seal_to_home_ms"), "~0.1 s", lambda v: v <= 0.12, note="includes the upload"),
        row("Cold take, M, B: confirm to chat open", f("cold_take_s"), "<10 s", lambda v: v < 10, note="home screen, `continue here`, 200.7 MB, 209 GETs"),
        row("Warm take, M, B, 1 file changed", [p["warm_take_ms"] / 1000 for p in passes], "<5 s", lambda v: v < 5, note="same take as the home screen, `vdemo-take`"),
        row("Take back, M, A, 1 file changed on the other side", [p["take_back_ms"] / 1000 for p in passes], "<5 s", lambda v: v < 5, note="same take as the home screen, `vdemo-take`"),
    ]
    return "\n".join([head] + rows)


def latency(passes):
    lines = ["| Pass | Machine | Seal after each tool call (ms) | Durable at the relay (ms) |", "|---|---|---|---|"]
    for p in passes:
        for who, name in (("a_latency", "A"), ("b_latency", "B")):
            c = p[who]["calls"]
            lines.append(f"| {p['pass']} | {name} | {', '.join(str(x['seal_ms']) for x in c)} | {', '.join(str(x['durable_ms']) for x in c)} |")
    return "\n".join(lines)


def push(passes):
    lines = ["| Pass | Durable to home (ms), five new chats | Seal to home (ms) |", "|---|---|---|"]
    for p in passes:
        lines.append(f"| {p['pass']} | {', '.join(map(str, p['push_durable_to_home_ms']))} | {', '.join(map(str, p['push_seal_to_home_ms']))} |")
    return "\n".join(lines)


def integrity(passes):
    lines = ["| Check | " + " | ".join(f"pass {p['pass']}" for p in passes) + " |", "|---|" + "---|" * len(passes)]
    yes = lambda b: "PASS" if b else "FAIL"
    checks = [
        ("`diff -r --no-dereference` A tree against B tree (`.git`, `.furrow`, `node_modules` excluded): no output", lambda i: yes(i["diff_r_exit"] == 0 and i["diff_r_lines"] == 0)),
        ("sha256 and mode of every file equal (`treehash.py`)", lambda i: yes(i["treehash_equal_ignoring_node_modules"] and i["modes_equal"])),
        ("`.env` 0600, `svc/api/.env` 0640, `web/client/app/.env.local` 0600, `run.sh` 0755 on B, bytes equal", lambda i: yes(all(s["a"] == s["b"] and s["bytes_equal"] for s in i["secrets"].values())) + " (" + "/".join(s["b"] for s in i["secrets"].values()) + ")"),
        ("withheld `node_modules` absent on B after the take", lambda i: yes(i["node_modules_on_b_before_rebuild"] == "no")),
        ("`git status` on B differs from A's only by the withheld folder", lambda i: yes(i["git_status_only_on_a_before_rebuild"] == ["?? node_modules/"])),
        ("`git log` and `git diff` hash equal", lambda i: yes(i["git_log_and_diff_equal"])),
        ("no `.codeaf/` in `git status` on either machine", lambda i: yes(not i["codeaf_in_git_status_b"])),
        ("`npm ci` on B rebuilds it (seconds in the row below) and `git status` then equals A's", lambda i: yes(i["git_equal_after_rebuild"])),
        ("rebuilt `node_modules` has the same files and hashes as A's", lambda i: yes(i["node_modules_rebuilt_equal"] and not i["node_modules_file_diff"])),
        ("rebuild time", lambda i: f"{i['rebuild_s']} s"),
    ]
    for name, fn in checks:
        lines.append(f"| {name} | " + " | ".join(fn(p["integrity"]) for p in passes) + " |")
    return "\n".join(lines)


if __name__ == "__main__":
    passes = load(sys.argv[1])
    for title, body in (("Steps", steps(passes)), ("Save latency per tool call", latency(passes)), ("Push samples", push(passes)), ("Integrity", integrity(passes))):
        print(f"### {title}\n\n{body}\n")
    if len(sys.argv) > 2:
        card = json.load(open(sys.argv[2]))
        shown = sum(1 for c in card if c["card"])
        print(f"### Resume card frequency\n\n{shown} of {len(card)} cold takes of small chats showed the card; facts present in {sum(1 for c in card if c['withheld'] or c['running'])}.\n")
