---
kind: changed
title: attached folders reach the next model request and can be removed
pr: 657
surface: [chat, engine, remote]
invalidates:
  - "Choosing a folder used to update the conversation record without telling the next model request. Explicitly attached folders now appear in its instructions with scoped project rules, and removal takes them out again."
  - "The local wire client could lack folder registration while the surface offered it. The engine now advertises folder support and the client exposes registration, removal and the remembered set."
  - "The folder picker previously lacked successive child columns and mouse actions. It now supports breadcrumbs, child browsing, explicit add/remove actions and removable folder indicators; search ranks path segments, initials and spelling slips, and discovers ordinary folders too."
  - "Attached instruction files could exceed their aggregate budget when earlier files left only a partial allowance. Each read now uses the remaining allowance and names any truncated source."
---

This entry describes the integrated engine, wire and folder browser behavior. File
attachments, previews and final scope integration remain under construction on the
draft wave branch; update this entry before review readiness.
