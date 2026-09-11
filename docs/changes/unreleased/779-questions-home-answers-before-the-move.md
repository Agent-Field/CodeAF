---
kind: fixed
title: a held row that is asking lands on its own answers, and the move card waits for enter
pr: 779
surface: [chat, docs]
invalidates:
  - "A launch into a project whose conversation was open in another window raised `Move this conversation here?` at once (#758), and that card takes the digits first — so over a row stopped on the model's own question, `1` moved the card's cursor onto `move it here` and the row's `1 publish it` answered nothing. When the row is waiting on a person the launch now lands pointed and quiet on the row's own chips; enter still raises the move card."
  - "The row's answer chips stayed on the band and on the foot strip while the move card stood beside the row, so one screen advertised `1 publish it` and `1 move it here` for the same key. They step aside while the card stands (internal/tui3's answersStepAside, asked by both drawings) and are back the moment it is answered or put down."
---

The wave's own e2e subtest, `TestQuestionsE2E/AnotherWindowAnswersAndTheFirstSaysWho`,
found this on dev once #758 had landed beside #765, #768 and #772: the second
window's `1` never reached the first. Nothing here changes what the move card is or
how it is answered — it changes only WHEN it is raised over a row that is already
asking something, which is the row a person launched for.
