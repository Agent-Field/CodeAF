---
kind: renamed
title: the product speaks its own name, codeaf, and a law keeps the old one out
pr: 1042
surface: [chat, build, docs]
invalidates:
  - "The product was called `aforge`, and one planned wordmark called it `openaf`. It is `codeaf`, lowercase, everywhere a person, a model or a build meets it — sentence starts, titles, release names, the wordmark and `--help` included."
  - "The manual had no account of the rename. `internal/manual/chat/starting-codeaf.md` now carries a `## The old name` section: the date, the old repository name, the `~/.aforge` adoption, the `AFORGE_*` and `.aforge-v3/config.json` fallbacks, and the fact that the `CodeAF` of the benchmarks is a different program."
  - "`## What it is called` said the surface used to spell one name in the wordmark and another in the prose under it. That sentence named two retired products and read as a tautology after the rename; the history is in `## The old name` now and that section states only what is true today."
  - "A wide textual pass was the only thing keeping retired spellings out of live files. `internal/namelaw` is a law now — it walks every Go source under `cmd`, `internal` and `bench`, the build, workflow and script surfaces, both manual corpora, the session prompts and the documents an agent reads, and fails the pull request naming file and line."
  - "Nothing said where the old name is still allowed. Three places are: the record (`CHANGELOG.md`, `docs/changes/`, `docs/design/`, frames, `bench-results/`, `audit-notes/`, `testdata`), a line marked `legacy-name`, and a Markdown section whose `## ` heading contains the words *old name*. Everywhere else is a build failure."
  - "`internal/manual/chat/running-from-the-terminal.md` told a person to pin a tag by putting `VERSION=<tag>` before `bash`, which sets the variable for `curl` and not for the script. The example is the piped form, `curl -fsSL … | VERSION=<tag> bash`, which is what `README.md`, `docs/rules/promotion.md` and `release.yml` already spell and what `internal/release`'s own test demands."
---

The mechanical pass in Stage 1 made every literal say `codeaf`. This is the half a
`sed` cannot do: the sentences that only made sense while there were two names, the
page a person reaches when they type "what happened to aforge", and a gate so the
next wide search never has to happen.

`CLAUDE.md` and `AGENTS.md` (one file, reached by a symlink) gain `## The name` and
`## The old name, and the three places it is still allowed`, because an agent whose
memory predates 2026-09-14 gets this wrong first and gets it wrong silently.
