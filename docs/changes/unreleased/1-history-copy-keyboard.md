---
kind: fixed
title: History arrows walk multiline prompts and copy mode offers Enter
pr: 1
surface: [chat, docs]
invalidates:
  - "After recalling a multiline prompt, Up moved the caret instead of reaching the older prompt. While browsing history, Up and Down now move whole entries in conversations and task rooms, and Down past the newest restores the draft."
  - "Copy mode only offered the unfamiliar y yank action. It now displays Enter copy, retains y as an alias, and shows clipboard sent after emitting the terminal clipboard write."
---

History keeps its existing ordering: cleared drafts, project history, then other
projects. Terminal clipboard permissions still apply; clipboard sent confirms
the write was emitted rather than claiming the terminal accepted it.
Narrow frames keep the history arrows and Enter copy visible with shorter hints.
