---
kind: fixed
title: aforge do takes its wall as a duration with a unit, and a bare number is still seconds
pr: 495
surface: [engine, docs]
invalidates:
  - "`aforge do -timeout` took an integer of seconds and refused anything else: `--timeout 5m` was `invalid value \"5m\" for flag -timeout: parse error`. It takes a duration now — `5m`, `2h`, `90s` — like `AFORGE_PRACTICE_IDLE`, `AFORGE_BRIEF_AFTER` and `--max-hours`. A bare number is still read as seconds for one release, so `-timeout 900` keeps working; the help line and docs/HEADLESS.md say so. Zero, negatives and nonsense are refused with an error that names the flag. The default is written `15m` and is the same fifteen minutes it was. First landed as #379 and reverted by #494 for spelling the default `15 * time.Minute`, which internal/exec's leaf-room law read as the subharness table's own shape. The default is spelled `15 * time.Minute` again: that law now matches the write that hands a leaf its room rather than the digits, so a run's wall is not one of its business whatever it is spelled as."
---
