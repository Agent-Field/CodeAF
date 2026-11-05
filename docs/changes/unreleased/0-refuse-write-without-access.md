---
kind: fixed
title: Jobs require write access before every step that can write
pr: 0
surface: [factory, engine, chat, docs]
invalidates:
  - "Write permission was not checked consistently before work. Jobs now use one permission rule before writing steps, job chat, publishing and merging."
  - "Positive source permissions stayed cached for the source's lifetime. They now expire after fifteen minutes."
---

Outside jobs without a recorded tier are refused. Personal jobs keep their
existing permission. Workers check the service's current permission before
writing rounds without changing the saved job or worker report format.
