---
kind: changed
title: Teams previews, shared conversations and scoped deletion
pr: 1750
surface: [chat, engine, remote, docs]
invalidates:
  - "Teams used the conversation grid as a creation surface and Chats' grid listed only open tabs. Teams now has a dedicated Name/Project multi-select picker; Chats owns the complete saved conversation grid and retains its card-selection New team flow."
  - "Chats used All for both team selection and the grid. The synchronized Teams dropdown now offers None, active teams and All teams when a global manager exists; the fixed All button opens the team-agnostic saved conversation library."
  - "Team overviews used equal-sized manager and member cards with row-aligned team tiles. Managers now show the latest user message and the beginning of its response in a larger card, followed by six-row recent interactions and compact members; All teams stacks variable-height cards independently and sorts siblings by latest conversation activity."
  - "Closing and reopening teams or saved Sessions rows left their lifetime unclear. Disbanding now ends coordination recursively while retaining conversations and history; permanent team deletion preserves conversations, conversation deletion removes all its tasks, and task deletion removes only its containment subtree."
  - "A conversation could newly manage unrelated teams and manager deletion offered follow-up replacements. New leadership must belong to one managed anchor and its descendants; ordinary manager deletion requires reassignment from existing members in Teams first, while the optional global manager can be deleted and recreated independently."
---

Membership removal preserves current work, conversations and other memberships.
Permanent deletion uses owner acknowledgements, locked scope checks and durable
tombstones. Late membership writers cannot restore deleted conversations, including
journal aliases. Unfinished hard-dependent work remains incomplete after deletion;
completed sibling results and tasks with suggested dependencies remain intact.

Cards follow their underlying chat model and use chat-style user/assistant previews,
with question alerts and bounded truncation. Teams and Chats share selection without
duplicating the conversation catalog or creation logic. Manuals and the design record
describe the revised controls, routing, limits and refusals.
