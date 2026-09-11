---
kind: changed
title: A namer that answers its plan is refused, so a task keeps the person's own words as its title
pr: 942
surface: [chat, engine]
invalidates:
  - "A task whose namer answered with a sentence was named from the front of that sentence: the rail row and the task page read `I'll start by` while the node's own record carried the person's words right beside the name it had been given. No longer true: an answer that opens as a sentence about the speaker or the plan is refused at any length, and the row keeps the fallback title — the person's own words, clipped by the column exactly as an unnamed task's title always was."
  - "The repair a namer's answer was read through (`cleanTitle` in `internal/session/title.go`) covered markup, quotes, announcements, instruction echoes and empty-subject answers, and a first-person preamble was none of those. No longer true: it refuses `I'll start by`, `Let me first`, `First, I will`, `We'll begin`, `okay so` — a first-person pronoun or contraction, a sequencing adverb holding its comma — before `firstWordsOf` cuts to three words, so the cut can never manufacture a label out of the front of a sentence."
  - "The session's namer and the task's namer each read an answer through its own hand, so the two could drift apart on what counts as a name. No longer true: the preamble vocabulary lives beside `openerVocabulary` in the one shared cleaner both namers call, so a conversation title and a task name are refused for the same words."
  - "A four-word answer was cut to three words whatever it was, and that same cut is what let a paragraph pass as a label. No longer true: `launch post for existing users` is still cut to `launch post for`, because that answer was a label that ran long; `I'll start by creating the four bakery landing pages` is not a label at any length."
---

The namer is the cheapest call in the session, asked for two or three lowercase
words, and the prompt is what kept it to that — until a run pinned every tier
and role to a reasoning model and the namer answered its plan instead: 264
completion tokens of "I'll start by creating the four bakery landing pages one
at a time." Nothing in the path refused a sentence. `firstWordsOf` cut it to
three words and trimmed the trailing marks, `taskNameNeeded` asked only whether
an answer was empty, an echo, a path or over three words, and the task was
titled `I'll start by` on the rail, on the task page and in its own record —
while `spec.title`, the person's own words, sat there as the fallback the
truncated preamble had overwritten. The refusal now lives where both namers
already read an answer: `cleanTitle` returns the empty string for an answer that
opens as narration, the task namer leaves the row's title exactly where it was,
and that title is the person's own sentence clipped by the column. A refusal
costs the same as a timeout and lands on a road that already worked, so the
session's namer gains the same refusal for free. The manual page for tasks
(`internal/manual/chat/tasks.md`, *Why my task is called something I did not
type*) says so in the same words.
