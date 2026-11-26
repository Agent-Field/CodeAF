---
kind: added
title: Render personal app records to PDF, Word, PowerPoint and CSV
pr: 2952
surface: [app, engine]
invalidates: []
---

Kit 2 apps declare named render Actions for one record or a query. A run can
render the same output and deliver a scheduled digest to the app inbox. Saved
files carry the app version, source and actor. A resumed run step reuses the
original file receipt.
