---
kind: added
title: Outside work becomes a factory job through codeaf factory add or POST /api/intake
pr: 2477
surface: [factory, app, engine]
invalidates:
  - "Only GitHub issues and work typed on the floor could become factory jobs. Now any source files work through internal/factory/inbox.Add, keyed by Item.SourceRef <source>:<external_id> with Origin the source and Num 0, so a repeat updates the same job."
  - "A factory job could always be launched once it existed. Now a job with no repository is refused at the launch with \"say which repository the work is on\" (factory.NoRepoRefusal) and the intake passes it by."
---
