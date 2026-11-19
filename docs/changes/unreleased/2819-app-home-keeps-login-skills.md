---
kind: fixed
title: codeaf app --home keeps reading other tools' skills from the person's own home
pr: 2819
surface: [app, engine, chat]
invalidates:
  - "Was true: under `codeaf app --home <dir>` the login home the engine read other programs' skills from followed `CODEAF_HOME`, so the desktop app looked for `<dir>/.claude/skills`, `<dir>/.codex/skills`, the Claude plugins and the other harnesses' folders and no longer imported the skills a person kept for Claude Code, Codex and the rest. Is true now: `codeaf app --home <dir>` records the person's real login home in `CODEAF_LOGIN_HOME` beside the `CODEAF_HOME` it sets, `home.Login()` answers it, and those folders, plugins and Codex's bundled skills are read from the real home exactly as a plain `codeaf app` reads them, in the engine and in the processes it starts. Codeaf's own user folder stays `<dir>/.codeaf/skills`, and the command line's `~/.codeaf/skills` is not read. A process that sets only `CODEAF_HOME` still gets that folder as its login home, so disposable runs and tests keep their quarantine."
---
