---
kind: fixed
title: Knowledge consent refreshes leave current answers available to other chats
pr: 0
surface: [chat, engine, build, docs]
invalidates:
  - "Was true: a consent refresh held the shared view mutex across SQLite waits, delaying even current cached answers and hanging concurrent hard-cap tests under a fake clock. Is true now: refreshes run outside the view mutex, current answers remain available, and pending readers share a completion channel."
---

The hard-cap fixtures introduced with #2364 exposed the existing shared consent
lock. A SQLite busy sleep could never wake while another fake-clock goroutine
waited for that mutex. Real chats could also delay background capture behind
another chat's refresh even when their shared consent answer was still current.

Consent reads retain their existing freshness ages, invalidation and failed-read
sharing. Admission still reads current consent. SQLite's busy timeout and the
knowledge close and job-learning wait bounds are unchanged. The two hard-cap
tests retain all assertions and deadlines; the shared vault fix also covers
other session tests that start knowledge capture inside a fake-clock bubble.
