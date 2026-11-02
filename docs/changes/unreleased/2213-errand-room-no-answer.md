---
kind: fixed
title: An errand gets the output room it asks for and says when it gave no answer
pr: 2213
surface: [app, engine]
invalidates:
  - "agents.errand held max_tokens to 4096. It now allows up to 8192, and agents.errand.models reports the new bound."
  - "An errand that was billed and said nothing came back with no reason, or as invalid_json. It now answers reason no_answer, keeps its usage and is not cached."
---

A reasoning model could spend its whole output bound on a repository's AI notes
and leave nothing to show, while the person was still billed.
