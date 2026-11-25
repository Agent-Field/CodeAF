---
kind: added
title: Fold moves work with Undo and Bring along carries files into organizations
pr: 2778
surface: [factory, engine, app]
invalidates:
  - "Bring along into an organization carried pages and decisions but omitted Files, upload bytes and task links. Import now carries those files, non-Factory tasks and task threads; a chat can bring only its own work into an existing destination. The source stays until the organization accepts it."
  - "The old child Fold moved only jobs and archived the child. Workstream updates now fold tasks, files and decisions into another open workstream, removing the source from Work and Archived, with durable guarded Undo. Governance on both sides is required; working tasks and nested sources must be resolved first. The desk carries its private chat index beside the move."
---
