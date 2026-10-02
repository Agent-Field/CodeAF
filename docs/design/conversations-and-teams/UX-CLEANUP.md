# Teams UX cleanup

The owner-approved feature is developed locally on `codex/teams-ux-cleanup`, with a built
binary and adversarial implementation review at each major milestone. No branch push or
GitHub pull request is authorized until the owner says so.

## Milestone 1: overview — approved

Teams uses equal-sized manager and member cards, compact paged interactions with expandable
replies, activity separate from unread, assistant previews, and explicit conversation links.
Only the manager has a role badge. Home and ordinary Chats navigation use All; team-origin
links choose that team's overlay. The composer names the recipient and selected team.

## Milestone 2: membership and overlays — revised

Add member lists Name and Project in columns without displaying IDs. Each member card has
only an `x` removal control with a default-cancel confirmation. Removal affects that membership, keeps work and context, and
preserves other memberships. Losing the reporting membership preserves independence.
New member creation waits for persisted membership before submitting its assignment.

The sidebar has a permanent top-level New team control after the active teams list. Add subteam sits beside and after
Add member in the selected team's header. Closed teams is back at the bottom of the sidebar.
Long sidebar lists keep creation and history controls reachable.

## Milestone 3: disbanding and deletion — owner review

Disband replaces Close/Reopen. It recursively ends coordination, lets current work finish,
preserves conversations and other memberships, and keeps read-only team history. Permanent
team deletion disbands active teams and removes their records and descendant history, never
conversations. Confirmation names the exact affected scope and scrolls when necessary.

Conversation deletion stops its owner, removes its transcript and every membership, and
requires leadership to be reassigned in Teams before deleting any active manager.
The engine checks the reviewed scope under the team-store lock. Stable deletion locks and
permanent tombstones protect against concurrent callers and stale windows. Team exchanges
and decisions survive; historical links retain stable conversation identities. Ambiguous
older links remain readable without guessing a destination. Deleting the last open chat
lands Home. Older hosted engines without checked deletion refuse these operations.

## Verification and review

Focused TUI and session regressions cover routing, membership boundaries, scope changes,
confirmation resizing/scrolling, owner stopping, concurrency, aliases and tombstones.
The complete lightweight teams, remote and enginehost suites check storage and protocol
behavior. The light gate checks build, vet, formatting, packed manuals, retrieval, and laws.
Home deletion hints and `/delete` routing name the existing conversation. Wrap-up timing
remains visible. Disband completion waits for its own store acknowledgement; refused writes
are reported on the visible page. A real-terminal smoke test deletes a throwaway conversation
and checks the saved tombstone and Home removal.
Builds use only `make build`, producing this worktree's `bin/codeaf`.

The milestone 3 review fixture is `/private/tmp/codeaf-teams-milestone-3`. It contains shared
conversations, an Interface cleanup team and an Interaction cleanup subteam, retained history
and a populated interaction table. Its launcher is `review.sh`; it does not use the owner's
normal profile. Later navigation ideas in the planning document remain separate proposals.

## Milestone 3 revision: every saved row can be deleted

The conversation confirmation puts `Stop work and permanently delete?` on the top border,
with only `cancel` (default) and `delete` inside. Muted `enter choose · esc cancel` hints sit at bottom right.
Deleting a managing conversation instead requires choosing another manager in Teams first.
Choose manager lists existing members only; new managers use Add member first.

Sessions offers x delete on every conversation, task, and subtask row. Conversations remove
all task records. Tasks remove their true containment subtree; leaf tasks remove only themselves.
Sibling and other-conversation records survive, including after restart. Store tombstones,
worker lifetime acknowledgements, and durable cleanup receipts cover late writes and retries.
The existing stop action remains separate; close/reopen is removed from row actions.

Settings and Disband sit on the right of the selected-team header, separate from add controls.
Disband has no trailing ellipsis. The New team sidebar control follows the active list.
Additional improvements remain pending a fresh discussion with the owner.

