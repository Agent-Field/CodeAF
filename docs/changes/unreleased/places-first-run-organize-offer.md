---
kind: added
title: People with many unplaced chats are offered Organize on All places
surface: [desktop]
invalidates:
  - "Organize now was reachable only in Settings. All places now offers it once when twelve or more chats are in no place and no plan was ever accepted; Not now is remembered by the engine and the offer returns only when the count doubles."
---

`GET /places/organize` carries an `offer` with the unplaced count while the offer is due, and `POST /places/organize/offer/dismiss` records Not now in `places-org.json`.
