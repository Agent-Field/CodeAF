---
kind: fixed
title: original-neighbor ranking no longer waits for emailed-receipt tokens
pr: 1233
surface: [chat, engine]
invalidates:
  - "rankEvidence preferred original neighbors for non-question queries only when two of emailed|receipt|purchase|confirmation|pdf matched. A short look-up (plumber invoice, shipping label, purchase order, emailed receipt links) now takes that path from embedding plus low query-term overlap; those five receipt tokens are no longer required."
  - "Decision-versus-refusal elevation ran only for an emailed-receipt topic and only when the session named billed-file / purchase-document plus auth. A standing decision (must / require / authenticated fetch) now outranks a neighbour that names the ask only to refuse it, on any short look-up; cafe, restaurant, abandon, and espresso are still not denylisted."
---

A4 gold is still the signed-in billed-file source chats. A7 and global stay
coverage-ranked. Live 10k re-eval is a later pass.
