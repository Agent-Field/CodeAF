---
kind: changed
title: Folders columns, distinct Coordinate chord, and reactive organize after explicit opt-in
pr: 1235
surface: [chat, docs]
invalidates:
  - "The Folders place was sequential drill-in and 80-col `esc` back. It is now Miller columns over the logical graph with a pinned details pane; 80-col is one navigation column plus switchable details."
  - "`→` on the Folders place opened the verb strip (`c` New folder). On that place `→` now drills (or focuses details for a leaf) and `shift+→` opens the strip; `c` stays New folder and `g` is Coordinate selected."
  - "`CreateFolder(name)` always made a Root folder, even inside Billing. Visible New folder now creates and nests in one action via `CreateFolderIn` (parent = the open/selected folder; empty at Root)."
  - "Automatic after-message organize was allowed whenever `workspace.organize` was on, and the survey woke only on the five-minute tick. Graph writes now wait for `workspace.reactive` (opt-in by Organize existing chats); enqueue kicks the existing standing pass; the five-minute tick is crash fallback."
  - "The organize survey always walked `Unfiled[0..]` and deferred at eight chats, so a no-action prefix never advanced. The per-lease cap of eight remains a slice; a durable job cursor continues past it."
---

Contracts only on `7fb6a803`. File owners: runtime `t-rx-runtime`, ui `t-rx-ui`, proof `t-rx-proof`, add-old hook `t-ux-add-old` (`Add existing chats` / `beginFolderAdd`). No second daemon, no filesystem mirror, no `ready.json`.
