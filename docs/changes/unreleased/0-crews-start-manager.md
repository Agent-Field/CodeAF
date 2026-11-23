---
kind: added
title: A crew can be started with its own manager and goal, and the global manager has an app door
pr: 0
surface: [engine]
invalidates:
  - "crews.create only stored a goal as the crew's text and never sent it. With the opt-in `start_manager` it now makes one new manager conversation for the crew and hands the goal to it as its first message at most once, keyed by an explicit `request_id`. Without the opt-in nothing changed: a goal is still only stored, and a start-only field without the opt-in is `bad_params`."
  - "There was no app door for the global manager; only the terminal's `+ Global manager` made the root (All teams) and its manager. `crews.root.start` is that door now. It makes nothing that is already there. A manager whose conversation folder is gone, or carries the permanent-deletion mark, is replaced and leaves the root. One whose folder cannot be checked is an error, not a replacement."
  - "A repeat of a start request is never taken for a new one when its earlier outcome cannot be read. The request is claimed durably (`app/crew-starts/<sha256>.json`, exclusive link, synced) before the crew is committed, and the goal moves only once that claim says it may. An unreadable claim, teams file or crew, a claim that was never committed, or two earlier records of one request answer `uncertain` and start nothing. `goal.state` says only what is known: `none`, `starting`, `not_sent` (provably never handed over) or `unknown` (a repeat whose goal may have gone, which is never sent again). There is no `earlier`."
  - "A created agent's first message that failed in the hand-over itself was armed for `agents.retry` as though it had never been sent, so a retry could send it twice. It is now `turn.done` `error_kind: \"uncertain\"` and is not offered again. A retry whose own hand-over fails answers `uncertain` and is not offered again either. A first message that failed BEFORE the hand-over is still retried, now with its original `client_id` (it was dropped). Ordinary `agents.send` errors keep their codes."
  - "`agents.create`'s new `said` is the person's words exactly as given, as `agents.send`'s is: it is not trimmed, and an empty `said` is an empty entry."
---

A start reads its attachments once, before anything is made, so a file that
disappears afterwards cannot fail a crew that was already committed. A start
whose goal is withheld after its crew exists returns the real crew and manager
ids, and the manager is held as an idle conversation. One start runs per
engine at a time; a concurrent one is `busy`. See
`docs/design/app-v3/PROTOCOL.md`, "Starting a crew with its own manager".

Not built here: the All teams UI and `crews.overview` (other owners), and the
manual corpus. This is app wire and not the chat's own feature.
