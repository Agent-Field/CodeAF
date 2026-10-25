"""Portable Markdown and SQLite projections, with a durable local change ledger."""
import datetime
import json
import os
from pathlib import Path
import shutil
import sqlite3
import tempfile
import fcntl

from knowledge import identity
from wikilinks import link_vault, markdown_text


def write_json(path, value):
    path = Path(path)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(value, ensure_ascii=False, indent=2))
    tmp.replace(path)


def save(vault, directory):
    """Persist one experiment outside the source tree; keep all prior change events."""
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (directory / ".writer.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            _save(link_vault(vault), directory)
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)


def _save(vault, directory):
    claim_ids = {c["id"] for c in vault["claims"]}
    note_ids = {n["id"] for n in vault["notes"]}
    if len(claim_ids) != len(vault["claims"]) or len(note_ids) != len(vault["notes"]):
        raise ValueError("Duplicate claim or note identity")
    for note in vault["notes"]:
        if any(i not in claim_ids for i in note["claims"]):
            raise ValueError("Note contains an unknown claim")
        if any(l["id"] not in note_ids for l in note["links"]):
            raise ValueError("Note contains an unknown link target")
    for note in vault["notes"]:
        for section in note.get("sections", []):
            for paragraph in section["paragraphs"]:
                if any(i not in claim_ids for i in paragraph.get("claims", [])):
                    raise ValueError("Article contains an unknown claim")
                if any(e.get("source") not in vault["sources"] or not e.get("quote") or
                       e["quote"] not in vault["sources"][e["source"]]["text"] or
                       (e.get("source_hash") is not None and e["source_hash"] != identity(vault["sources"][e["source"]]["text"]))
                       for e in paragraph.get("evidence", [])):
                    raise ValueError("Article contains unsupported source evidence")
    for claim in vault["claims"]:
        if any(e["source"] not in vault["sources"] or e["quote"] not in vault["sources"][e["source"]]["text"]
               for e in claim["evidence"]):
            raise ValueError("Claim contains unsupported source evidence")
    if any(r["from"] not in claim_ids or r["to"] not in claim_ids for r in vault["relations"]):
        raise ValueError("Relation contains an unknown claim")
    previous = json.loads((directory / "vault.json").read_text()) if (directory / "vault.json").exists() else {}
    stamp = datetime.datetime.now(datetime.timezone.utc).isoformat()
    ledger = directory / "events.jsonl"
    prior_hash = ""
    if ledger.exists():
        recovered = {field: {} for field in ("sources", "claims", "notes", "relations")}
        for line in ledger.read_text().splitlines():
            event = json.loads(line)
            claimed_hash = event.pop("hash")
            if event["previous"] != prior_hash or identity(event) != claimed_hash:
                raise ValueError("Knowledge event ledger failed integrity verification")
            prior_hash = claimed_hash
            field, operation = event["kind"].rsplit("_", 1)
            if operation == "upsert":
                recovered[field][event["id"]] = event["value"]
            elif operation == "retire":
                recovered[field].pop(event["id"], None)
        # The ledger can be ahead of disposable projections after an interrupted export.
        previous = {field: list(values.values()) for field, values in recovered.items()}
    changes = []
    for field in ("sources", "claims", "notes", "relations"):
        values = vault[field]
        values = list(values.values()) if isinstance(values, dict) else values
        old = previous.get(field, [])
        old = list(old.values()) if isinstance(old, dict) else old
        old_by_id = {v.get("id", identity(v)): v for v in old}
        for value in values:
            key = value.get("id", identity(value))
            if value != old_by_id.get(key):
                event = {"kind": field + "_upsert", "recorded_at": stamp, "id": key,
                         "value": value, "previous": prior_hash}
                prior_hash = identity(event)
                changes.append({**event, "hash": prior_hash})
        current_ids = {v.get("id", identity(v)) for v in values}
        for key in sorted(set(old_by_id) - current_ids):
            event = {"kind": field + "_retire", "recorded_at": stamp, "id": key,
                     "previous": prior_hash}
            prior_hash = identity(event)
            changes.append({**event, "hash": prior_hash})
    with ledger.open("a") as f:
        for event in changes:
            f.write(json.dumps(event, ensure_ascii=False) + "\n")
        f.flush()
        os.fsync(f.fileno())
    # Projections are disposable; the append-only event ledger retains old versions.
    handle, filename = tempfile.mkstemp(prefix="vault-", suffix=".sqlite", dir=directory)
    os.close(handle)
    try:
        with sqlite3.connect(filename) as db:
            db.execute("PRAGMA foreign_keys=ON")
            db.executescript("""
                CREATE TABLE sources(id TEXT PRIMARY KEY, session TEXT, role TEXT, observed_at TEXT, body TEXT);
                CREATE TABLE claims(id TEXT PRIMARY KEY, scope TEXT, kind TEXT, body TEXT, status TEXT, unsure INTEGER, subject_key TEXT);
                CREATE TABLE evidence(claim_id TEXT REFERENCES claims(id), source_id TEXT REFERENCES sources(id), quote TEXT, span_start INTEGER, span_end INTEGER);
                CREATE TABLE notes(id TEXT PRIMARY KEY, title TEXT, scope TEXT, folder TEXT, article_json TEXT);
                CREATE TABLE note_claims(note_id TEXT REFERENCES notes(id), claim_id TEXT REFERENCES claims(id));
                CREATE TABLE links(note_id TEXT REFERENCES notes(id), target_id TEXT REFERENCES notes(id), reason TEXT);
                CREATE TABLE relations(from_id TEXT REFERENCES claims(id), to_id TEXT REFERENCES claims(id), kind TEXT, reason TEXT);
                CREATE TABLE entities(claim_id TEXT REFERENCES claims(id), name TEXT);
                CREATE INDEX claims_scope_status ON claims(scope,status);
                CREATE INDEX evidence_sources ON evidence(source_id);
                CREATE INDEX note_claims_claim ON note_claims(claim_id);
                CREATE INDEX links_target ON links(target_id);
                CREATE INDEX entities_name ON entities(name);
                CREATE VIRTUAL TABLE knowledge_fts USING fts5(claim_id UNINDEXED, body, scope, topic);
            """)
            for source in vault["sources"].values():
                db.execute("INSERT INTO sources VALUES(?,?,?,?,?)", (source["id"], source["session"], source["role"], source["ts"], source["text"]))
            for claim in vault["claims"]:
                db.execute("INSERT INTO claims VALUES(?,?,?,?,?,?,?)", (claim["id"], claim["scope"], claim["kind"], claim["text"], claim["status"], int(claim.get("unsure", False)), claim.get("key", "")))
                for evidence in claim["evidence"]:
                    start = vault["sources"][evidence["source"]]["text"].index(evidence["quote"])
                    db.execute("INSERT INTO evidence VALUES(?,?,?,?,?)", (claim["id"], evidence["source"], evidence["quote"], start, start + len(evidence["quote"])))
                for entity in claim.get("entities", []):
                    db.execute("INSERT INTO entities VALUES(?,?)", (claim["id"], entity))
                if claim["status"] == "active":
                    db.execute("INSERT INTO knowledge_fts VALUES(?,?,?,?)", (claim["id"], claim["text"], claim["scope"], claim.get("topic", "")))
            for note in vault["notes"]:
                db.execute("INSERT INTO notes VALUES(?,?,?,?,?)", (note["id"], note["title"], note["scope"], json.dumps(note["folder"]), json.dumps(note.get("sections", []))))
                db.executemany("INSERT INTO note_claims VALUES(?,?)", [(note["id"], c) for c in note["claims"]])
            for note in vault["notes"]:
                db.executemany("INSERT INTO links VALUES(?,?,?)", [(note["id"], l["id"], l["reason"]) for l in note["links"]])
            db.executemany("INSERT INTO relations VALUES(?,?,?,?)", [(r["from"], r["to"], r["kind"], r.get("reason", "")) for r in vault["relations"]])
        os.replace(filename, directory / "vault.sqlite")
    finally:
        if os.path.exists(filename):
            os.unlink(filename)
    write_json(directory / "vault.json", vault)
    write_json(directory / "checkpoint.json", {"sources": len(vault["sources"]), "ledger_hash": prior_hash,
                                               "projection_hash": identity(vault), "recorded_at": stamp})
    export_markdown(vault, directory / "markdown")


