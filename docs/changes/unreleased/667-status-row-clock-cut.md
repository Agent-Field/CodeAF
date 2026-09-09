---
kind: fixed
title: the status row stops losing its last cell when the turn clock ticks mid-frame
pr: 667
surface: [chat]
invalidates:
  - "The status row's painted cluster was not held to the width its plain form was measured at: paintPart rebuilt the state word from app.now a second time, so a turn crossing 9s into 10s between the layout's read and the paint's was drawn one cell wider than the frame. The renderer cuts an over-wide row rather than wrapping it, so `⠋ working · 10s` was drawn `⠋ working · 10` and the keeping and money doors sat a column left of where they looked. The state word now carries its painting on the assembled part, so text and paint are one reading."
---

An over-wide status row does NOT scroll the terminal — bubbletea composes into a
cell grid exactly the frame's width and drops what does not fit — so the symptom
of this was a silently truncated clock and two doors pressed a column off, not a
doubled row.
