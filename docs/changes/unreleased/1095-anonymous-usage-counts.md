---
kind: added
title: Anonymous usage counts, on by default with a notice printed once
pr: 1095
surface: [docs]
invalidates: []
---

codeaf now sends anonymous usage counts — version, OS, mode (chat or task), and
banded session and error counts — so the parts people rely on get the work. It
never sends prompts, code, paths, names, or anything that identifies you. The
counts are on by default, and the first session prints a notice naming exactly
what leaves; `codeaf telemetry show` prints what is waiting to leave right now.
Turn it off with `CODEAF_TELEMETRY=off`, `DO_NOT_TRACK=1`, `codeaf telemetry
off`, `telemetry = off` in a project's `.codeaf/config.json`, or an empty
`CODEAF_TELEMETRY_ENDPOINT`. The whole contract is `docs/TELEMETRY.md`.
