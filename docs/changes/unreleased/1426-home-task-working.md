---
kind: fixed
title: Home conversations keep their working mark while tasks run
pr: 1426
surface: [chat, docs]
invalidates:
  - Home read an owned conversation's answering flag alone, so tasks could keep running after its working mark disappeared; Home now shares the tab's live working state, including tasks and background commands, until the last work settles. A task waiting for a decision no longer hides its conversation's other running work on Home, and a conversation whose first act is a `/task`, before it has a name, is marked too.
  - A stale Home task count could keep an owned conversation marked working after its tasks finished; the conversation's live status now takes precedence over the saved count.
---
