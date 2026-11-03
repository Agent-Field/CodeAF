---
kind: fixed
title: Factory asks retain their final punctuation
pr: 2304
surface: [factory]
invalidates: []
---

Typed step asks keep the owner's punctuation instead of trimming periods,
colons, semicolons and commas from either end. Question marks and exclamation
marks continue to be preserved. Terminal job asks keep punctuation after chips
are lifted out, removing orphan comma, semicolon and colon separators rather
than reusing the shortened title. Without chips, asks are only whitespace-trimmed. Short-title cleanup is unchanged.
