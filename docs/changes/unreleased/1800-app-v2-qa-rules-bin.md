---
kind: fixed
title: Ask before installing software or searching outside the workspace
pr: 1800
surface: [engine, chat, docs]
invalidates:
  - "Missing libraries could lead the agent to install silently. The prompt now asks it to try existing tools first and use ask before any installation; common install and virtual-environment commands also always ask through the bash permission gate."
  - "A missing named file could lead the agent to search outside the workspace. The prompt now requires ask before outside reads, finds or searches, and offers to create the missing file here."
---

Local npm installs keep their existing bash policy; the instruction to ask
before installing still applies. The workspace rule is prompt guidance, not
a filesystem sandbox. The prompt byte caps remain unchanged.
