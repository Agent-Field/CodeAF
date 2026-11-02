---
kind: fixed
title: attached media keeps container facts already present in its bounded probe
pr: 0
surface: [engine, app]
invalidates:
  - "Was true: RecordAttachment discarded WAV and MP4 container facts even when the existing bounded header read already held them. Is true now: it records a duration or frame size only when the same container parser can establish it from those held bytes. Larger, truncated and unsupported inputs stay unknown; the probe limit and file-read cost do not grow."
---

This is a bounded input-metadata correction, not complete format support or a
historical backfill. It does not trust a browser decoder or a requested duration.
