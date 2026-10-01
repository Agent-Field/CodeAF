---
kind: changed
title: Every recognised slash command is chipped wherever it stands, in the box and the sent transcript
pr: 1699
surface: [chat, docs]
invalidates:
  - "A chip marked only a command this surface would act on: a leading command, or a send-door tag (`/standing`, `/orders`, `/task`) elsewhere, while `/compact` mid-sentence stayed plain. A chip now marks any recognised command wherever it stands, and whether enter acts on it is unchanged."
---

A chip is a recognition mark: it says codeaf knows the word, not that enter will
run it. Enter still runs only a leading command, and still acts on a send-door tag
away from the head. A tag backspaced to plain words stays plain in the sent
transcript as it already did in the box, and that includes a message typed while an
answer was still coming, which waits its turn with the tag still plain.
The waiting block, a sentence steered into a running answer and a reopened
conversation keep recognised commands chipped and demoted send-door words plain.
