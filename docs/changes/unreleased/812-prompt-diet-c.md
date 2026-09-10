---
kind: changed
title: five laws that were on the page twice are on it once, and a second copy is now a build failure
pr: 812
surface: [chat, engine]
invalidates:
  - "The system prompt told the model to ask through `ask` and never in prose in two Tool Policy bullets, and the belt fact under them said it a third time. It is said ONCE now, on the decision-ladder bullet: \"Use `ask` only as the last rung, with why the decision is needed now, its stakes, and your pick, and ask THROUGH `ask`, never in prose: a typed-out question has no keys and no record.\" The `ask` belt fact has no present-case wording left at all — only the shelved one, which names the `questions` group and `load_capability` because the shelf law demands that pair."
  - "prompts/system.md taught how to write a very large file (\"in parts ... then `append:true` for the rest\") and to page a large read with `read` offset/limit. It teaches neither now: tools_write.go's appendSentence and internal/exec/bare's readDescription own both, word for word, and they ride with the verb."
  - "The page said \"Start ONE `watch` to follow something that changes\" while the tool's own description says at most three may run at once. That sentence is gone; a shape WITHOUT `watch` is still told \"There is no `watch` here\"."
  - "\"Never ask for something the record already answers\" was in the Tool Policy and `# Critical` said it again as \"MUST default to informed action\". One sentence carries both now, in the Tool Policy; `# Critical` has three bullets, not four."
  - "There was no general statement that handed-off work reports itself, only ten per-tool copies of it. The Tool Policy now carries one: \"Anything handed off — a job, a watch, a task, a quick task — reports itself into this conversation; never sleep, tail or poll for it.\" Tool descriptions are free to stop repeating it."
  - "Duplication in the fixed prefix was something an audit found. internal/session/lawregistry_test.go makes it a build failure: every law unit has an id, a delivery class (core, verb, event, demand) and a key sentence, and the test fails if that sentence occurs more than once across the widest page plus the marshalled tool block, or if a verb-class law names a tool the belt does not carry."
---

The fixed prefix is sent again in full on every tool round of every turn, so a
law written twice is a bill paid twice and two sentences to keep in step. This
is the diet's DELETE pass: 819 bytes came out of text that stated a law already
stated somewhere the model reads anyway, and 142 bytes went in as the one
sentence that makes ten per-tool never-poll copies deletable. The page went
23,391 → 22,714 bytes, the tool block is unmoved at 24,044, and the prefix is
46,758 — 1,242 under a budget nothing raised.
