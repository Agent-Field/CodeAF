---
kind: changed
title: the prompt says what a skill is, and a missed skill name says what the shelf holds
pr: 1347
surface: [chat]
invalidates:
  - "prompts/system.md never mentioned skills: a model was told how to call `use_skill` and what it answers, but never what a skill IS or that reaching for one beats improvising a method. It now carries the one rule — when a skill covers the work, open it before inventing a method — and prompts/bashtask.md carries the same rule in the worker page's voice."
  - "A `use_skill` get that matched nothing answered \"Skill 'x' not found.\" and nothing else. A miss now says how many skills are active and names the nearest handful, scored with internal/fuzzy against the name and the doc line, at most five; an empty shelf says so in one plain line. A name differing only in case resolves instead of missing, and the hit is answered with the shelf's own spelling. The hit result's shape is unchanged."
  - "`useSkillDescription` called skills \"execution-verified procedures\", the harness's own vocabulary. It now says what it always meant: procedures this project saved after watching them run."
---
