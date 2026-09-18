---
kind: fixed
title: a decision card under --yolo takes its own default instead of waiting for somebody
pr: 1117
surface: [chat]
invalidates:
  - "A decision card raised under `--yolo` put itself up and waited, the same as it would with somebody watching, so the run hung until something else timed it out. A card that carries a default now takes it and carries on, and the record says the default was taken automatically rather than chosen. A card with NO default is unchanged: it still parks, because it has nothing to take."
---
