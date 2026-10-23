---
kind: fixed
title: An app engine whose app has gone lets go of this machine's factory and leaves in bounded time
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: `codeaf app` left only when its stdin reached EOF, a `shutdown` request arrived or a signal came, and its Close had no bound. An engine whose app was killed could stay alive as an orphan for as long as anything kept its pipe open or any step of its Close waited, swallowing every SIGTERM, until SIGKILL. Is true now: the process that started it ending is the app leaving too; from the moment leaving begins it ends within 30 s (`appLeaveWait`, built from the factory's, the conversations' and the processes' own stop waits); and signals go through `internal/leave`, so a second SIGTERM, SIGINT or SIGHUP ends it at once."
  - "Was true: Close stopped the close watch first, which waits for any carry-on still opening a conversation, and only then let go of this machine's factory. Is true now: the factory is let go first (runner stopped as a quit stops it, then the service, `serve.json` and `serve.lock`), before anything that waits on a conversation."
  - "Was true: while another live codeaf held this machine's factory election, `factory.list` showed the app's own resumable row as `unreachable` with no reason, named `Your factory`. Is true now: that row carries `reason: \"Another codeaf on this machine is still running this factory. It comes back here once that one stops.\"`, and every list tries the resume again, so the first list after the holder lets go (or dies) serves the factory here."
---

Relaunching the app could leave the old engine alive as an orphan that held
this machine's factory, so the new engine refused to serve it and the app said
the factory was not answering, with no reason. The old engine now leaves on its
own when its app has gone, lets go of the factory first, and cannot outlive a
bounded wrap-up; the new one says why while it waits and takes the factory back
by itself.
