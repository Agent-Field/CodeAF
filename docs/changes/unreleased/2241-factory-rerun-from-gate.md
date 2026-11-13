---
kind: internal
title: Cover model step reruns from Manual approval gates
pr: 2241
surface: [engine, factory]
invalidates: []
---

Regression coverage checks Run again from here on Write while a Manual job
waits for approval before Test. Write calls its model promptly, finishes, and
asks for approval again without reporting a runner offline or a stopped chat
row. The checks cover the default worker capacity, two slots, and one slot.
