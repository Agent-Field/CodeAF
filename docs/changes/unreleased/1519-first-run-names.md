---
kind: fixed
title: a devaf first run reaches its own binary, shows the usage notice on screen, prints no raw lines
pr: 1519
surface: [build, chat, engine]
invalidates:
  - "A bash worker's `codeaf …` (the page teaches `codeaf patch`) resolved on the machine's own PATH, because the run put only `bin/plandb` there: `command not found` on a devaf, stageaf or `--name` install, or an older codeaf answering `there is no \\`codeaf patch\\``. The run's folder now also holds `bin/codeaf`, which execs the running binary that codeaf registered at start (`session.SetRunningCLI`). A process that registered nothing gets a one-line refusal with exit 127, never another codeaf."
  - "The worker's `plandb` was found by probing `<self> plandb status` beside the live store. That probe could meet `database is locked` and fall through to a PATH codeaf, or fail the task with `no plandb CLI found`. The registered running codeaf is now taken unprobed, right after `CODEAF_PLANDB_BIN`."
  - "A chat printed the telemetry notice to stderr and marked it seen just before the full-screen surface covered it. The first conversation's screen now draws it, and it is marked seen only after a frame drew it; nothing is sent before that. Task commands and `chat --once` still print it to stderr."
  - "A keyless first launch logged `media: no generation endpoint on this install: provider API key is required` onto the terminal. Keyless media is now absent without a line."
  - "The Model Pool judge kept the boot's settings, so a landing in the first session after setup was judged with no key and marked judged for good. It now reads the live key when it asks, and a judge with no key leaves the landing for the next start."
  - "A `--name devaf` install's receipt said `installed codeaf dev-…`. It now says `installed devaf · codeaf dev-… built …`. The installer never printed the telemetry notice, whatever the manual and GUIDE said."
---

Found by a fresh-install check of the published dev build, installed as `devaf`
through the real installer, with an older codeaf still on the machine's PATH.
