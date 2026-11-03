---
kind: added
title: People can publish past correctly skipped conditional checks
pr: 2323
surface: [factory]
invalidates:
  - "A correctly skipped conditional check under the every-check policy could only be resolved by rerunning with matching files or stopping."
---

A waiting job offers **Publish anyway** only when every unmet check under
`tests pass before anything posts` is a conditional check correctly skipped
with a reason and still does not match the known current files. Failed, pending
or unmeasured checks refuse it. The one-green policy is unchanged.

The authenticated person allowed to publish presses `factory.job_publish_anyway`
with `factory`, `job` and `expect.version`. Stale versions refuse. Hello announces
`factory.publish_anyway`, and `can.publish_anyway` reports the engine's offer.
Auto mode, automatic answers and budget changes cannot create the decision.

The job's `PublishAnyway` uses the existing `Approval` shape with who and when;
the existing approved decision ledger records the press. Consent binds the head,
reported change and skipped-check set, survives storage and wire round-trips,
and is cleared on rerun. Changed files or skips invalidate the earlier press.
The PR lists skipped checks and reasons, says who published anyway and when,
and leaves checks and proof unmeasured.

PR attribution shows a handle only when it is a plain login; otherwise it says
"by a person", preventing decision values from injecting PR lines. The stored
decision value remains unchanged.
