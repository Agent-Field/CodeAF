---
kind: fixed
title: a worker reads a task's notes by their envelope, so a long plan name no longer holds every note
pr: 0
surface: [engine]
invalidates:
  - "A plan named over 4096 bytes, such as one made from a very long first task title, held every note from every bash worker and listening program: the note page refused each row for its project or chat tag and nothing after the refusal was delivered. The inbox readers now use plandb.Store.NoteEnvelopesPage, which reads only a note's id, author, direction, words and client key, so the plan's name, conversation tag and time cannot hold them. plandb.Store.NotesPage and the hydrated readers are unchanged and still carry the tags."
  - "The chat manual's senior-dev page said a long plan name could hold every note and that nothing warned of it. It does not hold them. A note whose id, author, words, direction or client key is over its ceiling still holds the notes after it, is never skipped, and is still named by the unread report; codeaf still has no way to edit or delete one note."
---

The envelope has no project, chat or time field, so a worker cannot filter on a
value the page never read. A note written under the writer's bounds cannot be
refused by it; only one written before them, or outside codeaf, can.
