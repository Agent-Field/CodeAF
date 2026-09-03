---
kind: fixed
title: an ignore list never empties the set the request is sent to
pr: 586
surface: [engine]
invalidates:
  - "A request could name a lane in `provider.only` and the same lane in `provider.ignore`: the demand was applied after the ledger's vetoes and never looked at them. It cannot now — the demand outranks the veto, checked on the finished object in `wirePreferences`."
  - "The velocity ledger deliberately sent an ignore list covering every lane it knew, on the plan of paying the router's 404 and letting the ladder's first rung undo it. It now sends one at most once per model per process: the router's own `All providers have been ignored` is the evidence this process cannot count for itself, and after it the lane nearest its cooldown's end is released instead."
  - "`All providers have been ignored` was filed as the demanded machine's own refusal, so a self-inflicted 404 paced that lane and wrote it out of the serving set. That sentence now names no machine — it is a fact about a list, and a machine that never got the request has said nothing about it."
  - "`internal/manual/chat/lanes.md` said a refusal is final for that machine, immediately, with no exception. There is one, and the page now carries it."
---

The canary was logging `API error (404): All providers have been ignored` at a
steady rate — 102 rows across 44 cells, every one `served: null`, median 41ms,
and on a hedge both arms refused inside 40ms. Nothing was wrong with any
endpoint: the request was carrying this process's own list of struck lanes and
that list had removed everything the request was allowed to land on. The law is
now stated once, about the object, at the last moment before it goes out.
