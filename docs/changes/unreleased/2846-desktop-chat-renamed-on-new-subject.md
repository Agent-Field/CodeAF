---
kind: changed
title: A desktop chat that changes subject gets one new name, and a name you gave it is kept
pr: 2846
surface: [engine, app]
invalidates:
  - "Was true: a desktop conversation was named from its opening (and up to three exchanges after a vague one) and then never renamed, so a chat that moved to a different subject kept the old subject's name in its header. Is true now: a message that is a clear new subject (three or more content words, no back-reference, almost no words shared with the name and the last four exchanges) asks the title model once, which may answer KEEP_NAME; otherwise the conversation is renamed once for that change of subject. Ordinary messages make no extra call."
  - "Was true: a rename by the person and the opening words kept as a fallback were journalled the same way. Is true now: the naming state carries `person` for a name the person gave, which nothing replaces, and `engine` for a name the engine chose; an older literal name with neither is treated as the person's."
---

The terminal still names a conversation once. The desk needs no change: it already takes the title from every `agent.changed`.
