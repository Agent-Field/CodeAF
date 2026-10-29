---
kind: changed
title: Knowledge capture wakes on the chat's own writes, not every second
pr: 2048
surface: [chat, engine]
invalidates:
  - "Each live chat's knowledge capture woke every second and read its journal, even while idle. It now wakes when the chat writes its journal, looks once more a second later, and otherwise runs one five-second safety tick."
---

Lines this process writes are captured within about a second, as before. Lines
another process writes to the same journal are captured within five seconds,
and consent changed in another process with no write here within ten.
