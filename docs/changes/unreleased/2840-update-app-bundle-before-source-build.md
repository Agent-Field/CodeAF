---
kind: fixed
title: codeaf update and /update tell a desktop app's engine to update the app, not to rebuild it
pr: 2840
surface: [engine, chat, app, docs]
invalidates:
  - "Was true: a codeaf inside a macOS app bundle that was built from a bare commit, which the desktop app's engine always is, got the source-build answer from `codeaf update` and `/update` (`this codeaf was built from source · rebuild with make build, or install a release`), so the app-bundle refusal from #2820 could not be reached from the shipped app. Is true now: both ask the app-bundle refusal first and answer `this codeaf is part of WorkAF.app · that app updates it, so update the app`; other source builds still get the source-build answer."
---
