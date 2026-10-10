---
kind: changed
title: Desktop place Home follows the decision and knowledge section order
surface: [desktop]
invalidates:
  - "Populated place Home used Places, Chats and Sources sections. It now follows Decisions 12a: status, Since, Live, Decided automatically, and What the place knows, followed by the knowledge field and composer."
  - "Learning proposals could arrive in Home's suggestion slot. Place Home now omits that slot; proposals remain with their questions. Root Home and empty place actions keep their existing layout."
---

Status, decisions and knowledge are independent engine reads. A failed read
keeps available sections visible and offers Retry. Knowledge changes use the
existing guarded writes and receipt Undo; no optimistic saved line is drawn.
