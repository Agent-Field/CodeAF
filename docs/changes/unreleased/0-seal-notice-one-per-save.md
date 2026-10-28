---
kind: changed
title: one notice per save for files kept out of the saved history, and none for files git holds unchanged
pr: 0
surface: [chat, engine]
invalidates:
  - "Was true: each save announced every file it newly kept out of the saved history in a sentence of its own, so the first save of a fresh checkout whose tests carry fake keys said one line per fixture. Is true now: a save says one notice. One file keeps its sentence; several are a lead that counts them (`3 files look like they hold secrets, …`, with the environment-file and unreadable meanings kept) followed by one line per file and its rule."
  - "Was true: a file git tracks was announced like any other. Is true now: a file git holds at HEAD and that is unchanged in the index and on disk is kept out exactly as before but not announced. An edited, staged-only, untracked or ignored file, an unreadable path, and any file in a folder that is not a git checkout (or whose git read fails within 5 seconds) are still announced."
  - "Was true: a seal notice reached a surface only as words. Is true now: a grouped one is `session.Event` `NoticeKind` `kept_out` with `KeptOut` (lead and files with their rules), the app's notice entry carries `notice_kind: kept_out` and `kept_out: {lead, files: [{path, rule}]}`, and `text` still holds the lead and every file for an app that draws only words. `cellstore.Guard.Notify` and `SealWatch.Note` take a `cellstore.Notice`; `SealWatch.TakeFiled` hands over the facts."
---
The terminal and a factory item's log draw the notice's words, which list the
files under the lead. The desktop app shows the lead and folds the files behind
a toggle.
