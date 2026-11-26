---
kind: fixed
title: Keep app follow-up drafts clean and expose record Actions
pr: 2976
surface: [app, engine]
invalidates:
  - App run drafts could save simulated tool calls or status narration. Text steps now check their artifact and ask once more before preparing a save.
  - A record-writing run needed a separate command to appear in app agent grants. The run now has a grantable Action when it has no command.
  - A kit 2 app with declared outputs used to give the render tool to its run agents implicitly. An agent step now declares tools [render] to use it.
---

The follow-up approval shows the client message that will be saved. The Action
catalog exposes typed arguments, including prompted record fields. Check flags
assistant Action IDs that the app does not declare and record commands whose
effects need a typed run before they can be granted.
