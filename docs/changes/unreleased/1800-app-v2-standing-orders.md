---
kind: added
title: App standing orders persist proposals, run approvals, scopes and reminders
pr: 1800
surface: [engine, chat, docs]
invalidates:
  - "The app's standing-order and reminder routes were reserved 501 responses. They now persist cards and decisions in the binary's native standing store, with receipts and stream events."
  - "Standing trust counted clean unattended outcomes. App orders now count five explicit approvals, Skip counts neither way, Pause preserves the count, and Not right resets it. The terminal's clean-run track remains separate."
  - "Standing scope had only shared policy spellings. App orders now persist delivery scope and notify this chat, current crew members, all non-archived chats or only the Inbox without waking recipient models."
  - "Paused orders were skipped without observing their triggers. App orders now retain skipped trigger lines and counts; reminders bypass trust and retire after firing."
---

A7 uses schema 5 for app approval and scope metadata so an older binary cannot
silently execute an unapproved app order. Ordinary recurrence documents retain
the schema-4 fence. Saved per-run amounts remain unchanged; new orders use $0.40.
