---
kind: fixed
title: a session stopped on a sub-harness question says so instead of working
pr: 1292
surface: [chat, engine]
invalidates:
  - "A sub-harness run blocked on its own question left the session reporting itself as working. The question was registered and readable, but the predicate behind presence never read that lane, so no row appeared on home, no mark was drawn, and the work stopped in silence until somebody happened to open the conversation."
  - "That lane is now read like the lanes beside it, and it banks its row at the presence desk when it raises, so the session says a person is needed and the row carries the question itself."
---

Three sibling lanes share the shape this fixes and are deliberately left: sign-in,
the sub-harness offer and the harness design card each raise without banking a
desk row. They belong to the desk piece, which needs the owner's word on the
state words first.
