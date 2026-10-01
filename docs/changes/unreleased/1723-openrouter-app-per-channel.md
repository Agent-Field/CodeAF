---
kind: changed
title: dev and staging builds report to OpenRouter as their own apps; a release stays AgentField AI
pr: 1723
surface: [chat, engine, build]
invalidates:
  - "Every codeaf build reported to OpenRouter as AgentField AI (https://agentfield.ai). Only stable and release-candidate builds do now; a staging build reports as codeaf staging (https://staging.codeaf.agentfield.ai), and a dev build or anything built from source as codeaf dev (https://dev.codeaf.agentfield.ai)."
  - "internal/provider/attribution.go said the attribution values were constants nothing could vary. The binary's own release stamp now picks one of three identities; the environment still cannot change it."
  - "The release-tag grammar was spelled in internal/update. It lives in internal/buildinfo (Channel), and update reads its patterns from there."
---

The team's own dev, staging and source-build usage was landing on the release's
OpenRouter app page. Each identity is a separate origin because OpenRouter groups
referers by origin: `https://agentfield.ai/codeaf` resolves to the AgentField AI app.
