---
kind: added
title: Run a task on another paired computer while the chat stays here
pr: 2085
surface: [chat, engine, remote, docs]
invalidates:
  - "A task always ran on the computer the chat was on. It can now run on a paired Linux or macOS computer that runs `codeaf serve --take-tasks`, chosen with `/task --on <computer>`, the proposal card's computer choice, or the Run tasks on setting; the chat, conversation file and transcript stay here."
  - "codeaf serve only made a machine reachable for `--at`. With `--take-tasks` it also takes tasks from the identity's paired computers; plain `codeaf serve` still takes none and prints what it did before."
  - "propose_task had no way to name a computer. It gains an optional `computer` argument, present only after the Run tasks on setting names a paired computer; with the setting at this computer the schema is unchanged."
  - "A dev box only served `--at` chats. Its boot now passes `--take-tasks` when the box's installed build lists the flag, so a task can be sent to it."
---

The task's copy is prepared after the task appears, then travels as an
encrypted, content-addressed delta through the relay, the other computer runs it
with its own provider keys, and steps, phase and spend stream back here; the
change lands through the ordinary landing road, or Review and Apply with private
task copies on. A computer that stops answering is shown as possibly still
working with `run it here`; nothing re-runs by itself, and an abandoned
attempt's late result never lands. Only a task that works in its own copy can
go, both ends must be Linux or macOS, and a remote task runs alone in its
conversation.

Everything is opt-in. With defaults left alone, pairing adds no relay traffic,
and a local task's brief, records, the proposal card and the model's tool are
byte-identical to before; the Run tasks on settings row is the only new thing a
paired person sees.
