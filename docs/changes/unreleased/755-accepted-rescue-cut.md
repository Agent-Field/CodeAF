---
kind: fixed
title: Accepted rescue cuts reach the bounded call retry
pr: 755
surface: [chat, engine]
invalidates:
  - "When every hedge arm failed, the provider always reported the primary arm's error even after a later rescue had accepted and streamed. An accepted rescue's typed stream cut now reaches the session's bounded retry, which clears its partial reply before asking again."
---
