---
kind: added
title: Bring Google Workspace records into Personal apps
pr: 2984
surface: [app, engine, chat]
invalidates:
  - Gmail and Calendar were available to chat tools. Personal apps can now bind Gmail Messages, Calendar Meetings and Drive Documents to app records through delegated Google accounts.
---

Start with a fixture-backed Daily brief, then connect individual Google sources.
App runs can react to incoming mail, meeting start times and new transcript
documents. Approved source Actions can create a Gmail draft, create or update
a Calendar event, or post a Drive comment when Google grants access.

Partial source windows decline totals and joins. Fixture bindings cannot
reserve identities in the live crosswalk.
