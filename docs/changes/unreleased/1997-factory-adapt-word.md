---
kind: added
title: A pipeline file and the factory can say how much a plan may reshape a job's steps
pr: 1997
surface: [engine]
invalidates:
  - "Only a Markdown recipe could say `adapt`, `ask` or `fixed` (per section); a `.codeaf/factory.yaml` could not, so a YAML repository's plan always reshaped within the bounds. The file now takes a top-level `adapt:` (adapt, ask or fixed) for every pipeline it lists, the encoder writes it back, and a personal file may tighten it but never loosen it."
  - "The factory's own rules gain an optional `adapt` word (`GET`/`PUT /v1/rules`, `factory.rules_set {…, adapt?}`): omitted on a PUT keeps the word, `\"\"` clears it. It only tightens a repository's word, it travels with the job (`house_rules.adapt`), and a run's edit by the plan or the manager reads it on whichever machine runs it; a person's own edit is never bound by it."
---
