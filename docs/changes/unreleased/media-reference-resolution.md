---
kind: added
title: Existing media references resolve to their recorded source identity
surface: [engine]
invalidates:
  - "A caller with an explicit canonical image link had to list media itself to discover an existing source. Optional media.resolve now finds the owning conversation's newest recorded canonical-path match with the same source ID, versions, state and provenance as media.get. It creates no source, grants no path capability and never promotes viewed evidence into generated output. Partial or capacity-limited searches answer Busy rather than false absence."
---
