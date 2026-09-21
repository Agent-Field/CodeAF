---
kind: fixed
title: a headless launch and task-owner open reach a restarting host, not a crashed one
pr: 1272
surface: [chat, engine]
invalidates:
  - "A host takes its lock before it removes the old socket and begins listening, so for a moment the lock is held while a connect is refused. A headless launch (codeaf chat --once) that probed the host in that window read the refusal as proof no host was there and quietly went in-process, and a task-owner row opened for another workspace could refuse the same way and freeze on it."
  - "The probe now retries a refused or briefly missing connect only while the host lock is held, through one shared helper (enginehost.DialStartingHost, a non-blocking flock read of host.lock that is the same answer across pid namespaces), bounded to 250ms. A failure with the lock free, the ordinary state after a host crashed and left a stale socket that nobody restarted, answers at once as before, so a start pays nothing for it. On Windows errors.Is(err, syscall.ECONNREFUSED) never matches, so the retry never engages there and the prior behaviour stands."
---

The two raw callers, cmd/codeaf/chatv3_local.go and cmd/codeaf/chatv3_taskowner.go, now
share the one helper so they cannot drift, and the readiness gate a host stands behind
is unchanged. The retry is keyed on the host lock, not on the shape of the connect
error, because a refused connect alone does not say whether a host is coming up.
