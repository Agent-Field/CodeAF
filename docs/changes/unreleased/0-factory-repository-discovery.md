---
kind: fixed
title: GitHub setup can list large accounts without per-repository pipeline reads
pr: 0
surface: [engine]
invalidates: []
---

Connecting GitHub lists repositories before loading their pipelines. Pipeline
sources remain unknown until a selected repository is read, avoiding a timeout
for accounts with hundreds of visible repositories. Setup still starts no job
or runner. An opt-in live test checks real repository intake and checkout.
