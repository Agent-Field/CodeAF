---
kind: fixed
title: do --json answers an early refusal, no unasked merge after senior-dev, manual truth
pr: 1682
surface: [chat, engine, docs, build]
invalidates:
  - "`codeaf do \"\" --json` (and --best with --cheap, a bad --slots) exited 1 with nothing on stdout. Every refusal after the flags parse now prints the shared JSON envelope with ok:false, stop:\"error\" and the refusal as error; stderr and the exit code are unchanged."
  - "After a passed senior-dev run the chat could merge its branch into the person's branch on the wake turn. The outcome prompt now says, for every outcome, that bringing the branch over is the person's call, done only when they ask in a later message."
  - "worker-harness.md said a finished task always merges into the folder it was cut from; on main or master it keeps its branch and says `branch kept`. commands.md said /history and a bare /task open a place called tasks; they open the sessions place."
  - "scripts/hosted-drive.sh pressed ctrl+g for the task column and looked for a `#2` id; it now uses alt+l and the rows' titles, and passes on a healthy binary."
---
