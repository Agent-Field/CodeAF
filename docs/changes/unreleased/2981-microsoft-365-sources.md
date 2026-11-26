---
kind: added
title: Connect Microsoft 365 sources to Personal apps
pr: 2981
surface: [app, engine, docs]
invalidates:
  - Personal apps could read external records from SQL or declarative HTTP. They can now use delegated Microsoft Graph for Outlook, Calendar, OneDrive, SharePoint and permitted Teams transcripts.
---

App runs can react to matching mail, upcoming meetings and transcripts. Outlook drafts and calendar writes use the approval broker and leave effect receipts.
