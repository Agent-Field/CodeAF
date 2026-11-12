---
kind: changed
title: a person's message keeps the app's client id on its own journal line
pr: 0
surface: [engine, remote]
invalidates:
  - "A message's `client_id` lived only in the app process's memory (`appTurnMark`), so after an engine restart an entry could not be matched to the message the app sent. The id is now written on the accepted line (`session.Submission.ClientID`, at most 256 bytes) and read first; the process-local map is only a fallback."
  - "A plain `agents.send` with a `client_id` was a bare `Submit`. It is now a `SubmitMessage` with `Said` nil, so its words are not authored; queued sends carry their id too. Steer and crew messages are unchanged."
  - "An over-long or control-character `client_id` was accepted and echoed. A conversation that can keep ids now refuses it `bad_params` before the turn starts."
---

The id is correlation only: never in the model request, never an idempotency
key, never guessed for a line from before this. A remote engine advertises
`messageClientIds` in its welcome; a client refuses to send an id to one that
does not, and the app host then keeps the id process-locally as before.
