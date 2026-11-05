---
kind: fixed
title: codeaf job budgets cover all runs
pr: 2353
surface: [factory, app]
invalidates:
  - "Job budget stops previously compared only the current run. They now compare all known saved spending on the job."
  - "Admission retry expiry previously returned withdrawn work to Backlog. It now holds the round while reporting catches up."
  - "Budget edits previously conflicted with worker progress. They now conflict only with intervening budget edits or an ended job."
---

The job budget uses SpentBefore plus the current run's spending. Start,
Carry on, Rerun, Start over and Bring back keep that ceiling. An exhausted
job asks for a raise before paid work starts. Run as new remains a separate
job with zero spending. Older jobs use only their saved known total; money
lost from old saved records is not reconstructed from ledger tags.

The cap question and owned-chat refusal say job total. spending_limit.spent,
spending_limit.next_limit, questions[].spent and questions[].next_limit use
lifetime spending; cost remains this run and lifetime_cost keeps its existing
meaning. A person's continue chooses the next standard ceiling above lifetime
spending, doubling higher ceilings as needed. Auto never grants itself a raise.
A Manual raise above the total resumes work; at or below it work stays held.

A pipeline's default_budget supplies the job ceiling and now covers all runs.
Stage attempt limits and step costs, pipeline intake start.max_cost, the daily
rail and Pareto model selection retain their meanings. Existing cumulative
receipt accounting still retains growth once, including resumed segments,
replayed reports and late usage after a run ends; raising a cap clears no spend.

Budget edits accept the version a person read when only spend or run progress
moved. The store retains the revision of the latest budget change and checks
it under the same lock as the edit, including changes made outside the app.
Another budget edit still refuses, even if the budget was changed back.
That refusal is a conflict, so the desktop does not automatically retry it.
Finished and dropped jobs refuse. Existing clients need no wire change.
Older documents conservatively retain their first observed revision as the
budget baseline; an older untracked read must be refreshed once.

The server also checks its authoritative lifetime total under the job lock at
lease admission and worker reports, including step begin. An exhausted job keeps
its budget question and refuses execution. Current workers await step begin
acknowledgment; older workers obey the existing lease-withdrawn response on their
next report. No worker version gate is added. A late archived receipt cannot let
an admitted worker start from a stale accounting mirror.
Workers explicitly reporting that they are already waiting on a budget question
keep their parked lease and existing budget-before-continue signal order. The
server still replaces that question with its own total; a running-step report
cannot claim this exception.

Step admission first delivers the mirror's unreported spending through the same
serialized report goroutine as periodic reports. A server total below the cap can
therefore no longer admit another paid round when local spending crosses it.
Approved publication and queued merges retain their lease at the cap while no
pipeline phase is running. These forge writes make no model calls; a running
pipeline phase still passes the cap gate, and Stop retains its existing checks.

Admission also flushes spending recorded after an older pending report's
snapshot once that report is acknowledged. Publish anyway counts as a person's
approved publication at the lifetime cap; model calls remain gated. Raising a
stopped job's budget succeeds without continuing its queued manager reply or
pipeline; the person's Start remains required.

Resuming a paused run waits for its canceled control's pending spend receipts
to settle, then resumes the saved chat. It no longer tries to write through
that canceled control and refuse with "factory run: the item was stopped".

Running-job reports no longer block the heartbeat and signal loop. Admission
still waits for the factory's answer before a step starts; Stop cancels that
wait. Periodic ticks during a report are skipped. Final delivery and lease
release wait for reporting to finish. Heartbeat requests themselves remain
on the loop, each bounded to five seconds.

Later thread and usage reports on a job already waiting at its cap retain the
budget-wait acknowledgment. They no longer withdraw that parked lease and
cause a completed chat to resume and spend again after a budget raise.

A refused, unspent step admission now retries automatically after one second
once its reporter and previous drive are idle, within a two-minute recovery
window. It retains the same round and does not consume an attempt or invent a
cap question. Previously withdrawal could return the job to backlog instead.
One 5.5-second deadline covers the entire admission wait, including a queued
periodic report, pending-spend retry and fresh delta. Cancellation and lease-loss
proofs cover delivered and undelivered requests, late spend, joined reporting
and replayed accounting recovery through the initial empty report's receipt.

Admission retry expiry no longer finishes a withdrawn drive as Backlog. It
holds the same unspent round and keeps heartbeats alive while flushing reports.
Once the reports are acknowledged, the next one-second retry resumes that round
without consuming an attempt. The separate result-delivery recovery window and
its retained-results block still apply; admission cannot bypass reconciliation.
A work-loop proof covers answers before and after admission expiry, exact spend
deltas, factory-side cap enforcement and joined reporter shutdown.
