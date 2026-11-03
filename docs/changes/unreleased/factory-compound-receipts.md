---
kind: fixed
title: Compound check receipts require every exit before reporting a pass
surface: [factory]
invalidates:
  - "A zero exit could hide an unmeasured check in the same call. Every check must now have a zero exit before the call reads passed."
---

Any non-zero check exit keeps a compound call failed regardless of order.
Otherwise an unmeasured check keeps it completed and unpublished, even with PASS
output. Calls without checks retain legacy output classification, which now
rejects FAIL on the first line as well as later lines.
