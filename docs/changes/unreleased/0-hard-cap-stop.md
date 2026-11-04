---
kind: fixed
title: Recorded day-cap crossings stop running owned work
pr: 0
surface: [chat, engine, app, factory, docs]
invalidates:
  - "A turn crossing the daily cap previously finished while other crews kept running. Recording a crossing now wakes the existing job-budget watcher and stops each affected conversation's owned work."
  - "A cancelled provider send with no text or usage could disappear from accounting. A sent request now leaves an unknown-cost marker when cancellation prevents a price from arriving."
---

Day caps and job budgets share the epoch-checked StopWork route. Ledger-written
notifications and measured stream usage drive checks; they add no directory reads to the 100 ms job
clock. Pending tool results stay paired, partial usage is settled, and the stop
names the limit and how to raise it. Raising the cap permits a fresh submission;
old producers, retries and completion notices cannot restart stopped work.
Measured spend remains visible while its receipt is pending; already-banked usage
is counted once, and previous-day readings do not close a fresh day’s cap.
StopWork also cancels detached summaries.
Running adaptive workers read their owner's live day limit, and sampled job refusals
are rechecked after a raise. Known-free helpers are not refused because another
conversation reached the day cap. The HTTP gate checks the actual model, so a
free helper cannot substitute a paid seat after the cap.

Cancellation cannot undo provider charges. If L is the cap, S the settled spend
plus live usage already measured when cancellation begins, and U_i the remaining
unrecorded charge of each send already
in flight (including sends admitted during notification delay), final overshoot
is max(0, S + sum(U_i) - L). With N outstanding sends each bounded by M dollars,
it is at most max(0, S-L) + N*M. Without provider-enforced per-send maxima there
is no finite dollar bound; missing usage remains unknown, not a zero charge.

The same local fake-provider harness with four concurrent crews and a $0.24 cap
records $0.55 before ($0.31 past cap) and $0.31 after ($0.07 past cap).
A separate four-call HTTP fixture cancels the crossing stream too, settles $0.29
($0.05 past cap), and retains one unknown-price call after cancellation. This measures recorded spending, not
a real provider's cancellation latency or final bill.
