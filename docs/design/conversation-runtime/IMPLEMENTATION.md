# Conversation runtime implementation

Local integration branch: `santosh/conversation-runtime`, based on `origin/dev` at `b3922479f65ea060f61852b4262dc89ed3541fdf`. This work does not modify or publish the shared `dev` checkout. The earlier `santosh/pareto-fixes` branch supplied examples to investigate; its changes were not presumed correct or merged wholesale.

## Landed and checked

| Boundary | Change | Evidence |
| --- | --- | --- |
| Task result | Keep the full answer separately from the compact card. Carry a bounded excerpt with a retrievable overflow reference; retain the outcome qualification. | Scripted worker-to-owner delivery, checkpoint/reopen, long-answer retrieval and rejected-result tests. |
| Task context | Compile one bounded selection of attributed conversation excerpts and tool-result references at each admission door. Preserve exported records through checkpoints. | Source retrieval, repeated user-message identity, chronology after compaction, byte budget and admission-door tests. |
| Workspace ownership | Resolve alternate spellings of the same physical location when choosing a working copy, binding contract paths and checking parallel writable claims. | Existing macOS isolation failures reproduced on the base; symlink, missing output, shared input and separate sibling tests pass with the fix. |

Each integration merge has been rebuilt with `make build`. These focused checks are not a substitute for the final integrated suite or a live product walkthrough.

## In progress

- Shared local communication: distinguish the person, another agent and runtime news; select a live recipient and queue the message consistently; keep delivery claims separate across attempts.
- Task status: derive user-facing status from shared runtime facts, retaining unknown liveness and separating source-control disposition from result delivery.
- Local hosting: preserve active work and pending questions across terminal detach, then make the hosted experience the ordinary interactive default without silently discarding launch options.
- Compaction: retain useful result excerpts with references the available tools can actually open; reclaim headroom toward the target and remove quadratic bookkeeping.
- Steering: make a genuine direction update the effective assignment, with version checks around completion. An ordinary question must not rewrite the acceptance condition.
- Evaluation: deterministic fixtures and actual terminal scenarios with isolated state, exact model pins, pre-call enforcement and honest missing-cost reporting.

## Design limits

The context excerpt is deliberately incomplete. It is neither a durable universal constraint list nor a guarantee that every relevant earlier statement is selected. A reference is useful only if the recipient can retrieve its contents; a store identifier alone does not establish that.

A queued message has not necessarily been read. Delivery bookkeeping must survive interruption without suppressing news that never reached the recipient's record. Attempt identity and assignment revision answer different questions and must remain distinct.

Cross-session communication remains out of scope. The local addressing and provenance boundary should permit a later authenticated router, but no discovery service, global message bus or cross-session permission model is being introduced here.

No benchmark result establishes general superiority over another harness. Comparisons must name the tested door, workload, model, quality checks, repetitions, latency and actual cost; unsupported or unmeasured cells do not count as successes.
