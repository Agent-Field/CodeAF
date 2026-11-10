---
kind: fixed
title: A settled task keeps one current row in the conversation, a just-settled node opens its report, and the pool's index pull stops at close
pr: 2588
surface: [chat, engine, docs]
invalidates:
  - "Since #1893, answering a task's landing with `a` or `n` could leave the conversation showing only the decision receipt: the task's own `done` or `incomplete` row and its `open result` were never drawn, because the disclosure that holds them sat on a landing the render loop skipped. Every settled task now has its row in a `worked` or `background work` disclosure, and the answered your-call card stays hidden."
  - "A completion group whose disclosure anchor already held another turn's fold was dropped. Both groups now share that fold."
  - "After a process or window closed, a late model-pool index response could still fetch its signature and write the cache: #2583 joined the refresh, but the pull never checked its cancellation. It now stops before every request and every cache write."
  - "A task that landed quietly as done and then landed again (for example a conflict answered with drop it, its branch kept) showed both a `done` row and the newer `incomplete` card. A task now shows its current state once: a visible card takes the place of its row, and a dismissed card leaves the row with the newest state."
  - "Opening the result of a task node that had just settled showed no report and no files until a restart (v0.8.2 behaved the same). It now shows the node's report and changed files at once."
  - "Settings → Tasks → review task results described the check as an `auditor`. It now says in plain words what the check does and that with it off a task lands done without it."
---

Found by the pre-release hand test of dev, where the tmux suite's refused and nested landing subtests failed on it.
