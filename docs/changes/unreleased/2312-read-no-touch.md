---
kind: fixed
title: Opening a factory job leaves it untouched
pr: 2312
surface: [factory]
invalidates: []
---

`factory.job_chats` returns existing conversations without creating a manager or
counting a page view as personal work. `factory.job_send`, announced in hello,
creates the manager on first send; later `agents.send` messages also count as
personal work. Apps must handle `agent: ""` with a first-send composer; older
desktops omit that composer and need updating. Storage migration, lease reaping
and job settling retain their existing behavior. Existing touch marks remain.

Automatic brief writes can create the manager without personal touch. Manager
creation, brief generation, saved briefs and their system spending leave an
unworked issue eligible for closed-issue handling. Accepted messages through
`factory.job_send` or `agents.send` still record personal ownership.
