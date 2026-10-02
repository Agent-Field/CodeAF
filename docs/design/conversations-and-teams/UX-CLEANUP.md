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
only an `x` removal control. Removal affects that membership, keeps work and context, and
preserves other memberships. Losing the reporting membership preserves independence.
New member creation waits for persisted membership before submitting its assignment.

The sidebar has a permanent top-level New team control. Add subteam sits beside and after
Add member in the selected team's header. Closed teams is back at the bottom of the sidebar.
Long sidebar lists keep creation and history controls reachable.

## Milestone 3: disbanding and deletion — owner review

Disband replaces Close/Reopen. It recursively ends coordination, lets current work finish,
preserves conversations and other memberships, and keeps read-only team history. Permanent
team deletion disbands active teams and removes their records and descendant history, never
conversations. Confirmation names the exact affected scope and scrolls when necessary.

Conversation deletion stops its owner, removes its transcript and every membership, and
requires explicit replacement or recursive disband choices for all active managed teams.
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
