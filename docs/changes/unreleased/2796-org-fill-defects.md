---
kind: fixed
title: Org tasks made running start, doers tick their tasks, Asks reach their decider, Fold records it
pr: 2796
surface: [factory, engine]
invalidates:
  - "A task POSTed to the org server with state running was kept running with nothing behind it and later failed as stopped reporting back. It is now started through Start's door in the same write, and a create nothing can take is refused with nothing kept."
  - "Only a person task's owner could tick its checklist or finish it, so the doer of a task made from Afi's cards needed it handed over first. The person named as its doer may now tick and finish it; the owner still answers for it."
  - "A first `ask` on a task ignored its decider and went to the task's owner; only add_ask reached someone else. An ask naming a decider other than the owner now goes to that decider."
  - "Fold wrote no Decisions line on the server. It now writes '<who> folded “<name>” into this workstream' in the destination, and Undo removes it."
---
