# Engineering decisions (supervising review)

**18 September 2026.** These are corrections to implementation defaults, not added product scope. They replace contradictory draft statements; they are not appendices.

## Schema

Wave 1 migrates collections.db **v1 → v2** with only the metadata this slice needs: collection purpose/lifecycle/revision/timestamps, membership events (provenance), `root_state`, and atomic mutation+provenance in one writer transaction. Later issues add their owned tables through explicit transactional migrations (v3, v4, …). Additional schema versions are normal. Do not pre-create empty guidance/grant/delivery/execution tables in wave 1. Test every supported upgrade path, including direct v1-to-latest. Listing stays read-only and does not migrate. Foreign/future/corrupt databases are refused, never reset. No schema version change hidden behind a table-name check.

## Authority and escalation

In one persistent conflict discussion, relevant parent representatives may join, deduplicating common ancestors, then Root if needed. Every step stays within explicit grants, finite rounds/time/cost, and one cause ID. Root is not omnipotent. Missing authority can reach the user immediately; legitimate delegated parent resolution must work. Two turns may be a per-level starting budget, not a permanent prohibition on escalation. A17 is a concrete test with multiple parents and one deduplicated Root participant.

## Communication (owner clarification, Wave 3)

A shared discussion is **optional**, not mandatory. An existing ordinary chat can coordinate independent chats. Three patterns share **one router**, actor envelope, and receipts:

1. Direct authorized request/reply to one chat or folder representative.
2. Fan-out: send separately to several recipients (one delivery ID per recipient, shared cause ID).
3. Invite participants into the current discussion, or optionally create a separate discussion chat when a distinct history is wanted.

No manager subclass, no special collaboration mode, no fixed planner/critic product entities. Planner/critic are configurable roles. Reading history does not wake a chat. Joining or receiving a message never grants execution authority.

## Discovery (Wave 2)

Ship and evaluate an actual embedding adapter. Expanded-query lexical fallback is degraded capability when a provider fails, not a replacement. Model ID is configurable; availability is verified. Embeddings and organizer calls use real accounting. Measure scale in **passages/vectors**, not only chat count. Guidance loads on the actual main turn and checkpoints before affected mutations. Optional memory off does not disable the graph or discovery.

## Collaboration participants (Wave 3)

Planner/critic and folder/chat representatives need their own bounded invocation with role, applicable guidance, and evidence. They may share a provider, but a coordinator writing both sides is not validation. Attribute a representative as that representative, never as a live source chat's agent or the human. Capture per-participant invocation evidence.

## Execution (Wave 4)

Coordinators in Wave 3 have read/discuss/organize until Wave 4 adds explicitly delegated execution. Launch-or-join over both task roads. Runtime IDs qualified by run instance. Lease expiry ≠ worker dead. Do not mark exactly-once external effects as solved by a lease.

## Process and fixtures

Commit the candidate before affected-package and live proof. Receipt HEAD equals tested source. `make build` → `bin/codeaf`. Suite lock refusals/skips are not a pass. `mktemp` homes and run-unique tmux names. Isolate `CODEAF_HOME` and `CODEAF_PROFILE_DIR`. Never change `HOME`. Never inspect arbitrary process argv. Keys via `config.APIKeyAt` / e2e `liveKey`. Synthetic content only. New functions cyclomatic complexity ≤ 15. UI thin; typed domain functions; no I/O or model on draw; no import cycle.

## Owner branch

The owner explicitly requested branch-off-`santos/dev` (`7cda67c9`). Stay on `feat/collaborative-workspace-0918`. Do not merge, force-push, or push to `dev`, `santos/dev`, `staging`, or `main`.

## Journeys

[`USER-JOURNEYS.md`](USER-JOURNEYS.md) is the owner acceptance contract (J01–J35). Every issue links its assigned IDs. No silently omitted journey. [`TRY.md`](TRY.md) is the owner-facing playable recipe per completed wave.
