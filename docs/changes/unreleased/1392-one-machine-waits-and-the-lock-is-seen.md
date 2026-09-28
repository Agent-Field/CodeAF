---
kind: fixed
title: one machine is waited on, a pool is never one machine, and the suite lock is seen and freed
pr: 1392
surface: [engine, chat, build]
invalidates:
  - "#1358 said a verdict's wait after a cut was honoured whatever shape produced it. It was not: the turn loop answered every cut with `continue` ABOVE the wait, so a watched conversation against one machine that kept cutting was re-asked in a tight loop for ever, with no wait and no status line — forty cuts were forty-one requests in about a millisecond. The wait is paid now, climbing to ten seconds, and the line reads `no answer N times in Xm · still asking · esc stops`."
  - "The one-machine wait was said to climb to ten seconds and hold there for ever. Its doubling was a signed shift that wrapped to zero or below after about thirty-five asks, so even with the wait paid the loop went hot again on the thirty-sixth. It saturates now and holds at the ceiling however long the machine stays quiet."
  - "A cut was called one machine whenever the request drew no lane choice (#1343). That is the shipped router's default pool under `routing simple`, and also `routing off`, a talk lane naming the router, and `auto` while the gate holds the model. An ordinary pool user whose stream died before naming its server was treated as the only machine there is, and once no fallback model was left — `--one-model`, no chain, or a chain already walked — that was the endless loop above. One machine is now only a pin to one lane, a connected direct service, or a base with no router behind it."
  - "The directory lock a current tree takes for old checkouts (#1324) named the SUITE's pid, and an old checkout accepts a pid as alive only when its command line says `one-suite.sh`. So an old tree read a live lock as stale, moved it aside and ran its suite beside ours. The directory names the lock HOLDER now, which carries that name, and a holder drops the directory only while it still names that holder."
  - "A SIGKILLed or out-of-memory-killed holder freed the flock and left the directory, and every later run on the box was refused naming a dead pid, for ever. A run that holds the flock now takes back a directory whose pid is not a live old checkout."
---
The first two are one failure seen from two layers. The policy's endless wait
is safe only when two things are true: the wait between asks is really paid,
and "one machine" really means one machine. Before this change neither was true.

The lock half keeps the rule #1324 set: the flock is the truth, and the
directory is there only so that checkouts older than #1264 can see it. So the
directory is judged only by a run that already holds the flock, and it is judged
the way an old checkout judges it, because an old checkout is the only thing
that can still be holding it. `scripts/one-suite_test.sh` now runs the pre-#1264
reader itself against a live current holder, from a copy kept under
`scripts/testdata/pre-1264/` with a private lock path. Before, it only checked
that the directory existed.
