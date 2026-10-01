---
kind: fixed
title: skills context stays out of messages, rewind, reopened chats and titles
pr: 1713
surface: [chat]
invalidates:
  - "#1627 was believed to have fixed #1504 (the `Skills suited to this message:` list inside your message). It kept the journal and store to your words, but the live copy still carried the block into reopened conversations, `/export`, rewind drafts, compaction and title input. Now those keep only your words for new messages; the model's copy still carries the block."
---
`Skills suited to this message:` is context for the model. One shared helper
reads the message's injection mark so display, rewind, compaction, the turn's
explanation and title input keep only the person's words. The model's copy keeps
the block, and a block the person pasted keeps every word.

A conversation compacted by an earlier build may already have the block saved in
its journal. `/export` and rewinding to a message from before the update can still
carry that saved block; the conversation on screen does not show it. Nothing
removes it by matching its wording: a pasted block must keep every word.
