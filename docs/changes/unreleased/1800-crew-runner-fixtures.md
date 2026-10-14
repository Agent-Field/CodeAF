---
kind: internal
title: crew runner checks use deterministic folder and spending fixtures
pr: 1800
surface: [chat]
invalidates:
  - "The local directory attachment check expected an absolute display path even under the home directory. It now pins the display home and checks the shortened path while retaining the absolute registration assertion."
  - "The late-receipt spending check depended on wall time and could put its first receipt on yesterday near midnight. Its surface clock and receipts now use a fixed noon on the same day."
  - "Two home checks expected absolute project paths when the runner's temporary directory was under home. They now pin a display home and assert the project as ~/alpha."
---
