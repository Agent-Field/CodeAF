"""Observable replay and recall contracts; providers are replaced at the network seam."""
import unittest
import sqlite3
import tempfile
from pathlib import Path

from knowledge import build, recall, update_once, organize_hierarchy
from corpus import read_messages
from storage import save
from editor import compose_notes, review_articles


class ReplayTests(unittest.TestCase):
    def test_stale_raw_article_evidence_cannot_be_committed(self):
        vault = {"schema": 1, "sources": {"s": {"id": "s", "role": "agent", "session": "s", "ts": "2026-10-25", "text": "A fact."}},
                 "claims": [], "relations": [], "notes": [{"id": "n", "title": "Subject", "scope": "Scope",
                 "folder": [], "claims": [], "links": [], "sections": [{"heading": "Evidence", "paragraphs": [
                     {"text": "A fact.", "claims": [], "evidence": [{"source": "s", "quote": "A fact.", "source_hash": "wrong-version"}]}]}]}]}
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, "source evidence"):
                save(vault, directory)
            self.assertFalse((Path(directory) / "events.jsonl").exists())

    def test_incremental_update_keeps_articles_without_resending_their_full_text(self):
        message = {"id": "s:1", "session": "s", "role": "user", "ts": "2026-01-01",
                   "text": "Atlas uses SQLite."}
        def extract(role, payload):
            return {"claims": [{"text": message["text"], "scope": "Atlas", "topic": "Storage", "kind": "fact",
                                "evidence": [{"source": "s:1", "quote": message["text"]}]}]}
        previous = build([message], extract)
        previous["strategy"] = "reconciled"
        previous["notes"][0]["sections"] = [{"heading": "Database", "paragraphs": [
            {"text": "SQLite. " * 10000, "claims": [previous["claims"][0]["id"]]}]}]
        captured = []
        def update(role, payload):
            import json
            captured.append(len(json.dumps(payload)))
            return {"claims": [], "notes": [], "relations": []}
        result = update_once([{**message, "id": "s:2", "text": "SQLite remains our database."}], previous, update)
        self.assertEqual(result["notes"][0]["sections"], previous["notes"][0]["sections"])
        self.assertEqual(len(captured), 1)
        self.assertLess(captured[0], 5000)

    def test_live_tool_only_updates_preserve_knowledge_without_model_work(self):
        previous = {"schema": 1, "strategy": "reconciled", "sources": {}, "claims": [],
                    "notes": [], "relations": [], "rejected": [], "scope_aliases": []}
        message = {"id": "s:tool", "session": "s", "role": "agent", "ts": "2026-01-01",
                   "text": '[tool call: read_file({"path":"README.md"})]'}
        def model(role, payload):
            self.fail("Tool-only live updates should not spend tokens")
        result = update_once([message], previous, model)
        self.assertIn("s:tool", result["sources"])
        self.assertEqual(result["update_receipt"]["provider_calls"], 0)

    def test_tool_call_envelopes_are_archived_without_a_model_call(self):
        message = {"id": "s:tool", "session": "s", "role": "agent", "ts": "2026-01-01",
                   "text": '[tool call: read_file({"path":"README.md"})]'}
        def model(role, payload):
            self.fail("A tool-call envelope contains no conversational knowledge to extract")
        vault = build([message], model, strategy="reconciled")
        self.assertEqual(vault["sources"]["s:tool"], message)
        self.assertEqual(vault["claims"], [])
        self.assertEqual(vault["notes"], [])

    def test_correction_order_uses_instants_across_timezone_offsets(self):
        messages = [{"id": "s:1", "session": "s", "role": "user", "ts": "2026-01-01T10:00:00+02:00", "text": "Use Redis."},
                    {"id": "s:2", "session": "s", "role": "user", "ts": "2026-01-01T09:30:00+00:00", "text": "Correction: use SQLite."}]
        def model(role, payload):
            if role == "extract":
                return {"claims": [{"text": m["text"], "scope": "Atlas", "topic": "Storage", "key": "database", "kind": "decision",
                                    "evidence": [{"source": m["id"], "quote": m["text"]}]} for m in messages]}
            ids = [c["id"] for c in payload["claims"]]
            return {"notes": [], "relations": [{"from": ids[1], "to": ids[0], "kind": "supersedes"}]}
        vault = build(messages, model, strategy="reconciled")
        self.assertEqual(vault["claims"][0]["status"], "superseded")

    def test_invalid_graph_cannot_append_to_the_durable_ledger(self):
        with tempfile.TemporaryDirectory() as directory:
            vault = {"schema": 1, "sources": {}, "claims": [], "relations": [],
                     "notes": [{"id": "n", "title": "Atlas", "scope": "Atlas", "folder": ["Atlas"],
                                "claims": [], "links": [{"id": "missing", "reason": "bad target"}]}]}
            with self.assertRaisesRegex(ValueError, "link"):
                save(vault, directory)
            self.assertFalse((Path(directory) / "events.jsonl").exists())

    def test_an_unrelated_new_message_does_not_erase_an_existing_article(self):
        message = {"id": "s:1", "session": "s", "role": "user", "ts": "2026-01-01", "text": "Atlas uses SQLite."}
        def model(role, payload):
            return {"claims": [{"text": message["text"], "scope": "Atlas", "topic": "Storage", "kind": "fact",
                                "evidence": [{"source": "s:1", "quote": message["text"]}]}]} if payload["messages"][0]["id"] == "s:1" else {"claims": []}
        first = build([message], model)
        first["notes"][0]["sections"] = [{"heading": "Database", "paragraphs": [{"text": "Atlas uses SQLite.", "claims": [first["claims"][0]["id"]]}]}]
        revised = build([{**message, "id": "s:2", "text": "Hello."}], model, previous=first)
        self.assertEqual(revised["notes"][0].get("sections"), first["notes"][0]["sections"])

    def test_article_review_removes_a_sentence_that_is_not_supported_by_its_claim(self):
        vault = {"claims": [{"id": "c", "text": "AgentFS is proposed.", "kind": "reported"}],
                 "notes": [{"id": "n", "title": "AgentFS", "sections": [{"heading": "Design", "paragraphs": [
                     {"text": "AgentFS is deployed globally.", "claims": ["c"]}]}]}]}
        def model(role, payload):
            return {"checks": [{"id": "P0", "supported": False, "reason": "A proposal is not deployment."}]}
        result = review_articles(vault, model)
        self.assertFalse(result["notes"][0]["sections"])
        self.assertEqual(result["notes"][0]["article_rejections"][0]["reason"], "A proposal is not deployment.")
        self.assertEqual(result["claims"], vault["claims"])

    def test_recall_accepts_a_case_variant_of_a_semantic_project_name(self):
        vault = {"claims": [{"id": "c", "text": "Atlas uses SQLite.", "scope": "Atlas", "status": "active",
                              "evidence": [{"source": "s", "quote": "SQLite"}]}], "notes": []}
        answer = recall(vault, "Atlas storage", lambda role, payload: {"answer": "SQLite", "citations": ["c"]}, scope="atlas")
        self.assertEqual(answer["citations"][0]["source"], "s")

    def test_articles_explain_the_subject_and_keep_provenance_out_of_the_main_prose(self):
        claim = {"id": "c", "text": "The agent reported: AgentFS proposes content-addressed chunks.",
                 "status": "active", "kind": "reported", "scope": "AgentFS",
                 "evidence": [{"source": "s", "quote": "content-addressed chunks"}]}
        vault = {"claims": [claim], "sources": {}, "notes": [{"id": "n", "title": "AgentFS",
                 "scope": "AgentFS", "folder": ["Storage"], "claims": ["c"], "links": []}]}
        def model(role, payload):
            return {"notes": [{"id": "N0", "sections": [{"heading": "Design",
                              "paragraphs": [{"text": "AgentFS is a proposed architecture built around content-addressed chunks.",
                                              "claims": ["C0"]},
                                             {"text": "It is already deployed globally.", "claims": ["invented"]}]}]}]}
        result = compose_notes(vault, model)
        paragraphs = result["notes"][0]["sections"][0]["paragraphs"]
        self.assertEqual(len(paragraphs), 1)
        self.assertEqual(paragraphs[0]["claims"], ["c"])
        self.assertTrue(paragraphs[0]["text"].startswith("AgentFS is a proposed architecture"))
        self.assertEqual(result["claims"], [claim])

    def test_renaming_two_notes_keeps_their_links_attached_to_the_same_knowledge(self):
        messages = [{"id": "s:1", "session": "s", "role": "user", "ts": "2026-01-01",
                     "text": "Atlas uses SQLite. Atlas keeps backups."}]
        def model(role, payload):
            if role == "extract":
                return {"claims": [{"text": text, "scope": "Atlas", "topic": topic, "kind": "fact",
                                    "evidence": [{"source": "s:1", "quote": text}]} for text, topic in
                                   [("Atlas uses SQLite.", "Alpha"), ("Atlas keeps backups.", "Beta")]]}
            ids = [c["id"] for c in payload["claims"]]
            return {"notes": [{"title": "Beta", "scope": "Atlas", "claims": [ids[0]],
                                "links": [{"title": "Alpha", "scope": "Atlas", "reason": "Backups protect the database"}]},
                               {"title": "Alpha", "scope": "Atlas", "claims": [ids[1]],
                                "links": [{"title": "Beta", "scope": "Atlas", "reason": "The database needs backups"}]}], "relations": []}
        first = build(messages, model)
        revised = build([], model, strategy="reconciled", previous=first)
        for note in revised["notes"]:
            other = next(n for n in revised["notes"] if n["id"] != note["id"])
            self.assertEqual([l["id"] for l in note["links"]], [other["id"]])

    def test_hierarchy_repairs_links_without_changing_evidence_or_scopes(self):
        vault = {"claims": [], "sources": {}, "notes": [
            {"id": "a", "title": "Storage", "scope": "Atlas", "folder": ["Misc"], "claims": [],
             "links": [{"id": "c", "reason": "Both concern software"}]},
            {"id": "b", "title": "AgentFS", "scope": "AgentFS", "folder": ["Misc"], "claims": [], "links": []},
            {"id": "c", "title": "Chat spawning", "scope": "Atlas", "folder": ["Misc"], "claims": [], "links": []}]}
        def model(role, payload):
            return {"assignments": [{"id": "a", "folder": ["Atlas", "Storage"]}],
                    "bad_links": [{"from": "a", "to": "c", "reason": "Too broad"}],
                    "links": [{"from": "a", "to": "b", "reason": "AgentFS is a storage design alternative"},
                              {"from": "a", "to": "missing", "reason": "Invented target"}]}
        result = organize_hierarchy(vault, model)
        self.assertEqual(result["notes"][0]["folder"], ["Atlas", "Storage"])
        self.assertEqual(result["notes"][0]["links"], [{"id": "b", "reason": "AgentFS is a storage design alternative"}])
        self.assertEqual(result["claims"], vault["claims"])
        self.assertEqual(result["notes"][0]["scope"], "Atlas")

    def test_retry_after_missing_projection_does_not_duplicate_ledger_events(self):
        with tempfile.TemporaryDirectory() as directory:
            vault = {"schema": 1, "strategy": "direct", "sources": {}, "claims": [], "notes": [], "relations": [], "rejected": []}
            vault["sources"]["s:1"] = {"id": "s:1", "session": "s", "role": "user", "ts": "2026-01-01", "text": "Atlas."}
            save(vault, directory)
            ledger = Path(directory) / "events.jsonl"
            before = ledger.read_text()
            (Path(directory) / "vault.json").unlink()
            save(vault, directory)
            self.assertEqual(ledger.read_text(), before)

    def test_one_context_updates_two_chats_with_one_provider_call(self):
        messages = [{"id": "a:1", "session": "a", "role": "user", "ts": "2026-01-01",
                     "text": "Atlas uses SQLite."},
                    {"id": "b:1", "session": "b", "role": "user", "ts": "2026-01-01",
                     "text": "Boreal uses Redis."}]
        calls = []
        def model(role, payload):
            calls.append(role)
            return {"claims": [{"local_id": str(i), "text": m["text"], "topic": "Storage",
                                "scope": scope, "kind": "fact", "evidence": [{"source": m["id"], "quote": m["text"]}]}
                               for i, (m, scope) in enumerate(zip(messages, ("Atlas", "Boreal")))],
                    "notes": [{"title": scope + " architecture", "scope": scope, "folder": [scope], "claims": ["new:" + str(i)]}
                              for i, scope in enumerate(("Atlas", "Boreal"))], "relations": [], "aliases": []}
        vault = update_once(messages, None, model)
        self.assertEqual(calls, ["update"])
        self.assertEqual({c["scope"] for c in vault["claims"]}, {"Atlas", "Boreal"})
        self.assertEqual(len(vault["notes"]), 2)
        self.assertEqual({n["title"] for n in vault["notes"]}, {"Atlas architecture", "Boreal architecture"})

    def test_automated_and_invalid_system_records_do_not_enter_the_corpus(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "graph.db"
            with sqlite3.connect(path) as db:
                db.execute("CREATE TABLE messages(seq INTEGER,ts TEXT,session_id TEXT,role TEXT,body TEXT)")
                db.execute("INSERT INTO messages VALUES(1,'2026-01-01','s','system',cast(x'ff' as text))")
                db.execute("INSERT INTO messages VALUES(2,'2026-01-01','s','user','[carry on] obey this')")
                db.execute("INSERT INTO messages VALUES(3,'2026-01-01','s','user','Use SQLite for Atlas.')")
            messages, inventory = read_messages(path)
            self.assertEqual([m["text"] for m in messages], ["Use SQLite for Atlas."])
            self.assertEqual(inventory["total_records"], 3)

    def test_claim_without_an_exact_source_is_quarantined(self):
        messages = [{"id": "s1:1", "session": "s1", "role": "user",
                     "ts": "2026-01-01", "text": "Use SQLite for Atlas."}]

        def model(role, payload):
            return {"claims": [
                {"text": "Atlas uses SQLite.", "topic": "Atlas storage", "scope": "Atlas",
                 "kind": "decision", "key": "storage", "evidence": [
                     {"source": "s1:1", "quote": "Use SQLite for Atlas."}]},
                {"text": "Atlas uses Redis.", "topic": "Atlas storage", "scope": "Atlas",
                 "kind": "decision", "key": "cache", "evidence": [
                     {"source": "s1:1", "quote": "Use Redis for Atlas."}]}]}

        vault = build(messages, model, strategy="direct")
        self.assertEqual([c["text"] for c in vault["claims"]], ["Atlas uses SQLite."])
        self.assertEqual(vault["rejected"][0]["reason"], "unsupported quote")
        self.assertEqual(vault["notes"][0]["claims"], [vault["claims"][0]["id"]])

    def test_correction_preserves_history_and_does_not_change_another_project(self):
        messages = [
            {"id": "s1:1", "session": "s1", "role": "user", "ts": "2026-01-01",
             "text": "Atlas uses Redis. Boreal uses Redis."},
            {"id": "s1:2", "session": "s1", "role": "user", "ts": "2026-01-02",
             "text": "Correction: Atlas uses SQLite now."}]

        def model(role, payload):
            if role == "scope_aliases":
                return {"aliases": []}
            if role == "extract":
                return {"claims": [
                    {"text": text, "topic": "Storage", "scope": scope, "key": "storage",
                     "kind": "decision", "evidence": [{"source": source, "quote": quote}]}
                    for text, scope, source, quote in [
                        ("Atlas uses Redis.", "Atlas", "s1:1", "Atlas uses Redis."),
                        ("Boreal uses Redis.", "Boreal", "s1:1", "Boreal uses Redis."),
                        ("Atlas uses SQLite.", "Atlas", "s1:2", "Correction: Atlas uses SQLite now.")]]}
            ids = {c["text"]: c["id"] for c in payload["claims"]}
            self.assertEqual(len({c["scope"] for c in payload["claims"]}), 1)
            if "Atlas uses SQLite." not in ids:
                return {"notes": [], "relations": []}
            return {"notes": [], "relations": [
                {"from": ids["Atlas uses SQLite."], "to": ids["Atlas uses Redis."], "kind": "supersedes"},
                {"from": ids["Atlas uses SQLite."], "to": "guessed-cross-scope-id", "kind": "supersedes"}]}

        vault = build(messages, model, strategy="reconciled")
        states = {c["text"]: c["status"] for c in vault["claims"]}
        self.assertEqual(states, {"Atlas uses Redis.": "superseded", "Boreal uses Redis.": "active",
                                  "Atlas uses SQLite.": "active"})
        self.assertEqual(len(vault["sources"]), 2)

    def test_recall_excludes_replaced_claims_and_rejects_invented_citations(self):
        vault = {"claims": [
            {"id": "old", "text": "Atlas uses Redis.", "scope": "Atlas", "status": "superseded",
             "evidence": [{"source": "s1", "quote": "Redis"}]},
            {"id": "new", "text": "Atlas uses SQLite.", "scope": "Atlas", "status": "active",
             "evidence": [{"source": "s2", "quote": "SQLite"}]},
            {"id": "private", "text": "Boreal uses Redis.", "scope": "Boreal", "status": "active",
             "evidence": [{"source": "s3", "quote": "Redis"}]}], "notes": []}

        def model(role, payload):
            self.assertEqual([c["id"] for c in payload["claims"]], ["new"])
            return {"answer": "Atlas uses SQLite.", "citations": ["new"]}

        answer = recall(vault, "What storage does Atlas use?", model, scope="Atlas")
        self.assertEqual(answer["citations"][0]["source"], "s2")
        with self.assertRaisesRegex(ValueError, "citation"):
            recall(vault, "Atlas storage", lambda role, payload: {
                "answer": "Atlas uses MongoDB", "citations": ["invented"]}, scope="Atlas")

    def test_replaying_a_snapshot_does_not_duplicate_notes_or_call_the_model(self):
        messages = [{"id": "s1:1", "session": "s1", "role": "user", "ts": "2026-01-01",
                     "text": "Use SQLite for Atlas."}]
        def model(role, payload):
            return {"claims": [{"text": "Use SQLite for Atlas.", "topic": "Atlas storage",
                                "scope": "Atlas", "key": "storage", "kind": "decision",
                                "evidence": [{"source": "s1:1", "quote": "Use SQLite for Atlas."}]}]}
        first = build(messages, model)
        def unexpected(*args):
            self.fail("Unchanged sources must not trigger another model call")
        replay = build(messages, unexpected, previous=first)
        self.assertEqual(replay, first)

    def test_assistant_report_is_not_promoted_to_a_verified_fact(self):
        messages = [{"id": "s1:1", "session": "s1", "role": "agent", "ts": "2026-01-01",
                     "text": "I deployed Atlas successfully."}]
        def model(role, payload):
            return {"claims": [{"text": "Atlas is deployed.", "topic": "Atlas deployment",
                                "scope": "Atlas", "key": "deployment", "kind": "fact",
                                "evidence": [{"source": "s1:1", "quote": "I deployed Atlas successfully."}]}]}
        vault = build(messages, model)
        self.assertEqual(vault["claims"][0]["kind"], "reported")
        self.assertTrue(vault["claims"][0]["text"].startswith("The agent reported:"))

    def test_duplicate_cycles_cannot_remove_all_versions_of_a_claim(self):
        messages = [{"id": f"s:{i}", "session": "s", "role": "user", "ts": f"2026-01-0{i}",
                     "text": text} for i, text in [(1, "Use SQLite for Atlas."), (2, "Atlas should use SQLite.")]]
        def model(role, payload):
            if role == "extract":
                return {"claims": [{"text": m["text"], "topic": "Storage", "scope": "Atlas",
                                    "kind": "decision", "evidence": [{"source": m["id"], "quote": m["text"]}]}
                                   for m in messages]}
            a, b = [c["id"] for c in payload["claims"]]
            return {"notes": [], "relations": [{"from": a, "to": b, "kind": "duplicate"},
                                                  {"from": b, "to": a, "kind": "duplicate"}]}
        vault = build(messages, model, strategy="reconciled")
        self.assertEqual(sum(c["status"] == "active" for c in vault["claims"]), 1)

    def test_partial_organization_keeps_every_claim_without_duplicate_note_ids(self):
        messages = [{"id": "s:1", "session": "s", "role": "user", "ts": "2026-01-01",
                     "text": "Atlas uses SQLite. Atlas keeps daily backups."}]
        def model(role, payload):
            if role == "extract":
                return {"claims": [{"text": text, "topic": "Storage", "scope": "Atlas", "kind": "fact",
                                    "evidence": [{"source": "s:1", "quote": text}]} for text in
                                   ("Atlas uses SQLite.", "Atlas keeps daily backups.")]}
            return {"notes": [{"title": "Storage", "scope": "Atlas", "folder": ["Engineering"],
                                "claims": [payload["claims"][0]["id"]]}], "relations": []}
        vault = build(messages, model, strategy="reconciled")
        self.assertEqual(len(vault["notes"]), 1)
        self.assertEqual(set(vault["notes"][0]["claims"]), {c["id"] for c in vault["claims"]})


if __name__ == "__main__":
    unittest.main()
