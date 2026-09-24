---
kind: added
title: codeaf-probe, a test binary an agent drives to QA CodeAF the way a person uses it
pr: 1435
surface: [docs]
---

`codeaf-probe` is a separate binary a coding agent drives to test CodeAF:
it starts a pinned `codeaf chat` in its own isolated terminal and home, sends
keys, returns the settled screen, records every step, and ends only the
processes it started, by pid. It has no model of its own; the agent brings
the judgment. The design is in `docs/probe/DESIGN.md`, how to drive it in
`docs/probe/AGENT-GUIDE.md`, and how to run it in CI in `docs/probe/CI-RECIPE.md`.
