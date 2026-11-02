---
kind: added
title: The pipeline editor adds and edits check steps, whose command's exit code is the evidence
pr: 2005
surface: [engine, docs]
invalidates:
  - "Was true: factory.pipeline_preview's add_step always added a chat, and every step of another kind, a check included, was locked: its command could be changed, and the step added, moved or removed, only by editing `.codeaf/factory.yaml` by hand. Is true now: behind the feature `factory.pipeline_edit_checks`, add_step takes `kind` (chat, the default, or check). A check's prompt is the shell command it runs in the job's checkout; it takes until (done, clean, green or proven) and max but no effort or fanout, since no model reads it. A check is edited (its command), moved and removed like a chat; its kind never changes through an edit, and one added after proof is refused. A check spells `kind`, a runtime key, so it is previewed only for a file the factory attested. Posts and gates stay locked."
---
