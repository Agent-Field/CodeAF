---
kind: changed
title: Desktop conversation names fit tabs and vague openers get one second chance
pr: 1952
surface: [engine, remote, docs]
invalidates:
  - "Desktop model names could use forty characters. They now use at most twenty-four characters, while the first-message placeholder keeps its forty-character limit."
  - "Desktop naming used only the first message and never revisited a vague opener. It now seeks one final name within the first three finished exchanges when the opener has fewer than four words or the first model name is generic."
---

The title model is asked for two to four plain words, with the distinguishing
word first and the person's own words and names. Up to twenty other open app
conversation names are supplied to help it choose a different name.

The second chance uses the opening message, the current exchange's message,
and the first 400 characters of its reply. It checks at most three finished
exchanges, keeping the current name while no concrete topic appears. The first
concrete topic earns one final model name. Three vague exchanges exhaust the
chance. The budget and final decision persist across relaunch. A person's rename
always wins and stops further naming.

Model names end at whole words without an ellipsis. If the answer has no word
that fits, one shorter-answer request is allowed for that naming check. The
title prompt uses only topics supported by the person's messages or reply.
