---
kind: changed
title: Clean background process exits wait silently for the next turn
pr: 0
surface: [engine]
invalidates:
  - "Was true: every background process exit woke the agent to write a reply. Is true now: a plain process exiting 0 puts its complete ending into ambient context for the next turn, without asking the agent to relay it; non-zero exits still wake."
  - "Was true: bash had no explicit completion notification argument. Is true now: notify:true asks for a waking ending even on exit 0, when the person asked to be told when it ends. Commands whose call or task worker is still waiting retain their waking ending and release; task and render goroutine endings are unchanged."
---
