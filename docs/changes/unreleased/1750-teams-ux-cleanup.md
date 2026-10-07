---
kind: changed
title: Teams previews, shared conversations and scoped deletion
pr: 1750
surface: [chat, engine, remote, docs]
invalidates:
  - "Teams used the conversation grid as a creation surface and Chats' grid listed only open tabs. Teams now has a dedicated Name/Project multi-select picker; Chats owns the complete saved conversation grid and retains its card-selection New team flow."
  - "Chats used All for both team selection and the grid. The synchronized Teams dropdown now offers only active teams and All teams when a global manager exists, without a None option. A selected team chip has an independent x that restores ordinary Chats and All teams in Teams, while its name reopens the dropdown. The fixed All button opens the team-agnostic saved conversation library."
  - "Team overviews used equal-sized manager and member cards with row-aligned team tiles. Managers now show the latest user message and the beginning of its response in a larger card, followed by six-row recent interactions and compact members; All teams stacks variable-height cards independently and sorts siblings by latest conversation activity."
  - "Every subteam shrank into another nested box, eventually disappearing at narrow widths. All teams now names the parent and child count, connects single-column child cards with branch guides, and uses expandable tree rows after two nested box levels or when a child box would be narrower than 36 columns. Boxes and tree rows share collapse controls and retain collapsed descendants across resizing. The default team depth remains three total levels."
  - "Working member and manager cards showed a static state word. Their working label now carries the shared animated spinner, including the Global manager, and stops for questions, permission waits, idle and failed conversations. Only visible cards keep the animation clock running; ASCII and screen-reader views retain a static work mark."
  - "Recent interactions had one forward-only page button. Prev and Next now remain visible together, moving in both directions through six-row pages; unavailable directions are grey and stop at the first or last page."
  - "Closing and reopening teams or saved Sessions rows left their lifetime unclear. Disbanding now ends coordination recursively while retaining conversations and history; permanent team deletion preserves conversations, conversation deletion removes all its tasks, and task deletion removes only its containment subtree."
  - "Older deletion payloads could still replace or disband an ordinary manager's teams. The store now requires reassignment first; parent replacement explains which sibling subteam managers to assign first."
  - "Conversation closing released the journal while cancelled task-run workers could still write, and interrupted cleanup was hidden. Journal ownership now lasts through run completion, deletion joins with a bounded grace, unsafe task symlinks are refused before mutation, and durable pending cleanup is retried on startup and remains reachable from Home. Saved memories remain independent of transcript deletion."
  - "Closing-report acceptance could omit its terminal interaction after closing the team. The matching terminal event now lands under the lifecycle lock and is retryable without admitting ordinary writes to closed history."
  - "Older hosted whole-file writers could erase membership authority and delivery boundaries. Membership writes now require a capability on both ends; current builds preserve unknown member JSON and existing join timestamps, omit absent timestamps, and stamp new hosted joins at the engine."
  - "A running task's stop shortcut could capture typing in team dialogs or the Teams own-answer box, and narrow scrolled panes retained pointer targets on blank rows. Team text inputs own their keys and clipped pane targets remain keyboard stops only."
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
