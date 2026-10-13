---
kind: fixed
title: senior-dev's ending no longer says the build and tests passed when it ran none
pr: 1801
surface: [engine, docs]
invalidates:
  - "senior-dev ended `submitted a change, and the project's own build and tests passed` whenever its check came back clean, even in a folder with nothing to run. With zero commands it now ends `submitted a change; the project has no build or tests it could find to run`."
  - "The run's reason for a zero-command pass, which codeaf appends to the landing note, said `submitted, and its build and tests passed`. It now says `submitted, and found no build or tests to run`."
---

A real run on a folder holding only a README printed the pass sentence one line
above `senior-dev observed: the project has no build or tests it could find to run`.
The inner status is still `pass`; only what the ending claims changed.
