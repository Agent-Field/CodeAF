---
kind: fixed
title: the tmux suite finds the firing's second reminder even when the model drops its subject
pr: 1543
surface: [chat]
invalidates:
  - "`TestTUIE2E/the_firing_reaches_the_person` found the second reminder only by the word `stretch`, so it failed whenever the model saved the reminder without its subject, even though it stood and fired. It now falls back to the one record that is not the first reminder, and logs a finding."
---
