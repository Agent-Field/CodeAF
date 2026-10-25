"""Read a codeaf message snapshot without writing to the running application's store."""
from collections import Counter
from pathlib import Path
import re
import sqlite3

AUTOMATED = (
    "[carry on]", "[silent]", "[taking stock]", "[notice]", "[system",
    "Your team's replies", "while you worked:", "A note from the session, not from the person:",
    "[The person", "[Context", "[context", "[compacted", "[Turn",
)
FIXTURE = re.compile(r"^(reply (exactly|with exactly)|use propose_task to create|"
                     r"run `?(ls( -la)?|cat does-not-exist.txt|wc -w README.md)`?\b)", re.I)


def redact(text):
    text = re.sub(r"-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----",
                  "[REDACTED PRIVATE KEY]", text, flags=re.S)
    text = re.sub(r"\b(?:sk-(?:or-v1-)?|gh[pousr]_|github_pat_)[A-Za-z0-9_-]{16,}", "[REDACTED KEY]", text)
    return re.sub(r"(?i)((?:api[_-]?key|access[_-]?token|password|secret)\s*[=:]\s*)"
                  r"[\"']?[^\s\"',;}{]{12,}", r"\1[REDACTED]", text)


def read_messages(database, limit_sessions=0):
    """Return eligible messages plus an explicit inclusion/exclusion inventory."""
    uri = Path(database).resolve().as_uri() + "?mode=ro"
    with sqlite3.connect(uri, uri=True) as db:
        db.execute("BEGIN")
        total = db.execute("SELECT count(*) FROM messages").fetchone()[0]
        rows = db.execute("SELECT seq,ts,session_id,role,body FROM messages "
                          "WHERE role IN ('user','agent','assistant') ORDER BY seq").fetchall()
    skipped = Counter()
    skipped["system/tool record"] = total - len(rows)
    messages = []
    for seq, ts, session, role, body in rows:
        if role not in ("user", "agent", "assistant"):
            skipped["system/tool record"] += 1
            continue
        if body.lstrip().startswith(AUTOMATED):
            skipped["automated notice"] += 1
            continue
        if role == "user" and FIXTURE.search(body.strip()):
            skipped["recognizable test exercise"] += 1
            continue
        if not body.strip():
            skipped["empty"] += 1
            continue
        # A task receipt can be appended to a repeated user request by the engine.
        text = re.split(r"\s+done · ran |\s+ran and did not finish ·", redact(body), maxsplit=1)[0]
        messages.append({"id": f"{session or 'legacy'}:{seq}", "seq": seq, "ts": ts,
                         "session": session or "legacy", "role": role, "text": redact(text)})
    human_sessions = {m["session"] for m in messages if m["role"] == "user"}
    messages = [m for m in messages if m["session"] in human_sessions]
    sessions = sorted(human_sessions, key=lambda s: max(m["seq"] for m in messages if m["session"] == s))
    if limit_sessions:
        sessions = sessions[-limit_sessions:]
        messages = [m for m in messages if m["session"] in sessions]
    return messages, {"total_records": total, "eligible_messages": len(messages),
                      "sessions": len(sessions), "excluded": dict(skipped),
                      "characters": sum(len(m["text"]) for m in messages),
                      "date_from": min((m["ts"] for m in messages), default=None),
                      "date_to": max((m["ts"] for m in messages), default=None),
                      "selection": f"latest {limit_sessions} eligible sessions" if limit_sessions else "all eligible sessions",
                      "limitation": "Heuristic test/notice filtering; extraction also screens synthetic material. This is one database, not all accounts or machines."}
