---
kind: fixed
title: chat keeps arbitrary text intact while names and work happen in the background
pr: 483
surface: [chat, engine]
invalidates:
  - "Every line beginning with a slash was treated as a command. Only a recognized command or alias executes now; unknown slash-prefixed text is sent as prose unchanged."
  - "Folded paste chips unfolded only through the ordinary send door, and matching visible text could be replaced accidentally. Every task, standing, harness, room, queued, switched-draft and resumed send now unfolds only its tracked chip occurrence exactly once."
  - "A task or session could keep a transport label such as `paste 1`, a one-word model answer, a path or a sentence as its name, and naming could block or outlive the session. Naming is asynchronous, validated across every admission path, and owned by the session lifecycle."
  - "Cancelling a task before its runner installed a handle could let cancellation and completion both finalize it. Cancellation now either owns the unopened task or signals the runner, and exactly one side finalizes the node."
---
