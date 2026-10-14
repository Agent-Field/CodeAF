---
kind: fixed
title: provider connections read complete bounded model catalogs
pr: 1822
surface: [chat, engine, docs]
invalidates:
  - "Connection checks read only four KiB of a successful model list, so ai&'s valid larger catalog became incomplete JSON and its key could not connect. Successful model-list probes now read the complete response up to discovery's shared sixteen-MiB bound and refuse oversized responses instead of accepting a truncated prefix."
---
