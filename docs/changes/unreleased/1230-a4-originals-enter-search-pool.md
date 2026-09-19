---
kind: fixed
title: hybrid search admits original sessions when a nearer mailer family is larger
pr: 1230
surface: [chat, engine]
invalidates:
  - "SearchLexical and SearchEmbed truncated to `limit` passages, so two-turn abandoned-mailer chats could occupy every hybrid slot at 10k scale (`a4_abandoned_hits_in_top20=20`). They now read a wider window and keep unique sessions (plus a few extra turns), so signed-in originals enter SearchEvidence even when that mailer family is the majority of nearer hits."
---

Demoting hits that contain `abandon` cannot recover A4 when the ranked pool
never contained `a4-source`. Live 10k re-eval is a later pass.
