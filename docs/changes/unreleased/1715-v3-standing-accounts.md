---
kind: fixed
title: a standing firing's belt reaches the accounts its window is connected to
pr: 1715
surface: [chat]
invalidates:
  - "A scheduled firing built through `v3StandingPosture` left `session.Config.Connect` empty, so `services` and `use_service` were missing from every standing task even when the account was connected. The firing now inherits the window's accounts manager, and the detached `codeaf tick` resolves the profile's own."
---

A live window already resolves one accounts manager per process, and every
conversation's belt reaches it. The standing tick that window runs now hands the
same manager to its firing posture, so the two never keep separate caches over
one connect store and a token either refreshes is never stale in the other. The
detached `codeaf tick` has no process to borrow from, so it resolves the
profile's manager once for its single pass. Nil stays nil: no manager means no
account tools are fabricated for a disconnected profile.
