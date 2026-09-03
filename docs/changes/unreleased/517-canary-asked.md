---
kind: changed
title: a canary chat cell waiting on the person with nothing running reads asked, not wall
pr: 517
surface: [build, chat]
invalidates:
  - "A canary chat cell that sat on the needs-you card (`finished, but needs your look …` with `[a] accept · [l] look again · [n] not right · [d] decide these for me`) until the 900 s wall was reported as `wall`, indistinguishable from a cell whose task never finished. With the status line idle, no task on the rail and the call log quiet, the driver now ends the cell as `asked`, records the card line as `blocked_on` and the moment as `asked_s`. A card beside a running task is still `wall`: per #513 the top task is genuinely running, so the three 42b9e3ec rows on #407 keep their verdict."
---
