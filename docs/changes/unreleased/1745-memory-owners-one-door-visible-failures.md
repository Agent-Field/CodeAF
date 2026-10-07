---
kind: added
title: memories belong to an owner, every write walks one door, and failure is visible
pr: 1745
surface: [chat, engine, docs]
invalidates:
  - "The memory `scope` column was a label, not an owner: the router's shortlist, the dedup search and a forget query were unfiltered, so a project's memory could surface — and be updated — in an unrelated project. Every memory now carries an `owner` (`user`, `machine`, `project:<key>`), every read that can put a memory in front of a model filters on it, and the reads without one are refused rather than allowed to widen."
  - "A project's identity was the word `project` and nothing else. It is now proved from the workspace: the git origin's normalized URL (an SSH clone and an HTTPS clone of one repository are one project), or the canonical path when there is no remote. A memory whose project cannot be proved is quarantined, never re-attributed to the person at large; rows the migration can prove — through their source session's own workspace record — are re-homed to that project."
  - "The routed `remember…` command wrote through AddMemory directly, the one door with no duplicate check, so the same preference said three ways through three mouths could be three rows. Every mouth — /remember, the remember tool, the routed command, the post-turn extraction and the legacy import — now walks one door with one dedup rule, and both outcomes (kept, skipped) are journaled."
  - "Memory writes failed silently: an extraction the settle could not finish, a store refusal, an import that stopped halfway — nothing anywhere recorded it. Failures are now journaled (`memory_write_failed`, with the door and the reason), said once per five-minute window as a dim line, and fall back to `v3/memory-failures.log` when the store itself is what failed."
  - "The legacy memory.md import renamed the file whether or not the rows landed, and ignored the scanner's own error. The rename now happens only after every row landed; a scan error or a failed row leaves the file in place and retries later, and the retry is idempotent — kept lines are skipped, never duplicated."
  - "An explicit save needed the small memory model: when the decider could not answer, /remember lost the words. The decider's failure is journaled and the save still happens through the store's own dedup door, which needs no model."
  - "Turning memory off also closed the conversation index, so `search_conversations` disappeared with it — a feature nobody asked to lose. The conversation index is now its own handle on the same file: the memory row decides what is REMEMBERED, never what is SEARCHABLE, and a memory-off conversation still indexes itself and still carries the search verb."
  - "The FTS5 index was assumed. It is now proved by a probe at open: a build whose SQLite has no FTS5 opens with memory intact and only the lexical tier quiet, with no install step and no fallback data store."
  - "The memory listing could not page: a caller asked for the first N and stopped. The store has keyset pagination (`MemoryPage`) on the same newest-touched ordering, with a cursor that survives writes landing between pages."
  - "Sync was promised by the schema and delivered by nothing. The store now exports memory events after a cursor (`ExportMemoryEvents`) and folds a batch idempotently (`ApplyMemoryEvents`) with a receiver's owner policy: adds re-deliver as skips, tombstones never resurrect, first-applied wins on concurrent supersession, and refused owners are skipped, never widened. No transport ships with this — PR #1738's pairing carries it later."
---

The memory architecture review (docs/memory-architecture.md) found four
structural defects in the pipeline — scope leak, split write doors, silent
failure, lossy import — and this change lands the substrate that closes them.
The architecture document is updated in the same change to say what shipped
and what is deliberately deferred (embedding vectors, team owners, the sync
transport).
