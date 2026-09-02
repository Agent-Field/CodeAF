---
kind: added
title: one switch keeps the full record of a run, in a folder of its own that nobody else can read
pr: 361
surface: [chat, engine, docs]
invalidates:
  - "There was no debug switch: two environment pins in the settings registry, no flag, no slash command and no page. There is one switch now with three doors that mean the same record — AFORGE_DEBUG in a shell, --debug on `aforge chat`, `aforge do` and `aforge exec`, and /debug inside a conversation, which cannot be turned off again."
  - "The three doors do not have the same reach, and this is the line to hold on to: with the pin or the flag, every conversation this process holds is recorded, each into its own folder; /debug records only the conversation you typed it in. One process can hold several conversations, so a /debug that flipped a process-wide switch would write another person's prompts, files and replies into a folder they never asked for. The run id on the context is the scope of the switch, and a context carrying no run of its own records only under the pin or the flag."
  - "AFORGE_CALL_LOG_BODIES was the only way to get bodies, and it put them in calls.jsonl. It still does that, and it now ALSO means AFORGE_DEBUG for one release; the bodies are moving out of the call log and into the record, in a later change."
  - "The state root had one place aforge wrote a record of what it did, logs/calls.jsonl. There is a second now: logs/trace/<run-id>/, one folder per run, holding run.json — which door opened the run, which model was asked for, which build, which folder, when — and, as the pieces that record them land, the call bodies, the tool calls and the choices a run made. Folders are 0700 and files 0600, and a run with the switch off creates nothing at all."
  - "Nothing on disk said which run a record belonged to. Every door now mints a run id at the moment it opens — sixteen hex characters, on the context — and every line and file the record writes carries it. Every record also carries the piece of work it came from where the context names one (trace.WithNode), so a run reads as the plan it was rather than a pile of calls in time order."
  - "A credential was only ever caught by its shape — an authorization field, a Bearer scheme, an sk-… token — which is the shapes somebody thought of. Every door now registers the credentials this profile is actually configured with (config.Credentials walks the settings rows marked Secret), so a key with no recognisable shape at all — a Google AIza…, a Groq gsk_…, a self-hosted endpoint's plain token — is redacted from the record too, wherever a provider echoes it back."
---

Retention only ever removes a run that is OVER: a folder whose run still has a
recorder open is skipped however low `AFORGE_TRACE_KEEP` is set, so a second
conversation opening cannot delete the record of the one being debugged. A call
id is written to disk only as letters, digits, `_` and `-` — a tool call carries
the id the model wrote, and an id spelled as a path is stored under its hex form
instead of escaping the run's folder.

The record has a size law that is deliberately not a rotation: 256 MB per run
(`AFORGE_TRACE_MAX_MB`), and at the cap the run writes one last line saying so and
keeps what it had, rather than making room by dropping the start of the run —
which is where the choice that went wrong usually is. Twenty runs are kept
(`AFORGE_TRACE_KEEP`), pruned oldest-first and whole.
