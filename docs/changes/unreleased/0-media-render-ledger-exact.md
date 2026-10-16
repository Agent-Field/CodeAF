---
kind: fixed
title: a render list page is exact or refused, never stale
pr: 0
surface: [engine]
invalidates:
  - "Was true: render.list picked a page of renders and then read their later rows with the errors and the deadline thrown away, so a read that ran out of time served the first rows (running, uncharged) as though they were current; the forward read could also run from an old page to the end of the whole history; an oversize line was skipped; and a restore at open that could not finish still marked a partial set interrupted. Is true now: a page whose latest rows cannot all be read within the time and the 16 MiB resolve bound is answered busy (or with the read error) and no rows, an oversize line is an error, and a restore that cannot finish marks nothing, leaves those renders reading unknown, and retries on the next open."
---
