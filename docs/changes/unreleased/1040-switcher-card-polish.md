---
kind: changed
title: The switcher's foot is one instruction and its name column takes the spare width
pr: 1040
surface: [chat, docs]
invalidates:
  - "The `ctrl+k` card's foot read `▸ 9 more on this machine · → reach them`, and `← just the open ones` with the fold open. It now reads `→ show closed` and `← hide closed` — the key and what it does, no glyph and no count, because the head's `3 of 12` already carries the figure. `hopFoldKeyWord` and `hopShutKeyWord` are those two strings; anything quoting the old ones is stale, including the card drawing in `internal/manual/chat/keys.md`."
  - "The card drew a wrapped copy of the SELECTED row's title in a block under the list whenever the subject column had abbreviated it. That block is gone — `hopCardLines` builds no preview and `TestSwitcherShowsLongSelectedTitleBelowTheList` is replaced by `TestALongNameTakesTheRoomAndIsNotRepeatedUnderTheList`. The rows it used to displace on a short card stay drawn."
  - "The subject column was fixed at 34 cells (`hopSubjectCol`) and the clause took every cell left over. It is the other way round: the clause is `hopNoteCol` (32, narrowing to `hopTightNote`), and the subject takes what is left up to `hopSubjectMax` (64), keeping `hopGutter` (2) cells of air so a name that fills its column does not touch the clause. `hopSubjectCol` no longer exists. The narrowing ladder is project, then the clause's tail, then the clock, then the clause — the name is cut last."
---

All three asked for by the owner, off the running card: a foot spending a row on a
figure the head already says, a summary under the list that was never more than the
selected row's own title and did not appear for every row, and
`Investigate the parsing regres…nothing new` — a name cut at thirty-four cells
with forty cells of `nothing new` beside it.

The gutter is the part that is not about width. A long enough name fills whatever
column it is given, so without two cells held back at the end of the column the name
and the clause run together into one word however wide the card is.

`keys.md`'s switcher section was over its size rule by some way after three changes
landed in it, and the retrieval gate said so — a probe that used to reach it started
reaching four other pages. The layout and the ordering are now two sections of their
own with their own headings, which is what that rule is for.
