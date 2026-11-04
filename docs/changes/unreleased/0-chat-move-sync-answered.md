---
kind: fixed
title: a move that sync failed with an error no longer says sync could not be reached
pr: 0
surface: [engine, chat]
invalidates:
  - "Was true: every 5xx from the relay that named no refusal was only blobstore.ErrUnreachable, so a move it failed said \"Couldn't move it: sync could not be reached.\" Is true now: an unnamed 5xx other than 502, 503 or 504 is also blobstore.ErrAnswered, and the move says \"Couldn't move it: sync answered with an error. The chat is still on <computer>.\" Callers that degrade on ErrUnreachable still do."
---

A move that failed because the relay answered 500 told the person sync could
not be reached, although it was reached and returned an error (N21).

- **Its own sentence.** "Couldn't move it: sync answered with an error. The
  chat is still on <computer>." The detail still carries "server answered 500".
- **Gateways stay unreachable.** 502, 503 and 504 come from a gateway or the
  network edge in front of the relay, so they keep "sync could not be
  reached".
- **Nothing degrades differently.** `blobstore.ErrAnswered` also matches
  `blobstore.ErrUnreachable`, so retries and fallbacks are unchanged.
