---
kind: fixed
title: A namer that answers with its plan is refused, so the task keeps the person's own words
pr: 942
surface: [chat, engine]
invalidates:
  - "A task whose namer answered with a sentence was named from the front of that sentence: the rail row and the task page read `I'll start by` while the node's own record carried the person's words right beside it. No longer true: an answer that opens as a clause about the speaker is refused at any length, and the row keeps the fallback — the person's own words, clipped by the column exactly as an unnamed task's title always was."
  - "The repair both namers read an answer through (`cleanTitle` in `internal/session/title.go`) covered markup, quotes, announcements, instruction echoes and empty-subject answers, and a first-person preamble was none of those. No longer true: a first-person subject followed by an auxiliary — `I'll start by`, `We'll begin`, `Let me first`, `I am going to` — is refused before `firstWordsOf` cuts to three words, so the cut can never manufacture a label out of the front of a sentence. Both halves are required, so `we chat cutover` is still a name."
  - "Throat-clearing a model puts in front of an answer was the agreement it opened on — `Sure,`, `Okay!`. It is now also the sequencing adverb a plan narrates itself with, so `First, I will write the four pages` and `Okay so I'll look at the brief` have their opener taken off by the same `stripInterjection` every namer already shared, and `First, the bakery pages` is minted as `the bakery pages`."
---

The namer is the cheapest call in the session, asked for two or three lowercase
words, and the prompt was what kept it to that — until a run pinned every tier
and role to a reasoning model and the namer answered its plan instead: 264
completion tokens of "I'll start by creating the four bakery landing pages one
at a time." Nothing in the path refused a sentence. `firstWordsOf` cut it to
three words, `taskNameNeeded` asked only whether an answer was empty, an echo, a
path or over three words, and the task was titled `I'll start by` on the rail, on
the task page and in its own record — while `spec.title`, the person's own words,
sat there as the fallback the truncated preamble had overwritten.

The refusal is grammar rather than a list of sentences, and it lives where both
namers already read an answer: a first-person subject followed by the word that
makes it the subject of something about to happen. A refusal is the empty string,
which is a road that already worked — the task namer leaves the row's title
exactly where it was, and that title is the person's own sentence clipped by the
column. A noun phrase that merely ran long is still cut to three words and is
still a name, which is the whole difficulty of the fix: `launch post for existing
users` was a label, and `I'll start by creating the four bakery landing pages` was
never one at any length.
