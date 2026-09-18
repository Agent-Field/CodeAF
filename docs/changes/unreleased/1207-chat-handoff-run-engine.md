---
kind: fixed
title: "an approved chat hand-off launches through the run engine under the bash belt"
pr: 1207
surface: [chat]
invalidates:
  - "An approved task proposal was admitted to the session tree and ran on the legacy engine even under CODEAF_TASK_BELT=bash. A hand-off approved under the belt now starts through the run engine like a typed /task, joins the live root when one is running, and carries its declared dependencies into the run store."
---

Under CODEAF_TASK_BELT=bash, committing an approved proposal routes it into the
run plan through the same door a typed /task takes, instead of admitting it to
the session graph on the old engine. When a live root is already running the
task joins it, a proposal's declared dependencies travel into the run store,
and the spawn floor no longer refuses a dependency that lives in the run store.
