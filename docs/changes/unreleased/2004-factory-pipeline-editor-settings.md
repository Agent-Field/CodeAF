---
kind: added
title: The pipeline editor sets adapt, required steps and what starts on its own
pr: 2004
surface: [engine, docs]
invalidates:
  - "Was true: a repository's `adapt:` word, its `fixed:` steps and its `start.labels`, `start.max_size` and `start.max_cost` were read by the factory but could only be changed by editing `.codeaf/factory.yaml` by hand. Is true now: behind the feature `factory.pipeline_edit_settings`, factory.pipeline_source carries the file's `adapt`, `labels`, `max_size` and `max_cost` (the defaults when it says none), and factory.pipeline_preview takes `set_adapt` (adapt, ask or fixed; adapt is written as no key), `set_fixed` (one step marked required or not; proof is always required), `set_start` (max_size XS to XL, max_cost in dollars) and a `set_list` of the list `labels`. A preview with any of them answers `settings`, every setting whose words changed. The pull request is the same one every other edit opens."
---
