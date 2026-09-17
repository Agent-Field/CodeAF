---
kind: changed
title: The manual and the guide say the install proxy is live, because it is
pr: 1076
surface: [docs, chat]
invalidates:
  - "`internal/manual/chat/running-from-the-terminal.md` said the repository is private, that the raw installer answers 404 to anyone not signed in, and that `https://agentfield.ai/get/codeaf` is not serving yet. The repository has been public and that address has served `scripts/install.sh` from `main` since 2026-09-16; the section now opens with `curl -fsSL https://agentfield.ai/get/codeaf | bash`, names the raw GitHub line as the script under it, and gives `/dev`, `/staging` and `/rc` on the path beside the `bash -s --` flags."
  - "`docs/GUIDE.md` said a source checkout is the road that works today, that the proxy route is written and unmerged, that the clone needs repository access while it is private, and that the repository has no rc or staging publication. The installer now comes first under `## Install`, the clone line carries no access caveat, and the channel sentence is a condition: a push to `dev`, `staging` or `main` publishes that channel's build, and a channel with nothing published stops with `no <channel> build has been published yet`."
  - "Neither page said which shell to pipe the installer into. Both now say `bash`, not `sh`: the script uses `set -o pipefail` and `[[ ]]`, and `sh` is dash on Debian and Ubuntu, which rejects both."
---

The proxy refuses rather than misleads — a script without its channel line exactly
once, or a body that is not a shell script, is a 502 — and the manual now says so,
because that is what the route does. Still true and unchanged: nothing self-updates;
there is no `internal/update` on `dev`.
