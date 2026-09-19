---
kind: changed
title: Organize wakes on enqueue, surveys past eight, and waits for reactive opt-in
pr: 1235
surface: [chat]
invalidates:
  - "The organize survey always recapped Unfiled[0..] and deferred at eight chats. The cap is now a per-lease slice; a job cursor continues past a no-action prefix."
  - "observe_and_organize ran only when the five-minute standing tick leased it. Enqueue and a membership commit now kick one pass through that same lock; the interval is crash fallback."
  - "Automatic after-message organize ran whenever workspace.organize was on. Graph writes now also need workspace.reactive, which Organize existing chats turns on; Organize this chat does not."
  - "A greeting or empty line could still CreateFolder on the explicit survey. Tiny, empty, greeting, and no-action finish without a graph write."
  - "Organize did not consult the standing daily rail. RoleOrganize and RoleEmbed now reserve that rail and finish delayed when it is spent."
---

Visible New folder nests through CreateFolderIn in one transaction. No second
daemon and no FakeEmbedder in production.
