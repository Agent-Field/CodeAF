---
kind: fixed
title: Scoped client answers cross the app backend and relay end to end
pr: 1929
surface: [engine, chat]
invalidates:
  - "The app derived a cell-backed conversation's id from the transcript's immediate parent and called it .cell. It now uses the shared session layout resolver, so app methods and relay leases refer to the same chat."
---

The in-process round trip pairs a client holding no shared identity secrets,
opens and decodes a leased asks.list envelope, queues an irreversible answer,
checks acknowledgement and the actual conversation answer, reads its chat
receipt, and observes an empty scoped snapshot. The model-facing conversation
channel is scripted; relay, signatures, encryption and app method dispatch are
real. Focused checks also cover any-box creation, deterministic backoff, lease
changes during reads and restart idempotency. Closing the app joins its runner
before closing engines it may call.

Review follow-ups cover recovery after an unexecuted entry expires, enrollment
and any-box execution on a second full device, isolated refusal of oversized
snapshots, and startup without sync after the last client is unpaired. The
client must explicitly sign a missing-number skip; ordinary gaps stay refused.
