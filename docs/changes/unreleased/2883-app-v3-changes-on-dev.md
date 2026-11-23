---
kind: internal
title: Five changes that were only on app/v3 are now on dev
pr: 2883
surface: [app, engine, docs]
invalidates:
  - "Was true: app/v3 was landed on dev on 2026-11-11 and nothing on app/v3 was missing from dev, so a release cut from dev carried everything the desktop app's engine had. Is true now: five pull requests merged into app/v3 after that day (#2645 the GitHub connector, #2649 independent custom providers, #2819 `app --home` keeping the login home, #2820 and #2840 the update refusal for an engine inside a macOS app bundle) were absent from dev, and this change brings each of them over as its own commit, so dev has them too."
---

Each ported change keeps its own entry under its original pull request number.
