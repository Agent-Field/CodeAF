---
kind: fixed
title: Desktop Home hides empty sources and handles removal failures
surface: [desktop, chat]
invalidates:
  - "An empty Sources section could appear on populated place Home when Add was available. The section now appears only with real sources."
  - "Source removal failures were unhandled. They now keep the row and show the engine sentence, with repeated writes disabled while pending."
---

The focused SourcesList component uses the existing place action and shared
receipt Undo. Original files, folders and URLs are not deleted.
