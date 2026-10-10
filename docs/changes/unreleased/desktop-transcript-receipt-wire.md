---
kind: changed
title: Desktop renders structured decisions in the transcript
surface: [desktop, chat]
invalidates:
  - "Decision receipts, plans and remembered lines were folded as generic work notes. Structured asides now render in conversation order; grouped receipts expand, saved decisions redraw on reload, and live plan events render Go, Edit and Cancel."
  - "The learning tray only recognized learning metadata. It now recognizes the engine's canonical proposal field while retaining compatibility with older learning records."
---

Plan actions use the existing typed engine routes and retain the proposal on a
refusal. Engines that do not persist plan asides cannot restore them on reload.
Single decision asides have no recorded question identifier; their Why action
shows saved facts without inventing a question target. The decision-detail lane
still owns overturn and next-time controls.
