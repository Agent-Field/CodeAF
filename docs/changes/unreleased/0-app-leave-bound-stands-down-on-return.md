---
kind: fixed
title: The app engine's leaving bound stands down once the engine has finished leaving
pr: 0
surface: [engine]
invalidates:
  - "`codeaf app` armed a thirty-second bound on its wrap-up that its own deferred cancel started as it returned, so a process that lived on after it was ended thirty seconds later. A test binary does: `TestAppCommandAnswersHelloPingCrewsAndUnknownMethod` runs the door in-process, and the whole `cmd/codeaf` suite died about a hundred and fifty tests in with \"codeaf app: still leaving after 30s; ending now\" and no test to blame. The bound now stands down when the door returns; a wrap-up that outruns it while the door is still leaving is ended as before."
---

Provisional entry; the pull request number is filled when it opens. A shipped
engine was never ended early by this, because `main` exits as soon as the door
returns; the change matters to anything that keeps the process alive after it.
