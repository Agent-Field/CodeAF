---
kind: fixed
title: Skill questions reach a definition, capability answer and troubleshooting
pr: 1938
surface: [chat, docs]
invalidates:
  - "Asking what a skill is led with a folder list; the manual now returns a direct definition first."
  - "The four original skill capability questions had no explicit retrieval regression coverage; they now check their answer sections and memory-independent behavior."
---

The manual explains how to choose a skill, why one may be unavailable, and that
turning automatic memory saving off does not disable skills. Retrieval and
live-terminal tests cover the four questions from issue #1937.
