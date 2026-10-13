---
kind: added
title: App factories share the listener and elected worker with the terminal
pr: 1800
surface: [chat, engine, remote, docs]
invalidates:
  - "App factory connections, hand-off and power routes returned 501 stubs. They now create and mount solo, attach several teams, leave teams, prepare and send hand-off cards, and forward runner sleep and wake."
  - "The terminal worker could serve only its selected factory. The elected worker now serves several isolated factory mirrors and accepts app team sessions through a private memory-only control socket."
---

The app and TUI keep one shared home, solo host election and worker process.
Team session tokens stay in memory; attach.json stores only connection addresses
and modes. Origin work stops only after job creation, with its factory receipt.
Cards include a factory picker and use the factory's own idempotency door.
The solo mount preserves runner credentials through its proxy and takes over
when the previous solo host closes. Task hand-off decodes the task resource's
opaque id before graph or plan cancellation and refuses a changed source task
before creating a job.
