#!/usr/bin/env python3
"""What a move carries: one snapshot of everything a chat owns on one home, and a comparison of two snapshots.

  check.py snap <home> <cell-id> <workspace-or-"-"> <out.json>     read one home (run on that machine)
  check.py compare <a.json> <b.json>                               PASS/FAIL per check, JSON on stdout
  check.py repeats <a.json> <b.json>                               tool calls B repeated from A, JSON on stdout

A snapshot is plain data (text lines, hashes, small tables), so the two homes may be read on different
machines and compared on any machine. Paths that name a home or a workspace are rewritten to <HOME> and <WS>
before anything is compared, because those differ between machines by design.

The chat owns four places, and each is read on its own so that a gap names where it is:
  the cell (`.cell/`: transcript, turns, receipts, memories, tasks, meta, session, inventory),
  the chat folder beside it (card.json, plandb.db task graph, project tasks.jsonl, task working copies),
  the home's ledgers (usage.jsonl rows of this session, graph.db memories),
  the workspace (every file with its mode, git state, untracked files, secrets).
"""
import difflib
import glob
import hashlib
import json
import os
import re
import sqlite3
import subprocess
import sys

# Files that are this machine's own business: the other machine writing them is not a loss.
TRANSIENT = ("bin/", "meta.lock", "presence.json", "plandb.db-wal", "plandb.db-shm", "told.json", "work/", "resume.pending", "adopted.json")
WS_SKIP = (".git/", "node_modules/", ".furrow/")


def sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def norm(text, aliases):
    """Machine-specific paths become tags, longest first, so `<home>/v3/.../work` is a workspace and not a home."""
    for raw, tag in sorted(aliases, key=lambda a: -len(a[0])):
        if raw and raw != "-":
            text = text.replace(raw, tag)
    return text


def sh(cmd, cwd=None):
    r = subprocess.run(cmd, shell=True, cwd=cwd, capture_output=True, text=True)
    return r.stdout


def files_under(root, skip=()):
    out = {}
    for dirpath, _, names in os.walk(root, followlinks=False):
        for n in names:
            p = os.path.join(dirpath, n)
            rel = os.path.relpath(p, root)
            if any(rel.startswith(s) or rel == s.rstrip("/") for s in skip):
                continue
            if os.path.islink(p):
                out[rel] = {"link": os.readlink(p)}
                continue
            out[rel] = {"sha": sha(p), "mode": oct(os.stat(p).st_mode & 0o777), "size": os.path.getsize(p)}
    return out


def sqlite_rows(db, query):
    if not os.path.exists(db):
        return None
    try:
        con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
        return [list(map(str, r)) for r in con.execute(query)]
    except sqlite3.Error as e:
        return [["error", str(e)]]


def chat_dir(home, cell):
    found = glob.glob(os.path.join(home, "v3", "projects", "*", cell))
    return found[0] if found else None


def read_lines(path):
    if not path or not os.path.exists(path):
        return None
    with open(path, "rb") as f:
        return f.read().decode("utf-8", "replace").split("\n")


