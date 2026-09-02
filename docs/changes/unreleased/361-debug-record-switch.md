---
kind: added
title: one switch keeps the full record of a run, in a folder of its own that nobody else can read
pr: 361
surface: [chat, engine, docs]
invalidates:
  - "There was no debug switch: two environment pins in the settings registry, no flag, no slash command and no page. There is one switch now with three doors that mean the same thing — AFORGE_DEBUG in a shell, --debug on `aforge chat`, `aforge do` and `aforge exec`, and /debug inside a conversation, which turns the record on for the rest of that session and cannot be turned off again."
  - "AFORGE_CALL_LOG_BODIES was the only way to get bodies, and it put them in calls.jsonl. It still does that, and it now ALSO means AFORGE_DEBUG for one release; the bodies are moving out of the call log and into the record, in a later change."
  - "The state root had one place aforge wrote a record of what it did, logs/calls.jsonl. There is a second now: logs/trace/<run-id>/, one folder per run, holding run.json — which door opened the run, which model was asked for, which build, which folder, when — and, as the pieces that record them land, the call bodies, the tool calls and the choices a run made. Folders are 0700 and files 0600, no key in any shape is ever written into them, and a run with the switch off creates nothing at all."
  - "Nothing on disk said which run a record belonged to. Every door now mints a run id at the moment it opens — eight hex characters, on the context — and every line and file the record writes carries it."
---

The record has a size law that is deliberately not a rotation: 256 MB per run
(`AFORGE_TRACE_MAX_MB`), and at the cap the run writes one last line saying so and
keeps what it had, rather than making room by dropping the start of the run —
which is where the choice that went wrong usually is. Twenty runs are kept
(`AFORGE_TRACE_KEEP`), pruned oldest-first and whole.
