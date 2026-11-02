---
kind: fixed
title: Factory retries no longer say you stopped the job
pr: 2222
surface: [engine]
invalidates:
  - "Was true: replacing a step's execution reported stopped in its manager chat, which could appear as Stopped by you. Is true now: Try again and Send back to Write report trying the selected step again; giving the job back to the pipeline does not report a person's Stop."
---

Try again says `trying test again` for a step named test. Send back to Write says
`trying write again`. Replacing the previous execution no longer adds a stopped
row or stopped progress fact. Taking over and giving work back preserve the
recorded results. Stop job and Stop step still report `stopped`.
