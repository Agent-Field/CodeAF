---
kind: fixed
title: Empty local model servers stay editable until a model is available
pr: 2647
surface: [chat, engine, docs]
invalidates:
  - "A custom address could advance and save after listing no models; it now stays editable with recovery instructions, and the final save also requires usable models for custom and key-optional local connections."
---

Cloud account admission and existing credentials are preserved. Localhost means
the machine running the engine; unreachable does not imply a missing runtime.
