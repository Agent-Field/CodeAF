---
kind: added
title: factories have a configurable display name
pr: 1800
surface: [chat, docs]
invalidates:
  - "The host config had no factory name and /v1/about used the address host. Optional name and codeaf factory name now set a display name, defaulting to the first watched repository owner or the host's short hostname."
  - "Factory identity on /v1 carried only address and version. Every response carrying factory now also carries name; /v1/about keeps its top-level name and Contract remains 1."
---

Names are trimmed, at most 40 Unicode characters, and refuse control characters.
An empty quoted name clears the setting; omission remains omission. Restart the
factory service to apply a saved name or a new watched selection to its name.
