---
kind: fixed
title: A mistyped pipeline key in factory.yaml is flagged, and a job says why it runs the built-in steps
pr: 3001
surface: [factory, docs]
invalidates:
  - "A key such as `issues:` under `pipeline:` in `.codeaf/factory.yaml` loaded as a pipeline of your own with no word from the file's check, so every issue job ran the built-in steps and nothing said why. The check now reports a key that is a plural of, or one edit away from, `issue`, `pr` or `ci` and asks `Did you mean ...?`; the key still loads as the pipeline it is. Any other name under `pipeline:` stays a legitimate pipeline of your own and is not reported."
  - "Nothing in a job's record said it was running the built-in issue steps. When the repository has no `.codeaf/factory.yaml` on main, or the file has no `pipeline: issue:` list, the job now records one line saying so when its pipeline is decided, unless the factory's copy or library supplies an `issue` pipeline."
  - "The manual never said where `pipeline.issue` goes or that `size.issue` is an unrelated key. A new section covers both."
---

Only issue jobs get the line. A pipeline picked at Start, or by a routing rule,
is the person's own choice and gets none.
