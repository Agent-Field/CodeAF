---
kind: fixed
title: an observation the reply already answered is not raised again
pr: 948
surface: [chat, engine, docs]
invalidates:
  - "The end-of-turn reader could raise the SAME observation about the same stopped reply up to three times, and the manual said so: \"A reader that answers 'still not finished' about the same stopped reply three times running has stopped telling aforge anything new. The fourth time it says so, the reply ends instead.\" It now ends on the SECOND identical reading, with `stopping here · nothing moved since the last look and what is left is the same — … · saying it again would not change it`. The cap of three still stands and now bounds only a reader that says something NEW every time."
  - "The standstill floor — a reading that repeats itself stops the run — belonged to an unattended session alone, and a conversation you are sitting in front of had nothing of the kind. Both principals now hold the same `standstillFloor` and stop on the same sentence. On the attended road it is per stretch rather than for the rest of the session: the next thing you type clears it."
  - "`Steward.carriedUnmet`, `Steward.carriedBrief` and `stewardStandstillReason` are gone. The floor is one `standstillFloor` value and the sentence is `standstillReason`, both in internal/session/standstill.go, both used by `Person` and `Steward`."
---

Measured 2026-09-11 on `dev@333acc67d`, a live drive of the chat surface: a
reply had correctly reported that `zeta.txt` did not exist, and the reader
raised the same observation three times running, each one claiming the missing
file had not been reported. The model spent three visible turns explaining that
the observation was mistaken and the person read all three — an argument they
had typed no part of, drawn as ordinary chat.

The cause was one road missing a law the other road already had. #468 gave the
unattended principal a floor under carrying on and its own note predicted this:
the per-turn cap "still lets repeated echoes through up to the cap instead of
stopping on the first identical one". The floor is now one mechanism both
principals hold.

The second half of #888 — that the exchange is not DRAWN as chat — is not
landed. A continuation's reply cannot be told from a turn's genuine final answer
by anything the transcript carries, so every rule derivable from it also hides
real answers; #888 stays open with the seam written up.