Home, Chats and Sessions refuse manager deletion with the same instruction as member
removal: choose another member as manager before removing this one. Teams has a separate
Choose manager button, which only selects an existing member and preserves the former
manager's membership. A new manager requires Add member first, then Choose manager.
Deletion hides the dialog during the engine call, restoring it only for a failure; Sessions
has no transient deleting-status dialog. Deleting Home's final saved conversation leaves
no phantom start-tab session row.

## Milestone 3 revision: manager responsibilities and empty conversations

A manager conversation has one managed anchor and may also manage descendants of that
anchor. Ordinary multi-membership stays intact. The global manager cannot also manage an
ordinary team. Assignments and moves cannot introduce new conflicts; legacy conflicts stay
visible and are resolved by the person choosing replacement managers in Teams.

Deletion refusal names every active managed team and directs the person to Choose manager.
Launch cleanup and reuse protect empty conversations referenced by team records, including
retained history. Cleanup rechecks those records and emptiness under the team writer's lock;
unknown membership preserves the conversation. Missing transcripts remain unavailable and
are not recreated or selected as replacement managers.

The earlier demo is retained as evidence. Fresh review data uses distinct managers for
unrelated teams and a separate subteam manager.

The review pass removed writer reuse of old empty identities: startup now mints a fresh
conversation and reaps eligible leftovers under a strict lock. Named deletion refusals
scroll without hiding choices. Manager selection rechecks missing previews, and hosted
snapshots never resolve remote paths against the window's filesystem.

## Additional improvements: follow-up milestone 1

All teams now shows top-level team cards with smaller subteams nested inside them,
recursively. Cards show their own memberships' activity and unread counts separately,
spending when known, and a compact saved manager update. Card backgrounds open the team's
overview; manager aliases open Chats with that team's overlay. Organization suggestions,
Apply and Undo stay on Teams. Global manager controls and directly actionable decisions
remain above the cards. Closed records show complete ancestry paths, wrapping when needed.
The sidebar's Show closed checkbox includes those records in the sidebar list and overview
hierarchy. It reveals retained sidebar rows immediately and hides them again without deleting
history; retained cards are marked closed and open their read-only history.

Focused regressions cover nested targets, origin routing, long and narrow overviews,
global controls and decisions, activity refresh, and real Teams Apply/Undo. The isolated
review launcher is `/private/tmp/codeaf-teams-followup-milestone-1/review.sh`.

The next milestones change Chats navigation and its team-agnostic conversation grid,
then enlarge the selected team's manager preview and simplify Settings. Those changes
remain subject to their own binary review and are not implemented in this milestone.

## Additional improvements: follow-up milestone 2

Chats separates navigation from team management. The tab strip reserves a permanently
right-aligned All grid button before fitting conversation tabs. It remains reachable during
overflow and narrow layouts. The dropdown reads Teams when no overlay is selected and
offers None followed by active teams; it cannot change memberships, leadership or settings.

The grid shows all conversations open in this window, regardless of overlay. It retains
membership dots as context, conversation filtering, state previews, selection, view dismissal
and minimap scrolling. It offers no team filtering or management controls. Tab walks tiles.
Cancelling restores the original overlay and draft; opening any tile enters bare Chats.
Teams retains team creation and subteam naming, membership, manager and settings actions.

Focused regressions cover keyboard and pointer routing, cancelling and same-tile opening,
Unicode overflow and pinned managers across 12–200 columns, picker choices and retired keys.
The selected manager's larger faithful preview and Settings cleanup remain milestone 3.

Adversarial review caught filtered totals, hidden long-picker selection, borrowed viewport
persistence and historical membership dots. Those were repaired with focused regressions,
including picker resize bounds. The focused navigation/team/grid suites and lightweight
gate pass. Review data is isolated in `/private/tmp/codeaf-teams-followup-milestone-2`;
`review.sh` launches this worktree's canonical binary against that fixture.
