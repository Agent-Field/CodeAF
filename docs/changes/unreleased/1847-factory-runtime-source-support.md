---
kind: added
title: Pinned pipeline source reads confirm which runtime settings the factory supports
pr: 1847
surface: [engine]
invalidates:
  - "The app could not distinguish a factory that runs runtime step settings from one that ignores them. Pipeline edit-source replies now state runtime_fields explicitly, and the sidecar requires that confirmation for files containing kind, when, effort or fanout."
  - "The pinned source view omitted runtime step fields. It now carries those fields and their locks without enabling any additional edit operation or write capability."
---

Legacy files remain editable with older factories. Runtime files without positive support are refused, while parsed steps and locks continue to be checked against the exact file. Words appearing only in comments or prompts do not count as runtime keys.
