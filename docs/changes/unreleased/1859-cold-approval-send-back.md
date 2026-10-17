---
kind: fixed
title: Cold approval send-back resumes the preceding step with feedback
pr: 1859
surface: [engine]
invalidates: []
---

Sending a plan back when no worker holds the job now resumes the preceding step with the person’s feedback instead of immediately asking the same question again. The feedback is kept in the job’s notes for resumed work. Completed earlier steps remain completed, attempts advance consistently with a connected worker, and the next approval gate still requires a person’s answer. A gate with no earlier step stops cleanly and removes its waiting question.
