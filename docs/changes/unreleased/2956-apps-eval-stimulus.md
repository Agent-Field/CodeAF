---
kind: internal
title: Run platform stimuli in the app gallery eval
pr: 2956
surface: [app]
invalidates:
  - "The process adapter used to leave six platform probes unrun. The adapter now runs those probes through the data, process and run entry points when a gallery app publishes."
  - "The release eval used to send the initial status in its write fixture. It now checks that the lifecycle sets the initial status itself."
---

The finance case names the CSV record and fields expected by its checks.
CSV import uses the person's scope and the app reads the resulting totals
with its declared grant. The wire reports money in major units.
