---
kind: changed
title: cmd/codeaf runs in four shards in the touched-packages gate
pr: 2443
surface: [build]
invalidates:
  - "make test ran cmd/codeaf as one unsharded package; it is now in HEAVY_PKGS beside internal/tui3 and internal/session and splits into SHARDS shards."
---

Run whole, cmd/codeaf passed the fifteen-minute per-package ceiling on the
four-vCPU runner once pull requests into app/v3 began running the gate.
