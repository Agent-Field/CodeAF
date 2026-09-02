---
kind: fixed
title: a compile that comes back with no goal keeps the person's own words instead of ending the run
pr: 369
surface: [chat, resident, engine]
invalidates:
  - "A compile reply that decoded with a blank goal ended the run with `I couldn't apply that request: splice failed: compile request: compile intent: empty goal`, zero nodes and the whole compile's spend lost. The goal's own law already appended the verbatim instruction; now a blank gloss leaves those words standing as the whole goal, the rest of the brief (title, scale, method, parts) is kept, and the receipt carries `The compiler supplied no reading of its own, so your request stands as the goal, word for word.` On `aforge do` the same fact is one progress line, `compile — no reading of its own — your request stands as the goal, word for word`."
  - "A structured answer cut off mid-object was continued with the same wire shape hint (JSON mode or a schema) as the attempt. A fragment has no shape, and a model bound to JSON mode restarted a whole object from the field after the cut instead — measured on the intent compile, where the restarted object carried every field but the goal. The continuation now goes out with the room and no shape hint; the re-ask keeps its hint."
  - "A shaped answer the seam gave up on reported only `finish_reason` and `completion_tokens`. It now also quotes the head of the reply (`reply=\"…\"`, 200 bytes), because a streamed call's row in the model-call log carries no body and the error was the only record of what the model said."
  - "The issue's own first reading — that the second compile call was the format-contract re-ask — was wrong: the failing run's kept store shows a `structured_repair` of kind `continued`, so the reply was an unclosed object, not prose."
---

The intent compile is the cheapest call in a job and the only one nothing after
it can run without, and it was the one place a cosmetic failure — a model
leaving one field blank — could forfeit the run. The instruction was in the
request the whole time, and on `aforge do` it was going to be the goal anyway.
The rule is now general: the person's words are always a valid goal, whatever
the compiler wrote or failed to write, and a substitution they cannot see is
never silent.
