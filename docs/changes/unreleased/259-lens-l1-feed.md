---
kind: internal
title: one feed reduces events to entries, and the chat is the first surface on it
pr: 259
surface: [chat]
invalidates:
  - >-
    Live event ingestion in internal/tui3 was written twice: the chat's family on
    *app (formTool, announceTool, beginTool, claimAnnounced, closeTool,
    settleCompaction, closeLive, resolveUnfinished, the thought settling in
    thinking.go) and an eleven-function hand-copy in room.go (roomFormTool,
    roomCloseTool, and the rest). There is now ONE reducer — the `feed` type in
    internal/tui3/feed.go — and the chat runs on it. The room's mirrors are still
    there and still what a task room uses; they are deleted in L2, which is when
    a room stops being the copy that falls behind.
  - >-
    A surface that needed ingestion to do something extra used to copy the
    ingestion. It installs a feedHooks function instead: the chat's propose_task
    spawn card, its refusal read, and its ambient-count cache are three hooks set
    in newApp, and a feed with no hooks installed still reduces.
  - >-
    a.entries, a.live, a.think and a.turn are still spelled exactly that way and
    still mean exactly that — the fields moved onto an EMBEDDED feed, not into a
    named one, so no reader had to be respelled. Anything that remembers them as
    plain fields on the app struct is looking in the wrong file, not at the wrong
    name.
---

Lane L1 of docs/design/lens/DESIGN.md, Decision 1, and nothing a person can see
moved: no rendering, no folding, no steering, and the chat keeps its
oldest-of-name tool-end pairing until ruling 3 lands in L2. The event pump keeps
everything that is not entry work — EventToolEnd still prefetches the file that
was just written, EventCompacted still rebases the scrollback and re-reads the
meter — because that is the difference between two pages, where the row a call
leaves behind is not.
