---
kind: added
title: skills kept in OpenCode's and Goose's folders are found too
pr: 1409
surface: [chat, engine]
invalidates:
  - "Skill discovery read six folders — .codeaf, .agents, .claude, .codex, .cursor and .gemini skills — so a skill kept only for OpenCode or Goose was invisible. It now also reads .opencode/skills and .goose/skills, under the project and the home directory, after the other six."
---

The two folders rank last in each scope: a name already held in any of the first six folders owns it, and the OpenCode or Goose copy is kept in the result marked shadowed. Issue #1277's day-one list names both.
