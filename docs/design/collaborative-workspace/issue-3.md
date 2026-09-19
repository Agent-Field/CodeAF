# Issue 3: Inspectable collaboration: ordinary chats coordinate, with optional shared discussion

**Branch:** `feat/collaborative-workspace-0918`
**Design:** [`PRD-TDD.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/PRD-TDD.md) · [`ENGINEERING.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/ENGINEERING.md) · [`COLLABORATION-CLARIFICATION.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/COLLABORATION-CLARIFICATION.md)
**Depends on:** issues 1 and 2 committed.
**Journeys:** [`USER-JOURNEYS.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/USER-JOURNEYS.md) **J19–J26**.

Required: P5, P8 (shared discussion without merging folders), A11 (collab half), A12, A16 (selected vs dynamic scope), A17.
**Owner clarification supersedes any mandatory group-chat-only interpretation.** An existing ordinary management chat must coordinate independent chats via (1) direct request/reply, (2) sending to several separately, and (3) inviting participants into the same discussion. A new group chat is **optional**. One router. No mandatory manager subclass/mode. No fixed planner/critic product entities.

---

## User-visible outcome

A person, in an **existing ordinary chat**, says to coordinate selected feature chats (J20). That chat keeps its identity and becomes the management conversation. It can:

- privately ask one chat for progress and receive a source-linked reply (direct);
- send one update to several chats separately, each with its own receipt (fan-out);
- invite two participants into the **current** discussion to resolve a conflict (joint);
- optionally create a **separate** discussion when a distinct history is wanted, and file it in multiple folders.

Planner and critic on one issue are **configurable roles** invited into the current chat, not a compulsory new group or a special collaboration mode (J19). Distinct attributed contributions come from actual separate participant invocations.

If a recipient session is not running, the message is queued. When that session is resumed, the line appears **once** (J23). Accepted, recorded, and processed are three different states.

Two folders that share one discussion do **not** merge the rest of their contents (J22). Coordinators have **read/discuss/organize** until issue 4 adds delegated execution (J26).

---

## Engineering scope and module ownership

| Module | This issue |
|---|---|
| `internal/workspace` | **Schema v3 → v4.** `participants`, `deliveries`. Actor IDs minted by software, never by the model. No Wave 4 grant/execution tables yet. Test v1-to-v4. |
| `internal/wscollab` | **New.** One durable envelope/outbox for **direct, fan-out, and shared-discussion** contributions. Route to owning engine host; append through the recipient’s **single-writer** journal seam; ack **recorded** separately from queue **accepted** and from **processed**. Reuse mailbox `deliveryID` / `durableDelivery`. Offline → pending. Citing a chat as evidence does **not** wake it. Wire like `RegisterRunEngine`: session must not import the router package. |
| `internal/wsapi` | `CoordinateSelected` (snapshot of marked IDs), `ManageFolder` (dynamic descendants), `Deliver` (one or many recipients), `InviteToDiscussion`, `CreateDiscussion` (optional separate chat). Scope records selected-id vs folder-dynamic. |
| `internal/session` | Mailbox stays local. Cross-session goes through `wscollab`. Assignment law unchanged: representative text is `fromAgent`, never `fromPerson` (A11). Coordinator tools: read/discuss/organize only. |
| `internal/enginehost` | Wake/reconnect or leave a durable pending delivery if the host has retired. |
| `internal/tui3` | Mark members as a convenience, not a required ritual. Natural-language “coordinate these” is the primary path. Visible sent/request/reply activity with source links. Joint discussion looks like a normal chat with participant labels. Narrow: still sequential. |
| Tools | Coordinator chat gets deliver / invite / inspect-scope tools bound to this issue’s grant (read/discuss/organize). Execute tools wait for issue 4. |
| Manual | Inter-chat communication denial removed. State: ordinary chats coordinate; group chat optional; three patterns; selected snapshot vs whole-folder; Root escalation is hierarchical, not “always ask after two turns”. |
| Owner try | TRY.md: selected coordination and visible participants, including a direct message — not only a group demo. |

