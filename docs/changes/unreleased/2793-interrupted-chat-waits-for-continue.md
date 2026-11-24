---
kind: changed
title: A chat stopped mid-turn by a quit or crash waits for Continue instead of resuming by itself
pr: 2793
surface: [app, engine, docs]
invalidates:
  - "Quitting the desktop app mid-reply made that chat carry on by itself on the next launch, behind \"Resumed after quit\", and spend; now it reads \"Interrupted\" with Continue and nothing in it runs until the person presses Continue (agents.continue), which carries it on once."
  - "An unexpected app close resumed a working chat by itself up to twice in a row (with a three-starts breaker and a one-hour age limit, and \"The app closed while this was working · send a message to carry on\" after that); now every such chat is marked Interrupted with Continue, at any age, and the attempt counter, breaker and pending-wake replay are gone."
  - "Opening a chat in the app resumed its interrupted task runs and turned its task frontier, so a stopped chat was woken again by its own background task; an app open of an Interrupted chat now holds that work (session Config.HoldInterruptedWork) until Continue, or until the person sends a message there."
---

A demo machine spent about $1.60 on Opus, unasked, because a chat that was
mid-turn at quit resumed on every relaunch (OWNER-DECISIONS 2026-11-17 §12).
Organization task runs keep their server-side resume-once, and the terminal's
own reopen still recovers tasks as before; this is the desktop app's chats.
