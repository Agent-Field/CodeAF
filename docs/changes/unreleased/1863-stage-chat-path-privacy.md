---
kind: fixed
title: Withhold stage conversation paths from job readers
pr: 1863
surface: [engine]
invalidates: []
---

Queue, job, step-edit and mutation responses no longer disclose the runner-local filesystem path of a stage conversation. The authenticated worker that holds the job lease still receives the conversation reference needed to resume its stage. Stored phase data and the shared worker codec are unchanged.
