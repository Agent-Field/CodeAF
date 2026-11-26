---
kind: added
title: Move app data between live Postgres, a mirror and native storage
pr: 2964
surface: [app, engine, chat]
invalidates:
  - A live Personal Postgres read used to stay live for the life of its binding. A kept type can now use a mirror, adopt native write authority and return to the source.
---

Review row counts and conflicts before applying a cut-over. The app version and
record IDs hold across modes. The mirror polls for source changes, keeps
source revisions for approved write-back, and reports freshness. Returning
from native replays edits through the approved reverse mapping.
