---
kind: changed
title: runs record the files they touched, and elsewhere sees the same repository from any folder
pr: 1443
surface: [chat]
invalidates:
  - "A run on the worker harness left no row in the project's record (`tasks.jsonl`), so `<elsewhere>`, the `tasks` tool and the sessions rows never saw what it touched. It now writes one row when it ends or is stopped, with `files` read off its working copy's diff against the commit the copy was cut from, a worker's own commits included."
  - "A row with no files was the only way to say a file list was unknown. A row now carries `filesUnread` with the reason when its list could not be read, and `<elsewhere>` and the `tasks` tool say `files unknown` for it."
  - "`<elsewhere>` read only the project folder codeaf was launched from. It now also reads every other project folder for work on a repository this chat is on (rows carry `repo`, the git common directory, so worktrees of one repository match). Such rows say `in <project>`, and rows are ranked before the caps: shared files, then same repository, then same folder."
  - "A worker-harness run in flight made its window read as idle outside it: its presence file named no work and the project's record had no row until the end. The run and its joined hand-offs are now in presence, and a `running` row is written when it starts or is carried on, so home, the sessions rollup, `<elsewhere>` and the `tasks` tool count it as running."
  - "A run cut short by its conversation closing left no row, and one whose process died was closed as `failed`. Both now leave an `interrupted` row with the files touched so far, new files included; carried on and finished, the final row supersedes it. The run's copy record now keeps the commit it was cut from (`checkBase`), so a carried-on run still names every file."
  - "A finished run was listed twice in its own conversation's `tasks` answer. It is now listed once, from its plan."
  - "`<elsewhere>` went quiet whenever it had nothing to show. When this chat's repository cannot be resolved, or another folder's record cannot be read, it now says so in one line under the lead."
---

Measured on 2026-09-24: of 26 cases where two chats touched the same file
within a day, the block could show 9. The other 17 were one repository
reached from chats filed under different project folders, and no run on the
worker harness had recorded its files. Both holes are closed here, on the
files that exist today; the shared index in the brain design is still ahead.
