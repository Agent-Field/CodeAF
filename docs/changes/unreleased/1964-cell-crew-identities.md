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
