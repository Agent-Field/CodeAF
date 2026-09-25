---
kind: added
title: Skill facts gain trust, cost card and digest; briefs gain an ordered skills list
pr: 1327
surface: [engine, resident, chat]
invalidates:
  - "A skill's fact record carried only doc, scope, artifact, status and provenance. It now also carries trust (defaulting to authored), a cost card, and a sha256 content digest computed at install, and ActivateSkill takes that digest as a third argument — the two-argument call form no longer exists anywhere in the tree."
  - "store.NodeBrief had no skills field. It now journals an ordered Skills []string with the brief, and the worker prompt renders an attachment block (one doc line plus one shelf path per skill, earlier entries win conflicts) whenever the list is non-empty."
---

Tranche 1 of skills as attachments: the record work (trust, cost card, digest,
consumption-marked serving) and the attachment plumbing (ordered skills on briefs)
land first; the chat shelf, the use_skill worker tool, and the live wiring that
populates briefs from real proposals are landing on the same pull request. The
design and its reasoning are on issue #1277.
