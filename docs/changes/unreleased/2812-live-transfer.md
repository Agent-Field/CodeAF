---
kind: added
title: Chats move live between paired computers while hosted sync storage is off
pr: 2812
surface: [chat, engine, remote, docs]
invalidates:
  - "With hosted sync storage off, a chat could not move between paired computers and stayed on the computer that made it; it now moves live, straight through the sync service with nothing stored, while both computers are running codeaf."
  - "The manual said that moving, continuing or opening a chat on the hosted sync service needs sync storage approval, and that new chats and files wait for it; chats and their files no longer wait, while saved keys, memories, teams, shared preferences, idle handoff and task copies still do."
  - "A chat on a computer that was not running codeaf simply could not be reached; continue here now fails with the sentence `<computer name> has this chat and is not on right now; open codeaf there, then continue here`, names the computer, keeps the typed words in the box, and works as soon as codeaf runs there again."
  - "Taking a chat on a second computer did not make that computer a source for it; the take is now remembered there, so a third computer can take the chat from the second while the first is off."
  - "With storage off the hosted sync service held nothing about a new chat; while live transfer is the only road it keeps a record per chat with its id, the computer that has it, times, hold state and sealed keys, never the title or folder, so the other computer lists the chat as untitled until it is taken, and the title then comes from the chat itself."
  - "A chat that only lived on computers stayed there until storage was approved by hand and then re-saved; when storage is approved later, those chats now upload in the background by themselves, and until that finishes they can still be taken from a computer that is on."
  - "There was no switch for this; CODEAF_LIVE_TRANSFER=off restores the earlier behaviour in which chats stay on their computer while storage is off, any other value leaves live transfer on, and it does nothing when storage is approved or on a self-hosted sync service."
  - "A chat living only on another computer could be listed on a computer that cannot fetch it and fail with a bare could-not-fetch error; a computer with live transfer off now says in a plain sentence that live transfer is off there, and an older codeaf ends the continue with its usual could-not-continue line, never a hang."
  - "The manual sections on continuing a chat on its own computer, and on tools stopping when a chat moves away, while storage is off described the only road; they now say they describe the road used when live transfer is off or unavailable."
---
Hosted sync storage is still off for an account until it is approved, and the
hosted sync service still stores no chat or file content while it is off.
Live transfer moves a chat's already-encrypted bytes from the computer that has
it to the computer taking it, through the sync service, only while both are
running codeaf (a terminal, the app or a background engine). It covers ordinary
chats and their files for a person with at least one other paired computer.
Saved keys, memories, teams, shared preferences, idle handoff and task copies
still wait for storage approval and behave as before.
