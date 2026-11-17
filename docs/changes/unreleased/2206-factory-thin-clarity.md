---
kind: fixed
title: A read issue is thin only when its rubric scored the wanted result unclear
pr: 2206
surface: [engine]
---

Every read issue counted as thin, because the read no longer writes a
readiness number. The `thin` search word and a step on `when thin` now read
the rubric's clarity score.
