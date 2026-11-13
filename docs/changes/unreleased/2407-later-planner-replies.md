---
kind: fixed
title: Refuse lost later planner replies and give repairs output room
pr: 2407
surface: [engine, chat]
invalidates:
  - "Later cut-off replies and empty syntax repairs could silently settle as no change. They now leave a failure note while keeping the current plan and continuing the run."
  - "Syntax repairs reused the original output cap even when it was smaller than the reply they had to repeat."
---

Opening and later syntax repairs request max(8192, 2 × reported completion
tokens), preserving a larger requested recovery cap. This reuses the opening
retry rule from #2404 without raising any limit constant. Provider and spending
checks still apply. Complete explicit `{}` later replies remain no change.
