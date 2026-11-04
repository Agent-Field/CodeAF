---
kind: fixed
title: Bound automatic retries of ordinary memory builds
pr: 2291
surface: [chat, docs]
invalidates:
  - "Ordinary memory builds previously retried unchanged failed input without a limit. They now stop after three compilation attempts in total, with the count retained across restarts."
---

Ordinary memory builds again report `Completed progress is retained; retrying
automatically.` while attempts remain. After three failed compilation attempts
in total, they stop and say to use Build or add new sources. Changed input or
Build starts a fresh count; success clears it. Attempts survive restarts, and
each can make several paid requests. Old databases with `failed_input` still
load. Memory off still prevents compilation.
Factory sources remain excluded from independent background compilation, with
failures, ownership and usage accounting handled by the owning job.

Documentation follows dev wording while preserving guidance for capabilities
retained in this tree, including desktop reply suggestions, question Hold,
conversation commands, service cleanup and local saving recovery.
