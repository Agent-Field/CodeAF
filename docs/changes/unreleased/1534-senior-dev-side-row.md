---
kind: fixed
title: a side row hides its clock while its page is open; senior-dev manual matches its row and card
pr: 1534
surface: [chat, docs]
invalidates:
  - "On c34a3a76d, a senior-dev row on the side list kept showing the age from the moment its task page was opened, beside the page's live time. The row now draws no clock while its page is open, and the live age returns when the page closes."
  - "The senior-dev manual said the side-list row shows the step and spend under it. The row is one line (#1494). The step and spend are in the row's hover hint and the task page header."
  - "The senior-dev manual said the landed card says `ended`. It says `done` for a finished run, `stopped` for a stopped one, and `ended` only for a run that ended without finishing."
---
