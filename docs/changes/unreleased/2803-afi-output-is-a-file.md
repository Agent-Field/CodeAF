---
kind: changed
title: what Afi makes on a task is a file, and a long agent comment becomes a document with one line
pr: 2803
surface: [engine, chat, app]
invalidates:
  - "Was true: an agent's ws_task_comment could post a draft, a report or a list of any length onto a task's thread (up to 2000 characters). Is true now: a comment from Afi or a chat over 400 characters or 3 lines is kept as a document in the workstream's Files, made by the task, and the thread gets one `made` event (text = the document's name, file = its id) that the desk draws with Open. A person's comment is never converted, and the comment schema's maxLength is 16000."
  - "Was true: the manual described the Tasks list choosing the first waiting task by itself beside a 500-pixel list, a task's Uses list with Attach and Remove from this task, Add a step on the checklist, More › Waiting on, More › Add a due date… and More › Move. Is true now (build spec v2): nothing opens until a row is picked, the list beside a task is 420 wide, search and state chips (Open default) sit over the list with Up next and Done as folded rows, there is no Uses list and no Add a step, Waiting on is a row under the fact controls, Due is only the fact control, and ⋯ holds verbs: Mark done, Hand to someone, Move to another workstream, Stop, then Ask someone, Send a request to, Delete."
---

The build review found Afi posting whole drafts as comments. What an agent makes belongs in Files, where it can be read, edited and commented on, so the engine now enforces it rather than relying on the prompt alone.
