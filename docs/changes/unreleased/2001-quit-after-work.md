---
kind: fixed
title: Quitting right after work saves quietly and never makes a stray branch chat
pr: 2001
surface: [chat, engine, remote]
invalidates:
  - "Quitting right after a background task could print 'could not save this chat' and leave the chat held, so the other computer saw this one as offline; the quit now joins the save in flight, publishes the newest turn and releases, printing nothing."
  - "The quit stopped waiting for the save after about ten seconds while saying 'up to 30s', and reported a failure even when the save went on to succeed; it now waits the stated bound, and a save still under way is reported as still finishing, never as failed."
  - "Every launch sealed an empty turn onto the chat, even one another computer held, and a later take published those turns as a branch chat nobody typed; launches no longer seal empty turns, and cost, usage and title bookkeeping are adopted on a take instead of kept as edits."
  - "A removed computer's quit printed 'could not save this chat … return on the next start'; it now saves locally and prints nothing, because its chats stay on that computer."
---
Found by the last real two-computer round, on the published dev build. Every
other step of the round passed.
