---
kind: added
title: skills kept for OpenCode and Goose are read too, including their ~/.config folders
pr: 1691
surface: [chat, resident]
invalidates:
  - "codeaf read skills from six folders (.codeaf, .agents, .claude, .codex, .cursor, .gemini under skills/). It also reads .opencode/skills and .goose/skills in the project and the home, and ~/.config/opencode/skills and ~/.config/goose/skills in the home only."
---
OpenCode and Goose keep a person's global skills under ~/.config rather than in a
dot folder named after the tool, so those are read in the home scope, after every
shared folder, where a name kept by hand earlier still owns it.
