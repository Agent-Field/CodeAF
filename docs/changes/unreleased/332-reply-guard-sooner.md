---
kind: changed
title: The reply guard reads a kilobyte, watches the thinking, and a reply you stopped mid-soup is not kept
pr: 332
surface: [engine, chat]
invalidates:
  - "The repetition guard could not cut anything under four kilobytes of reply. It now also reads the last kilobyte, so one token or one line over and over is cut after about a kilobyte of it; a paragraph repeated is still cut by four."
  - "A code fence that opened and never closed hid everything after it from the guard for the rest of the stream. Text inside a fence is still never judged, up to sixty-four kilobytes of one block; past that, a fence that has not closed is read like everything else."
  - "Only the answer channel was watched. The thinking pass is watched too now, on the reasoning channel and as working fenced inside content, and a loop there ends the call the same way: a cut, the lane struck, no response."
  - "A reply the person stopped by hand was kept verbatim as an assistant message, in the transcript and the journal, and replayed on every later request. A stopped reply that had lost its thread is now kept nowhere, and the person reads `the reply you stopped had lost its thread — that text was not kept`. With `reply guard` off nothing is judged and everything stopped is kept."
  - "`provider.LostItsThread` is new: the guard's own judgement, offered once over text that has already streamed. `keepPartial` and `keepSteeredPartial` take the event hub now."
---

A serving endpoint looped the model's own `</think>` into the answer of a
`z-ai/glm-5.3` turn on 2026-09-01, and a person watched fourteen seconds of it
before stopping the turn by hand. The tokens were the endpoint's — aforge has no
`<think>` handling on the receive path and the same shape reproduces by plain
curl (sgl-project/sglang#36669) — but everything that made a bad minute into a
bad afternoon was aforge's: the compression test would have condemned the text
at once and was not allowed to speak under four kilobytes; an open fence could
blind it for the rest of the stream; the thinking channel, where the reproduced
upstream failure lives, was never read; and the stop wrote the junk into the
conversation for every request after. The one condition aforge supplied — every
chat turn went out at temperature 0, greedy decoding, because the chat's client
never set the field — was already removed by #308.
