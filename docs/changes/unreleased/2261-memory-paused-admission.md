---
kind: fixed
title: codeaf keeps paused job memory pending without spending
pr: 2261
surface: [engine]
invalidates:
  - "A memory pass could keep spending after its job paused without appearing in the job's spending."
---

Pause and Stop end a job's background memory work, including work left behind
by a finished step. Paid requests already sent still charge the job when their
receipts arrive within the existing settlement window. Paused jobs admit no new
memory calls. Pending sources remain for a later finished turn after Resume;
Resume itself does not start another memory pass. Completed steps stay done.