def snap(home, cell, ws, out, aliases=()):
    """aliases: (path, tag) pairs for every home and workspace of the rig, so both snapshots read alike."""
    cd = chat_dir(home, cell)
    s = {"home": home, "cell": cell, "chat_dir": cd, "aliases": list(aliases)}
    n = lambda t: norm(t, aliases)
    s["transcript"] = [l for l in (read_lines(cd and os.path.join(cd, ".cell/transcript.jsonl")) or [])]
    s["cell_files"] = files_under(os.path.join(cd, ".cell")) if cd else {}
    s["chat_files"] = files_under(cd, TRANSIENT + (".cell/",)) if cd else {}
    for name in ("meta.json", "session.json", "memories.jsonl", "tasks.json", "turns.jsonl"):
        s["cell:" + name] = [n(l) for l in (read_lines(cd and os.path.join(cd, ".cell", name)) or [])]
    s["seal_times_ms"] = [json.loads(l)["sealed_at_ms"] for l in s["cell:turns.jsonl"] if l.strip()]
    s["cell:inventory"] = json.loads(n(open(os.path.join(cd, ".cell/env/inventory.json")).read())) if cd and os.path.exists(os.path.join(cd, ".cell/env/inventory.json")) else None
    s["chat:card.json"] = [n(l) for l in (read_lines(cd and os.path.join(cd, "card.json")) or [])]
    s["chat:meta.json"] = [n(l) for l in (read_lines(cd and os.path.join(cd, "meta.json")) or [])]
    s["plandb_tasks"] = sqlite_rows(os.path.join(cd, "plandb.db"), "select id,title,status from tasks order by id") if cd else None
    proj = os.path.dirname(cd) if cd else None
    pt = read_lines(proj and os.path.join(proj, "tasks.jsonl")) or []
    s["project_tasks"] = [n(l) for l in pt if l.strip()]
    ledger = read_lines(os.path.join(home, "v3", "usage.jsonl")) or []
    mine = [json.loads(l) for l in ledger if l.strip() and f'"session":"{cell}"' in l]
    # The rebuilt ledger rounds cost to 6 decimals and may name a row's role differently, so rows are keyed by what was spent.
    s["usage_rows"] = sorted((r["model"], r["calls"], r["in"], r["out"], round(r["usd"], 6)) for r in mine)
    s["usage_usd"] = round(sum(r["usd"] for r in mine), 6)
    s["memories_db"] = sqlite_rows(os.path.join(home, "graph.db"), "select * from memories order by 1")
    s["memories_db_cols"] = sqlite_rows(os.path.join(home, "graph.db"), "select name from pragma_table_info('memories')")
    if ws and ws != "-" and os.path.isdir(ws):
        s["workspace"] = workspace(ws, home)
    json.dump(s, open(out, "w"), indent=1)


def workspace(ws, home):
    w = {"files": files_under(ws, WS_SKIP)}
    for key, cmd in (("status", "git status --porcelain=v1 --untracked-files=all"), ("branches", "git branch -a --format='%(refname:short) %(objectname)'"),
                     ("stash", "git stash list"), ("head", "git rev-parse HEAD"), ("headref", "git symbolic-ref -q HEAD"), ("log", "git log --oneline --all"),
                     ("diff_sha", "git diff | sha256sum"), ("staged_sha", "git diff --cached | sha256sum"), ("untracked", "git ls-files --others --exclude-standard"),
                     ("ignored", "git ls-files --others --ignored --exclude-standard | grep -v '^node_modules/' | grep -v '^.codeaf' | grep -v '^.furrow/'")):
        w[key] = sh(cmd, ws).strip().split("\n")
    # Every task and restored branch with its commit: what a kept task is, as the project repo's refs (row 9a), and every
    # commit object, so a branch lost while its commit stays as an unreferenced object can be told from a commit lost too.
    refs = sh("git for-each-ref --format='%(refname) %(objectname)' refs/heads/task refs/heads/restored", ws).split("\n")
    w["kept_branches"] = {r.split()[0][len("refs/heads/"):]: r.split()[1] for r in refs if r.strip()}
    objs = sh("git cat-file --batch-all-objects --batch-check='%(objectname) %(objecttype)'", ws).split("\n")
    w["commit_objects"] = sorted(o.split()[0] for o in objs if o.endswith(" commit"))
    w["ref_mtimes_ms"] = {os.path.relpath(p, ws): int(os.stat(p).st_mtime * 1000) for p in glob.glob(os.path.join(ws, ".git/refs/heads/**/*"), recursive=True) if os.path.isfile(p)}
    w["codeaf_dirs"] = [p for p in glob.glob(os.path.join(ws, ".codeaf*"))]
    w["node_modules"] = os.path.isdir(os.path.join(ws, "node_modules"))
    return w


# ------------------------------------------------------------------ compare --

CATEGORIES = ("card.json", "plandb.db", "logs/jobs/", "logs/stubs/", "tasks/", "trees/", "meta.json")
# Task journals are truth, so they live in .cell/tasks and travel with the cell; none belongs beside the chat.
JOURNAL_HOME = "tasks/"
# Derived files are made again from the cell on open (cellindex's rule: the sealed cell is the only truth), so their absence
# on B right after a take is not a loss by itself; what they say is checked through the facts in the cell.
DERIVED = ("card.json", "plandb.db", "meta.json")


