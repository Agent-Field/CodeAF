---
kind: fixed
title: other computers list more of this one's chats, sooner, and say how many are not shared yet
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: a background publish of an existing chat had 30 seconds (cellstore.SealWithin) for the whole chat, so a chat whose saved folder could not be uploaded in 30 seconds failed on every attempt and was never listed on the other computer. Is true now: the budget is two minutes without progress (syncsetup.PublishIdleWithin); every answer from the relay starts it again, and an upload cut short keeps the frames it sent (cellsync.Publisher.Upload records them), so the next attempt resumes."
  - "Was true: the publication pass scanned the disk once per launch and offered pending chats in map order. Is true now: every pass rescans (a glob and a stat for folders not seen yet) and offers chats not tried yet newest first, then chats already tried smallest upload first."
  - "Was true: older chats (a journal, no cell) were copied and published only while a terminal was open, and the app engine and a terminal on the same home each ran their own publication pass. Is true now: whichever process holds the home's sharing lock (`v3/sharing.lock`) publishes and copies at most one older chat a round, the app engine included; the copy in the project's `.moving` folder is published by the ordinary pass. The 30-day and home-folder rules still decide which older chats are copied in the background."
---

A paired computer lists only the chats this computer has uploaded in full, and
this computer uploaded them one at a time, under a 30-second budget for the
whole chat, in no particular order, from a disk scan made once per launch.
Older chats were only copied while a terminal was open.

- **Progress, not size, bounds an upload.** A large chat uploads for as long as
  the relay keeps answering; two minutes with no answer ends the attempt, and
  the retry sends only what is still missing.
- **Small and recent first.** Chats not tried yet go newest first; a chat that
  already failed goes behind them, smallest upload first.
- **New chats are found.** Every pass looks for chat folders it has not seen,
  so a chat restored to disk or written by another process is shared without a
  restart.
- **The app copies older chats too.** One process per home publishes: the app
  engine or a terminal, whichever holds the home's sharing lock. The other
  takes over when the holder quits.
- **The count is said.** `sync.get` and `sync.changed` carry `Sync.unshared`,
  `{total, waiting, failing, kept_here}`, for this computer's chats with no
  directory record. The terminal says it on the home devices row ("12 chats on
  this Mac aren't shared yet") and, with why, on `/devices`. The desktop does
  not show it yet.
- **The log says how each chat went.** One line per chat and outcome: how long
  the attempt took and how many bytes it sent of how many, so a stalled
  connection reads differently from a slow upload.
