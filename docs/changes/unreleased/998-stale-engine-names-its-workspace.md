---
kind: fixed
title: The stale-engine refusal names the workspace in the command it tells you to run
pr: 998
surface: [chat, engine, docs]
invalidates:
  - "`staleEngineHostSentence` told a person to run `aforge engine --stop` with NO `--workspace`. That flag defaults to the HOME directory, never to the workspace being complained about — so copying the line exactly as written from inside a checkout stopped the healthy home engine and left the offending one running, and the line came back on the next launch. Measured on the owner's laptop: a host on wire 14, started 2026-09-11 11:23, survived eight rebuilds and twenty-two hours of that advice. Both arms now spell `--workspace <path>`, taken from the host's own answer (`remote.HostSelf.Workspace`) rather than from what this process thinks it opened."
  - "`internal/manual/chat/running-on-another-machine.md` quoted both sentences without the flag and now quotes them with it, plus a paragraph saying why typing it matters. If you remember the remedy as a bare `aforge engine --stop`, that was the defect."
---

`sessionHeldElsewhereSentence` in chatv3.go already spelled the flag, for this
exact reason, and its comment names `staleEngineHostSentence` as its voice. The
voice was the one place not doing it.

This does not touch the version-mismatch branch itself. Refusing to splice a
surface onto a host whose frames it cannot read is correct, and the gentler
`busyEngineHostSentence` road cannot honestly be offered across incompatible
wires.
