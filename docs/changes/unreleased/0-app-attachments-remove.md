---
kind: added
title: codeaf app drops a staged attachment it will not send
pr: 0
surface: [engine]
invalidates:
  - "A staged attachment stayed on the agent until a send consumed it, so a canceled draft or a partly staged message left chips behind. attachments.remove {agent, id} now drops exactly one; a missing or already sent id is not an error."
---

New method `attachments.remove {agent, id}` → `{}`. An engine that does not
keep staged attachments answers `unknown_method`. A send naming an id that is
not staged still consumes none of the others.
