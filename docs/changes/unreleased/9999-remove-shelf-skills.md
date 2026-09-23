---
kind: removed
title: the first skills design comes out of the chat, so the settled one in #1409 lands on a clean tree
pr: 9999
surface: [chat, engine, resident]
invalidates:
  - "The chat belt carried `use_skill`, which listed the store's skill shelf and resolved a name to its path. It is gone from the belt, the belt facts and the manual."
  - "`/skill` and `/skills` opened a picker that attached skills to a conversation and drew a chip above the box. Neither command exists now, and there is no chip."
  - "Every message was matched against the shelf and carried a block of the skills its words chose, announced as a `skills carried:` notice and drawn as a dim `skills ·` row. No message carries a skills block now, and `Event.Skills` is gone."
  - "The system prompt carried an `Available skills` section read from the store, and with `memory.enabled` off it said skills were switched off. Neither section is rendered, and `Config.SkillsAwaitMemory` is gone."
  - "The system prompt's `# Skills` section told the model to open a skill before inventing a method. It is gone."
  - "The v3 launch and the resident imported SKILL.md folders from Claude Code, Codex and other harnesses into the store as `imported-provisional` facts (`ReconcileImportedSkills`). Nothing imports them now; both doors instead retire any such fact a store still holds."
  - "The manual pages `use-skill`, `putting-a-skill-in-front` and `skills-a-turn-used` are deleted. The manual now says skills from other harnesses are coming and are not in this build."
---

This was the first design from #1277's thread: a memory-backed shelf, the
`use_skill` tool, a per-message automatic choice, a `/skill` picker that
attached a skill, and the notice naming what a turn carried. #1409 replaces it
with the design #1277 settled on, which reads skill folders from disk and needs
no store.

What stays: `internal/skills` (discovery) exactly as it was, and the resident's
own forged skills. The resident still promotes procedures it watched run onto
its `.codeaf/skills` shelf, links them into `bin/`, and its planner still
attaches shelf skills to a task's leaves.
