---
kind: added
title: a launch says when a newer codeaf is out, and /update installs it and restarts
pr: 1060
surface: [chat, build]
invalidates:
  - "The chat manual's install section ended `Nothing self-updates: run it again when you want a newer build.` That sentence is gone and the opposite is now true: `/update` (alias `/upgrade`) downloads the newest stable release, checks its sha256, replaces the running executable and restarts on it with `--session` pointing at the same transcript, and `codeaf update` does the same from a shell. `running-from-the-terminal.md` has a new section, `Is there a newer version — how do I update codeaf — /update — codeaf update — why does it say this every time I start`, which states all of it."
  - "A launch said nothing about newer releases. A build stamped with a stable tag that is behind the newest stable release now draws one dim transcript line naming both versions, `/update`, and the curl line. A release candidate gets it once its stable line is published. A `dev-*` or `staging-*` channel build, a `make build` sha and an unstamped binary make NO request at all and draw nothing, so a developer's build is never told it is behind. The check is asynchronous with a three-second budget, never delays the first frame, and says nothing when it fails; its answer is cached for a day in `update-check.json` beside `config.json`. `CODEAF_NO_UPDATE_CHECK=1` turns the launch check off without touching `/update` or `codeaf update`."
  - "The release-tag grammar lived in `cmd/codeaf-release/version.go`. It is now `internal/update`, which the release tool reads; there is one parser. `scripts/install.sh` was accepting any `dev-*` or `staging-*` tag and semver parts with leading zeroes where `internal/update` accepts only the shapes the release workflow publishes — it now uses the same grammar, so the two install roads cannot pick different builds. The publish-time ordering #1056 added is unchanged; only which candidates are eligible."
  - "`codeaf update` is a new command, and it sits under housekeeping in `codeaf --help` rather than beside the read-only rows: `--check` prints one line and exits 0 when this release is newest, 3 when a newer one exists and 1 when it could not ask, while a plain run installs stable or whatever `--rc`, `--dev`, `--staging` or `--version <tag>` names. A binary built from source refuses to replace itself in place and names its path; an unwritable executable is left untouched and offered the curl line; sudo is never attempted."
  - "`codeaf update --check --rc`, `--dev`, `--staging` and `--version <tag>` were believed to apply the selected channel's rule after selecting its tag. They did not: every answer ran through the stable-only comparison, so a newer rc or disposable channel build could be called newest with exit 0 and the selected tag omitted. Every answer now names that tag; stable and rc compare their semantic versions, dev and staging compare for equality because channel builds are disposable, and an exact tag is equal or different with no invented ordering."
  - "A stable build ahead of the newest published stable was believed to get nothing. That was true only of the launch reminder and `--check`: both `codeaf update` and `/update` silently installed the older release. An implicit update now refuses, names both versions, and points at the deliberate override; `--version <tag>` and `/update <tag>` still install exactly what the person named."
  - "A deliberate detach in `internal/remote` kept the twenty-second torn-link grace that exists for a terminal killed without warning, so a window that had announced its departure still looked attached for two more call deadlines. It now clears that grace once the last view leaves, which is what lets a freshly installed codeaf take the engine the old process had already let go of. Live turns, tasks, unanswered questions and other views still refuse retirement."
---

The owner asked for two things that turn out to be one: tell somebody a newer
codeaf exists, and give them the command. A line that only names a version makes
a person go looking, so this one carries `/update` and the curl line with it, and
`/update` finishes the job rather than printing homework.

Everything else follows from not wanting to be wrong in front of somebody. The
check is silent on failure because a network complaint at launch is noise about a
thing nobody asked for. A developer's own build is never told it is behind. The
restart waits for the door, after the frame is down and the terminal is back, and
carries the transcript so the conversation the person was having is the one that
comes back.

The curl line every one of those roads offers is `curl -fsSL https://agentfield.ai/get/codeaf | bash`,
the supported address that serves `scripts/install.sh` from `main`, so the launch
line, the refusals and the README hand out one road.
