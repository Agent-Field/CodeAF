---
kind: added
title: Take app requests through forms, email files, file drops and webhooks
pr: 2954
surface: [app, engine]
invalidates:
  - Kit 2 intake used to stop at declarations. A kept app can now create records from local form submissions, .eml files, CSVs, PDFs, images and connector events.
---

Intake records name their channel in history. Repeated deliveries return the saved receipt, and a channel can start a run for each new record.
