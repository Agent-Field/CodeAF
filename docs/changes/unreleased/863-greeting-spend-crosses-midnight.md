---
kind: fixed
title: the greeting's spend fixture no longer fails between midnight and one
pr: 863
surface: [chat]
invalidates:
  - "`TestTheGreetingsFirstFrameCarriesTheSpend` (#842) dated the row it means to be TODAY'S at `now.Add(-time.Hour)`, which is yesterday for the first hour of any day — so `internal/tui3` was red on a clean `dev` every night between 00:00 and 01:00 and green again by morning. If you saw that failure and recorded it as a flake, it was the calendar."
  - "`internal/tui3` has a second red this does not touch: `TestADecayedBeliefDrawsNoTail` sets its belief from the real `time.Now()` and reads the view at the fixed `timeNow()` test clock, so the decay stops firing once real time passes that date. It fails identically on a clean `origin/dev@84bf71d61`."
---

The current instant is the only time that is always inside today.

Third fixture on this work to fail with no change under it, after #759's legacy cache aged past
its TTL overnight. A test whose result depends on the wall clock reports the calendar.
