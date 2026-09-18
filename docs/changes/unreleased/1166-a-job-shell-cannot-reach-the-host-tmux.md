---
kind: fixed
title: a job's shell cannot reach the tmux server hosting codeaf
pr: 1166
surface: [engine]
invalidates:
  - "A bash call the model ran inherited the parent's environment, TMUX and TMUX_PANE included, so a bare `tmux` it ran targeted the server hosting the chat — `tmux kill-server` once took down the chat that ran it and, on a shared socket, every run on the box."
  - "TMUX_TMPDIR was whatever the host had (usually unset). Every model command's shell now gets TMUX and TMUX_PANE removed and TMUX_TMPDIR set to the profile's own tmux directory, so a job's `tmux` reaches its own server and nothing else."
---

A model's shell was handed this process's whole environment. `runShell`
(`internal/exec/tools.go`) starts `bash -lc` from `os.Environ()` only when a
shelf must be added and otherwise leaves `cmd.Env` nil, and the background-job
registry (`internal/exec/jobs.go`) and the bare streaming env
(`internal/exec/bare/streaming.go`) did the same — so the child inherited
`TMUX` and `TMUX_PANE` verbatim. A bare `tmux` the model ran then targeted the
server hosting codeaf itself. That is how a worker probing issue #576 ran
`tmux kill-server` and killed the chat it was running in; on a shared socket the
same reach took every run on the box. A person running codeaf inside tmux had
the same exposure.

**The floor is both halves, not either.** Unsetting `TMUX`/`TMUX_PANE` alone
lets a bare `tmux` land on the user's default socket — the host server is safe,
but the user's own tmux is still reachable. Setting a private `TMUX_TMPDIR`
alone leaves the inherited `TMUX`/`TMUX_PANE` pointing straight at the host.
Only together do they name a namespace a job's `tmux` can reach and nothing
else.

One helper, `JobShellEnv` (`internal/exec/tools.go`, beside `replaceEnv`), takes
an environment slice and returns it with `TMUX` and `TMUX_PANE` removed and
`TMUX_TMPDIR` set to the profile's own tmux directory — the directory source is
`config.ProfilePath(config.ProfileDir(), "tmux")`, i.e. `CODEAF_PROFILE_DIR`'s
`tmux` directory when that is set and the state root's otherwise. It creates the
directory if missing. A nil slice is read as `os.Environ()`, so the bare path
that used to inherit by leaving `cmd.Env` nil now hands an explicit,
TMUX-stripped environment instead. It is applied at all three seams: `runShell`,
the background-job env, and `bare.StreamingEnv` (which the foreground bash tool
and the session's job registry both reach).

A test that needs its own tmux still works: it gets the private `TMUX_TMPDIR`
rather than a stripped-to-broken env, and can start its own server there.
