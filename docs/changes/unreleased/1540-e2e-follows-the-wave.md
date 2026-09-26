---
kind: fixed
title: the tmux e2e suite follows the six-place bar, the new digits and questions on their own rows
pr: 1540
surface: [chat]
invalidates:
  - "TestTUIE2E still drove the four-word bar and `alt+3` for spend, and waited for a `needs you` panel. On dev c34a3a76d it failed 10 of 19 subtests, 9 of them for this reason or load. The suite now reads the six places on the wordmark row, presses `alt+5` for spend, answers a question from its conversation's own row, and walks the sessions place to a task's record."
  - "The manual's answer-from-home section said the top `needs you` row draws the answers on its second line. There is no `needs you` row. The selected row's description carries the question and its answers, and a digit answers from anywhere on home."
---