def category(rel):
    return next((c for c in CATEGORIES if rel == c or rel.startswith(c)), "other")


def machine_local(rel):
    """Files the move rebuilds or never carries by design: the job registry's bookkeeping, and a task copy's own git and
    engine folders (re-cut on B, so their ids and objects differ)."""
    base = os.path.basename(rel)
    if base.startswith(".retention") or base.endswith(".retention") or "/node_modules/" in "/" + rel:
        return True   # node_modules is withheld by design and named in the inventory, a lockfile rebuilds it
    return rel.startswith("trees/") and any(p in (".git", ".furrow") for p in rel.split("/")[2:3])


def chat_folder_checks(a, b):
    """The chat folder beside the cell, one verdict per kind of file, so a gap names what is missing."""
    out = {}
    for cat in sorted({category(r) for r in list(a["chat_files"]) + list(b["chat_files"])} - {JOURNAL_HOME}):
        fa = {r: v.get("sha") for r, v in a["chat_files"].items() if category(r) == cat and not machine_local(r)}
        fb = {r: v.get("sha") for r, v in b["chat_files"].items() if category(r) == cat and not machine_local(r)}
        r = check_map(fa, fb, cat)
        r["only_on_a"], r["only_on_b"], r["differ"] = r["only_on_a"][:6], r["only_on_b"][:6], r["differ"][:6]
        if cat in DERIVED:
            r["derived"], r["pass"] = True, True
        out["folder:" + cat] = r
    return out


def reference_checks(a, b):
    """Files the transcript itself points at (spilled tool output, job logs) must be readable on B."""
    refs = sorted(set(re.findall(r"logs/(?:stubs/[0-9a-f]+\.txt|jobs/\d+\.log)", "\n".join(a["transcript"]))))
    on_b = set(b["chat_files"])
    missing = [r for r in refs if r not in on_b]
    return {"transcript_references_readable": {"referenced_by_a": len(refs), "missing_on_b": missing[:8], "n_missing": len(missing), "pass": not missing}}


# ------------------------------------------------------- strict moved-work rows --

# The most one chat-folder file may weigh for the move to carry it; a bigger file is named in the inventory's withheld list.
EVIDENCE_CAP = int(os.environ.get("CONT_EVIDENCE_CAP_BYTES", 1 << 20))
EVIDENCE_DIRS = ("logs/jobs/", "logs/stubs/", "tasks/")
TASK_FIELDS = ("id", "status", "title", "outcome", "cost", "model")   # not where, ground, transcriptUri, artifactUri: machine paths


def resolved_tasks(lines):
    """The project task index as the history page reads it: the newest row (last line) per (sessionId, id)."""
    rows = {}
    for l in lines:
        if not l.strip():
            continue
        r = json.loads(l)
        rows[(r.get("sessionId", ""), str(r.get("id")))] = {f: r.get(f) for f in TASK_FIELDS}
    return rows


def project_tasks_resolved(a, b):
    """Row 4a: both machines resolve to the same task rows. Raw line counts are detail, since an index may repeat rows."""
    ra, rb = resolved_tasks(a["project_tasks"]), resolved_tasks(b["project_tasks"])
    differ = sorted(k for k in ra.keys() & rb.keys() if ra[k] != rb[k])
    return {"a_rows": len(ra), "b_rows": len(rb), "a_lines": len(a["project_tasks"]), "b_lines": len(b["project_tasks"]),
            "only_on_a": sorted(ra.keys() - rb.keys()), "only_on_b": sorted(rb.keys() - ra.keys()),
            "differ": [{"key": k, "a": ra[k], "b": rb[k]} for k in differ], "pass": ra == rb}


def kept_branches_of(w):
    """branch -> commit; older snapshots carry only the `branches` lines ('<name> <sha>'), which say the same."""
    if "kept_branches" in w:
        return w["kept_branches"]
    pairs = (l.rsplit(" ", 1) for l in w.get("branches", []) if l.startswith(("task/", "restored/")))
    return {n: sha_ for n, sha_ in pairs}


