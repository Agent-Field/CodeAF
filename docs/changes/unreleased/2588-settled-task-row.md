---
kind: fixed
title: A settled task keeps its row in the conversation, and the pool's index pull stops at close
pr: 2588
surface: [chat, engine]
invalidates:
  - "Since #1893, answering a task's landing with `a` or `n` could leave the conversation showing only the decision receipt: the task's own `done` or `incomplete` row and its `open result` were never drawn, because the disclosure that holds them sat on a landing the render loop skipped. Every settled task now has its row in a `worked` or `background work` disclosure, and the answered your-call card stays hidden."
  - "A completion group whose disclosure anchor already held another turn's fold was dropped. Both groups now share that fold."
  - "After a process or window closed, a late model-pool index response could still fetch its signature and write the cache: #2583 joined the refresh, but the pull never checked its cancellation. It now stops before every request and every cache write."
---

Found by the pre-release hand test of dev, where the tmux suite's refused and nested landing subtests failed on it.
