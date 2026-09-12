---
kind: added
title: a launch sweep names live sessions on an older build
surface: session, tui3
pr: 1015
---

Every launch now sweeps `~/.aforge/v3/projects/*/presence.json` and names any
live session holding a rev other than this binary's, with the serving pid —
the third binary-shadow incident ends with a visible warning instead of a
session that "stopped working" on the old rev.

`internal/session` exports [SweepStaleBuilds] — a fail-open glob + freshness
filter, `presenceWindow` the only liveness rule — and `internal/tui3`'s open
path calls it once beside the landing-keys note.

The law the sweep obeys is the presence file's own: age is the only liveness
check (the pid is reported for a person's `kill`, never consulted), which is
the same rule every other reader of those files has.

Manual: starting-aforge.md's `/status says an older build is holding this
conversation` documents the line.
