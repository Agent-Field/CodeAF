---
kind: added
title: The model can ask through one shared question door
pr: 728
surface: [chat, engine]
invalidates:
  - "The question contract described assumption and ratification shapes, but the model had no tool that could raise any question through it. The model now has an `ask` tool whose full Question input passes the shared gate and whose result is the whole Answer."
  - "Every question without a lane-specific clock waited forever. Per-project `autonomy.json` now selects ask, recommend-then-auto with a wait, or decide for each question kind; irreversible questions and clarifications still always wait."
  - "Decision records were written to `decisions.jsonl` but were not carried into the model's context. The compact `the record` section is now rebuilt into the system context whenever a decision lands."
  - "An explained answer against the model's pick stayed only on the decision record. With memory enabled, that explanation now also becomes a durable, forgettable preference."
---

The tool follows the decision ladder: consult the record, state safe assumptions,
act and ratify reversible work, show outcomes, prefer structured choices, and ask
only when those cheaper rungs cannot settle the decision.
