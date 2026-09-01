---
kind: fixed
title: a landing that needs your look asks on every surface at any depth, and a timeout says who could not
pr: 292
surface: [engine, chat]
invalidates:
  - "The landing card in the conversation was written for ROOT tasks only, and the roster row and the task's room both read the answers row through that card — so a nested part that landed needing a look had no way to be answered on any surface at all. The card is now written for any node that needs a look, whatever its depth."
  - "While a parent was still running, a child that nobody had decided about was filed under `done` on the roster and counted there in the footer. It is filed under `needs you` from the moment it lands; what a running parent changes is how loud the row is, never whether it is there."
  - "The checker's window running out used to be reported as \"no answer in 5m0s, so nothing was accepted\", which reads as a window the person was given and missed, and as a decision a clock is not entitled to make. It now reads \"nobody could check it in 5m0s\", and accepted or not-right stay reserved for what somebody actually said."
  - "When a parent settled, each still-undecided child had its whole landing note re-delivered to the model a second time. It now gets one sentence saying the question has changed hands, because the demand itself is on internal/session's PendingDecisions() and on every surface that draws it."
---

A question to a person is never on a timer: it waits, visibly, on every surface,
until it is answered. A part two levels down is a decision somebody has to make
in exactly the way a root is.
