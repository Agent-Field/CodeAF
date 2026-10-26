---
kind: changed
title: Desktop conversation names fit tabs and vague openers get one second chance
pr: 1952
surface: [engine, remote, docs]
invalidates:
  - "Desktop model names could use forty characters. They now use at most twenty-four characters, while the first-message placeholder keeps its forty-character limit."
  - "Desktop naming used only the first message and never revisited a vague opener. It now seeks one final name within the first three finished exchanges when the opener has fewer than four words or the first model name is generic."
---

The title model is asked for two to four plain words in sentence case, keeping
proper nouns, with the distinguishing word first and the person's own words and
names. Up to twenty current open app
conversation names are read for each naming call, including shorter repairs,
to help it choose a different name. Renames, closures, archives and new tabs
are reflected without reopening the engine.

The second chance uses the opening message, the current exchange's message,
and the first 400 characters of its reply. It checks at most three finished
exchanges, keeping the current name while no concrete topic appears. The first
concrete topic earns one final model name. Three vague exchanges exhaust the
chance. The budget and final decision persist across relaunch. A person's rename
always wins and stops further naming.

Model names end at whole words without an ellipsis. If the answer has no word
that fits, one shorter-answer request is allowed for that naming check. The
title prompt uses only topics supported by the person's messages or reply.

A model echo of a vague opening keeps the exact placeholder and leaves the
remaining exchange budget available for a concrete topic.

Greetings and empty requests keep their placeholder while the person supplies
no topic or file, even if a model proposes a different generic label. A later
concrete message or attachment can still earn the final name within the budget.

Only handled naming answers consume the persisted check budget. Cancelling a
check, including the third one, leaves that slot available after relaunch.
Queued and in-flight checks use bounded in-memory reservations.
