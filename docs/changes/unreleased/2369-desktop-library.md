---
kind: added
title: Desktop Library shares knowledge guides and sources with the terminal
pr: 2369
surface: [app, engine, chat, docs]
invalidates:
  - The desktop previously exposed only saved memory lines; it can now browse the terminal’s knowledge guides through explicit Library commands.
  - The default knowledge model previously followed an unversioned Flash name; it now starts on DeepSeek v4.1 Flash 1017 and retains a saved choice.
  - Pausing learning previously blocked explicit desktop questions too; Home can now answer from saved guides without resuming capture, while automatic recall remains disabled.
  - Guide clicks previously kept another desktop tab; ordinary Library navigation now follows pages in the current tab, with explicit new-tab actions.
---

Library supports source-backed reading and recall, revision-checked edits and deletion,
source navigation, saved-reply capture, and choices for disagreements. Choices retain
replacement history and leave unrelated disagreements available. Rejected accounts
cannot resurface through older notes; their source chats remain reachable from Changes. No new consent switch
or independent knowledge store is introduced.
Library questions deferred to Later survive restarts and remain available while learning is paused.
The desktop sidebar separates the review queue from collapsed collections; long guide summaries wrap without overlapping other guides.

The pre-merge complexity gate also exposed seven functions already over its ceiling on
app/v3. Their existing admission, wait, checkpoint, worktree, schema and bounded-page
phases are now separate private helpers; limits, locking and user-visible behavior stay the same.
