---
kind: fixed
title: enter in the home input no longer freezes the screen while a shared conversation opens
pr: 1743
surface: [chat, tui]
invalidates:
  - "On a shared legacy connection, pressing enter in the home input ran the whole launch assembly — subharness wiring, the memory graph, the foreign-skill scan — inside the keystroke, and typing and drawing froze for as long as the engine took. The door opens off the update loop now, on the line every other door uses; only the swap onto the shared agent stays on the loop, so shared behavior is unchanged. Home stays visible until creation succeeds, and a refused door keeps both drafts where they were typed."
  - "A conversation opened from a place row, from /manual on home, or from home with a draft sent its opening sentence in a batch beside the open. With the door off the loop the sentence could land on the conversation still on screen. The send now rides the door's fold after the swap, in the same order the synchronous road gave it, so the words always reach the conversation they were typed for."
---

Enter in the home chat input starts a new conversation without taking the
screen with it. The engine door is asked off the update loop — the same line
every other door uses, with the same "opening conversation · esc cancels"
courtesy — and the commit that swaps the agent in place runs on the loop
after the door answers, which is all a shared handle ever needed
synchronous. The sentence you typed goes out after the swap, home's own rows
keep their pins, and a door that refuses says so where you are standing with
both drafts intact.
