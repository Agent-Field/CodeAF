---
kind: fixed
title: a `read` of lines the conversation already holds answers with a pointer
pr: 1015
surface: [engine, docs]
invalidates:
  - "`read` fetched from disk on every call, no matter what was already in the transcript — measured: one planning chat issued eighteen reads of the same file as seven sliced ranges. A range fully covered by bytes the conversation still carries, with the file's stamp unmoved, now comes back as the one-line pointer `[already read] <path> lines X–Y …`, and a partial overlap or a changed file still fetches whole."
  - "`read`'s tool description priced only tokens, so a model slicing a file small had no reason to broaden it. It now states that each call is one round trip and that a range already in the conversation answers as a pointer, the system prompt's BELT_FACTS area teaches the pointer sentence beside the stub-pointer line, and the chat manual's `what-i-can-do` page quotes the pointer and the fresh-after-a-change rule."
---

The ledger (internal/session/heldreads.go) is per conversation and in memory
only. A span counts as held only while its transcript message still carries the
bytes verbatim — a stubbed or folded result drops out of coverage by itself —
and a changed stamp retires the file's whole entry, never merging two
generations of the file into coverage no version ever had.
