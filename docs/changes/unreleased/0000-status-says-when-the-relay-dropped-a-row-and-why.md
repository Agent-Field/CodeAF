---
kind: fixed
title: status says when the relay dropped a row and why
pr: 0000
surface: [engine]
invalidates:
  - "A row the relay refused by line, and one the pending cap dropped, were retired as `dropped` with the reason thrown away: the outbox file held `{\"dropped\":\"<nonce>\"}` and no more. `pool status` counted only the pending rows, so it printed `pending 0 · can send yes · can read yes` for an install whose every measurement was being thrown away and for a working one alike, and a person could not tell the two apart."
  - "A dropped marker now carries the reason — the relay's own text after `line N:`, trimmed and capped at 200 bytes, or `over cap` for a row the pending cap aged out — and the outbox answers them with `Dropped()`, oldest first. `pool status` adds a dropped segment only when something was dropped — `pending 0 · dropped 2 (last: <reason>) · can send yes · can read yes` — and `--json` carries `dropped` (the count) and `dropped_last` (the most recent reason) beside the pending count. A file written before reasons were kept still parses: its drops read with the reason empty and the line says the count alone."
---
