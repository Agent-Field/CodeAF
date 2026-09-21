---
kind: fixed
title: chat names unread config keys once before using default models
pr: 1239
surface: [chat, engine]
invalidates:
  - "Unread profile config keys were reported only to process logs that chat users do not see. Hosted and no-host chat now name those keys in the existing conversation notice once per profile, and a changed key set shows once again."
---

The notice uses the unread-key result from profile loading rather than classifying
config keys again. Hosted mode carries that result across the engine and client
boundary; no-host mode carries it directly into the same notice mechanism.
