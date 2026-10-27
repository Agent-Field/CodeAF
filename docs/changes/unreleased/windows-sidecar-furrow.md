---
kind: fixed
title: Windows desktop sidecars carry the pinned native furrow engine
pr: 1987
surface: [engine]
invalidates:
  - "The Windows desktop sidecar cleared staged furrow engines and shipped without one even after native Windows artifacts were pinned. It now fetches and verifies its pinned engine before building, and a failed fetch stops the build."
  - "Explicit FURROW_ARTIFACT and FURROW_SOURCE overrides silently bypassed the release pin. The fetcher now announces the staged path, target platform and pinned version without preventing deliberate developer overrides."
---

Windows sidecars retain plain Go builds because Microsoft Defender quarantined
the garbled executable. They do not need to install garble. Native Windows
extracts the embedded engine as an `.exe` on first use.