def kept_runs(a):
    """The branches the checkpoint records as kept (runs[].merge == "kept"): what the project repo must hold on both machines."""
    nodes = json.loads("\n".join(a["cell:tasks.json"]) or "{}")
    return sorted({r["branch"] for r in nodes.get("runs", []) if r.get("merge") == "kept" and r.get("branch")})


def kept_branch_checks(a, b):
    """Row 9a: each kept task branch is a ref in B's project repo, at the commit it names on A (the bare object being there is detail)."""
    wa, wb = a["workspace"], b["workspace"]
    ka, kb = kept_branches_of(wa), kept_branches_of(wb)
    names = kept_runs(a) or sorted(n for n in ka if n.startswith("task/"))
    rows = {n: {"a": ka.get(n), "b": kb.get(n), "commit_object_on_b": ka.get(n) in set(wb.get("commit_objects", []))} for n in names}
    ok = bool(names) and all(r["a"] and r["a"] == r["b"] for r in rows.values())
    return {"kept_branches": {"branches": rows, "pass": ok},
            "git:kept_branches": {"a": ka, "b": kb, "pass": ka == kb}}


def withheld_paths(s):
    """Chat-folder files the cell's inventory names as withheld (the move keeps them back on purpose, over the size cap)."""
    return {w.get("path", "") for w in ((s.get("cell:inventory") or {}).get("withheld") or [])}


def is_withheld(rel, withheld):
    return any(w == rel or w.endswith("/" + rel) for w in withheld)


def evidence_verdict(rel, fa, b_files, withheld, cap):
    """One of A's evidence files on B: 'equal' (same bytes), 'withheld' (over the cap and named), or the way it is lost."""
    fb = b_files.get(rel)
    if fb and fb.get("sha") == fa.get("sha"):
        return "equal"
    if fa.get("size", 0) > cap and is_withheld(rel, withheld):
        return "withheld"
    return "differ" if fb else "missing"


def tasks_in_cell(s):
    """The task journals of a snapshot: every file of the cell's own tasks/ folder."""
    return {k: v for k, v in s["cell_files"].items() if k.startswith(JOURNAL_HOME)}


def journal_home_checks(a, b):
    """Row 10e: neither machine keeps a task journal beside the chat, so one reader looking in .cell/tasks finds them all."""
    beside = {n: sorted(r for r in s["chat_files"] if r.startswith(JOURNAL_HOME))[:6] for n, s in (("a", a), ("b", b))}
    return {"task_journals_in_cell": {"beside_chat": beside, "on_a": len(tasks_in_cell(a)), "on_b": len(tasks_in_cell(b)),
                                       "pass": not any(beside.values()) and len(tasks_in_cell(a)) == len(tasks_in_cell(b))}}


def chat_evidence_checks(a, b, cap=EVIDENCE_CAP):
    """Row 10: every file of A's logs/jobs/, logs/stubs/ and tasks/ is byte-equal on B, or over the cap and named withheld."""
    wh = withheld_paths(b)
    # Task journals live in .cell/tasks on both machines; the logs and stubs sit beside the chat.
    a, b = (dict(s, chat_files={**s["chat_files"], **tasks_in_cell(s)}) for s in (a, b))
    files = {r: v for r, v in a["chat_files"].items() if r.startswith(EVIDENCE_DIRS) and "sha" in v and not machine_local(r)}
    verdicts = {r: evidence_verdict(r, v, b["chat_files"], wh, cap) for r, v in files.items()}
    bad = sorted(r for r, v in verdicts.items() if v in ("differ", "missing"))
    res = {"cap_bytes": cap, "a_files": len(files), "equal": sum(v == "equal" for v in verdicts.values()),
           "withheld": sorted(r for r, v in verdicts.items() if v == "withheld"),
           "missing_on_b": [r for r in bad if verdicts[r] == "missing"][:8], "differ": [r for r in bad if verdicts[r] == "differ"][:8],
           "n_bad": len(bad), "pass": bool(files) and not bad}
    return {"chat_evidence_bytes": res, "transcript_job_logs_readable": job_log_checks(a, b, cap, wh)}


