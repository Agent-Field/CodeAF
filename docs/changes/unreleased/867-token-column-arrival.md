---
kind: fixed
title: Opening a task's page or switching tabs no longer counts the token figures up from zero
pr: 867
surface: [chat, docs]
invalidates:
  - "Every page opened its token column at nothing and walked the first reading up from zero with the glow lit, so opening a task from the rail or taking a conversation up from a tab replayed the climb to the same figures on every visit. A figure coming onto the screen is now drawn whole and unlit on the first frame (tokencol.go's chaseFigure); only a rise after it walks and glows. A climbing, lit figure now always means something actually arrived."
---

The owner switched between two running tasks and watched `↑ 79.6k  ↓ 37.6k`
count up and light up each time, and read a page being built as a task being
busy. The `+3.4k` receipt already treated a first reading as a figure arriving
rather than growing; the walk and the glow now keep the same rule.
