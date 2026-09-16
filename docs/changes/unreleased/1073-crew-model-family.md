---
kind: added
title: A model family toggle under the crew, so a preset can answer from open weights or from everything
pr: 1073
surface: [chat, engine]
invalidates:
  - "The three crew preset words (frugal, balanced, max) always resolved to open-weight models, and `crewModels` in `internal/config/crew.go` was the whole table of what they meant. There is now a second table `crewAllModels`, and `CrewModelsForSource(source, preset)` picks between them on the new `models.crew.source` setting (`open` default, `all` for closed and frontier). A profile that never answers the row still resolves the open table."
  - "The careful seat was only ever required to be a different vendor from the worker by convention. It is now asserted by a test over both families: within each preset of `crewModels` and `crewAllModels`, the worker and high ids name different providers."
  - "`models.crew` was the only crew setting. The crew row's derivation and `ApplyCrew` now also read `models.crew.source`, so the preset word and the five ids it summarizes can never be drawn from different families; flipping the family reads the crew as `custom` until a preset is re-applied, which is the point of a meaning toggle rather than a write."
---

The crew shipped open-weight models for a reason: a default nobody's pricing can
move under them. That reason still holds for the default, so the toggle defaults to
`open` and changes nothing for anyone who does not ask. It is for the person who
has decided the frontier models are worth it on a given machine, and wants the same
three preset words to mean them.

The two families are a table apiece, not a filter over one, because the picks are
argued seat by seat rather than chosen by a rule about vendors or price. The open
table is untouched; the all table is new and mirrors its three presets.
