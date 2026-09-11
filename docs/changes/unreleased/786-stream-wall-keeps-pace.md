---
kind: fixed
title: a long reply still writing at its lane's pace is no longer cut at the stream wall
pr: 786
surface: [chat, engine]
invalidates:
  - "A reply that ran longer than five times its lane's longest finished reply was cut at that wall, whether or not it was still writing. The wall now asks whether the stream kept pace — at least a fifth of what the lane's measured rate would have produced over the period, `1/streamWallFactor` — and re-arms for another period if it did, up to `streamWallCeiling` (20m), which stays absolute. Only a stream that did not keep pace is cut, with the unchanged sentence."
  - "The wall's history could only grow from a reply that finished, so a lane whose wall was too short for a long reply could never earn a longer one. That loop is closed by the pace test alone: a stream keeping pace now finishes, `noteRun` records it, and the next wall is derived from it."
  - "A tool call's argument deltas were HIDDEN progress to the waiting controller, the same as a run of thought, so a long `write` was judged against the model's thinking-duration distribution and could raise a rescue arm. They are VISIBLE now (`control.Reading.Visible`), the phase is writing on both clocks, and a call forming on the screen counts as the answer being read for the one-voice rule."
  - "`StreamCut.Tokens` counted answer text only, so a cut tool call journaled zero output tokens. It is now the stream wall's own count of everything the model wrote — answer, thought and call arguments — by the one estimate `tokensOf`."
  - "A lane with no measured rate has no pace to be judged at. It is held to `LagRate` (30 tok/s, so six a second at the wall), which also covers every lane under `routing off`, where the rate ledger is not kept."
  - "The manual said every request carries a wall it is cut at \"whether or not it is still writing\". It now says a reply still writing at its endpoint's normal speed gets another wall, up to 20 minutes."
---

Measured on 2026-09-10: a task writing one HTML file on `moonshotai/kimi-k3` had
three attempts cut at their walls (2m30s, 2m30s, 10m39s) while each streamed at
24–41 tok/s, on a lane that had only ever finished 5–9 second tool-call turns.
$1.46 of the task's $1.74 was output thrown away, including an 18k-token rescue
arm the controller raised because it took the write for a long think.
`docs/design/waiting/DESIGN.md` §L has the evidence, the law, and the
alternatives rejected — a `max_tokens` wall, a higher measured floor, pushing
the wall per chunk. Salvaging a cut `write` is owed, at `hedgeRace.flip` or
`internal/session/salvage.go`.
