---
kind: changed
title: Every factory recipe writer saves the YAML pipeline
pr: 1800
surface: [chat, engine, docs]
invalidates:
  - "Banking stages or habits, the first recipe offer, settings save and factory_recipe wrote .codeaf/factory.md. They now write .codeaf/factory.yaml through pipeline's shared encoder; Markdown is read-only compatibility input when YAML is absent."
  - "codeaf factory steps printed the older Markdown defaults and different pipeline groups. It now prints pipeline's YAML built-in prompts, stop sentences and round caps."
---

YAML edits preserve start rules, size thresholds and unrelated step definitions.
Policy and habits share the YAML rules list. Taught changes still use a temporary
worktree and factory/recipe-<word> branch; codeaf factory convert still prints
YAML without writing either input or output.
