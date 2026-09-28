---
kind: fixed
title: a closed loopback has no engine left running, so a test's home can be removed at once
pr: 1653
surface: [remote]
invalidates:
  - "remote.Loop.Close used to return as soon as it had shut the surface's end of the pipe, so a test that needed the engine gone had to wait on Served itself. Close now returns only after the engine goroutine has finished Serve and the conversation's close, and Served still holds the answer."
  - "A Loop could be built by hand next to its own Serve goroutine, as driver_test.go's loopSession did. Every Loop is now made by loopOver, because Close waits on a signal only that constructor provides."
---
TestHostedWelcomeCarriesUnreadProfileKeysToSurface failed about once in eighty
runs under load, with TempDir's cleanup reporting a place folder that was not
empty (#1647). The writer was the conversation's own close. The engine behind a
loopback closes it after it reads the EOF, and that close writes one last
presence heartbeat and then removes it. The loop's Close had already returned by
then, so the write could land in the folder that cleanup was walking. The fix
makes Close wait for the engine, not the test. Every other test that closes a
loopback and then lets its temporary folders go gets the same guarantee.
