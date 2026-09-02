---
kind: changed
title: the chat manual stops saying a nested question is hidden, and names the folder that would not freeze
pr: 447
surface: [chat, docs]
invalidates:
  - "The chat manual said that while a parent task was still working, a sub-task of it that needed a look was not put in front of you and that the roster did not file that family under `needs you`. It is filed under `needs you` from the moment it lands, at any depth, and its landing card, its roster row and its own room all offer the four answers; what the parent's running changes is only how loud the row is."
  - "The manual said you are never asked about several pieces of one job while the job is still running. You can accept, look again at, or reject a sub-task before its parent finishes."
  - "The failure table's `could not prepare a working copy` row named no error text. When the folder a task is cut from could not be frozen the error reads `this task's world could not be sealed:` followed by git's own words, it is the folder and not the brief that is wrong, and nothing was started and nothing was spent."
  - "The list of words a task page's header draws was missing `sizing the work`, and it is now written out complete, including `waits: <the work it waits on>` and the merge words. `briefing a worker` is not one of them: that word is on the status line under the message box."
  - "The manual placed the setting `task.autoapprove_seconds` in the spending category of the settings panel. It is on the `Safety` tab, as the row labelled `task countdown`."
---

Four of these are sentences somebody could have read, believed, and been wrong
about work sitting in front of them: a decision that was described as invisible
has been visible on every surface since #292, and a page that says a question
cannot be answered is worse than a page that says nothing.
