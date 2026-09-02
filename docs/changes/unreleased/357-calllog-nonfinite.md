---
kind: fixed
title: the model-call log never loses a row over a number JSON cannot spell
pr: 357
surface: [engine]
invalidates:
  - "A start row with no end row beside it in `~/.aforge/logs/calls.jsonl` meant a call that was still in flight. Until now it far more often meant a call that ENDED and whose end row was thrown away: `write` returned on the marshal error, and `encoding/json` refuses the `+Inf` the wait controller puts in `cost_s` whenever it holds no alternative lane. Every lane-watched call the controller acted on lost its end row, so the log understated spend and hid exactly the slow calls it exists to explain."
  - "A failure inside the call log always silenced the log and said so once. A record that will not serialize now says so once and leaves the log ON: it is a bug in one row's builder, not a broken disk, and the calls after it keep their rows."
---

A row of this log may be short of a number; it may not be absent. So a float
JSON has no spelling for — `+Inf`, `-Inf`, `NaN`, in `waste_usd`, `wait_s`,
`cost_s` or `cost` — is taken off the record and named in words on the row
(`cost_s was +Inf and is not on this row.`), and the rest of the row is written
whole.

Taken off rather than written as a string or a null because the field's type is
what every reader of this file decodes with, and a missing field already has a
meaning here — the emptiness law, nothing was measured. The note is what tells
"infinite" apart from "unknown", and the call's own sentence still comes first.

Measured on a headless `aforge do` on deepseek-v4-pro: a 39.6 s `compile` the
controller hedged at its ceiling now leaves an end row carrying status 200, 2,718
completion tokens, `stop` and $0.0099. Before this it left a start row and
nothing else, and the manual's own words for that state are "still in flight".