def export_markdown(vault, directory):
    """Use stable IDs as file names so model-generated titles cannot escape the vault."""
    vault = link_vault(vault)
    directory = Path(directory)
    directory.mkdir(exist_ok=True)
    claims = {c["id"]: c for c in vault["claims"]}
    notes = {n["id"]: n for n in vault["notes"]}
    for note in notes.values():
        lines = ["---", "id: " + note["id"], "title: " + json.dumps(note["title"]),
                 "scope: " + json.dumps(note["scope"]), "folder: " + json.dumps(note["folder"]),
                 "---", "", "# " + note["title"], ""]
        article_evidence = {}
        active = [claims[i] for i in note["claims"] if claims[i]["status"] == "active"]
        history = [claims[i] for i in note["claims"] if claims[i]["status"] != "active"]
        if note.get("sections"):
            for section in note["sections"]:
                if section["heading"]:
                    lines += ["## " + section["heading"], ""]
                for paragraph in section["paragraphs"]:
                    refs = " ".join("[^" + i + "]" for i in paragraph["claims"])
                    for evidence in paragraph.get("evidence", []):
                        key = "source-" + identity([evidence["source"], evidence["quote"]])
                        article_evidence[key] = evidence
                        refs += " [^" + key + "]"
                    lines.append(markdown_text(paragraph) + " " + refs + "\n")
        else:
            for claim in active:
                prefix = "Not sure: " if claim.get("unsure") else ""
                if claim["kind"] == "proposal":
                    prefix += "Proposed: "
                lines.append(prefix + claim["text"] + " [^" + claim["id"] + "]\n")
        if note.get("questions"):
            lines += ["## Questions to understand next", "", "Unresolved in inspected evidence, or awaiting an evidence review.", ""]
            for question in note["questions"]:
                refs = " ".join("[^" + i + "]" for i in question["claims"])
                lines.append("- " + question["question"] + " " + refs)
                if question.get("why"):
                    lines.append("  " + question["why"])
        if note.get("conflicts"):
            lines += ["", "## Unresolved disagreements", ""]
            for conflict in note["conflicts"]:
                refs = " ".join("[^" + i + "]" for i in conflict["claims"])
                lines.append("- " + conflict["reason"] + " " + refs)
        if note["links"]:
            lines += ["## Related notes", ""]
            for link in note["links"]:
                lines.append(f"- [[{link['id']}|{notes[link['id']]['title']}]] — {link['reason']}")
        if note.get("backlinks"):
            lines += ["", "## Mentioned by", ""]
            for backlink in note["backlinks"]:
                source = notes[backlink["id"]]
                lines.append(f"- [[{source['id']}|{source['title']}]]")
        if history:
            lines += ["", "## History", ""]
            lines += [f"- {c['status']}: {c['text']} [^{c['id']}]" for c in history]
        lines += ["", "## Sources", ""]
        for key, evidence in article_evidence.items():
            src = vault["sources"][evidence["source"]]
            lines.append(f"[^{key}]: {src['id']} · {src['role']} · {src['ts']} — {json.dumps(evidence['quote'], ensure_ascii=False)}")
        for claim in active + history:
            sources = []
            for evidence in claim["evidence"]:
                src = vault["sources"][evidence["source"]]
                sources.append(f"{src['id']} · {src['role']} · {src['ts']} — {json.dumps(evidence['quote'], ensure_ascii=False)}")
            lines.append(f"[^{claim['id']}]: " + " / ".join(sources))
        (directory / (note["id"] + ".md")).write_text("\n".join(lines) + "\n")


def viewer(vaults, comparison, research, directory):
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    source = Path(__file__).parent / "viewer"
    for name in ("index.html", "viewer.css", "viewer.js"):
        shutil.copyfile(source / name, directory / name)
    data = json.dumps({"vaults": {key: link_vault(value) for key, value in vaults.items()}, "comparison": comparison, "research": research}, ensure_ascii=False)
    script = "window.KNOWLEDGE_LAB=" + data.replace("<", "\\u003c") + ";\n"
    (directory / "data.js").write_text(script)
    standalone = (directory / "index.html").read_text()
    standalone = standalone.replace('<link rel="stylesheet" href="viewer.css">',
                                    '<style>' + (directory / "viewer.css").read_text() + '</style>')
    standalone = standalone.replace('<script src="data.js"></script>', '<script>' + script + '</script>')
    standalone = standalone.replace('<script src="viewer.js"></script>',
                                    '<script>' + (directory / "viewer.js").read_text() + '</script>')
    (directory / "standalone.html").write_text(standalone)
