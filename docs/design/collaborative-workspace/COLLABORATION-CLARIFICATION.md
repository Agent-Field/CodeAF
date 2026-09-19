# Collaboration clarification from the owner

**18 September 2026 · supersedes any mandatory shared-group-only interpretation of Wave 3.**

The owner clarified that management is an ordinary chat coordinating other independent chats. Selecting chats must not force a special product mode or a brand-new group discussion. Planner/critic are examples, not fixed features.

## Product model

1. **Any existing ordinary chat can coordinate.** The user says “keep these five features moving” or gives a folder an ongoing responsibility. The current chat keeps its identity and history; a scoped responsibility gives it permitted actions. It may also be started as a new chat if the user wants. There is no manager subclass or separate management UI to administer.
2. **Direct conversation.** The management chat can message one chat/folder representative and receive a reply. Original feature chats keep their histories, work and effective owners. Show source-linked sent/request/reply activity in the management chat; routine exchange detail can be expanded.
3. **Send to several.** One update can be delivered separately to selected recipients. Each receives it in its own conversation; each has a durable receipt. This is fan-out, not implicitly a group conversation. Preserve a shared cause ID and one delivery ID per recipient for retries.
4. **Discuss together when useful.** Invite participants to the current management chat to resolve a question in one shared history, with explicit actor attribution. A separate discussion is optional when a distinct question warrants its own history. The separate chat may be filed in multiple folders. Never merge or replace the original chats.
5. **Useful context, independent reasoning.** Joint participants see the common question, authorized applicable guidance, relevant shared discussion, and bounded source excerpts. Source histories are not copied wholesale into every participant. Each contributor gets a real bounded model invocation/context; the manager does not fabricate both sides. A representative is labelled honestly rather than impersonating an ongoing source agent or a person.
6. **One communication implementation.** Direct requests/replies, fan-out, and contributions to a shared discussion use the same trusted actor envelope, durable delivery/outbox and receipts. One journal owner serializes each conversation's appends. Do not build three buses or three chat types. Routing destinations and participant/scope records express the difference.
7. **AI chooses within scope.** Natural-language requests are the primary initiation path. A picker/selection action is a convenience, not a required ritual. The agent can choose direct, broadcast, or joint discussion based on the need, subject to existing grants/budgets. The person can explicitly steer that choice and inspect what happened. Do not require the user to configure communication topology.
8. **No accidental authority multiplication.** Joining a discussion or receiving a message never grants broader execution authority. Multiple coordinators can contribute, but the one effective work assignment and launch-or-join rules still hold. Parent/Root escalation happens in one discussion with deduplicated participants and bounded effort.

## Concrete five-feature journey

- The user has A, B, C, D and E feature chats. In an existing chat: “Coordinate A, B, C and D.” That chat becomes the management conversation; E stays outside the selected scope.
- Manager privately asks A for progress. A's representative answers, with attribution and source links. The user can inspect the exchange from either relevant history.
- Manager sends an interface decision to B and C separately. Both get the update once and acknowledge independently. They do not gain access to an unrelated private scope.
- B raises a conflict with D. Manager invites B and D into the current management conversation; both actually contribute. The user intervenes. A resolved decision is recorded with sources and applied only through authorized assignment changes.
- A distinct investigation warrants a separate discussion. Manager creates it, files it in the relevant folders, and links the outcome back. The original five chats remain individually accessible.
- New F joins Billing. A selected-four responsibility does not silently grow; a separately granted “manage Billing's current and future work” responsibility does include eligible F.

This clarification changes Wave 3 journeys J19–J22 and the issue wording. Wave 1 work can continue independently. Keep all four waves and the remaining PRD requirements. Record and test all three communication patterns, including actual messages in the TUI; do not prove only one group-chat demo and infer the rest.
