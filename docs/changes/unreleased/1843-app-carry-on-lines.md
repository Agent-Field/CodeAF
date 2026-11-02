---
kind: fixed
title: codeaf app keeps each reply of a carried-on turn apart and says nothing of the carry-on
pr: 1843
surface: [engine, chat]
invalidates:
  - "The app's pump ended a message only at a tool call or the turn's end, so a turn sent back to work ran its replies together: a withdrawal drew as `…from the team.[no change]` and later messages took the wrong id. Each reply now ends at session.EventAssistantDone and is its own msg, live as in agents.entries."
  - "The app drew `the ask is not finished · carrying on rather than stopping here` and the two nudges as notice entries. They are never entries now; session.IsCarryOnNotice is the one reading of them, and the lines that end a turn stay notices."
  - "A reopened conversation drew the load and ask nudges and the output-limit continuation as messages the person typed, in the terminal and in agents.entries, shifting every later u<n>. They are continuation context now, with the carry-on."
---

The carry-on's own line still shows in the terminal while the turn runs and
folds away with its work; only the app, which has no fold for a notice, leaves
it out.
