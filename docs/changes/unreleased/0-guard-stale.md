---
kind: fixed
title: Completion guard drops stale diagnostic obligations
pr: 0
surface: [engine, factory, chat]
invalidates:
  - "A failed shell script could recreate an obligation for measured scratch checks already classified as diagnostic."
  - "Unrun literal Go test attempts could remain owed after later measured covering passes."
---

The completion guard honors outside-checkout diagnostic ownership for shell
history, including deleted scratch fixtures. Later measured Go test passes on the
current checkout resolve earlier literal refused, cancelled, timed-out or
unmeasured attempts when scope and environment match. Unknown compound commands
remain conservative obligations. Real failures without covering reruns, missing
receipts, narrower passes and mismatched targets still hold completion.

A hold names every remaining obligation on one line. All guarded completion paths,
including no-change repair reports, stop after three unchanged refusals or six
refusals overall, retaining incomplete work rather than declaring success.
