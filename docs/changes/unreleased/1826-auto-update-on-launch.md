---
kind: changed
title: a released codeaf installs the newest stable and restarts onto it before the chat opens
pr: 1826
surface: [chat, docs]
invalidates:
  - "In v0.8.0 an update never happened at launch: the chat opened first, counted down ten seconds in the keys line and installed in the background for the NEXT launch. Now a stable build named codeaf with `automatic updates` on installs a newer stable release before the chat opens and restarts onto it with the same arguments; the chat's countdown is only the fallback when that could not finish."
  - "In v0.8.0 the countdown and background install ran for every channel, so devaf, stageaf, dev builds installed as codeaf and release candidates replaced themselves unasked. Now only a stable release in a file named codeaf is ever installed without being asked (`update.AutomaticFor`); every other build is told on one line and installs only on `/update` or `codeaf update`."
  - "With `automatic updates` off, a newer release was named only inside the chat. It is now also printed in the terminal before the chat opens, `codeaf <newest> is out · you have <running> · codeaf update installs it`, using the file's own name for devaf, stageaf and other installs."
  - "There was no way to stay on an older release: the next automatic update moved the file forward again. `codeaf update --version <tag>` and `/update <tag>` now pin the file at that release, the automatic road refuses to move a pinned file, and a plain `codeaf update` or `/update` unpins it."
  - "A finished background install said `codeaf <tag> installed · this session keeps running · new windows open the updated app; work already running keeps its current engine until all its windows and work close`, and the keys line said `codeaf <tag> installed · this session keeps running`. Both now say `codeaf <tag> installed · restart to use the new version`."
  - "`CODEAF_NO_UPDATE_CHECK=1` used to mean only that no launch check was made. It now also means no update is installed as codeaf opens, because that installation depends on the same launch check."
---

The launch's own road lives in `cmd/codeaf/prelaunch.go` and is bounded so that
it never holds a terminal hostage. It runs only where a person is waiting: a
terminal on both ends, `CI` unset, and not `--once`. The check answers from a
day-long cache when it can. Otherwise it waits at most 1.5 seconds, and a check
that got no answer is not waited on again for an hour. The install waits at most
twenty seconds. A slow download, a failure or ctrl+c opens the build already on
disk and says so in one line.
