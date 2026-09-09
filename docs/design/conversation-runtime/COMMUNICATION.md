# Communication ownership and extension boundary

This is the implementation target, not a claim about current behavior. The local wave remains on `santosh/conversation-runtime`; no push, pull request or trunk merge is authorized for this wave. Coding delegates initially used Claude Code Opus; the owner subsequently authorized OMP with GLM 5.3 as its replacement. Live model tests use DeepSeek V4 Flash through OpenRouter, pinned across roles. Commercial OpenRouter models are excluded.

## One delivery mechanism, distinct meanings

The existing `userMessage`, `enqueueNote`, `Steer`, `SteerTask`, `deliverTaskNote`, and `postTaskNews` already share part of the delivery path. Extract and consolidate that path before inventing a message bus. A task is an addressable conversation with an assignment and an owner. Main chat has the same conversation delivery mechanics, but different responsibilities. Keep task scheduling, assignment evaluation, source control and terminal drawing outside the mailbox.

A common envelope needs stable message ID, source and recipient conversation identities, origin (authenticated user, agent, tool, runtime), kind, body or content reference, optional correlation/reply ID, and applicable assignment revision. A task number alone is not globally unique; qualify it by session. A content hash identifies content, not an event: the user repeating an instruction after a correction is a new event.

Kinds express a small set of real differences:

- User direction: authentic source, explicit scope, acknowledged receipt and read state. It may update the assignment revision. A quoted user line is not an authenticated grant.
- Work request: a scoped assignment through admission, not arbitrary remote execution.
- Result: a durable answer and evidence references, linked to the assignment revision; preview text is separate.
- Question and answer: correlated and owned, with a named blocked dependency. A child question must reach and wake its owner.
- Progress: a concise replaceable observation for people; it does not wake a model or become an established fact.
- Runtime notice: a typed state change or actionable failure; it is not a person speaking.

Do not expose this envelope vocabulary to the user. The surface says that the correction arrived, work needs an answer, or a result is ready.

## Delivery and wake-up are one boundary

Append to the recipient's queue and signal the eligible reader as one operation. Separate calls that append and later wake leave interleavings where the reader wakes to an empty queue or sleeps over an unread message. A closed or unknown recipient is an explicit outcome; accepted does not mean read. A receipt distinguishes accepted, read, superseded and rejected. Persist pending accepted directions needed after reconnect before acknowledging them as durable.

The recipient owns wake policy. A main conversation can begin a turn for owed results. A task has one worker runner; delivering a message signals that runner rather than starting a competing loop. A child result or clarification answer can release the corresponding wait. A user direction is considered at a legal model/tool boundary. Ambient progress is coalesced without inference. Siblings with independent work keep going while one dependency is blocked.

Persist delivery IDs so retries can be deduplicated. Do not promise exactly-once external effects: a network send may succeed before its receipt is stored. Consequential tools need their own idempotency or reconciliation. Message retry alone cannot make them safe.

## Assignment revision is separate from message delivery

A running task must be able to receive a correction while working or finishing. A receipt must never claim a correction was applied if nobody will read it. Keep the original request and ordered user changes; workers and checks share the current revision. Finalization compares the revision under the task's ownership lock before accepting a result. A result of an older revision is retained as previous work, not presented as satisfying the current request.

Do not classify every message with a separate model. Route using the current room and explicit tool target; use the existing conversation reasoning to resolve ambiguous scope. An ordinary question about progress must not silently reset acceptance. Changes to scope, permissions, and side effects follow authenticated user intent and existing policy. No descendant can increase its own authority.

## Context is a view over sources

Admission composes the current assignment, applicable user constraints, selected quoted exchanges, accessible evidence references, and relevant dependency results. These are distinct inputs. A recent-quote window does not replace durable constraints; dropping an old quote must not revoke a still-applicable restriction. A source reference is useful only if the recipient can retrieve it within its permissions. Test retrieval through the actual worker tools, not only formatting.

Compaction may summarize older discussion and reduce successful tool output while retaining source references. It must preserve unresolved questions, current user changes, tool failures that still matter, and unfinished effects. Do not reclassify assistant narration as fact or silently make unavailable evidence appear accessible.

## Future cross-session support

Keep recipient identity and delivery interface independent of in-memory pointers. Start with a local implementation behind existing calls; a future router can resolve a permitted session and use the same delivery contract. It will additionally require authenticated cross-session authority, recipient admission policy, replay protection and source-access checks. None of that is implemented or implied by today's extraction. Do not add global discovery, remote fan-out, a service registry or a distributed event store in this wave.

## Verification boundaries

Exercise duplicate delivery, close/enqueue races, child-result and question wake-up while the parent is parked, progress without model calls, correction versus finalization, queued direction after restart, and inaccessible source references. Preserve old checkpoint loading and the real terminal door. The success criterion is correct work and understandable receipts with less duplicated logic; moving code into more files alone is not a successful refactor.
