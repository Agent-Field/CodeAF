---
kind: changed
title: codeaf app's first-run setup is three steps, and a setup kept in four resumes on its step
pr: 0
surface: [engine, docs]
invalidates:
  - "Setup was four steps (welcome, connect a model, defaults, how to start) and `setup.set` took steps 1 to 4. Setup is now three — connect providers, defaults, how to start — and `setup.set` refuses a step past 3 (`step is 1 to 3 or \"done\"`)."
  - "Setup connected exactly one provider before it would continue. Setup now takes any number of connected providers, or none; `Setup.provider` is only the provider last chosen, and the connected set is what `providers.list` marks `signed_in`."
  - "A setup kept by a four-step build would now be read past its step, or refused with the whole app state file (a kept 4 is no step in three). codeaf now reads such a step into three where it keeps it — 1 or 2 is 1, 3 is 2, 4 is 3, done is done — and marks what it keeps itself with `setup_steps: 3` in `app/state.json`, so a person who quit setup halfway and then updated resumes where they were."
  - "The schema said a setup step was an object `{N, Done}`, the Go struct it is held in. It now says what travels: an integer from 1 to 3, or \"done\"."
---

A step read from the app's own file is no longer refused by its range, so a
file kept by an older build always loads; `setup.set` still refuses a step past
the last on the wire. An older build that writes the file back drops
`setup_steps`, so its step is read as counted in four again: right when that
build moved setup itself, and one step early when it only changed a setting
while setup was halfway.