### Where the model is called

- The management chat is an ordinary talk session.
- A representative contribution: software retrieves the source chat **read-only**. Each participant gets a **real bounded invocation** with its role, applicable guidance, and evidence. The manager does not fabricate both sides. The model never supplies `from_person`.
- No model on render. No per-folder daemon.

### Collaboration defaults (product)

- Several coordinating chats allowed; no manager subclass (J21).
- Selected coordination: adding a sibling **elsewhere** does not enlarge the snapshot (A16 / J20).
- Whole-folder `Manage this folder`: future descendants included; shared objects deduped.
- Conflict (J24 / A17): **one** discussion, both positions. Relevant parent representatives may join, deduplicating common ancestors, then Root if needed. Two turns may be a per-level starting budget, not a prohibition on escalation. Root cannot exceed user delegation. Missing authority reaches the user.
- Pause coordination (J25): stop **new** autonomous decisions. Closing a view does not pause. Archive suppresses automatic wake-ups.

---

## Exact TUI journey (real model, tmux)

Isolated mktemp home. Cover J19–J26. **Do not prove only one group-chat demo.**

### Ordinary chat hosts planner/critic (J19)

1. In an ordinary chat about a toy issue, ask for a planner and critic to examine different concerns. Invite them into the **current** discussion. **Pass:** no compulsory new chat; distinct attributed contributions from actual separate invocations; user can intervene.

### Five-feature coordination (J20)

2. Create 3–5 short real feature chats (prefer five). In an **existing** ordinary chat: coordinate A, B, C, D. E stays out.
3. **Pass:** that chat becomes the management conversation; original chats keep independent histories.
4. Manager privately asks A for progress. **Pass:** direct request/reply; source links; A’s history still its own.
5. Manager sends an interface decision to B and C separately. **Pass:** fan-out; each receipt independent; not a group conversation.
6. Invite B and D into the current management chat to resolve a conflict. **Pass:** both actually contribute; user intervenes.
7. Optionally create a separate discussion, file it in both folders (J22). Original chats remain accessible.
8. Add a fifth chat to Billing after snapshot coordination. **Pass:** it does **not** join the selected-four. Start **Manage this folder**. **Pass:** the fifth **does** appear in dynamic scope (A16).

### Offline delivery (J23 / A12)

9. Quit so a recipient host can retire. From a second isolated window on the same `CODEAF_HOME`, send a direct line. Reopen the member chat. **Pass:** the line appears exactly once. accepted ≠ recorded ≠ processed.

### Conflict escalation (J24 / A17)

10. Instruct Billing and Security incompatibly. **Pass:** one conflict discussion, deduplicated parents, Root may join when needed, Root cannot exceed user. Not “always ask after two turns” as a ban on parent join.

### Attribution (J26 / A11)

11. A participant claims to be the user. **Pass:** assignment/goal does not move; line labelled as representative/agent. Coordinator cannot execute (that is issue 4).

Restart: quit, reopen, discussion and deliveries intact.

**Fail:** mailbox used as a global bus; duplicate lines on resume; selected snapshot growing with new siblings; invented speaker names; execute happening here; coordinator-written pretend planner/critic dialogue; proving only a group chat and inferring direct/fan-out.

---

## Required automated tests

- Router: accept/record/process states; dedupe; offline resume (A12); **direct, fan-out, and joint** on the same path.
- Snapshot vs dynamic scope (A16).
- One conflict discussion, multiple parents, one deduplicated Root (A17).
- fromPerson vs fromAgent on assignment overlay (A11).
- Per-participant invocation evidence (not a single coordinator transcript faking two speakers).
- TUI: coordinate from an existing chat; P12 selection stability while deliveries arrive.

Live tmux journey above is mandatory on Spark. Commit before affected-package proof.

---

## Acceptance slice

P5, A12, A16, A17, A11 (collab), P8 (discussion placement), J19–J26.

Not done: launch-or-join, unattended execute, dual-road claims (J27–J35).
