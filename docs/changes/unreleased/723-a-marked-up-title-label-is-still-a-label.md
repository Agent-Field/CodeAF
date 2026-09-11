---
kind: fixed
title: A list marker in front of a naming label is markup, and a "Sure!" before one is still an answer
pr: 723
surface: [chat, engine]
invalidates:
  - "A conversation whose namer answered its two labels behind a markdown list marker — `- full: … / - tab: …`, `1. `, `1) `, `+ `, or a bold label inside any of them — lost the tab label: the strip drew a word-clipped copy of the full title instead. A marker is now taken off with the rest of the markup, before the label is looked for, so a listed pair names the conversation exactly as the bare pair does."
  - "A namer answering one plain line behind a marker was named with the marker on it — a tab reading `[- porting]` and a journal line `\"title\":\"- porting the parser\"`. The name a person reads now has no marker in it."
  - "An answer whose first label line opened with the throat-clearing a model agrees with a request in — `Sure! full: …`, `Okay, full: …` — was discarded whole and the conversation stayed `Untitled` after spending every rung of the ladder and all three attempts. That line is now read past the interjection, by the same rule `stripOpener` already applied to a plain name."
  - "`stripMarkup` was described as knowing three marks — emphasis, a code span and a heading. It knows four: a leading list marker as well, one only, and only when a space separates it from the words, so `-v flag handling` and `v1.2.3 release notes` keep every character they have."
---

What did not move is the refusal. A labeled answer with no usable `full:` still
mints nothing, because that refusal is what leaves the conversation drawn under
the person's own opening words with the namer free to ask again.
