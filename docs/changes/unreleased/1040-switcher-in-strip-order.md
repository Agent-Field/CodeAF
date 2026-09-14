---
kind: changed
title: The ctrl+k card lists conversations in the tab row's order, not by recency
pr: 1040
surface: [chat, docs]
invalidates:
  - "The switcher's open rows were ordered MOST RECENTLY IN FRONT FIRST with the conversation you are in always drawn last. They are now in the tab row's own order — leftmost tab, first row — and `you are here` is wherever that conversation's tab is. `hopReading` still walks `app.prev` to BUILD the rows; `app.hopStripOrder` then lays them out, and the recency walk survives only as the tie-break for a conversation with no tab on the row. Asked for by the owner: two readings of one set of conversations, drawn one line apart, disagreed about where each one was."
  - "Row zero is no longer the conversation `tab` would go to, so `hopFirstStop` stopped being `the first row that is not here`. It is now a method that walks `app.prev` for the most recent conversation that is not the one in front, so `ctrl+k` `enter` still means the last one. Anything that assumed the cursor opens on row zero is wrong."
  - "The digits `1`…`9` on the card now name TAB POSITIONS rather than positions in a recency ring, and `ctrl+shift+k` enters at the last row — the rightmost tab — rather than at `the open conversation longest unlooked-at`. `internal/manual/chat/keys.md` said the second of those and no longer does."
---

The strip's order is first-entered and never moves, on chattabs.go's own
reasoning: a person reaches for the position, not for the word. The card taking
that order buys the same stability — a conversation keeps the number you last saw
it at — and is what makes the digits worth drawing.

A conversation whose tab was dismissed with `ctrl+w` is still held, still running
and still on the card, and has no position on the row to borrow. Those rows sort
after every tabbed one, in the order the recency walk handed them over, which is
why the sort is stable.

The card reads the strip that was DRAWN (`app.chatTabs`) rather than calling
`app.tabList`, which rebuilds the row in place and would have the card writing to
the thing it is reading. On a frame too short to draw the strip there is no
visible order to follow and the rows keep the walk's own.
