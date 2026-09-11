---
kind: fixed
title: Reviewed trunk fixes remain intact in the conversation integration
pr: 653
surface: [chat, engine, docs]
invalidates:
  - "The conversation draft predated the reviewed fixes on dev. It now includes their file-evidence, job-delivery, lane-pin, checkpoint, write-accounting and landing-provenance behavior."
  - "The imported check-source tests assumed that a shaped brief could declare executable checks through prose. The integrated runtime requires an explicit checks field; the original pasted-command reproduction now exercises that boundary through StartTask and keeps a separate explicit-contract control."
  - "The manual still said that backticks in a done-condition started a check. Only an explicit verification contract grants that permission."
  - "A moved commit on an unchanged branch now retains the task branch while preserving the runtime's instruction to inspect the result and honor the requested delivery workflow. Its recorded home commit survives storage and restoration."
---
