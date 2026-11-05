---
kind: fixed
title: Let replying calls finish at the daily spending cap
pr: 2364
surface: [chat, engine, app, factory, docs]
invalidates:
  - "Lowering the saved daily cap below recorded spending previously left waiting calls running until another receipt arrived. Active watchers now detect the edit within 100 ms."
  - "A planner that finished without starting a worker could leave its chat without a daily-limit notice. Adaptive runs now check recorded spending at completion."
  - "The job-budget watcher previously kept its 100 ms clock running while idle. It now polls recorded daily and job spending only while capped work is moving, and every owned-work start wakes it."
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

The process-wide ledger writer only increments an atomic counter, so it cannot
send into a synctest-owned channel. The watcher samples that counter every
100 ms only while capped model calls, tools or owned work are moving. Model,
tool, background-job, task-node, orchestration, harness and task-belt starts
wake the watcher. Repeated watcher setup is idempotent. Idle conversations hold
no polling timer; uncapped conversations never poll. A shared daily drain keeps
each observed owner's epoch-bound stop even when its last reply finishes before
its own watcher samples again.

Budget admission leaves context cancellation to the transport, retaining
uncapped helpers' cancellation and cleanup path. Children follow their owner's
selected daily authority, and known-free auxiliary
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

Adaptive runs check recorded daily spending at completion with the admission
epoch captured when the run was registered. A planner that finishes without
starting a worker can no longer leave its idle chat without a daily-limit
notice. Worker and planner admission refusals remain model-free; the shared
stop path records the notice once and a raise accepts a fresh message. The live
four-crew test now names chats missing notices and logs their transcript tails
and orchestration snapshots on that wait's failure.

A profile-only daily-cap decrease now triggers the active watcher's recorded-spend
check within its existing 100 ms poll interval. The watcher remembers the last
selected cap alongside the ledger counter, so unchanged polls do not reread
the ledger and idle chats still hold no timer. Regression fixtures lower the
saved profile cap while three crews run, measure provider admission timestamps,
check cancellation and one-time reply accounting, persist one notice per owner,
and resume after a raise. The idle fixture refuses the next message without
provider admission or background budget checks.
