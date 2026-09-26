---
kind: fixed
title: telemetry --help exits 0, the manual names six --help groups, a wall tile shows its first age
pr: 1533
surface: [chat, docs]
invalidates:
  - "On c34a3a76d, `codeaf telemetry --help` answered `telemetry takes one of: status, info, show, on, off` and exited 1, and each telemetry verb's `--help` printed `flag: help requested` and exited 1. The group and every verb now print their usage and exit 0, and `off --help` changes nothing."
  - "The manual said `codeaf --help` prints five groups. Since senior-dev (#1488) it prints six: Talk to it, Hand it work, Hand it a whole task, Look at what happened, Housekeeping, Plan work by hand. A build that carries no program prints five."
  - "On c34a3a76d, the wall drew an idle conversation's tile with a blank activity line on its first read after launch. The tile now says `updated … ago` from the transcript's last write."
---
