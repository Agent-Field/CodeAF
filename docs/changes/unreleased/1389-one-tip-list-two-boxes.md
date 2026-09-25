---
kind: changed
title: one list of twenty-two tips, said two ways — home rotates them, a conversation ranks them
pr: 1389
surface: [chat, docs]
invalidates:
  - "Home never drew an earned tip: the picker refused the page outright. Home now has a tip row directly above the rule over its box, right-aligned, led by a bulb and closed by a cross a pointer can press."
  - "A hint row named which box it could draw beside, and the conversation's foot ranked its rows by a per-row `priority` number. There is ONE table now and no priority column: the table's own order is the ranking, every row draws on both boxes, and a row filed under home's slot fails the build."
  - "Copy mode had a tip on the list, `ctrl+b freezes the screen so you can read and copy from it`. It has none. THE FEATURE IS UNTOUCHED — ctrl+b freezes the viewport, v/a/y work in it, /copy opens it, and the mouse copies alongside it through OSC 52 — and only the row teaching it came off. This branch deleted copy mode for one day on 2026-09-22 and put it back on 2026-09-23 at the owner's word, so a memory of it being gone is a memory of that day."
  - "/image was a command that attached a picture. It is gone from the table, the dispatch, home's gate and the path completion; /attach already told a picture from a file by its name, and typing /image is answered as any unknown word is."
  - "/folder did two different things depending on the screen it was typed on — gave a conversation a folder, or pinned the folder the next conversation opens in. The pin is /project now, which is home's alone: `/project <path>` takes the path and opens nothing, a bare /project opens the browser, and in a conversation it says which screen it lives on. /folder means one thing everywhere, and never moves the directory codeaf is standing in."
  - "/attach took a file. It takes a file or a folder — a folder goes through the same seam /folder uses — and a bare /attach on home opens the browser aimed at the next conversation's folder."
  - "Home's box had no @ completion, and the project sat on home's rule. The @ list works on home now (files and folders, never tasks), and the project moved to the right end of the keys row under the box, in a conversation as well as on home."
  - "/manual printed the manual's pages as written with no model call. It is a turn now: the question goes to the model, told to answer from the manual tool and name the page. The as-written reading is still `codeaf manual` at the terminal."
  - "The Workspace tab's row was `ui.hints` meaning shown. The row reads `disable hints`, off by default; the persisted key keeps its bytes and internal/config inverts once on the way in and out."
  - "A showing was every visible change of hands of a tip row. On home a showing is now a tip that stood twenty seconds where it could be seen; in a conversation it is one session, counted when the slot takes the tip. A ledger written under the old rule is read once with the rows that rule spent forgiven, and the tips a gesture retired stay retired."
  - "The rewind tip read `/rewind takes back an earlier message`, and for one day `esc esc takes back the last message`. It names both doors — `esc esc or /rewind takes back an earlier message` — because #1388 gave the chord back and a row teaching only the chord leaves nobody a word to type into `/` or ask the manual about. Either door retires it."
  - "For one day this branch taught esc as the door home, after the surface stopped treating two spaces that way on 2026-09-17. #1388 restored the gesture, so TWO SPACES IN AN EMPTY BOX open home and esc is the interrupt, the layer peel and the arming half of rewind. Every manual passage and every test here says so."
---
There is one table of tips and there are two boxes, and the wave ends with the
two boxes saying them by rules of their own, because a conversation is a screen
you sit in and home is a screen you pass through.

A CONVERSATION RANKS. The tip is the lowest rung of the keys row at the foot,
taken from the rest state and outranked by every state with keys of its own.
The first eligible row in the table's order wins, so a tip that has just become
true is said at once, and a gap of two turns keeps a busy first session from
reading as a slideshow. No clock, no cross.

HOME ROTATES. The tip is the row over the rule, and every tip that is true gets
its turn — moving on with every visit and every two minutes at rest. The cross
means enough of these for now: the row goes blank and nothing takes its place
until home itself leaves the frame, and the tip put away is charged nothing.

The list is twenty-two rows, from a survey of forty-eight lines worth saying
and three reads of the whole thing by the owner. Using a gesture on either box
retires its tip on both.
