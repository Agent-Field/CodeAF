---
kind: changed
title: The install story, the compiled-in manual and the branch rules say what is true on release day
pr: 1050
surface: [docs, build]
invalidates:
  - "The README said the repository has no rc or staging publication, so those channel selections stop with `no <channel> build has been published yet`. A push to `dev`, `staging` or `main` now publishes that channel, all of them marked prereleases; only the bare curl line still stops, because `--stable` is the default and reads `releases/latest`, which excludes prereleases, until a person dispatches `Release` on `main`."
  - "The README and `internal/manual/chat/running-from-the-terminal.md` both said the repository is private today and the raw installer URL answers 404. Both now state that as a condition rather than a date — 404 while the repository is private, working once it is public with `scripts/install.sh` on `main` — so neither sentence goes stale when the repository flips."
  - "`https://agentfield.ai/get/codeaf` was the first line of the README's install block and was labelled the preferred form. It still is not serving, so the raw GitHub road goes first and the proxied address is described as the form that will be supported once it serves."
  - "Nothing in the README or the chat manual said that codeaf downloads a third party's executable. `codeaf do` fetches the pinned rtk v0.45.0 from `github.com/rtk-ai/rtk` in the background when none is resolvable and `CODEAF_RTK` is unset, and both surfaces now say so with its bounds: two minutes, 64 MB per response, sha256 against the published checksums, four platform pairs, `$CODEAF_HOME/bin/rtk`, and `RTK_TELEMETRY_DISABLED=1` on every call. `CODEAF_RTK=off` refuses it."
  - "`.github/rulesets/README.md`, `docs/rules/ci.md` and `CLAUDE.md` said branch rules are unavailable until the org moves to GitHub Team. GitHub Free enforces rulesets on public repositories, so the condition is GitHub Team or a public repository, and the two checked-in rulesets are applied the day this one goes public."
---
