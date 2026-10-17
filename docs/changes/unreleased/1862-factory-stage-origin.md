---
kind: fixed
title: Keep factory stage ownership and instructions truthful
pr: 1862
surface: [engine]
invalidates: []
---

Factory-owned crews and stage memberships now carry explicit additive origin flags. A stage's recorded opening instructions are redacted before storage and exposed read-only, without a fictitious manager or edit history; ordinary member briefs remain editable. Existing jobs can recover ownership from their recorded room and stage transcript references without guessing instructions. A person's unrelated crew named `factory` is left untouched when a dedicated factory crew is needed.

Legacy recovery can only mark stage chats still named in the job record. An earlier round whose chat reference was overwritten remains unmarked; ownership is never guessed from a member name or handle.
