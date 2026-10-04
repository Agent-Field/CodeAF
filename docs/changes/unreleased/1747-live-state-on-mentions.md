---
kind: added
title: live state on @chat-name mentions carries the strip working and needs-you mark in the prose
pr: 1747
surface: [chat]
invalidates:
  - a @chat-name mention in a message looked the same whether that conversation was at rest or working; the token now paints the strip's working glyph in place of its @ while that conversation's turn is in flight and the warning glyph when it waits on the person, and clears when the turn ends.
  - a tab held while its turn was already running showed no working mark until a later watcher event fired; the frame of the hold now carries the mark.
---

A message that names a conversation with `@chat-name` was already highlighted and clickable; now the token also says whether that conversation is working or waiting on you. The mark reads the tab strip's own signal (`tabSignalFor`, the single reader), so the prose and the strip can never disagree on one frame, and a conversation this window cannot refresh stays unmarked — a mark that meant "probably" would be a claim the surface cannot keep. The hover hint gains the state word, and holding a tab whose turn is already in flight seeds the mark on the frame of the hold.
