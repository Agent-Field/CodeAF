---
kind: changed
title: Organize existing chats is an explicit durable observe_and_organize job
pr: 1227
surface: [chat]
invalidates:
  - "A journalled person message could enqueue observe_and_organize that CreateFolder'd on a fresh empty Root. Automatic jobs now record no-action until a person-created folder exists; only Organize existing chats (coalesce key organize_existing) may invent folders on an empty tab."
  - "workspace.organize off cancelled every observe_and_organize row, including an explicit survey. Off still cancels automatic after-message apply; it does not cancel organize_existing."
  - "There was no OrganizeExistingChats / OrganizeStatus / CancelOrganize door. Those methods enqueue, report queued/running/delayed/done/cancel, and cancel the live row on the existing job table."
---

Repeated clicks coalesce while pending or leased. Restart leaves a queued or
running row for the standing pass. No second scheduler or FakeEmbedder in
production.
