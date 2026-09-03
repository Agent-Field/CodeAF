---
kind: changed
title: a task's page runs the conversation's reducer, and learns durations, retries and notices
pr: 337
surface: [chat]
invalidates:
  - >-
    A task's page never said how long a call took. EventToolFinished was not in
    room.go's switch at all, so a node's rows carried no figure; they carry the
    same `1.4s` / `12s` / `2m04s` the conversation's rows do, written the moment
    the call itself reports finishing rather than when its result comes back.
  - >-
    A retry inside a task drew nothing: the cut attempt's half-answer stayed on
    the page above the live one with no explanation. A task's page now drops the
    dead attempt's text and forming rows and writes the same dim line the
    conversation gets — `the model went quiet mid-reply — asking again`.
  - >-
    A notice, a nudge and a guardian line were dropped on the floor inside a
    task. All three now reach a node's page as the same dim `· ` lines the
    conversation draws.
  - >-
    The chat paired a tool END with the OLDEST live row of that tool's name. It
    now prefers the row whose arguments match and falls back to oldest, which is
    the rule a task's page already had: two `bash` calls running at once resolve
    onto the call each result is actually about, so the one that finishes first
    no longer takes the other's duration and output.
  - >-
    room.go held eleven hand-copies of the chat's event ingestion (roomFormTool,
    roomAnnounceTool, roomBeginTool, roomCloseTool, roomClaimRunning,
    roomSettleCompaction, roomSay, roomThink, roomSettleThought,
    roomCollapseThought, roomCloseLive, roomResolveUnfinished). They are deleted.
    taskRoom embeds the same `feed` the app does, so `room.entries`, `room.live`,
    `room.think` and `room.turn` still mean exactly what they meant — the fields
    moved house, not name — and a new event kind is wired once, in feed.ingest.
  - >-
    app.appendText and thinking.go's appendThought are gone as methods on the
    app; they are feed.say and feed.reason (the method cannot be called `think`
    because feed.think is the index of the block it grows). app.said, app.note,
    app.noteWritten, app.dropLive and app.dropRetryingFormingTools moved to the
    feed too — every call site is spelled the same, because the feed is embedded.
  - >-
    app.settledTurn, app.mdAt and app.pendingReplyTags are fields on the embedded
    feed now rather than on the app struct. `a.settledTurn`, `a.mdAt` and
    `a.pendingReplyTags` still read and write the same values; a room has its own
    three, which is what lets one reducer serve both pages.
---

Lane L2 of docs/design/lens/DESIGN.md — Decision 1 applied to the task page,
Decision 4's event list, and ruling 3. The salience contract lands with it:
`internal/tui3/salience_test.go` walks every `session.EventKind` — read back out
of internal/session's own const block, so a kind added there and forgotten here
fails the build — through both pumps and asserts the room has a block for
whatever the conversation drew. **THE LENS MAY LOWER SALIENCE; IT MAY NOT DROP A
FACT.** The exclusions are listed with their reasons and each one is itself
tested: an event a node cannot receive must leave a node's page untouched.

What is deliberately unchanged: a consent question and a task proposal are still
the conversation's alone, because a node's policy never asks this keyboard; a
node's spend is still folded by its pilot and never twice; and a room's steer is
still an ordinary line rather than an elbow, which is lane L3.
