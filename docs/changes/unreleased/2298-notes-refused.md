---
kind: fixed
title: Execution notes and completion guard share trustworthy command outcomes
pr: 2298
surface: [engine, factory, chat]
invalidates:
  - "Execution notes previously called approval and policy refusals completed. They now say refused and retain the real refusal reason."
  - "Partial PASS output could previously label an interrupted command passed. Cancellation and timeout receipts now keep their interrupted outcome."
  - "Printed cancellation or timeout text in legacy journals does not establish an interruption. Background launch success means started, never passed."
---

Factory execution notes read the journal's producer outcome and refusal mark, with a
conservative fallback for older approval receipts. Refusals, cancellations and
timeouts remain unresolved until later measured covering evidence qualifies. Later stage briefs retain
the reason instead of implying that the refused command ran. PR validation
continues to publish only passed command receipts.

The completion guard honors outside-checkout diagnostic ownership for shell
history, including deleted scratch fixtures. Later measured Go test passes on the
current checkout resolve earlier literal refused, cancelled, timed-out or
unmeasured attempts when scope and environment match. Unknown compound commands
remain conservative obligations. Real failures without covering reruns, missing
receipts, narrower passes and mismatched targets still hold completion.

A hold names every remaining obligation on one line. All guarded completion paths,
including no-change repair reports, stop after three unchanged refusals or six
refusals overall, retaining incomplete work rather than declaring success.