def job_log_checks(a, b, cap, withheld):
    """Row 10d: each job log B's transcript names by absolute path maps (A's chat folder -> B's) to a file B can read, with A's bytes."""
    homes = {d.rstrip("/") for d in (a.get("chat_dir"), b.get("chat_dir")) if d}
    named = set(re.findall(r"(/[^\s\"'\\]*?)(logs/jobs/\d+\.log)", "\n".join(b["transcript"])))
    rows = {}
    for prefix, rel in sorted(named):
        fa = a["chat_files"].get(rel)
        verdict = evidence_verdict(rel, fa, b["chat_files"], withheld, cap) if fa else "missing_on_a"
        rows[prefix + rel] = verdict if prefix.rstrip("/") in homes else "foreign_path"
    bad = [p for p, v in rows.items() if v not in ("equal", "withheld")]
    return {"named_by_b": len(rows), "verdicts": rows, "bad": bad[:8], "pass": bool(rows) and not bad}


VOLATILE = {"root", "ground", "launch_dir"}   # where a folder is rooted is rewritten for the machine, by design


def scrub(v):
    if isinstance(v, dict):
        return {k: scrub(x) for k, x in v.items() if k not in VOLATILE}
    return [scrub(x) for x in v] if isinstance(v, list) else v


def structured(a, b, key):
    ja, jb = json.loads("\n".join(a[key]) or "null"), json.loads("\n".join(b[key]) or "null")
    return {"a": scrub(ja), "b": scrub(jb), "pass": scrub(ja) == scrub(jb)}


def task_graph(a, b):
    """The nodes and runs of the cell's tasks.json (the truth the task graph is rebuilt from), root paths scrubbed."""
    r = structured(a, b, "cell:tasks.json")
    n = lambda s: [(x.get("id"), x.get("status"), x.get("title") or x.get("name")) for x in (s or {}).get("nodes", [])] if isinstance(s, dict) else s
    return {"a_nodes": n(r["a"]), "b_nodes": n(r["b"]), "pass": r["pass"]}


def missing_rows(ra, rb):
    left = [list(r) for r in rb]
    n = 0
    for r in ra:
        if list(r) in left:
            left.remove(list(r))
        else:
            n += 1
    return n


def title_of(s):
    m = re.search(r'"title": "([^"]*)"', "".join(s["chat:meta.json"]))
    return m and m.group(1)


def spent(s):
    m = re.search(r'"spentUsd": ([0-9.e-]+)', "".join(s["chat:meta.json"]))
    return m and round(float(m.group(1)), 6)


def effort_of(s):
    m = re.search(r'"effort": "([^"]*)"', "".join(s["cell:session.json"]))
    return m and m.group(1)


def note_in(s):
    rows = json.dumps(s["memories_db"] or []) + "".join(s["cell:memories.jsonl"])
    return "Okafor" in rows


def lines_of(text):
    return [l for l in text if l != ""]


def missing_before_last_message(ta, tb):
    """What of A's conversation B lacks: every line up to A's last message (user, assistant or tool). The lines after that are
    the session's own bookkeeping, written once the answer is out (cost, pace and the aux model calls), and are reported apart."""
    last = max((i for i, l in enumerate(ta) if json.loads(l).get("type") == "message"), default=-1)
    have = set(tb)
    return [(json.loads(l).get("type"), json.loads(l).get("role")) for l in ta[: last + 1] if l not in have]


def check_transcript(a, b):
    ta, tb = lines_of(a["transcript"]), lines_of(b["transcript"])
    first_diff = next((i for i, (x, y) in enumerate(zip(ta, tb)) if x != y), None)
    ok_prefix = first_diff is None and len(tb) >= len(ta)
    missing = ta[len(tb):] if first_diff is None and len(tb) < len(ta) else []
    kinds = lambda ls: [(json.loads(l).get("type"), json.loads(l).get("role")) for l in ls]
    # the last line of each kind that matters for continuing: the last tool result and the last assistant reply
    def tail_present(kind):
        idx = [i for i, l in enumerate(ta) if (json.loads(l).get("type"), json.loads(l).get("role")) == kind]
        return bool(idx) and ta[idx[-1]] in set(tb)
    return {
        "a_lines": len(ta), "b_lines": len(tb), "first_difference_at_line": first_diff,
        "missing_from_b": [(json.loads(l).get("type"), json.loads(l).get("role"), l[:100]) for l in missing],
        "last_tool_result_on_b": tail_present(("message", "tool")),
        "last_assistant_reply_on_b": tail_present(("message", "assistant")),
        "missing_conversation_lines": missing_before_last_message(ta, tb),
        "pass": ok_prefix and len(tb) == len(ta),
    }


