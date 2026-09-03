---
kind: fixed
title: a canary cell installs the environment its base was measured in, and the do verdict reads the exit ladder
pr: 596
surface: [build]
invalidates:
  - "A canary cell's environment was whatever pip resolved at that hour, so a recipe that measured green could fail to install later (virtualenv, `--group dev`, pip resolution-too-deep on 2026-09-03 across four tables). `pick.py` now captures a constraints file from the measured venv into `lib/constraints/`, records it on the pool entry, and `cell.sh` installs every rung under it; a cell that had to fall back unpinned says `unpinned` in its why column."
  - "The rig read every non-zero `aforge do` exit as `partial`. The door now exits on a ladder (0 done, 1 could not run, 2 ran and did not finish, 3 a limit stopped it, 4 needed a person) and the verdict column reads it: `ok`, `could-not-run`, `partial`, `wall` or `limit`, `asked`."
  - "A base suite that counted nothing with a red pytest exit was recorded as `0/0/0`, which a judge would read as a baseline. It is now `base_suite: null` with pytest's last line as the note, and the measurement runs pytest under its own `TMPDIR`."
---
