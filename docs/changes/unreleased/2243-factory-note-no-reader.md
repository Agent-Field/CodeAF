---
kind: fixed
title: codeaf job notes say when no remaining step will read them
pr: 2243
surface: [engine, chat]
invalidates:
  - "A saved job note was described as being for its stages even after every step had finished. The answer now names its next reader or says no remaining step will read it."
---

Notes stay saved on the job, including after its steps finish. The approval
asks to save a note on the job. The result says the note is not in the pull
request and, with no remaining reader, points to commenting on the PR in
GitHub. Saving a note does not post a comment or change the PR body.

The app's source issue comment method is not presented as a PR comment tool.

Checks, publication steps and approval gates are not counted as note readers.
