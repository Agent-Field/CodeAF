---
kind: fixed
title: a re-ask for a quotation-only failure asks for the missing field alone instead of re-buying the whole verdict
pr: 515
surface: [engine]
invalidates:
  - "Every re-ask bought the whole verdict again at the doubled ceiling, even when the only thing wrong was a missing quotation that the model could fill in one short call over the answer already held. A quotation-only refusal now triggers a targeted repair that asks for the missing field alone, merges the reply, and accepts the result without re-buying the rest."
---

When a shaped answer failed only the quotation rule — the caller's contract demanded a
quote and the model left it empty — the re-ask bought the whole verdict again, at the
doubled ceiling. The re-ask now asks for the missing quotation alone over the answer
already held, and the merged result is accepted.