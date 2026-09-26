---
kind: changed
title: groom the standing card ask and its yes clause
pr: 1520
surface: [chat, docs]
invalidates:
  - "The standing card ask led with 'wants to keep an eye on:'. It now leads with 'wants to set this up:' across all kinds."
  - "The standing card yes clause unconditionally stated 'it keeps happening until you stop it'. For one-off scheduled items, it now states 'it happens at the time, and then it retires'."
---

The standing card lead was inaccurate for rules and reminders that do not keep an eye on anything. The lead is now generic across all kinds, and the confirmation clause branches on one-off schedules rather than promising recurring execution.
