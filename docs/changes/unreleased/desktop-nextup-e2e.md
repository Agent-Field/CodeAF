---
kind: added
title: Desktop Next up end-to-end coverage
surface: [desktop]
invalidates:
  - "Next up browser coverage only walked two conversations and measured bulk Accept in isolation. A five-item, three-place engine fixture now checks the shell queue, ordering, Skip, answering, origin return, bulk Accept and Undo, and the four-second arrival banner in both themes and browser engines."
---

The fixture also keeps a question in the starting conversation to prove it is
excluded from the frame pill and bulk suggestions. Irreversible suggestions stay
outside bulk acceptance, and clock-controlled checks avoid real timer waits.
