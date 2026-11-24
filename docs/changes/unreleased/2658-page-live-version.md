---
kind: fixed
title: a page's history names the version the page holds, and the agent's kept text can be restored
pr: 2658
surface: [engine, factory]
invalidates:
  - "Was true: a page's versions (GET /v1/workstreams/{id}/pages/{pid}/versions, factory.org_workstream_page_versions) carried no word for which one the page holds, so a client called the highest version current — and the server agent's text on a page edited live, kept as the newest version but never applied, was called current and offered no Restore. Is true now: the list answers `live_version`, the newest version the page held (a snapshot of the live document or a restore), and every version the agent kept and nobody restored carries `kept: true`; a client marks `live_version` current, never the highest number. Restoring a kept version applies it and clears `kept`."
  - "Was true: the agent said a kept page edit \"was kept as version N in its history; anyone who can edit it restores that version to apply it\". Is true now: it says the text was not applied, was kept as version N in the page's History marked Kept · not applied, and that anyone who can edit the page opens History, chooses that version and presses Restore this version to apply it."
---
