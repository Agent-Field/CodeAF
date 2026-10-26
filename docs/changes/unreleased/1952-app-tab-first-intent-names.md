---
kind: changed
title: Desktop conversation names fit tabs and vague openers get one second chance
pr: 1952
surface: [engine, remote, docs]
invalidates:
  - "Desktop model names could use forty characters. They now use at most twenty-four characters, while the first-message placeholder keeps its forty-character limit."
  - "Desktop naming used only the first message and never revisited a vague opener. It now names once more after the first completed reply when the opener has fewer than four words or the first model name is generic."
---

The title model is asked for two to four plain words, with the distinguishing
word first and the person's own words and names. Up to twenty other open app
conversation names are supplied to help it choose a different name.

The second ask uses the first message and the first 400 characters of the reply.
A person's rename wins over either model answer. Naming never runs a third time
or starts again on reopening a named conversation. A failed ask keeps the current
name. The second attempt is recorded before it is dispatched.

Model names end at whole words without an ellipsis. If the answer has no word
that fits, one shorter-answer request is allowed for that naming opportunity.
A vague opening with a vague reply keeps its placeholder; the title prompt
uses only topics supported by the person's message or reply.
