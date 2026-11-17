---
kind: fixed
title: Cell-backed crew helpers keep distinct conversation identities
pr: 1964
surface: [engine]
invalidates: []
---

App agent identity now comes from the session folder for cell and legacy
transcripts. Accepting a crew proposal preserves the original manager and each
helper's identity, assignment and reporting relationship. Existing desktop state
containing collapsed `.cell` identities is not migrated.

Inbox Later now puts a crew proposal aside without answering it. The proposal
stays available in Later after reopening and can return to Waiting through Back.
Its manager and proposed helpers remain unchanged until it is accepted or kept
as one agent.
