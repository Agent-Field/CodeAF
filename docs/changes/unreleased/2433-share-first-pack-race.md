---
kind: fixed
title: Serialize first-share capture with concurrent store writers
pr: 2433
surface: [engine, chat, app]
invalidates:
  - Different codeaf transports and daemon sockets used to open one physical store concurrently; their requests now share a cross-process store operation lock.
  - Canceling a daemon caller used to close its connection while its accepted request could still write; its store operation lock now stays held until the engine answers.
---

Concurrent first capture planning and sealing could report `durable object pack is
missing` even though the pack was present. furrow 0.3.6 can combine an earlier
missing-pack observation with a newer durability frontier. A persistent lock keyed
by the physical store now orders standalone and daemon requests, including
separate codeaf processes and path aliases. Different stores remain independent.
The bounded evidence capture and one fresh-plan retry remain.

Linux regressions stop the released engine just after its missing-pack syscall
and try the first durable seal through another transport. They cover both a
standalone reader and a second daemon. A minimal furrow-only reproduction is
`python3 internal/furrowbin/testdata/first_pack_race.py /path/to/furrow`.
