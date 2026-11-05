---
kind: fixed
title: Let replying calls finish at the daily spending cap
pr: 2364
surface: [chat, engine, app, factory, docs]
invalidates:
  - "A recorded daily crossing could be discarded between a completed call and its tool starting, leaving the tool running until another ledger write. Tool starts and command reruns now wake a fresh recorded-spend check."
  - "Daily-cap crossings previously cut replies mid-stream. Calls already returning text, reasoning or tool fragments now finish and record their full usage."
  - "The e2e checker classified plain tool-result display rows as unanswered calls. It now checks actual tool-call entries and still rejects genuine missing results."
---

Daily caps close admission when recorded spending reaches the limit. Calls
waiting for their first reply token are cancelled; calls already replying finish
and record their full usage. Parent crew cancellation waits for those replies,
so inherited contexts cannot cut a child's reply. Remaining owned work stops
through the epoch-checked StopWork route. A raise permits a fresh submission
once old work joins. Job caps and a person's Stop retain immediate cancellation.

Ledger writes, tool starts and command reruns wake daily checks. The 100 ms
job-budget clock does not read the daily ledger. Idle conversations remain idle;
children follow their owner's selected daily authority, and known-free auxiliary
calls retain their admission exemption. Only recorded spending counts toward the
cap. Interim stream usage and pending receipts do not close admission.

The mixed four-call HTTP fixture records $0.45 against $0.24: $0.21 past cap,
one waiting call cancelled at zero recorded cost, and both other replies
finishing at $0.10 each. Overshoot is bounded by the remaining cost of calls
already replying, with no fixed dollar bound unless the provider bounds each
call. Missing prices remain unknown; receipts belong to their recorded local
date, including after midnight.

The shared stop path persists its budget explanation after the turn and tools
settle, including a turn that finished naturally while draining. A child budget
refusal reaches its owner directly: a watcher that runs after the last worker
ends cannot discard the notice as idle. The app/v3 comparison reproduced a
missing idle crew-owner budget notice. Ordinary Stop mid-reply and between calls
already preserve tool pairs and accept the next turn. No product transcript
repair or spend-accounting change was needed for the e2e checker's false alarm.

Billing reports fetch final generation receipts with a fresh lookup deadline
after the run ends, show recorded versus billed cost per call, and exclude the
raised cap's resume calls from pre-raise overshoot. Fake e2e mode drives four
real v3 crews, all four stop notices, their tool histories and resumed turns,
as well as the concurrent recorder.

The interim-usage regression remains: usage is not recorded until settlement,
so four already replying calls still record $0.55. The pending-receipt regression
now allows the resumed reply to finish when the delayed receipt crosses the cap;
it asserts the completed answer, persisted notice and blocked next admission
instead of expecting mid-reply cancellation. Receipt recovery remains bounded
by 70 seconds; unavailable receipts can remain unpriced.

Synctest memory fixtures now own and stop their ledger writers inside the
fake-clock bubble. The shared-writer close fixture creates its agent outside
the bubble and closes it inside, so ledger notifications cannot send to a
channel owned by another scheduler. All existing memory, usage-drain and
hard-cap assertions remain in place.
