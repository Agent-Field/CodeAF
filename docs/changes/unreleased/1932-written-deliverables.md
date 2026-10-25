---
kind: fixed
title: Conversation writes and edits record deliverables with stable identities
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "The conversation's write observer discarded overwrites, and edit had no observer, so a successful mutation could leave Files empty or its content hash stale. Its own writes, appends and edits now record their exact authored bytes inside deliverable roots and retain the ID on rewrites."
  - "Automatic conversation recording accepted only named document and media extensions. Other file types are now recorded as files, while paths outside deliverable roots remain excluded."
  - "Chat /files limited raw rows before deduplicating paths, so repeated rewrites hid other files. Its 200-item limit now counts distinct paths from the recent index rows."
  - "Keep and write recording used only a process-local mutex, allowing a desktop Keep to restore stale source metadata after another process recorded an overwrite. An OS file lock now coordinates their lookup-through-append transactions across processes."
  - "A write or edit could adopt a shared-only identity and prevent an interrupted share from reconciling. It now creates a separate authored identity and leaves the shared origin and version untouched."
  - "Media, task, share and plain artifact appends could bypass the OS lock held by a deliverable mutation. Every index appender now uses one locked append funnel."
---

The desktop opener already carries the process-wide artifacts index and creates
a journal for a plain home-folder conversation; neither requires Git. The
creation-only guard, missing edit observer and extension filter prevented that
conversation's own authored files from reaching Files.

Successful conversation writes, appends and edits record the same session and
canonical path, retaining the deliverable ID, creation time and kept state while
refreshing the hash and descriptor identity. Task-owned rows remain separate.
Media rewrites publish a current preview source. Generic files sniff at most 512
authored bytes for MIME; a generic binary or text sniff retains a known MIME.
When a path has both authored and independently shared identities, the authored
identity wins even if the share is newer; the independent share stays unchanged.
When only a shared identity exists, the first conversation write or edit creates
an authored identity rather than converting the shared one. A share retry can
still find its original planned identity, origin and version after that mutation.
Legacy rows with unknown authorship also remain unchanged; only an authored
identity is eligible for write/edit refresh.

Every index append now goes through one funnel that holds both the process mutex
and an OS file lock. Keep and write recording prepare their updates inside that
funnel, holding the same locks from lookup through append without double locking.
Media, task, share and plain artifact appends participate in the same exclusion.
The structural guard checks append-flag references throughout each source file,
including package-level constants and variables, so moving flags out of a
function cannot hide an unlocked writer. Negative fixture packages cover
literal, constant and variable flags; the real locked funnel remains accepted.
The existing cross-platform lock uses flock on Unix and LockFileEx on Windows.
It coordinates processes sharing one local index; separate hosts with separate
index files are out of scope. A process
mutex alone did not protect a desktop Keep from a separate conversation host.
The mutation reads the latest snapshot, merges only the caller's fields and
appends before releasing the locks.
A Keep paused after copying old bytes cannot restore that source's old hash,
descriptor, attribution or preview after an overwrite. Its copy metadata still
identifies exactly the bytes it copied.

The OS lock preserves complete snapshot records: the current reader, incremental
path cache and existing clients all treat the latest update as a complete row.
Partial field records would require a new reader contract for those clients.
Old index files remain readable and the deliverables wire output is unchanged.
Every process mutating an existing row must use the locking writer; older
processes must be restarted with the update to participate in that coordination.

An absent file used to choose a lexical mutation queue, so a symlinked directory
and its target could write and record out of order. Resolving the parent directory
before disk mutation serializes those aliases. The regression pauses the first
writer before ledger acquisition and uses different bytes for the second writer.
Dangling leaf links also resolve their target before choosing that queue, with
at most 40 symlink hops so cycles cannot loop in queue selection. Relative,
absolute and chained links serialize their writes and observers with the absent
target's path. Target components resolve before `..` is interpreted, including
when the link or its target passes through a directory alias.

Chat /files deduplicates paths before applying its 200-item limit, preserving
other files after repeated rewrites. The existing recent-index window remains.
An incremental path index avoids rescanning history on every new authored path;
expired bounded lookups retain progress and finish recording.

Shell coverage remains pictures declared in `bash.outputs`; no filesystem scan
is added. Task recording, worktree media and landing/settlement retain their
creation-only behavior from the base branch.

Out of scope: task/worker deliverables unchanged from app/v3
