---
kind: fixed
title: a task's worker can stop its own job, and a run's checker checks its own copy
pr: 1515
surface: [chat, engine]
invalidates:
  - "A bash-belt worker's one call had to name bash: the envelope refused `jobs`, `read_document` and `manual` with \"is not on this belt\" although the belt carried them and the worker's page named two of them. One call per response may now name any tool the belt carries, so a worker reads and stops its own job with `jobs`; a name the belt does not carry is still refused."
  - "A run's checker was never told where the work was, and could start in the person's checkout, decoded from the project folder's name. Its instructions now say the work is in its working directory, the run's copy, and that the person's checkout is not the work."
---
