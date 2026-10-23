---
kind: added
title: The app can ask a factory to check GitHub now, and reads when it last did
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: a factory read GitHub only on its own tick (or when a repository was watched or a read grant arrived), and nothing on the wire could ask it to read now. Is true now: `POST /v1/github/poll` wakes the poll at once and answers before the read, and the app's `factory.refresh {factory}` (feature `factory.refresh`, answer `{}`) sends it."
  - "Was true: `desk.sources[].polled` was sent as `0001-01-01T00:00:00Z` when no read had gone well yet. Is true now: it is absent until one has. It is when GitHub was last read without a failure, the time a client shows as last checked; no second field says the same thing."
---
