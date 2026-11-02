---
kind: added
title: The pipeline editor adds an approval step to one pipeline, with who it waits on
pr: 2125
surface: [engine]
invalidates:
  - "Was true: a step added in the repository's pipeline editor was a chat or a check, and an approval could be added only to one job or by editing the file by hand. Is true now: add_step takes kind gate, an approval with an optional prompt and an optional who (@handle, operator, or the job's person), behind the feature factory.pipeline_edit_gates; who there also needs factory.pipeline_edit_approvers, and both are previewed only for a file its factory attested."
---
