---
kind: fixed
title: Stop running work when recorded spending reaches its cap
pr: 2364
surface: [chat, engine, app, factory, docs]
invalidates:
  - "A recorded daily crossing could be discarded between a completed call and its tool starting, leaving the tool running until another ledger write. Tool starts now wake a fresh recorded-spend check."
  - "Unsettled stream usage and pending receipts do not close admission. Only recorded spending counts toward the cap."
---

Daily caps stop active provider calls, tools, detached summaries and owned crews
through the existing epoch-checked StopWork route. Ledger writes and tool starts
wake checks; the 100 ms job-budget clock does not read the daily ledger. Idle
conversations remain idle, and turns waiting for their first provider admission
report a refusal through their event stream without latching stopped. Running
children follow their owner's selected daily authority. Raising the named cap
permits a fresh submission after old work joins.

The four-call HTTP fixture now sends the crossing usage in its final chunk and
closes that stream, matching the normal route. All three other held calls are
cancelled: recorded spending is $0.29 against $0.24, $0.05 past cap, with one
unknown-price call retained. The completed crossing call keeps its answer;
stop notices and closed admission apply to the other calls still running.

Two limits are pinned by named regression tests:

- A route reporting interim usage while its stream stays open does not stop
  before settlement. Four calls record $0.55 against $0.24, $0.31 past cap,
  with zero of four cancelled before release. Final responses are decoded
  together before banking so cancellation after release cannot discard a peer's
  final usage chunk. This is the known limit, not a requested future behavior.
- A stopped call whose cost is known only through a pending receipt leaves
  recorded spending and the chat's cost at $0.00. The chat retains `working`
  and says `everything running here was stopped, the reply with it`. There is
  no daily-limit notice or pending-receipt warning. An unchanged $0.24 cap
  admits a fresh submission. Once the $0.25 receipt is recorded, running work
  stops with `daily limit reached · $0.25 spent of $0.24 · /budget day changes it`.
  Receipt recovery is bounded by 70 seconds from queueing, including request
  allowances and retry delays; an unavailable receipt can remain unpriced.

The separate four-crew settlement measurement records $0.31 against $0.24,
$0.07 past cap, with all three other calls cancelled. These local scripted
fixtures measure durable dollars, not provider cancellation latency. There is
no finite dollar overshoot bound without provider-enforced per-call maxima.
Cancelled charges are retained when measured; missing prices remain unknown.
Spending belongs to its recorded local date, including receipts after midnight.
