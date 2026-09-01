---
kind: changed
title: a proposed task waits while you type, a bare no means no, and the default window is fifteen seconds
pr: 279
surface: [chat, engine, remote]
invalidates:
  - "The proposal card's clock kept running while a person typed, and a real card was gone before their `no` (started at +5s, `no` at +17s). The first rune typed into the box now holds the proposal: the meter reads `waiting on you`, the engine stops counting, and deleting the rune does not restart it."
  - "`no` typed alone into the redirect lane was sent as a redirect named `no`. A bare `no`, `nope`, `n`, `stop`, `cancel` or `don't` declines; a bare `yes`, `y`, `ok`, `okay`, `go` or `sure` approves as briefed; anything longer is still a redirect."
  - "The empty proposal box reserved bare `r` as a redirect-focus shortcut, so typing `run tests first` sent `un tests first`. No bare letter is a proposal shortcut now; redirects may begin with any letter, and the visible redirect option remains reachable by pointer or arrows and `enter`."
  - "The default task countdown was 5 seconds. It is 15; a persisted value, 0 included, still wins."
  - "The remote protocol was version 9. It is 10, with `Task.Hold` on the wire; both ends must be rebuilt."
  - "`docs/remote-access-testing.md` told you to run `aforge chat --session new`. `--session` is a transcript path — that command writes a file called `new` into the workspace; an empty directory starts a fresh session on its own."
---
