---
kind: fixed
title: concurrent provider-memo saves preserve every learned fact at restart
pr: 1824
surface: [engine, chat]
invalidates:
  - "Atomic provider-memo replacement kept valid JSON but could let an older in-process snapshot overwrite a newer learned fact. Saves now serialize from before snapshot through replacement, while requests still persist asynchronously; separate processes do not merge their caches."
---
