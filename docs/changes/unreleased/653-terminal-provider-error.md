---
kind: fixed
title: Reject explicit failed completions before learning provider speed or cache affinity
pr: 653
surface: [engine, chat]
invalidates:
  - "An HTTP 200 completion ending with finish_reason:error could return success, refresh cache affinity and teach generation speed before the session rejected its empty answer. Both response formats now reject that terminal failure before successful-answer learning and release the automatic cache preference."
---

Reported usage still reaches billing once; a missing usage block can use the
existing generation receipt path. The normal bounded upstream recovery policy
handles the error, without changing provider choices, spending limits or
normal tool and reasoning completion semantics.
