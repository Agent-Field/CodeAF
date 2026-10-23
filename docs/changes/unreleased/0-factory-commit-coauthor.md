---
kind: fixed
title: Factory-created commits include the shared harness co-author credit
pr: 0
surface: [engine, factory]
invalidates:
  - "Factory-created commits omitted the co-author trailer used by ordinary chat commits. The factory now uses that same account while retaining the person's commit authorship."
  - "The chat landing path owned the CONTRIBUTING attribution exception. Its bounded policy reader is now shared with factory commit creation."
---

Attribution is recorded when the factory creates a commit, before proof approval
binds its object ID. Clean, approved or published commits are never rewritten just
to add credit. Publication and maintenance keep the exact approved head. PR body
footers and publishing credentials are unchanged.
