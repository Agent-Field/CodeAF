---
kind: fixed
title: Desktop connections add separate custom endpoints and refuse empty models at save
pr: 2649
surface: [app, engine]
invalidates:
  - "The app's custom connection flow reused the first endpoint when adding another address. Explicit add now returns a separate connection identity and refuses stale identity collisions."
  - "An interactive app save trusted an earlier connection check and could accept a provider without usable models. It now proves the current catalog before adopting credentials or saving the connection."
---

The additive mode and connection ID preserve older client behavior and offline
launch credential handover. Profile write failures do not adopt credentials;
retry after repairing storage keeps one row. Browser sign-in is unchanged.
