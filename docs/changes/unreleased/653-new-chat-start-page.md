---
kind: added
title: Open a New chat start page from plus without starting work
pr: 653
surface: [chat]
invalidates:
  - "The chat header had no plus control; creating a conversation required /new. Plus now opens a distinct New chat view with recent conversations and its own composer; creation happens on first submit."
  - "Closing or switching from a start page had no separate draft owner. Its draft now stays with the window, while existing conversation and task drafts retain their owners. The start-page draft is not crash-persisted."
  - "Tabs did not display live status. They now have a fixed status slot for a human question or a running turn/task, read from existing UI state and watcher caches without per-frame engine or disk calls."
---

The plus control and New chat tab have separate hover/click targets. Escape or
closing New chat restores the previous page; opening a recent chat sends nothing.
Task return preserves the task kind and conversation identity, including a fresh
read-only connection for another conversation's task. First-send creation failures
leave the page and both drafts intact. An unnamed old chat with an unsent draft
refuses creation rather than moving those words to a different conversation.

A selected compact paste opens its editor before any conversation is created.
Commands chosen from the start-page menu run in the newly created conversation.
The start page shows its own footer instead of the old chat's costs and identity.
Task placeholders remain on the composer row when an attachment tray precedes it.

A human question takes priority over running work. Internal waits and countdowns
do not claim the person must respond. Status marks do not animate or change tab
width. Actual switches on a shared engine connection still end the previous
conversation; opening, cancelling, and dismissing a view do not.
