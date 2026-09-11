---
kind: fixed
title: Read finished tasks as request and answer
pr: 653
surface: [chat]
invalidates:
  - "Finished task rooms left every narration visible between phase chips. They now collapse completed work by exchange, leaving the request, corrections and final answer visible."
  - "An initial runtime-written assignment could replay as only its internal heading. Canonical task assignments now get a three-line expandable request preview with readable section labels and the child's own work first."
  - "A long task could lose its opening request from the saved view. The same entry budget now reserves the request and an explicit history seam before the recent tail."
  - "Room expansion bookmarks used entry indexes as work keys. They now track each fold's start fingerprint and apply only to the same fold style."
---

All model instructions and original journal payloads remain intact. Later runtime
notes stay in their own lane. Running pages keep phase folds; completion resets
phase expansion so it cannot open the entire answer's work by accident.
