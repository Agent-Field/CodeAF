---
kind: changed
title: The assignee's runner publishes factory pull requests
pr: 1800
surface: [chat, engine, remote, docs]
invalidates:
  - "Publishing used a watched GitHub source and could select a shell account ahead of the app's handoff. The leased assignee's runner now checks its GitHub login and uses one local token for the approved commit's push and PR or comment, including solo factories and repositories it does not watch."
  - "Finding an existing PR only returned its URL. The runner now updates its body with the approved proof and Factory-Approval trailer."
  - "An expired publish lease silently queued its approval again. It now stays in Needs you with Runner went offline mid-step until the assignee retries on that machine or cancels."
---

The server reads GitHub and records the runner's publish receipt. Stable
publish failure codes, the publish_failed gate and HTTP Idempotency-Key replay
remain in place. Team and solo acceptance drive HTTP approval, a real lease,
the worker, git and a fake forge; they cover PR creation/update, proof comments,
PR review comments and forbidden publishing with the assignee's credential.
