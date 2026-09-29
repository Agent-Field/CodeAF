---
kind: added
title: codeaf do --continue carries a kept run on by id
pr: 0
surface: [engine, chat]
invalidates:
  - "Nothing on the command line carried a headless run on: the only way back to unfinished work was to type the whole request again. `codeaf do --continue <id>` starts a new run from the kept record's original assignment, how it ended and what its plan reached, in the same directory; words after the id are this round's finding."
---
`record kept at <path>` is now preceded by `continue it with: codeaf do --continue <id>`.
The continuation reads the kept plan, composes one brief, and runs as an ordinary
`do`; a continuation of a continuation names the first request once. Clean runs still
keep no record unless `--keep` is passed, so continuing a finished run needs `--keep`.
