---
kind: fixed
title: Reopened agents keep background process identity and history links
pr: 0
surface: [engine]
invalidates:
  - "Kept job rows replayed with a different process id, no command or clock, and a made-up zero exit code; processes.list could be empty after open."
  - "Older bash history had no process link even when its result named the job it started."
---

Kept rows now retain the full command, start, end, optional exit code and owning
tool call. Replay uses the live process id and Label(), preserves failed state,
and leaves unknown facts unset (command "" and a zero start, as before; no ended_at or exit_code). A settled row with a start and elapsed time can
supply its ending when no explicit end was kept.

The app reads history once on open to fill old rows' command and tool link only
from bash results naming that job. History entries carry the same process link.
Process listing includes kept rows missing from the process shelf.
