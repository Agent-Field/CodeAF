---
kind: fixed
title: a message waiting above the box stays waiting when you leave for Home or another chat, and still goes when its answer ends
pr: 1385
surface: [chat]
invalidates:
  - "A conversation switch used to fold every message parked above the box into the draft and drop the pictures parked with it, so a follow-up parked before esc, enter came back as unsent text without its picture and was never sent. The parked queue now travels with the conversation as itself — words, pictures, pasted documents, standing mark, order — and comes back as the same waiting block."
  - "A parked message used to go only when its conversation was in front at the turn's close. It now also goes when the turn ends while the conversation is held behind the screen: the keeper sends the oldest one through that conversation's own agent, one per finished turn, after any ctrl+q follow-up the session already holds."
  - "/new no longer drops the messages waiting in the conversation it leaves; they stay with that conversation and go when its answer ends. Opening a session from the welcome box still drops them and says so."
  - "On a connection that holds one conversation at a time the waiting words still fold back into the box, and their pictures and pasted documents now come back onto the tray with them instead of being lost."
---

Since #1071 esc opens Home, and Home stands the cursor on the conversation before
this one, so the natural esc, enter is a switch. Santosh met it as "esc when a
message is waiting does not seem to send it, it seems to just cancel the running
one". The switch is what lost the message, and a switch is not a quit: the turn
keeps running, so the message can keep waiting and go when it ends.
