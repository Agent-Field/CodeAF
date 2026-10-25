---
kind: fixed
title: Written and edited files appear in deliverables with stable identities
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "The write observer discarded overwrites, and edit had no observer, so a successful mutation could leave Files empty or leave its content hash stale. Both tools now record their exact authored bytes inside deliverable roots, updating the existing identity on rewrites."
  - "Automatic recording accepted only named document and media extensions. Other file types are now recorded as files, including task-owned writes, while paths outside deliverable roots remain excluded."
---

Diagnosis: the desktop opener already carries the process-wide artifacts index
and creates a journal for a plain home-folder conversation; neither requires
Git. The missing paths were the creation-only guard in `recordWritten`, the
absent observer on `edit`, and the extension filter. Index and journal wiring
are pinned by a startup regression rather than duplicated with a fallback.

Successful writes, appends and edits append a full-snapshot update for the same
session, path and source, retaining the deliverable ID, creation time and kept
state while refreshing the hash and descriptor identity. Updates notify the
surface after the index append. Workers use their owning conversation's index.
Media rewrites publish their current preview source on the update row; Keep and
Remove restate that source without publishing it twice.

Shell coverage stays limited to the existing reliable signal: pictures declared
in `bash.outputs`. Ordinary shell-created documents and undeclared outputs are
left unrecorded; no filesystem scan is added.

Review diagnosis: Keep and writes selected snapshots outside the append lock;
directory aliases could therefore also mint duplicate identities. State changes
must merge into the latest snapshot atomically. Settlement keeps branch
provenance but adds a checkout identity, which later checkout writes must match.
In-place worker media writes need the same fresh preview facts as owner writes.
First writes must use an incremental canonical-path index rather than rescan
history, and an expired lookup attempt must retain progress and finish recording.


The fixes serialize write upserts and state merges at the ledger seam. Settled
checkout identities match later owner writes without combining unmerged branch
sources. Owner and in-place worker writes share byte-based media fact creation.
A path-keyed offset index reads history once, incrementally catches up and retries
expired bounded attempts until recording completes. Deterministic regressions
cover all five findings, and first-write benchmarks include unrelated history.
