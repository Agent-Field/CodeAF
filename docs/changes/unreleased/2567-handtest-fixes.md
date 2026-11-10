---
kind: fixed
title: What the pre-release hand test found is fixed, and older codeaf leftovers are announced
pr: 2567
surface: [chat, engine, remote, docs]
invalidates:
  - "v0.8.2 standing orders and rules vanished on the update with no word (#1831 does not migrate them). The first interactive launch of a home that holds them now says how many were found, that they were not carried over, and where the list is (v3/standing-upgrade-report.txt); v3/standing/ is still not read."
  - "After pairing, a turn typed in v0.8.2 during a rollback stayed in the original transcript and silently never reached the new build's copy. It still is not merged (#2560), but opening the chat now says how many turns typed in an older codeaf are kept in which file."
  - "Background knowledge compilation recorded its cost only in knowledge.sqlite, so the day total, /cost and Spend left it out and the daily limit did not stop it. Every priced compiler answer now writes a `knowledge` ledger line, and one daily-limit gate covers turns, helpers, watches, the compiler and every provider retry; a reached limit pauses knowledge building."
  - "settings.md said knowledge capture had to be enabled per project. A fresh profile turns it on by itself (unchanged); the manual now says so."
  - "A folder outside home and the workspace was accepted and then dropped from the session record on restart (since #1888). It is now kept as an absolute path."
  - "After a computer took a chat back, a stale retained-branch redirect made it a viewer of its own chat and every tool was refused, across restarts. The take-back now clears that redirect."
  - "`codeaf cluster lead` was documented as transferring leadership with a receipt. It never returned one (#2559) and is removed from this release; `codeaf cluster status` shows who coordinates."
  - "A local task whose landing conflicted with the person's file ended `done` with no way to decide. It now ends `your call` with `a resolve it`, keeps the person's file as `<name>.yours`, and survives a restart."
  - "An ask-here follow-up sent while the reply streamed doubled the reply and lost its card. It now waits for the reply to finish."
  - "Home's search card missed a bill that landed after the turn sealed, and /cost lost its conversation/tasks split for chats in the .cell layout. Both now read the whole bill."
  - "A time limit ended a task as `stopped`, the word for the person's own stop. It now ends `incomplete`."
---

Found by driving the real binary against a real model in throwaway homes on Linux and macOS, with an upgrade from
v0.8.2 and a rollback to it. Each fix is its own commit with a test that fails on the old code.
