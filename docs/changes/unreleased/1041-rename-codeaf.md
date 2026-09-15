---
kind: renamed
title: The product, its binary, its module path, its state root and its variables are codeaf
pr: 1041
surface: [chat, engine, build, docs]
invalidates:
  - "The product was called aforge (also AForge, and aforge-v2 for the repository; openaf was the final name docs/CHAT-V3.md Decision 26 planned and never shipped). Its one name and one spelling is codeaf, lowercase everywhere a person, a model or a build meets it — sentence starts, titles, release names, the wordmark and `--help` included. The binary is `bin/codeaf`, the command is `cmd/codeaf`, the tools are `cmd/codeaf-*`, and the module path is `github.com/Agent-Field/codeaf`."
  - "A wide textual pass was the only thing keeping retired spellings out of live files. `internal/namelaw` is a law now — it walks every Go source under `cmd`, `internal` and `bench`, the build, workflow and script surfaces, both manual corpora, the session prompts and the documents an agent reads, and fails the pull request naming file and line. Three places may still say the old name: the record (`CHANGELOG.md`, `docs/changes/`, `docs/design/`, frames, `bench-results/`, `audit-notes/`, `testdata`), a line marked `legacy-name`, and a Markdown section whose `## ` heading contains the words *old name*."
  - "The state root was `~/.aforge`, moved by `AFORGE_HOME`. It is `~/.codeaf`, moved by `CODEAF_HOME`. A `~/.aforge` found on the first real start with no `~/.codeaf` beside it is adopted — renamed, with `~/.aforge` left as a link to it — so conversations, keys, settings and an old `~/.aforge/bin` on PATH are where they were; when neither variable is set and only the old folder exists, or the move cannot happen, the old folder is read as it is. Nothing moves when either variable is set."
  - "Every `AFORGE_*` variable is `CODEAF_*`. The old spelling is still read, for one release, whenever the new one is unset or empty; product code reads them through `internal/env` and nowhere else, and a law in that package holds the door. Skills receive both `CODEAF_SKILL_DIR` and `AFORGE_SKILL_DIR`; the timer units the product writes carry both `CODEAF_HOME=` and `AFORGE_HOME=`."
  - "A repository's own settings lived in `<workspace>/.aforge-v3/config.json`, its task worktrees and media under `<workspace>/.aforge-v3/`, and autonomy and subharness bundles under `.aforge/`. They all live under `<workspace>/.codeaf/`; the old folders are still read — a task worktree left under `.aforge-v3/tasks` is still found, resumed and cleaned — and nothing rewrites a person's repository on its own."
  - "Release assets were `aforge-<os>-<arch>` under the title `AForge <tag>`, installed into `~/.aforge/bin` by an installer that named the repository aforge-v2. They are `codeaf-<os>-<arch>` under `codeaf <tag>`, installed into `~/.codeaf/bin`; `release.yml` publishes to `${{ github.repository }}` so it is right before and after the GitHub rename, and the installer still finds older releases' `aforge-*` assets and the old repository name until then."
  - "The standing timer's units were `aforge-tick.timer`/`.service` and `ai.agentfield.aforge.tick`. They are `codeaf-tick.*` and `ai.agentfield.codeaf.tick`; installing the new timer removes an old one so no machine ticks twice, and a unit written with `AFORGE_HOME=` is still read."
  - "Task commits were authored `aforge <aforge@localhost>`. They are authored `codeaf <codeaf@localhost>`, and the old author is still recognised as the task system's own work."
  - "The remote pairing bytes — the device-key prefix, the relay name salt, the HKDF label, the protocol tokens, the HTTP headers and the PAKE identities — still spell the old name on the wire, permanently and marked `legacy-name`, because a relay and a device that already paired must keep matching; only the words a person reads changed."
  - "The manual had no account of the rename. `internal/manual/chat/starting-codeaf.md` (formerly starting-aforge) carries a `## The old name` section: the date, the old repository name, the `~/.aforge` adoption, the `AFORGE_*` and `.aforge-v3/config.json` fallbacks, and the fact that the `CodeAF` of the benchmark threads is a different program. The packed-corpus build tag `aforge_packed_manual` is `codeaf_packed_manual`."
  - "`internal/manual/chat/running-from-the-terminal.md` told a person to pin a tag by putting `VERSION=<tag>` before `bash`, which sets the variable for `curl` and not for the script. The example is the piped form, `curl -fsSL … | VERSION=<tag> bash`, which is what `README.md`, `docs/rules/promotion.md` and `release.yml` already spell and what `internal/release`'s own test demands."
---

One pull request, so nothing lands half-renamed: a mechanical pass over every live
file, then the compatibility layer an existing install needs (the state root, the
variables, the repository-local folders, the timer, the task identity, the
installer), then the words — the manual's account of what it used to be called,
the prompt, the tmux needles — and a law so the next wide search never has to
happen. `CLAUDE.md` and `AGENTS.md` (one file, reached by a symlink) gain
`## The name` and `## The old name, and the three places it is still allowed`,
because an agent whose memory predates 2026-09-14 gets this wrong first and gets
it wrong silently.

Not in this change: the GitHub repository rename (an owner action; GitHub
redirects the old name), the consumers in other repositories, and the local
checkout's directory name.
