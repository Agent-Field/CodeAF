---
kind: added
title: Factory steps can carry a readable label without changing their identity
pr: 2307
surface: [engine]
invalidates:
  - "Was true: a step's visible name could only be derived from its id. Is true now: an optional label supplies its readable name while the id remains unchanged."
---

Factory step definitions accept an optional `label`, such as `Security review`.
It travels with the saved job, pipeline definitions and job API, including the
current step, and supplies the brief fact's displayed label. Empty or whitespace
labels use the existing display. Jobs saved without a label still omit it.
State, routing, receipts and evidence continue to use the original step id.
Desktop clients can display `label` when present and otherwise keep their current
name formatting; actions must continue to send `name`.

The engine announces `factory.job_step_edit_label` for labels on added and
existing job steps (`edit.add[].step.label` and `edit.label`), and
`factory.pipeline_edit_label` for `edits[].label` on pipeline `set_step` and
`add_step`. Pipeline previews list label changes; labels are trimmed and bounded
to one line of 200 characters. Empty labels clear the saved key. Labels were
previously readable in pipeline edits but could not be changed there.
