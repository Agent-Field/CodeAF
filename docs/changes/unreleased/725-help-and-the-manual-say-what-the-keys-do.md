---
kind: fixed
title: /help and seven manual pages say what this build actually does
pr: 725
surface: [chat, docs]
invalidates:
  - "`/help` said `ctrl+e         open the model's thinking, streaming or finished`. Since #653 the empty-box press asks the RUNNING TURN'S window first ([app.toggleLatestWorkfold]) and reaches thinking only as a fallback, so the row now reads `ctrl+e         open the running turn's compact steps · the newest worked chip · or the thinking`, and input.go's comment over `case \"end\", \"ctrl+e\"` names both functions rather than the second one alone."
  - "internal/manual/chat/keys.md said the switcher's `ctrl+w` asks a working row `keep running` / `stop work` / `cancel`. [app.hopAway] sends only the conversation you are IN through the card; every other open row is dismissed outright and the switcher says `tab closed · <name>`, while a row that is not open here answers `that one is not open here — enter opens it`."
  - "running-on-another-machine.md pinned `this build speaks protocol 3 and the surface speaks 4`. The sentence is formatted from [remote.Version], which is 13 — the page now shows the shape and says the numbers are the two builds' own versions."
  - "permissions.md listed the pre-approved bookkeeping as `note`, `track`, `recall`, `forget`. No belt has a `note` or a `forget`; [v3BuiltinApprovals] seeds `remember`, `track` and `recall`."
  - "choosing-a-folder.md listed `more of this folder is not shown` among the words the pane uses. Nothing draws [folderCutWord] and nothing reads [folderListing.cut] — the page states the five-thousand-row bound and says plainly that the pane does not announce it."
  - "when-the-connection-drops.md answered \"my wifi died in the middle of a reply\" with the five-minute `--host` redial. An ordinary local chat has no second connection: the model request is the one that dropped, and its answer is `waiting for connection` and a two-minute wait. The section says which is which before it says either."
  - "screen.md said the tab strip stands down \"under 12 columns wide, or on a terminal too short for a blank row above the message box\". [app.tabsHeight] wants [app.breathingRows] at 2, which arrives at 16 rows — one blank row is not enough."
  - "sessions-and-rewind.md capped the full name at 80 characters and the label at 32. `clip` bounds BYTES on a rune boundary, so a name written in Chinese or Japanese fits about 26 characters and its label about 10."
  - "`Chat().Search(\"how do I preview a file before attaching it\")` never returned choosing-a-folder and `Chat().Search(\"scroll the tab bar\")` returned places three times over. Both headings carry the asker's words now, and ten probes in internal/manual/chat_test.go hold every corrected page."
---

The three manual gates check that a NAME is MENTIONED, never that the sentence
around it is true, so every one of these pages was green while it was wrong —
and the chat reads these pages to answer "what does this key do". Nothing about
any key, tool or bound moves here; only what a person and the model are told.

`collections.md` also says where a conversation ID comes from, because a model
asked for one invented an `aforge conversations` command that does not exist.
