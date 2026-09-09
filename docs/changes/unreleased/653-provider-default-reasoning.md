---
kind: changed
title: Leave default reasoning to the model
pr: 653
surface: [engine, chat]
invalidates:
  - "Unconfigured chat and worker calls forced high reasoning. They now omit the reasoning override and use the selected provider/model defaults."
  - "The thinking settings row called absence off. It now calls it auto; old off settings remain readable, and explicit saved levels are preserved."
---

Auto is absence of a request override, not an instruction to disable thinking or
an OpenRouter effort value. The CLI accepts auto to clear the launch override
and inherit lower scopes. Explicit conversation, task and role settings retain
their precedence. Tests inspect both tool and final-answer HTTP requests from
fresh, auto and explicitly high profiles. Provider defaults may still reason;
this change does not promise a fixed response time.
