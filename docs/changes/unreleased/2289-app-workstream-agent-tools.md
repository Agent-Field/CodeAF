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

Prompt briefs use a non-blocking cached snapshot and fall back when unavailable.
Agent record and page writes check done inside the write transaction, including
when the person marks done after the agent's read. Markdown list items always
start with a paragraph, and malformed inline delimiters take linear work.
Tool arguments refuse repeated keys at every object depth before calling a tool.
Workstream tools use the shared argument decoder and mark input refusals before
running. Whole-valued decimal job and wave numbers are accepted; proposed caps
retain fractional dollars. A plan version conflict gets one fresh read and save.
