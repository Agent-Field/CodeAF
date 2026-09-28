---
kind: fixed
title: the tmux suite never touches the developer's own background timer
pr: 1638
surface: [build]
invalidates:
  - "Running `TestTUIE2E/the_firing_reaches_the_person` rewrote the developer's own `~/.config/systemd/user/codeaf-tick.service` to the test's temporary state root and the checkout's `bin/codeaf`, and every five-minute pass failed once that checkout was removed. Every codeaf the e2e package starts now runs with `systemctl`, `launchctl` and `crontab` stubs first on PATH and HOME at a throwaway login folder, so the approval writes and enables only the rig's own timer, and the subtest asserts the machine's timer files are byte-identical before and after."
  - "A launcher in `internal/e2e` could start codeaf with a bare `exec.Command` and the developer's HOME. The untagged `TestEveryLaunchOfCodeafStandsBehindTheHostGuard` now fails the pull request, naming the file and line, for any launch outside `startWithEnv` or `guardedCommand`."
---

The product writes the unit files itself before it calls the scheduler, so a PATH stub alone
would still have replaced them; the guard moves HOME as well. `CODEAF_HOME`, the provider key
and the engine socket path are unchanged.
