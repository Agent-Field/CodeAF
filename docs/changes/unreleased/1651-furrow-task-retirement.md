---
kind: fixed
title: Completed task forks retire their Furrow timelines before their directories
pr: 1651
surface: [engine]
invalidates:
  - "Task cleanup treated Furrow's keep-files option as timeline retirement. It retains history; completed task forks now retire before their files disappear."
  - "A sweep previously removed the task checkpoint even when Furrow cleanup failed. It now preserves the copy and checkpoint for a later retry."
---

Work kept for review retains its fork and timeline. Failed cleanup after a
successful merge is reported and retried from the saved task record when the
conversation is closed. Previously orphaned timelines are not automatically
purged.
