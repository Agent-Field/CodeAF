---
kind: fixed
title: a file kept out for a secret during a background save no longer prints through the chat screen
pr: 2030
surface: [chat]
invalidates:
  - >-
    The guard's doc said a notice with no route is one line on stderr, and that
    was believed safe because every chat seat passes a route (WithNotices). The
    older-chat copier runs inside the full-screen surface with an engine that
    has no route, so its notices were raw rows through the frame. A notice with
    no route now goes to the standard logger, which the surface keeps in
    chat.log.
---

While the chat was open, the background copy of older chats sealed whole
project folders. Each file it kept out because it looked like it held a secret,
such as test fixtures with fake `sk-` keys, was written straight to the
terminal: `codeaf: Tests/…/BrowseTests.swift looks like it holds a secret
(api-key-sk), …` rows landed across whatever page was open and stayed until the
next repaint covered them.

The copier builds its engine with `cellstore.EngineFor`, whose guard has no
notice route, and the guard's fallback wrote to `os.Stderr` directly, past the
logger the surface parks in the profile's `chat.log` (#404). The fallback now
writes through the standard logger: the line goes to `chat.log` while the
surface owns the terminal and to stderr outside it, prefixed `seal guard:`. A
seat with a route still shows its notices where it did.
