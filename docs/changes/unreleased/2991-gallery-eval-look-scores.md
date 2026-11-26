---
kind: added
title: Score built apps against the gallery in the live evals
pr: 2991
surface: [app, engine]
invalidates: []
---

The live gallery eval used to report only whether each built app passed its behavior checks. It now also asks a Critic model for a look report comparing each app's screens with reference captures, keeps that score separate from behavior, and fails a run that was asked for look evidence but did not produce all of it.
