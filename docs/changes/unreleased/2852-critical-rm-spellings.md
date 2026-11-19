---
kind: fixed
title: A recursive delete asks in any spelling, and a no tells the model it covered the act
pr: 2852
surface: [chat, engine, docs]
invalidates:
  - "Only `rm -rf /path` and `rm -fr /path` were critical commands, so `rm -r /path`, `rm -R /path`, `rm --recursive /path`, `rm -r -f /path`, `/bin/rm -rf /path`, `rm -rf -- /path` and a quoted path ran under an allow without asking. Every recursive `rm` aimed at an absolute path, `~`, `$HOME` or a bare `*` now asks, reported as `critical command \"rm -rf /*\"` or `critical command \"rm -rf *\"`."
  - "permissions.md promised that `rm -rf *` asks, but no table entry matched it, so it ran without asking. It now asks, and a test fails the build if the manual names a critical command the gate does not ask about."
  - "After a person's no, the model was told only `denied by the person: <rule>`, which names the pattern it was refused by and invites another spelling. The sentence now goes on: `They said no to this action, not to this spelling: ask before doing the same thing another way.` That sentence is what the model reads now."
---

Relative targets are unchanged: `rm -rf build` and `rm -r ./tmp` still run
without asking under an allow. `find -delete` and scripts that remove a tree are
not graded; the table stays a floor and not a sandbox, and the new sentence the
model reads after a no is what covers those.
