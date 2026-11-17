---
kind: added
title: Each try of a factory job's step says how it ended in a word and a counted reason
pr: 2106
surface: [engine]
invalidates:
  - "A job read's steps[].rounds[] said only whether a try was rejected and the step's own one-line summary. Each try now also carries result (the step's rule word, met or not, such as clean or not clean) and reason (a count such as 2 findings), judged from the structured report and never from the summary."
---

The hello says `factory.job_try_result`. A worker's report never sends the two
fields, because an older factory decodes reports strictly; the factory judges
each try from the result it folds.
