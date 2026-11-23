---
kind: fixed
title: A quit while the sync service is down reports the local save again
pr: 2643
surface: [chat, engine]
invalidates:
  - "After #2381, quitting while the sync service was unreachable could report a chat whose turns were saved on this computer as not saved, because the hold renewal tried before the upload returned its error unclassified. The renewal's failure is now the same owed upload a failed upload reports: saved here, upload pending."
---

The renewal is the upload's own first step after an outage, so it reports its
failure the same way.
