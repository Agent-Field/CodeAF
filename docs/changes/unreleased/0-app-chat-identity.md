---
kind: fixed
title: Desktop chats keep independent identities and transcripts with cell storage
pr: 0
surface: [engine]
invalidates: []
---

The desktop backend now reads the conversation folder through the session layout
API. Cell transcripts live inside `.cell/`; treating that directory as the agent
ID gave every new chat the same identity. New chats now retain separate records,
transcripts and ownership when another chat opens or the first chat is reopened.
Legacy conversation folders retain their existing identities.

This prevents new collisions. It does not repair desktop tabs or drafts saved
with the old `.cell` identity.
