---
kind: fixed
title: a job's log path is spelled the way the session spelled its folder
pr: 0
surface: [chat, engine]
invalidates:
  - "Since #1627 a job's `log at …` sentence, its row and its footer printed the symlink-resolved folder — `/private/var/…` for a macOS temporary folder, the real target for a linked home or chosen folder — and three `internal/session` landing tests failed on every macOS `make test-laws`. They print the folder as the session named it again; only the retention walk uses the resolved path."
---

The resolved anchor exists so retention can refuse links planted inside the
owned `logs/jobs` subtree, and it still does. It had leaked into `logPath`,
which is the pointer a person and the model read, not a handle anything walks.
