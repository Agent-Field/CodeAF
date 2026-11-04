---
kind: changed
title: Experiment with stopping on recorded spending alone
pr: 2364
surface: [chat, engine, app, factory, docs]
invalidates:
  - "Measured stream usage previously closed admission and woke budget watchers before settlement. This local experiment checks recorded spending alone."
  - "Pending measured receipts previously kept the cap closed before a ledger write. Pending receipts no longer count until their cost is recorded."
---

This experimental branch removes live usage readings and keeps the existing
running-call registry, ledger-written notifications, job budget checks and shared
StopWork route. Interrupted calls still settle through billing and receipts;
unpriced cancellations retain the unknown-cost marker. Open streams can exceed
the cap without detection until a recorded total crosses it.

The unchanged four-crew measurement runs five times at both the starting head
and this revision: all record $0.31 against a $0.24 cap, $0.07 past cap, with
three of three other calls cancelled. Its crossing call completes before the
ledger check. Each other call costs $0.10 when completed or $0.02 when cancelled.
The measurement file's SHA256 is
`6f75e880778f6b67ff1f7a1d61b71a11d711ad561a8abea4802efb569c79db7e`.

The retained four-call HTTP tests for day and job caps fail: the crossing stream
reports $0.25 against $0.24 and stays open for the fixture's one-second wait.
Together the streams have reported $0.29, already $0.05 over cap, but none has
recorded its price. No call is cancelled before release; final recorded spending
is $0.55, $0.31 past cap. The $0.24 increase over the measurement's $0.07 overshoot
is 2.4 ordinary $0.10 calls. This has no general finite bound without a maximum
provider charge per call. Neither failing behavior test was removed or weakened.

Five tests only about live readings were removed:

- `TestResumeBeforeReceiptKeepsKnownCapClosed`: requires an interim stream price
  to trigger the stop and keep admission closed before its receipt is recorded.
- `TestRaiseAdmitsWhileReceiptPending`: requires that same unrecorded price to
  close the original cap before testing a raise.
- `TestAdmissionDuringInterimBank`: requires admission to count a live reading
  while the ledger callback is blocked.
- `TestRunningSpendAcrossMidnight`: checks attribution of live readings to days.
- `TestLiveStageSampleDoesNotLatchAcrossRaise`: samples an unrecorded price
  through the removed live-budget interface.

Accounting and previous-day tests remain, with fixture synchronization using
streamed text instead of the deleted live-reading map. Idle watcher cleanup now
checks the ledger callback registry. Other focused session behaviors and provider
settlement tests pass, including stop explanations, transcript pairing, raising
and resuming, retry refusal, idle chats, owner raises and free helpers.
