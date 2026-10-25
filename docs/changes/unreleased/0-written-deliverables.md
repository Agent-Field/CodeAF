---
kind: fixed
title: Conversation writes and edits record deliverables with stable identities
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "The conversation's write observer discarded overwrites, and edit had no observer, so a successful mutation could leave Files empty or its content hash stale. Its own writes, appends and edits now record their exact authored bytes inside deliverable roots and retain the ID on rewrites."
  - "Automatic conversation recording accepted only named document and media extensions. Other file types are now recorded as files, while paths outside deliverable roots remain excluded."
  - "Chat /files limited raw rows before deduplicating paths, so repeated rewrites hid other files. Its 200-item limit now counts distinct paths from the recent index rows."
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

An absent file used to choose a lexical mutation queue, so a symlinked directory
and its target could write and record out of order. Resolving the parent directory
before disk mutation serializes those aliases. The regression pauses the first
writer before ledger acquisition and uses different bytes for the second writer.

Chat /files deduplicates paths before applying its 200-item limit, preserving
other files after repeated rewrites. The existing recent-index window remains.
An incremental path index avoids rescanning history on every new authored path;
expired bounded lookups retain progress and finish recording.

Shell coverage remains pictures declared in `bash.outputs`; no filesystem scan
is added. Task recording, worktree media and landing/settlement retain their
creation-only behavior from the base branch.

Out of scope: task/worker deliverables unchanged from app/v3
