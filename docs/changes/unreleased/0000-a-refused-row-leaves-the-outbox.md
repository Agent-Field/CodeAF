---
kind: fixed
title: A row the relay refuses by line leaves the outbox and the rows around it are sent
pr: 0000
surface: [engine]
invalidates:
  - "An outbox Send that met a refused batch left every row of it, and every row behind it, pending: the relay refuses a whole batch on the first line it cannot validate and answers `400 {\"error\":\"line N: <why>\"}`, so one row the pool's schema would never accept — a payload written by an older or newer client, or schema drift — was retried at the head of the batch on every judged run and every start-up, refused again, and nothing from that install reached the pool again."
  - "A 400 whose reply names `line N` now marks the named row dropped — the one marker that keeps it gone across a reopen — and posts the rest of the batch again within the same Send, one POST per refused line at the most and still inside the Send's budget. A 400 that names no row, a 413 and a 429 still answer an error and keep their rows pending: none of them names a row, so retiring one would be a guess, and the pending cap still drops rows from the old end when the retries would pile up. 5xx is unchanged."
---