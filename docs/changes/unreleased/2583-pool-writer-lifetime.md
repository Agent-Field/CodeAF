---
kind: fixed
title: The model pool's writers stop with their owner, and the tmux suite runs on the engine road
pr: 2583
surface: [chat, engine, docs]
invalidates:
  - "The model pool's startup push and landing scorer could write pool/install and pool/outbox.jsonl, and send requests, after the chat, shell run or headless command that started them had returned. Close now cancels them, waits at most one second, and a late response neither writes nor sends; a cancelled score is redone on the next launch and a cancelled push keeps its unsent rows."
  - "The tmux suite's subtests used CODEAF_HOME paths long enough to break the 103-byte socket limit, so some ran on the in-terminal road, and its home test asserted that Automations was not on the bar although #1831 made it a permanent tab. Every subtest now gets a short state root, the home test asserts the permanent tab, and the landing tests wait for the answered turn to settle."
---

Found as a TempDir race in the Full check on dev after #2577.
