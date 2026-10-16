---
kind: added
title: every paid image request is a render record, and can be stopped
pr: 0
surface: [engine, chat]
invalidates:
  - "A synchronous `generate_image` call left no attempt record: start, failure, a stop, or a charge with nothing saved were known only from the result text and the artifact rows, and `render.cancel` could not stop an image (a `job:0` record answered an error). Each request now writes `kind:\"image\"`, `job:0` into the existing render ledger before the provider is asked, with the call's exact `run` and `call`, the model the request named, and the provider's own charge (`unknown` when absent). It ends `finished`, `failed` or `cancelled`; the states did not change. `render.cancel` answers `cancelled`, `too_late` or `already_ended` for it, and a receipt after a stop lands on the same cancelled record."
  - "An image whose start row cannot be journaled (closed session, unwritable history, no free slot of the 64 process-wide, unsaved earlier facts) is now refused with `Could not start the image: … No request was sent.` Before, it was sent with no record."
  - "A refused image or video in a session that had not yet started a video or music render saved its record but emitted no `render.changed`; it now does. The notifier is installed when the session is built, which also removes a data race on it."
  - "Tool rows and history entries gain `media_attempt` (true-only): this exact call made at least one render record with its own `run` and `call`. Absent is unknown; it is never inferred from a tool name. No daily cap, routing, permission, retry or naming change."
---

The attempt runs on the tool call's own goroutine and holds one ledger entry and
one retention slot while the provider works. Design and review:
`MEDIA-ADMISSION-CONTRACT.md` (R1) in the panel deliverables log.
