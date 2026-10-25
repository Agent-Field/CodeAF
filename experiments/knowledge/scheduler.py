"""Replay scheduling policies without starting a daemon or modifying live codeaf."""
from datetime import datetime


def simulate(messages, quiet_seconds=30, max_wait_seconds=120, max_chars=24000):
    """Debounce independently per chat, with bounded waits and bounded batch sizes."""
    pending, batches = {}, []
    def flush(session, at):
        item = pending.pop(session)
        batches.append({"session": session, "at": at, "messages": item["messages"],
                        "characters": item["characters"], "oldest_wait_seconds": at - item["first"]})
    for message in sorted(messages, key=lambda m: (m["ts"], m["id"])):
        at = datetime.fromisoformat(message["ts"].replace("Z", "+00:00")).timestamp()
        for session, item in list(pending.items()):
            due = min(item["last"] + quiet_seconds, item["first"] + max_wait_seconds)
            if due <= at:
                flush(session, due)
        session = message["session"]
        item = pending.setdefault(session, {"first": at, "last": at, "characters": 0, "messages": []})
        item["last"] = at
        item["characters"] += len(message["text"])
        item["messages"].append(message["id"])
        if item["characters"] >= max_chars:
            flush(session, at)
    for session, item in list(pending.items()):
        flush(session, min(item["last"] + quiet_seconds, item["first"] + max_wait_seconds))
    return {"quiet_seconds": quiet_seconds, "max_wait_seconds": max_wait_seconds,
            "capture_events": len(messages), "extraction_batches": len(batches),
            "max_observed_wait_seconds": max((b["oldest_wait_seconds"] for b in batches), default=0),
            "mean_messages_per_batch": round(len(messages) / max(1, len(batches)), 2),
            "batches": batches}
