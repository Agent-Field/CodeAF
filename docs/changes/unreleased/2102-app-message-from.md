---
kind: added
title: codeaf app keeps who sent a message on the person's behalf
pr: 0
surface: [engine, remote]
invalidates:
  - "Was true: a message an assistant sent for the person through agents.send or crews.message was recorded and shown as the person's own, and the model could not tell. Is true now: both take an optional `from` (a name of at most 64 bytes of letters, digits, spaces and `-_.:@`, such as `afi`). The person's entry keeps the words as sent with `from` set, on the live entry.added, agents.entries and after a restart; the model reads the same words under one line, `[sent by <from> on the person's behalf]`. A message without `from` is unchanged. A message with `from` is refused unknown_method by an engine without the message door, refused by a remote engine whose welcome lacks `messageFrom`, and refused busy (never queued) when the agent is in the middle of a turn."
---
