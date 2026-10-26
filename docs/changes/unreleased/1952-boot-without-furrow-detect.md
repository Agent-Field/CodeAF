---
kind: fixed
title: Show the first frame without waiting on furrow detection
pr: 1952
surface: [chat, engine, docs]
invalidates:
  - Building a session ran furrow to find out whether the folder is watched; it now starts that question in the background and the session never waits for it.
  - The workspace snapshot, restore, fork and merge tools were decided when the belt was built; they now appear on the first belt built after furrow answers, so a session asked for its belt before then goes without them for that one belt.
---

A cold launch used to block the engine hello behind `furrow --version` and
`furrow status`, two to three seconds on a folder furrow was never pointed at.
Each session now asks furrow from a goroutine it owns and joins at close, and the
belt reads the remembered answer. A cold engine on a real profile draws its first
frame in about 0.45 s instead of 3.87 s. An unwatched folder still gets none of
the four tools, and a cancelled question is never remembered as an answer.
