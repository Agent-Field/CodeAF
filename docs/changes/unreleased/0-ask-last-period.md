---
kind: fixed
title: Factory asks retain their final punctuation
pr: 0
surface: [factory]
invalidates: []
---

Typed step asks keep the owner's punctuation instead of trimming periods,
colons, semicolons and commas from either end. Question marks and exclamation
marks continue to be preserved. Terminal job asks retain the original input
rather than reusing the shortened title. Short-title cleanup is unchanged.
