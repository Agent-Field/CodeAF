---
kind: fixed
title: a replay draws the record once, whatever was already on the screen
pr: 1342
surface: [chat]
invalidates:
  - A replay arriving onto a surface that was already drawing the conversation
    kept every row of it and drew the record above itself, so the same answers
    stood on the screen twice and the compaction seam was drawn twice. Only the
    rows said into the window since the last replay are kept now.
  - A sentence typed while the record was still in flight is still kept and
    still sits below the history that arrives behind it, which is what that
    keeping was written for.
---
session.EarlierHistory states the law in its own words: the file holds the same
conversation twice, once as it happened and once as the pass rewrote it, and
drawing both would show the session to itself twice. app.replayList could break
it on its own, because rows a previous replay drew from the record are not
something said afterwards and nothing told the two apart. The count is taken in
the walk that draws the rows, since two rows of one conversation are equal in
every field a comparison could reach.

The production road to a second replay is unproven and this does not claim one.
Every caller of attachConversation clears the drawn conversation first, and the
off-loop read folds with here=false for a conversation the person has left. What
is fixed is a property replayList owes whatever calls it.
