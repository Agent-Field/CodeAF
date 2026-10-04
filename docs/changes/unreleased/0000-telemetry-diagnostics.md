---
kind: changed
title: Usage receipts distinguish routing, model families and missing counts
pr: 0
surface: [engine, remote, build, docs]
invalidates:
  - "Usage telemetry carried only positive token totals. It now includes bounded routing and model-family categories and explicitly records missing provider receipts without token totals."
  - "Provider usage appends ran on background goroutines. Completed receipts are now appended before accounting returns, while network delivery stays periodic."
  - "Missing release-download credentials silently passed the scheduled job. Production reporting now fails visibly when required credentials are absent."
---
