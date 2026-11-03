---
kind: fixed
title: codeaf gives paid job calls a bounded settlement on quit
pr: 2261
surface: [engine]
invalidates:
  - "Quitting cancelled the worker's final accounting request before it could report paid calls."
---

After cancelling work, a quitting worker gives its existing accounting receipt
up to five additional seconds to report paid calls before removing its local job
copy. This applies to ordinary stage calls and memory alike. Later receipts are
not guaranteed to reach the owning job, and pending recovery is not durable.
