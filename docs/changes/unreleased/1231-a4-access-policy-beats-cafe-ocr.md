---
kind: fixed
title: emailed-receipt search prefers the access-policy decision over a billed-file refusal mention
pr: 1231
surface: [chat, engine]
invalidates:
  - "On f1423514, SearchEvidence for `emailed purchase confirmation PDF` ranked Cafe dinner slip OCR (restaurant paper, not a billed-file hyperlink policy) into every A4 top-20 slot. An emailed-receipt topic now prefers the session whose decision is authenticated / signed-in billed-file access over one that names billed-file only to refuse it; cafe, restaurant, abandon, and espresso are not denylisted."
---

Abandoned-mailer hits stay out of that A4 page. Live 10k re-eval is a later pass.
