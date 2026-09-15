---
kind: fixed
title: the installer takes the newest channel build, not the first one GitHub lists
pr: 1056
surface: [build]
invalidates:
  - "`scripts/install.sh` picked the first tag matching `--rc`, `--dev` or `--staging` in the order `releases?per_page=100` returned them, which is not publish order — with `--dev` on 2026-09-15 that chose `dev-20260915-e2ae913b7d0c`, published 13:43Z, while `dev-20260915-56a22c20ec53` at 14:56Z and `dev-20260915-4b6ec83cfbfa` at 15:21Z both existed. It reads the whole list now and takes the matching tag with the newest `published_at`, falling back to `created_at` when a release carries no publish time, so a channel flag installs the newest build wherever GitHub happened to put it. `--stable` and `--version <tag>` are unchanged."
  - "When the release API refused, the installer said `a rate limit?` and offered `export GITHUB_TOKEN to raise the limit`, which named the wrong cause for the 404 a private repository returns to an anonymous caller. The sentence is now `GitHub's API could not be reached or refused (a rate limit, or a repository you cannot read?); pin VERSION=<tag>, or export GITHUB_TOKEN`, so both causes are on screen."
---

The list the API hands back is not ordered by anything the installer cares about,
so a channel flag is a claim about time and has to be answered with the releases'
own timestamps. `extract_dated_tags` reads them the way `extract_tags` already
read tags — no `jq`, POSIX awk, bash 3.2 — and stops at a release's `assets` so an
asset's `created_at` can never decide the pick.