def check_map(a_map, b_map, what):
    only_a, only_b = sorted(set(a_map) - set(b_map)), sorted(set(b_map) - set(a_map))
    differ = sorted(k for k in set(a_map) & set(b_map) if a_map[k] != b_map[k])
    return {"a": len(a_map), "b": len(b_map), "only_on_a": only_a, "only_on_b": only_b, "differ": differ,
            "pass": not (only_a or only_b or differ)}


def check_lines(a, b, key):
    return {"a_lines": len(a[key]), "b_lines": len(b[key]), "pass": lines_of(a[key]) == lines_of(b[key])}


def secrets_check(a, b):
    out, ok = {}, True
    for p, am in a["workspace"]["files"].items():
        if re.search(r"(^|/)\.env(\.|$)", p) or p.endswith(".env"):
            bm = b["workspace"]["files"].get(p)
            same = bm == am
            out[p] = {"a": am.get("mode"), "b": bm and bm.get("mode"), "bytes_equal": bool(bm) and bm["sha"] == am["sha"]}
            ok = ok and same
    return {"files": out, "pass": ok and bool(out)}


def workspace_checks(a, b):
    wa, wb = a["workspace"], b["workspace"]
    res = {"files_and_modes": check_map({k: v for k, v in wa["files"].items()}, {k: v for k, v in wb["files"].items()}, "files")}
    res["secrets"] = secrets_check(a, b)
    own = lambda xs: [x for x in xs if not x.startswith(".furrow/")]   # the engine's per-machine ids are not the user's files
    for key in ("status", "branches", "stash", "head", "headref", "diff_sha", "staged_sha", "untracked", "ignored", "log"):
        va, vb = own(wa[key]), own(wb[key])
        res["git:" + key] = {"a": va, "b": vb, "pass": va == vb}
    res["no_codeaf_dir_in_workspace"] = {"b": wb["codeaf_dirs"], "pass": not wb["codeaf_dirs"]}
    res["node_modules"] = {"a": wa["node_modules"], "b": wb["node_modules"], "note": "withheld on purpose; rebuilt by the resume card's setup", "pass": True}
    return res


def compare(a, b):
    res = {"transcript": check_transcript(a, b)}
    t = res["transcript"]
    res["transcript_conversation"] = {"missing": t["missing_conversation_lines"], "last_tool_result_on_b": t["last_tool_result_on_b"],
                                      "last_assistant_reply_on_b": t["last_assistant_reply_on_b"],
                                      "pass": t["first_difference_at_line"] is None and not t["missing_conversation_lines"] and t["last_tool_result_on_b"] and t["last_assistant_reply_on_b"]}
    res["cell_files"] = check_map({k: v.get("sha") for k, v in a["cell_files"].items()}, {k: v.get("sha") for k, v in b["cell_files"].items()}, "cell files")
    res.update(chat_folder_checks(a, b))
    res.update(reference_checks(a, b))
    res.update(chat_evidence_checks(a, b))
    res.update(journal_home_checks(a, b))
    res["spend_in_chat_meta"] = {"a": spent(a), "b": spent(b), "pass": spent(a) == spent(b)}
    res["title"] = {"a": title_of(a), "b": title_of(b), "pass": bool(title_of(a)) and title_of(a) == title_of(b)}
    res["effort"] = {"a": effort_of(a), "b": effort_of(b), "pass": bool(effort_of(a)) and effort_of(a) == effort_of(b)}
    res["memory_note_asked_for"] = {"a": note_in(a), "b": note_in(b), "pass": note_in(a) and note_in(b)}
    res["task_graph"] = task_graph(a, b)
    res["session_settings"] = structured(a, b, "cell:session.json")
    for key in ("cell:memories.jsonl", "project_tasks"):
        res[key] = check_lines(a, b, key)
    res["project_tasks_resolved"] = project_tasks_resolved(a, b)
    # chat_dir names the folder the carried files were in, so it differs between machines by design.
    inv = lambda s: {k: v for k, v in (s["cell:inventory"] or {}).items() if k != "chat_dir"}
    res["cell:inventory"] = {"a": a["cell:inventory"], "b": b["cell:inventory"], "pass": inv(a) == inv(b)}
    res["usage_ledger"] = {"a_rows": len(a["usage_rows"]), "b_rows": len(b["usage_rows"]), "a_usd": a["usage_usd"], "b_usd": b["usage_usd"], "missing_rows_on_b": missing_rows(a["usage_rows"], b["usage_rows"]), "pass": a["usage_rows"] == b["usage_rows"]}
    # What a memory says, not the counters and sequence numbers a home keeps for itself (use count, created and updated seq).
    content = lambda s: sorted(tuple(r[1:7]) for r in (s["memories_db"] or []))
    res["memories_db"] = {"a_rows": len(a["memories_db"] or []), "b_rows": len(b["memories_db"] or []), "a": content(a), "b": content(b), "pass": content(a) == content(b)}
    if "workspace" in a and "workspace" in b:
        res.update(workspace_checks(a, b))
        res.update(kept_branch_checks(a, b))
    else:
        res["workspace"] = {"pass": False, "note": "a workspace was not snapshotted on both sides"}
    return res


