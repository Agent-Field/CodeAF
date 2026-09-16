---
kind: fixed
title: an answered landing carries its answer's fate, and an in-flight settle holds the question down
pr: 1086
surface: [chat, engine]
invalidates:
  - "A landed task's `your call` question was re-raised with the same words on every node move while the node stayed unverified, even after somebody answered it — the session that reported it accepted the same card twelve times in an hour (#1077). The raise now consults the decision record, and a re-raised card leads with the answer's fate: `accepted 18:20 · nobody could check it`."
  - "A notice emitted while an accept, refute, re-audit or merge round was still running re-asked the question the person had just answered, seconds later and with no new fact. Node notices now carry `TaskNotice.Settling`, and a notice that names a resolution in flight raises nothing and withdraws nothing."
  - "An answer to a replayed landing card — a fresh window, a restored graph — settled the node but recorded no decision, so restarts lost it from the record (the reporting session had fourteen `you took this as done` receipts against twelve records). Replayed-card answers record now."
  - "A killed re-audit and a failed merge round left the node unverified with no word back: the question was held down or silently swallowed. Those roads release the claim and emit, so the question comes back with the failure as its fate."
---
