---
kind: fixed
title: A state path too long for a socket answers at once, in words, and starts nothing
pr: 704
surface: [chat, engine, docs]
invalidates:
  - "A state root too deep for a unix socket used to cost every `aforge chat` a ten-second blank screen before it fell back. The refusal is settled before a host is started now, and the launch is immediate."
  - "The fallback notice read `engine host: no host answered` whatever the reason. It now names the real one in a person's words, and a path past the ceiling says so and names `AFORGE_HOME`."
  - "`enginehost.Spawn` took only the command and gave the child `/dev/null` for stderr, so a host that died at birth left nothing behind. It takes the workspace first and appends the child's stderr to that workspace's `host.log`."
  - "The socket ceiling was the unexported `socketLimit`. It is `enginehost.SocketLimit`, and both the sentence a person reads and the manual page carry the same 104."
  - "The chat manual said only that a socket path could be `too long`. `staying-on-that-machine` now states the 104 bytes, why it is not Linux's 108, and what a person sees and types when they meet it."
---

The host road itself is unchanged: an ordinary launch still takes it, still
starts a host when nobody answers, and still waits the birth wait for one that
had somewhere to listen. What changed is the one failure that could never
resolve — a path a socket cannot be named in — which is now answered at the
door with no lock, no process, no directory and no wait.