# ------------------------------------------------------------------ repeats --

def calls(lines, aliases=()):
    """(tool, arguments) of every tool call in a transcript, in order, with its result text."""
    out, results = [], {}
    recs = [json.loads(l) for l in lines if l.strip()]
    for r in recs:
        if r.get("role") == "tool":
            results[r.get("toolCallId")] = r.get("content", "")
    for i, r in enumerate(recs):
        if r.get("role") == "assistant" and r.get("toolCalls"):
            tcs = r["toolCalls"]
            tcs = json.loads(tcs) if isinstance(tcs, str) else tcs
            for tc in tcs:
                fn = tc["function"]
                out.append({"line": i, "id": tc["id"], "tool": fn["name"], "args": norm(canon(fn["arguments"]), aliases), "result": results.get(tc["id"])})
    return out


def canon(args):
    try:
        return json.dumps(json.loads(args), sort_keys=True)
    except (TypeError, ValueError):
        return str(args)


def nearest(call, a_calls):
    """How alike the closest call of A is (same tool, argument text ratio): a re-run with a flag or two changed is a repeat of the work."""
    same = [difflib.SequenceMatcher(None, call["args"], c["args"]).ratio() for c in a_calls if c["tool"] == call["tool"]]
    return max(same, default=0)


def repeats(a, b, b_from_line):
    """Calls in B's transcript after b_from_line (where B's own turns begin) whose tool and arguments equal a call A completed."""
    al = a.get("aliases", [])
    a_calls = [c for c in calls(a["transcript"], al) if c["result"] is not None]
    done = {(c["tool"], c["args"]) for c in a_calls}
    b_calls = [c for c in calls(b["transcript"], al) if c["line"] >= b_from_line]
    rep = [c for c in b_calls if (c["tool"], c["args"]) in done]
    near = [c for c in b_calls if (c["tool"], c["args"]) not in done and nearest(c, a_calls) >= 0.6]
    return {"b_calls": [(c["tool"], c["args"][:160]) for c in b_calls], "repeated": [(c["tool"], c["args"][:160]) for c in rep], "count": len(rep),
            "near_repeats": [(c["tool"], c["args"][:160]) for c in near], "near_count": len(near),
            "dangling_calls_in_b": [(c["tool"], c["args"][:120]) for c in calls(b["transcript"], al) if c["result"] is None and c["line"] < b_from_line]}


if __name__ == "__main__":
    cmd = sys.argv[1]
    if cmd == "snap":
        snap(*sys.argv[2:6])
    elif cmd == "compare":
        r = compare(json.load(open(sys.argv[2])), json.load(open(sys.argv[3])))
        print(json.dumps(r, indent=1))
    elif cmd == "repeats":
        print(json.dumps(repeats(json.load(open(sys.argv[2])), json.load(open(sys.argv[3])), int(sys.argv[4])), indent=1))
