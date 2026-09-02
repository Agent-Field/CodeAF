---
kind: fixed
title: The engine-host socket tests make their own short home, so a deep temp root no longer fails them
pr: 411
surface: [remote, build]
invalidates:
  - "`internal/enginehost TestTheSocketMovesWithTheStateRoot` failed on macOS because the test's temp home was too long for a unix socket and was skipped by CI. The socket tests make their own short home now; the line is out of `.github/known-red.txt`."
  - "The way to get the engine-host tests green on a Mac was `TMPDIR=/tmp/eh`, and CLAUDE.md said so. Whether the package passes is no longer a function of how deep `$TMPDIR` is, so the workaround is gone from CLAUDE.md and from the test file's own comments."
  - "`TestAttachGivesUpQuietlyWhenNoHostCanStart` used `t.TempDir()` for its home, so with a deep temp root it was refused at the socket door and passed for the wrong reason. It goes through `shortHome` now and fails only when a spawn that starts nothing is claimed to have started one."
---

A unix socket path may weigh 104 bytes, and a Mac's own temp root spends most of
them before a test has said anything. The test that checks WHERE a socket lands
was being answered by the refusal that guards HOW LONG a path may be — a law
with a test of its own — and never reached its question. `shortHome`, which the
stale-host tests already used, now names its root directly under `/tmp` for every
test in the package that stands a real socket, with a logged fallback to
`t.TempDir` only where `/tmp` cannot be written. The limit is unchanged, nothing
is skipped on any platform, and the one test that wants a path past the limit
still builds its own on purpose.
