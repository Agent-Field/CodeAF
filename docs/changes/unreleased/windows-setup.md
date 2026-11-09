---
kind: fixed
title: First setup uses the shared model default and reports connection and machine readiness
surface: [chat, engine, app]
invalidates:
  - "Fresh app setup omitted its default model. It now supplies the CLI default; saved model choices remain intact."
  - "Launch and doctor trusted the presence of an OpenRouter key. Launch now checks auth once in the background, caches the timestamped result for setup and health, and records a real-turn 401 as invalid; doctor checks auth directly."
  - "Doctor omitted git identity, gh authentication and Windows shell readiness. Doctor and health.check now report these with bounded probes and fix hints in the user's normal environment."
  - "Windows browser opening and Git Bash installation left first-run recovery to the user. The opener now uses Windows file protocol handling and the installer updates the user PATH or prints the exact command."
---
