---
kind: added
title: Workstream agents propose records and draft jobs and write their own pages
pr: 2289
surface: [app, chat]
invalidates:
  - A workstream's agent previously had no workstream tools or prompt; it now receives both in the app.
---

Each workstream's own agent gets six tools and a short workstream prompt.
Decisions and questions remain proposals; job proposals append plan drafts
without starting jobs. Go-ahead proposals are decision records and grant
nothing. Page writes enforce agent ownership in the store transaction and
preserve the agent mark, source and time in page history. Reads use recorded
facts, and settled agent turns notify the app to refresh workstream spend.
Ordinary conversations and child workers receive no workstream capability.
