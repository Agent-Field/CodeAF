---
kind: fixed
title: Keep chat startup and resume responsive during storage and engine delays
pr: 1948
surface: [chat, engine, docs]
invalidates:
  - Recovery notices previously waited behind storage capture; chat can now draw and accept typing while the initial capture is pending.
  - The resume picker previously read saved conversations on the input loop; it now loads beside the loop while filtering and Escape remain available.
---

Large All teams overviews allocate fewer membership copies and skip offscreen
border composition while retaining card heights and navigation targets.
Performance fixtures distinguish visible terminal response from engine setup
and model latency, and keep capture barriers for tools and movement.
