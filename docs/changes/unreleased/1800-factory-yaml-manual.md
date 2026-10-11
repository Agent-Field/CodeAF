---
kind: changed
title: The factory manual teaches the YAML pipeline and its default-branch rules
pr: 1800
surface: [chat, docs]
invalidates:
  - "The manual taught .codeaf/factory.md knobs and stages, not a pipeline. The shared file is .codeaf/factory.yaml; a step is a prompt with optional until and max, and approve is a person."
  - "The manual described a main-branch recipe read requiring a checkout. Rules are read through GitHub from the forge's default branch; a checkout is an offline fallback, and a PR cannot change its own rules."
  - "Banking or saving Markdown was described as changing the shared recipe. Those doors still write Markdown, but an existing YAML file takes precedence; edit YAML on the default branch instead."
---

Point to docs/design/factory/FACTORY-YAML.md, document pipeline, steps, fixed and
rules, and explain the runner's round and name limits. `codeaf factory steps`
currently prints the older Markdown defaults; its prompts can be copied into
YAML, but its pipeline groups differ from the YAML defaults.
