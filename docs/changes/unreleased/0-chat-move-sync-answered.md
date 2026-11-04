---
kind: fixed
title: a big move from the hosted relay goes through, and a move sync failed with an error says so
pr: 0
surface: [engine, app, chat]
invalidates:
  - "Was true: every 5xx from the relay's store that named no refusal was only blobstore.ErrUnreachable, so a move it failed said \"Couldn't move it: sync could not be reached.\" Is true now: an unnamed 5xx from the store, other than a gateway's or proxy's (502, 503, 504, 511) or a CDN's (520-527, 530), is also blobstore.ErrAnswered, and the move says \"Couldn't move it: sync answered with an error. The chat is still on <computer>.\" Callers that degrade on ErrUnreachable still do."
  - "Was true: a move asked the relay for up to 256 objects per batch get (blobstore.MaxGetMany) whatever the relay. Is true now: against a hosted relay (syncwords.Hosted) a move or a task copy asks for at most cellsync.HostedGetMany (16) per request, in the same order; a relay of the person's own still gets 256."
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
- **Smaller batches from the hosted relay.** A big chat's move from the hosted
  relay failed with a 500 on every try: its batch get read up to 65 MB at
  once. A move (and a task's working copy) now asks the hosted relay for at
  most 16 objects per batch get, in the same order, so the relay reads about
  8 MB per request. A big move makes about three times
  as many requests (about 1,900 instead of 600 for a 1.4 GB chat); a relay of
  your own still answers 256 at a time.
