---
kind: added
title: App model choices, binary settings and resumable first launch
pr: 1800
surface: [engine, docs]
invalidates:
  - "The app transport exposed keys but no model or settings resources. It now lists connected models, saves default and pinned choices, exposes Checks and Planner, and commits binary settings atomically."
  - "The app had no durable welcome step and no setup day cap. Its four-step first launch resumes after restart, uses the provider's first returned model, and writes a fresh $20 day cap to the same native settings keys."
  - "Browser provider sign-in was available only through terminal launchers. The app now starts and cancels OpenRouter PKCE sign-in, connects its memory credential and lets the authenticated native core claim it once for secret storage."
---

App effort is low, medium or high. New agents read the saved model, effort,
budget and approval mode; explicit creation fields win. Conditional settings writes compare an
opaque profile version under the shared file lock and preserve terminal keys.
New app keys are stored and validated; their live enforcement remains with
the owning resource lanes. UI-only settings never enter the binary profile.
