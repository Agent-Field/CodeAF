---
kind: internal
title: Record dev ancestry in app/v3 with a true merge
pr: 2291
surface: [build]
invalidates: []
---

PR #2099 already integrated these dev features through a single-parent commit.
This true merge records the 49 dev commits in app/v3 ancestry while retaining
the published #2099 choices and later app/v3 work. Land this change as a merge
commit so future dev merges use the recorded ancestry.
