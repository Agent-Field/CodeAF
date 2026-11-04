---
kind: fixed
title: a move whose chat copy sync failed with an error no longer says sync could not be reached
pr: 0
surface: [engine, app, chat]
invalidates:
  - "Was true: every 5xx from the relay's store that named no refusal was only blobstore.ErrUnreachable, so a move it failed said \"Couldn't move it: sync could not be reached.\" Is true now: an unnamed 5xx from the store, other than a gateway's or proxy's (502, 503, 504, 511) or a CDN's (520-527, 530), is also blobstore.ErrAnswered, and the move says \"Couldn't move it: sync answered with an error. The chat is still on <computer>.\" Callers that degrade on ErrUnreachable still do."
---

A move that failed because the relay's store answered 500 while copying the
chat told the person sync could not be reached, although it was reached and
returned an error (N21).

- **Its own sentence.** "Couldn't move it: sync answered with an error. The
  chat is still on <computer>." The detail ends with "blobstore: server
  answered 500".
- **Gateways stay unreachable.** 502, 503, 504, 511 and a CDN's 520-527 and
  530 come from something in front of the relay, so they keep "sync could not
  be reached".
- **Only the chat copy.** A 5xx from the relay's directory (reading or taking
  the chat's record) still says "sync could not be reached".
- **Nothing degrades differently.** `blobstore.ErrAnswered` also matches
  `blobstore.ErrUnreachable`, so retries and fallbacks are unchanged.
